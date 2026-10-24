package sluzhba

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
)

// Окно (index.html) получило 28.08 вторую дверь к установке найденного
// обновления — ту же ручку /api/obnovlenie_postavit, что уже дёргает тычок
// по пузырю трея (cmd/kelevra/trey_windows.go: tychokVPuzyr →
// sluzhba.PostavitNaydennoe). До этой правки подпись под кнопкой честно
// писала «Доступна версия X» (s.novaya_versiya_dostupna, /api/sostoyanie), а
// клик всё равно звал только ПРОВЕРКУ (api("obnovlenie")) — ставить было
// нечем.
//
// Тесты ниже кладут находку в naydennoeObnovlenie тем же путём, каким её
// кладёт фоновая проверка (ProveritObnovlenieFonom, sluzhba.go:352), и бьют
// по ручке напрямую — так же, как её дёргает и трей, и теперь окно.
// PerezapuskPosleObnovleniya не подключаем: её же комментарий говорит,
// что nil — обычное дело для стенд-тестов внутри этого пакета, тогда эта
// копия не гасит себя, а файл на диске подменяется по-настоящему тем же
// Postavit(), каким пользуется бой (см. её комментарий про PutSebya на
// живом процессе).

// vosstanovitSebya возвращает файл САМОГО СЕБЯ (os.Executable) в исходное
// состояние после того, как PostavitNaydennoe по-настоящему подменит его на
// диске: Postavit() уводит старое тело в <путь>.old и никогда сам не трогает
// его обратно (это дело следующей копии, UbratHvost при холодном старте) —
// без этого шага следующий запуск ЭТОГО ЖЕ собранного тестового бинаря
// унаследовал бы нашу подмену вместо настоящего теста.
//
// put берём РОВНО ОДИН РАЗ и отдаём вызывающему: второй вызов PutSebya()
// уже ПОСЛЕ Postavit() — та самая ловушка из комментария
// PerezapuskPosleObnovleniya (sluzhba.go) — os.Executable() у ещё живого
// процесса следует за переименованием inode и вернул бы путь «...old».
func vosstanovitSebya(t *testing.T) string {
	t.Helper()
	put, err := obnovlenie.PutSebya()
	if err != nil {
		t.Fatalf("не знаю, где лежу: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Rename(put+".old", put)
	})
	return put
}

// sborkaPohozhayaNaSebya собирает содержимое поддельной «новой сборки» так,
// чтобы она была того же РОДА, что и подменяемый файл: первые байты берутся
// у него самого (на Windows это "MZ" настоящего .exe, на линуксе "ELF").
//
// Зачем. obnovlenie.Postavit с 31.08 отказывается ставить на место
// приложения файл, не похожий на исполняемый, — ровно так ловится
// скачанная вместо .exe страница-заглушка прокси или обрезок закачки
// (беда 28.08: (цитата убрана)). Голая строка "PODMENA..."
// под это правило попадает по делу, поэтому тест обязан подделывать сборку
// правдоподобно, а не отменять проверку.
func sborkaPohozhayaNaSebya(t *testing.T, put, hvost string) string {
	t.Helper()
	telo, err := os.ReadFile(put)
	if err != nil {
		t.Fatalf("не прочитал себя (%s): %v", put, err)
	}
	if len(telo) < 4 {
		t.Fatalf("файл %s подозрительно пуст: %d байт", put, len(telo))
	}
	return string(telo[:4]) + hvost
}

