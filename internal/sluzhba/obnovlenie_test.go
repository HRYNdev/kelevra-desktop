package sluzhba

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// Пункт «Проверить обновление» в настройках (index.html) появился 23.08:
// обновление и так ставится само и молча (cmd/kelevra/obnovlenie.go), но
// человеку негде спросить прямо сейчас, свежая у него версия или нет
// (эталон телефона — SimpleSettingsScreen.kt). Тесты ниже держат саму
// ручку /api/obnovlenie (internal/sluzhba/sluzhba.go).

func relizObnovleniya(teg, fayl string, razmer int) string {
	return fmt.Sprintf(`[{"tag_name":%q,"draft":false,"prerelease":false,"assets":[{"name":%q,"browser_download_url":"http://primer/%s","size":%d}]}]`,
		teg, fayl, teg, razmer)
}

func podmenitVersiyu(t *testing.T, novaya string) {
	t.Helper()
	stariy := podpiska.Versiya
	podpiska.Versiya = novaya
	t.Cleanup(func() { podpiska.Versiya = stariy })
}

func TestObnovlenieRuchkaVidyatNovuyuVersiyu(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()
	podmenitVersiyu(t, "0.4.0")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, relizObnovleniya("app-v0.9.0", obnovlenie.ImyaFayla, 42))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KELEVRA_RELIZY", srv.URL)

	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/obnovlenie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("код ответа %d: %s", w.Code, w.Body.String())
	}
	var o otvetObnovleniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	if o.Novaya != "0.9.0" {
		t.Fatalf("не увидел новую версию: %+v", o)
	}
	if o.Beda != "" {
		t.Fatalf("не ждали беды: %+v", o)
	}
	if o.Tekushchaya != "0.4.0" {
		t.Fatalf("не сказал текущую версию: %+v", o)
	}
}

func TestObnovlenieRuchkaSvezhayaVersiyaBezNovoy(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()
	podmenitVersiyu(t, "0.9.0")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, relizObnovleniya("app-v0.9.0", obnovlenie.ImyaFayla, 42))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KELEVRA_RELIZY", srv.URL)

	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/obnovlenie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	var o otvetObnovleniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	if o.Novaya != "" || o.Beda != "" {
		t.Fatalf("на своей же версии не ждали ни обновления, ни беды: %+v", o)
	}
}

// Сеть на стенде окна (stend/oblik_snimok.py) намеренно недоступна — ручка
// обязана ответить понятной бедой, а не повесить запрос: окно не должно
// зависнуть на «Проверяем…» навсегда (жалоба 22.08, эталон — телефон).
//
// До 28.08 текст был один на все беды — «не удалось проверить»: человек не
// понимал, нет ли у него сети, лежит ли GitHub или тот просто думает долго.
// Ниже — четыре теста на четыре РАЗНЫЕ причины (net-сеть, статус, таймаут, мусор), и
// каждый ждёт СВОЙ текст.

// TestObnovlenieRuchkaBedaNetSeti — порт закрыт (127.0.0.1:1 никто не
// слушает): connection refused, самая частая беда — нет сети дома.
func TestObnovlenieRuchkaBedaNetSeti(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()
	t.Setenv("KELEVRA_RELIZY", "http://127.0.0.1:1/nikogo-tam-net")

	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/obnovlenie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("ошибка сети не должна ронять ручку: код %d, %s", w.Code, w.Body.String())
	}
	var o otvetObnovleniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	const zhdu = "нет интернета, проверить не у кого"
	if o.Beda != zhdu {
		t.Fatalf("беда = %q, ждал %q", o.Beda, zhdu)
	}
	if strings.Contains(strings.ToLower(o.Beda), "http") || strings.Contains(o.Beda, "dial") {
		t.Fatalf("беда несёт сырой сетевой текст, а не понятную подпись: %q", o.Beda)
	}
}

// TestObnovlenieRuchkaBedaStatus — GitHub жив, но отвечает 503: сервер
// работает, значит дело не в сети человека, а в самом GitHub.
func TestObnovlenieRuchkaBedaStatus(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KELEVRA_RELIZY", srv.URL)

	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/obnovlenie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	var o otvetObnovleniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	const zhdu = "GitHub ответил ошибкой 503"
	if o.Beda != zhdu {
		t.Fatalf("беда = %q, ждал %q", o.Beda, zhdu)
	}
}

// TestObnovlenieRuchkaBedaTaymaut — сервер молчит дольше отведённого срока:
// человек видит подпись «GitHub не ответил за N секунд», а не зависшую
// «Проверяем…» и не ту же надпись, что при отсутствии сети.
//
// Срок на время теста сокращён (иначе 6 настоящих секунд на каждый прогон);
// после теста возвращается обратно — srokProverkiObnovleniya это var именно
// ради такой подмены (см. sluzhba.go).
func TestObnovlenieRuchkaBedaTaymaut(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()

	staryy := srokProverkiObnovleniya
	srokProverkiObnovleniya = time.Second // не 0: "0 секунд" нечитаемо человеку
	t.Cleanup(func() { srokProverkiObnovleniya = staryy })

	otpushcheno := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-otpushcheno:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(otpushcheno); srv.Close() })
	t.Setenv("KELEVRA_RELIZY", srv.URL)

	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/obnovlenie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	var o otvetObnovleniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	const zhdu = "GitHub не ответил за 1 секунд"
	if o.Beda != zhdu {
		t.Fatalf("беда = %q, ждал %q", o.Beda, zhdu)
	}
}

