package sluzhba

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func TestObnovleniePostavitRuchkaStavitNaydennuyuVersiyu(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()
	put := vosstanovitSebya(t)

	soderzhimoe := "PODMENA-SBORKI-DLYA-TESTA"
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

// Второй тычок, пока первый ещё качает, обязан получить ВНЯТНЫЙ ответ, а не
// повиснуть и не уйти молча — то, из-за чего окно вправе просто заблокировать
// кнопку на время запроса (idetUstanovkaObnovleniya уже держит замок на
// стороне службы, sluzhba.go:418).
func TestObnovleniePostavitRuchkaVtoroyTychokPokaIdetUstanovka(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()
	vosstanovitSebya(t)

	nachalos := make(chan struct{})
	mozhnoProdolzhat := make(chan struct{})
	soderzhimoe := "PODMENA-SBORKI-DLYA-GONKI"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(nachalos)
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

	r2 := httptest.NewRequest("POST", "/"+s.klyuch+"/api/obnovlenie_postavit", nil)
	w2 := httptest.NewRecorder()
	m.ServeHTTP(w2, r2)
	close(mozhnoProdolzhat)
	<-kodPervogo

	if w2.Code != http.StatusBadRequest {
		t.Fatalf("второй тычок пока идёт первый: ждал 400, получил %d: %s", w2.Code, w2.Body.String())
	}
	var beda map[string]string
	if err := json.Unmarshal(w2.Body.Bytes(), &beda); err != nil {
		t.Fatalf("не разобрал ответ второго тычка: %v", err)
	}
	if !strings.Contains(beda["beda"], "уже идёт") {
		t.Fatalf("второй тычок ответил не по делу: %+v", beda)
	}
}