func TestObnovleniePostavitRuchkaStavitNaydennuyuVersiyu(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()
	put := vosstanovitSebya(t)

	soderzhimoe := sborkaPohozhayaNaSebya(t, put, "PODMENA-SBORKI-DLYA-TESTA")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, soderzhimoe)
	}))
	t.Cleanup(srv.Close)

	// Тот же путь, каким кладёт находку фоновая проверка (sluzhba.go:352) —
	// тест лежит в том же пакете, прямой доступ к полю оправдан.
	s.zamok.Lock()
	s.naydennoeObnovlenie = &obnovlenie.Novaya{Versiya: "9.9.9", Ssylka: srv.URL, Razmer: int64(len(soderzhimoe))}
	s.zamok.Unlock()

	// Ровно то, что видит окно в /api/sostoyanie перед тем, как показать
	// «Доступна версия 9.9.9» и позволить нажать установку.
	rSost := httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil)
	wSost := httptest.NewRecorder()
	m.ServeHTTP(wSost, rSost)
	var sost otvetSostoyaniya
	if err := json.Unmarshal(wSost.Body.Bytes(), &sost); err != nil {
		t.Fatalf("не разобрал /api/sostoyanie: %v", err)
	}
	if sost.NovayaVersiyaDostupna != "9.9.9" {
		t.Fatalf("окно не увидело находку в /api/sostoyanie: %+v", sost)
	}

	// Ровно та ручка, которую теперь зовёт кнопка окна (index.html:
	// $("knopka-obnovlenie").onclick), когда s.novaya_versiya_dostupna не пуст.
	r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/obnovlenie_postavit", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("код ответа %d: %s", w.Code, w.Body.String())
	}
	var o struct {
		Gotovo  bool   `json:"gotovo"`
		Versiya string `json:"versiya"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	if !o.Gotovo || o.Versiya != "9.9.9" {
		t.Fatalf("не поставил найденную версию: %+v", o)
	}

	novoe, err := os.ReadFile(put)
	if err != nil {
		t.Fatalf("не прочитал файл после установки: %v", err)
	}
	if string(novoe) != soderzhimoe {
		t.Fatalf("на диске не подмена сборки: %q", novoe)
	}
}

// Второй тычок, пока первый ещё качает (так бывает всегда, когда тихая
// установка postavitTiho уже идёт, а человек жмёт «ОБНОВИТЬ» в окне), не
// ставит второй раз и НЕ отвечает бедой: ждёт итога первой установки и
// отвечает им. До 24.10 он получал 400 «установка уже идёт», и окно рисовало
// «Обновление не установлено» при вставшей версии (ноут 01-3).
//
// Заодно — что видит окно в /api/sostoyanie: пока ставится —
// obnovlenie_idet, после — obnovlenie_postavleno; и что третий тычок после
// удачи отвечает успехом сразу, не качая сборку снова.
func TestObnovleniePostavitRuchkaVtoroyTychokZhdetItoga(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()
	put := vosstanovitSebya(t)

	nachalos := make(chan struct{})
	mozhnoProdolzhat := make(chan struct{})
	var zaprosov atomic.Int32
	soderzhimoe := sborkaPohozhayaNaSebya(t, put, "PODMENA-SBORKI-DLYA-GONKI")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if zaprosov.Add(1) == 1 {
			close(nachalos)
		}
		<-mozhnoProdolzhat
		fmt.Fprint(w, soderzhimoe)
	}))
	t.Cleanup(srv.Close)

	s.zamok.Lock()
	s.naydennoeObnovlenie = &obnovlenie.Novaya{Versiya: "8.8.8", Ssylka: srv.URL, Razmer: int64(len(soderzhimoe))}
	s.zamok.Unlock()

	kodPervogo := make(chan int, 1)
	go func() {
		r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/obnovlenie_postavit", nil)
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		kodPervogo <- w.Code
	}()

	select { // первый тычок уже держит idetUstanovkaObnovleniya=true
	case <-nachalos:
	case <-time.After(5 * time.Second):
		t.Fatal("первый тычок не дошёл до скачивания за 5с — ручка не отвечает, а не держит замок")
	}

	if sost := sostoyanieOkna(t, s); !sost.ObnovlenieIdet || sost.ObnovleniePostavleno != "" {
		t.Fatalf("пока ставится, окно должно видеть obnovlenie_idet: %+v", sost)
	}

	vtoroy := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/obnovlenie_postavit", nil)
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		vtoroy <- w
	}()
	select {
	case w := <-vtoroy:
		t.Fatalf("второй тычок ответил, не дождавшись итога первой установки: %d %s", w.Code, w.Body.String())
	case <-time.After(300 * time.Millisecond):
	}
	close(mozhnoProdolzhat)
	if k := <-kodPervogo; k != 200 {
		t.Fatalf("первый тычок: код %d", k)
	}
	var w2 *httptest.ResponseRecorder
	select {
	case w2 = <-vtoroy:
	case <-time.After(5 * time.Second):
		t.Fatal("второй тычок не ответил за 5с после конца первой установки")
	}
	if w2.Code != 200 {
		t.Fatalf("второй тычок пока идёт первый: ждал 200 (итог первой), получил %d: %s", w2.Code, w2.Body.String())
	}
	var o struct {
		Gotovo  bool   `json:"gotovo"`
		Versiya string `json:"versiya"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &o); err != nil || !o.Gotovo || o.Versiya != "8.8.8" {
		t.Fatalf("второй тычок ответил не итогом первой установки: %s (%v)", w2.Body.String(), err)
	}

	if sost := sostoyanieOkna(t, s); sost.ObnovlenieIdet || sost.ObnovleniePostavleno != "8.8.8" {
		t.Fatalf("после установки окно должно видеть obnovlenie_postavleno=8.8.8: %+v", sost)
	}

	r3 := httptest.NewRequest("POST", "/"+s.klyuch+"/api/obnovlenie_postavit", nil)
	w3 := httptest.NewRecorder()
	m.ServeHTTP(w3, r3)
	if w3.Code != 200 {
		t.Fatalf("третий тычок после удачи: %d %s", w3.Code, w3.Body.String())
	}
	if n := zaprosov.Load(); n != 1 {
		t.Fatalf("сборку качали %d раз, ждал ровно один", n)
	}
}