// TestObnovlenieRuchkaBedaMusor — GitHub (или проксёр между ним и человеком)
// ответил 200, но телом, которое не JSON: страница-заглушка, HTML capitve
// portal и подобное.
func TestObnovlenieRuchkaBedaMusor(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html>не json</html>")
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KELEVRA_RELIZY", srv.URL)

	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/obnovlenie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	var o otvetObnovleniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	const zhdu = "GitHub ответил непонятным"
	if o.Beda != zhdu {
		t.Fatalf("беда = %q, ждал %q", o.Beda, zhdu)
	}
}

// Дальше — сцены починки жалобы 02.09 «уведомлялка некрасивая»: про одну и ту
// же находку человека дёргали ДВАЖДЫ — модальным диалогом в самом окне и
// пузырём в трее Windows поверх него. Правка сводит это к одному пути, и
// решает всё один замер: смотрит ли человек прямо сейчас в окно (см.
// povestitEsliNovaya и поле oknoSprosilo в sluzhba.go).

// sprositSostoyanie — один опрос /api/sostoyanie. Метка okno=1 — ровно то,
// что ставит открытое окно приложения (index.html) и не ставит никто больше:
// ни сторож окна, ни проверка живой копии, ни стенды.
func sprositSostoyanie(t *testing.T, s *Sluzhba, kakOkno bool) {
	t.Helper()
	adres := "/" + s.klyuch + "/api/sostoyanie"
	if kakOkno {
		adres += "?okno=1"
	}
	w := httptest.NewRecorder()
	s.Obsluzhit().ServeHTTP(w, httptest.NewRequest("GET", adres, nil))
	if w.Code != 200 {
		t.Fatalf("состояние ответило %d: %s", w.Code, w.Body.String())
	}
}

// TestPuzyrMolchitPriOtkrytomOkne — окно открыто, значит про находку скажет
// диалог в нём: он подробнее, с кнопкой и с процентами скачивания. Пузырь при
// этом обязан молчать — второе сообщение об одном и том же и есть жалоба.
func TestPuzyrMolchitPriOtkrytomOkne(t *testing.T) {
	s := stend(t)
	skazano := ""
	s.OblachkoObnovleniya = func(versiya string) { skazano = versiya }

	sprositSostoyanie(t, s, true) // окно только что спросило состояние
	s.povestitEsliNovaya("0.9.0")

	if skazano != "" {
		t.Fatalf("пузырь сказал про %q поверх открытого окна — человека дёрнули дважды об одном", skazano)
	}
	if s.Nastroyki.ObyavlennoeObnovlenie != "0.9.0" {
		t.Fatalf("версия не помечена объявленной (%q): закрыв окно кнопкой «ПОЗЖЕ», человек получил бы про неё ещё и пузырь",
			s.Nastroyki.ObyavlennoeObnovlenie)
	}
}

// TestPuzyrGovoritPriZakrytomOkne — обратная половина того же решения. Копия
// висит в трее без окна (обычный режим работы: свернул и забыл), и пузырь —
// единственный способ вообще сказать человеку про находку. Убери его совсем —
// и находка останется немой до следующего открытия окна.
func TestPuzyrGovoritPriZakrytomOkne(t *testing.T) {
	s := stend(t)
	skazano := ""
	s.OblachkoObnovleniya = func(versiya string) { skazano = versiya }

	s.povestitEsliNovaya("0.9.0") // окно состояние не спрашивало ни разу

	if skazano != "0.9.0" {
		t.Fatalf("пузырь промолчал при закрытом окне (сказано %q) — про находку человек не узнает вовсе", skazano)
	}
}

// TestOtmetkaOknaStareet — окно умирает молча (крестик, Диспетчер задач,
// авария), прощального слова от него не будет никогда. Поэтому «окно есть» —
// не тумблер, а свежесть отметки: замолчало дольше срока — значит закрыто, и
// пузырь снова получает право говорить. Не старей отметка — человек, закрывший
// окно однажды, не услышал бы про обновления больше никогда.
func TestOtmetkaOknaStareet(t *testing.T) {
	s := stend(t)

	sprositSostoyanie(t, s, true)
	if !s.oknoOtkryto() {
		t.Fatal("окно только что спросило состояние, а служба считает его закрытым")
	}

	s.zamok.Lock()
	s.oknoSprosilo = time.Now().Add(-srokZhivogoOkna - time.Second)
	s.zamok.Unlock()
	if s.oknoOtkryto() {
		t.Fatalf("окно молчит дольше %s, а служба всё ещё считает его открытым — пузырь замолчал бы навсегда", srokZhivogoOkna)
	}
}

// TestChuzhoyOprosNeSchitaetsyaOknom — по тому же адресу ходят не только окна:
// сторож окна и проверка второй копии (kopiya.Otvechaet) дёргают корень, а
// стенды и мои проверки — сам /api/sostoyanie обычным curl. Считай мы их за
// открытое окно — пузырь у человека с закрытым окном заглох бы навсегда.
func TestChuzhoyOprosNeSchitaetsyaOknom(t *testing.T) {
	s := stend(t)
	sprositSostoyanie(t, s, false)
	if s.oknoOtkryto() {
		t.Fatal("обычный опрос состояния без метки okno=1 сочли открытым окном")
	}
}