// Первая установка провалилась — ждавший второй тычок получает ЕЁ отказ, а
// не молчание и не успех.
func TestObnovleniePostavitVtoroyTychokPoluchaetOtkazPervogo(t *testing.T) {
	s := stend(t)
	_ = vosstanovitSebya(t)

	nachalos := make(chan struct{})
	mozhnoProdolzhat := make(chan struct{})
	var odin sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		odin.Do(func() { close(nachalos) })
		<-mozhnoProdolzhat
		http.Error(w, "нет", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	s.zamok.Lock()
	s.naydennoeObnovlenie = &obnovlenie.Novaya{Versiya: "6.6.6", Ssylka: srv.URL, Razmer: 10}
	s.zamok.Unlock()

	pervyy := make(chan error, 1)
	go func() {
		_, err := s.PostavitNaydennoe()
		pervyy <- err
	}()
	select {
	case <-nachalos:
	case <-time.After(5 * time.Second):
		t.Fatal("первая установка не дошла до скачивания за 5с")
	}
	vtoroy := make(chan error, 1)
	go func() {
		_, err := s.PostavitNaydennoe()
		vtoroy <- err
	}()
	time.Sleep(200 * time.Millisecond)
	close(mozhnoProdolzhat)
	if err := <-pervyy; err == nil {
		t.Fatal("первая установка на ответ 500 должна была провалиться")
	}
	select {
	case err := <-vtoroy:
		if err == nil || !strings.Contains(err.Error(), "не установлено") {
			t.Fatalf("второй тычок: ждал отказ первой установки, получил %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("второй тычок не дождался итога первой")
	}
	if sost := sostoyanieOkna(t, s); sost.ObnovlenieIdet || sost.ObnovleniePostavleno != "" {
		t.Fatalf("после провала окно не должно видеть ни хода, ни «поставлено»: %+v", sost)
	}
}

// ДОЛЯ СКАЧАННОГО (02.09). Ручка установки отвечает ОДИН раз и уже в конце, а
// окно тем временем опрашивает /api/sostoyanie раз в 2 секунды — до этой
// правки там не было ни одного поля про ход, и диалог обновления мог сказать
// только «Скачиваю…» под неопределённой полосой. Теперь доля едет полем
// obnovlenie_dolya, и оба теста ниже смотрят на неё ровно оттуда, откуда её
// берёт окно.

// dolyaVSostoyanii — то же самое, что делает окно каждые 2 секунды.
func dolyaVSostoyanii(t *testing.T, s *Sluzhba, m http.Handler) float64 {
	t.Helper()
	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	var o otvetSostoyaniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал /api/sostoyanie: %v", err)
	}
	return o.ObnovlenieDolya
}

func TestSostoyanieNesetDolyuPokaIdetUstanovka(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()
	put := vosstanovitSebya(t)

	// Сборка нарочно крупная: на коротком теле вся загрузка проходит одним
	// чтением, и «половина скачана» подсмотреть негде — тест зеленел бы по
	// конструкции, а не по факту.
	soderzhimoe := sborkaPohozhayaNaSebya(t, put, strings.Repeat("K", 200_000))
	otdali := make(chan struct{})
	dokachat := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(soderzhimoe)))
		polovina := len(soderzhimoe) / 2
		fmt.Fprint(w, soderzhimoe[:polovina])
		w.(http.Flusher).Flush()
		close(otdali)
		<-dokachat
		fmt.Fprint(w, soderzhimoe[polovina:])
	}))
	t.Cleanup(srv.Close)

	s.zamok.Lock()
	s.naydennoeObnovlenie = &obnovlenie.Novaya{Versiya: "7.7.7", Ssylka: srv.URL, Razmer: int64(len(soderzhimoe))}
	s.zamok.Unlock()

	kod := make(chan int, 1)
	go func() {
		r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/obnovlenie_postavit", nil)
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		kod <- w.Code
	}()

	select {
	case <-otdali:
	case <-time.After(5 * time.Second):
		t.Fatal("сервер не дошёл до отдачи первой половины за 5с")
	}

	// Опрашиваем ровно так же, как окно, — пока доля не появится. Ждём, а не
	// смотрим один раз: между отдачей байт сервером и их чтением клиентом
	// проходит неизвестное нам время, и разовая проверка ловила бы гонку.
	var dolya float64
	srok := time.Now().Add(5 * time.Second)
	for time.Now().Before(srok) {
		dolya = dolyaVSostoyanii(t, s, m)
		if dolya > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if dolya <= 0 {
		close(dokachat)
		<-kod
		t.Fatal("половина сборки уже скачана, а /api/sostoyanie доли не несёт — окну нечего показать")
	}
	if dolya >= 1 {
		close(dokachat)
		<-kod
		t.Fatalf("доля %.3f при скачанной половине — окно показало бы 100%% посреди загрузки", dolya)
	}

	close(dokachat)
	if c := <-kod; c != 200 {
		t.Fatalf("установка не удалась, код %d", c)
	}
	// Установка кончилась — доли больше нет вовсе: застывшие «47%» в окне
	// после конца загрузки такое же враньё, как выдуманные проценты.
	if d := dolyaVSostoyanii(t, s, m); d != 0 {
		t.Fatalf("установка кончилась, а доля осталась %.3f", d)
	}
}

// Обрыв связи посреди скачивания: установка проваливается, и доля обязана
// исчезнуть вместе с ней. Иначе окно на следующем же опросе показало бы
// «Скачиваю… 50%» на загрузке, которой давно нет.
func TestOborvannayaZakachkaNeOstavlyaetDolyuVSostoyanii(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()
	// vosstanovitSebya тут не нужна: обрыв случается ДО того, как Postavit
	// трогает файл приложения (её же железное обещание), — подменять на
	// диске нечего.
	put, err := obnovlenie.PutSebya()
	if err != nil {
		t.Fatalf("не знаю, где лежу: %v", err)
	}
	polovina := sborkaPohozhayaNaSebya(t, put, strings.Repeat("K", 100_000))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Обещаем вдвое больше, чем отдаём, и обрываем ответ.
		w.Header().Set("Content-Length", fmt.Sprint(2*len(polovina)))
		fmt.Fprint(w, polovina)
	}))
	t.Cleanup(srv.Close)

	s.zamok.Lock()
	s.naydennoeObnovlenie = &obnovlenie.Novaya{Versiya: "6.6.6", Ssylka: srv.URL, Razmer: int64(2 * len(polovina))}
	s.zamok.Unlock()

	r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/obnovlenie_postavit", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatalf("обрыв закачки прошёл как успех: %s", w.Body.String())
	}
	if d := dolyaVSostoyanii(t, s, m); d != 0 {
		t.Fatalf("закачка оборвалась, а доля в состоянии осталась %.3f", d)
	}
	if _, err := os.Stat(put + ".old"); err == nil {
		t.Fatal("приложение отодвинуто в .old, хотя скачивание оборвалось")
	}
}
