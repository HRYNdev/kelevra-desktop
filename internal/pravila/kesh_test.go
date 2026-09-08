package pravila

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// telo — минимальный «набор правил»: кеш проверяет только подпись SRS.
var telo = []byte("SRS-proba")

// serverNaborov — раздача одного набора с ETag, считающая полные отдачи.
//
// Считаем именно ОТДАЧИ ТЕЛА: весь смысл условных запросов в том, что при
// втором заходе сервер отвечает 304 и мегабайт по сети не едет.
func serverNaborov(t *testing.T, etag string) (*httptest.Server, *int32) {
	t.Helper()
	var otdano int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		atomic.AddInt32(&otdano, 1)
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", "Mon, 08 Sep 2026 10:20:00 GMT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(telo)
	}))
	t.Cleanup(s.Close)
	return s, &otdano
}

// Вторая сверка не должна качать то, что не менялось.
//
// До 09.09 обновление кеша шло голым GET без единого заголовка: сервер каждый
// раз отдавал все наборы целиком (≈1 МБ на 23 набора), хотя 19 из них не
// менялись неделями. Без условных запросов этот тест краснеет на второй
// отдаче тела.
func TestSverkaNeKachaetNeizmenivshiesyaNabory(t *testing.T) {
	papka := t.TempDir()
	server, otdano := serverNaborov(t, `"proba-1"`)
	adresa := map[string]string{"ads": server.URL + "/ads.srs"}

	itog, err := Obnovit(context.Background(), nil, adresa, papka)
	if err != nil {
		t.Fatalf("первая сверка: %v", err)
	}
	if itog.Obnovleno != 1 || itog.Svereno != 1 {
		t.Fatalf("первая сверка: сверено %d, обновлено %d — ждали 1 и 1", itog.Svereno, itog.Obnovleno)
	}

	itog, err = Obnovit(context.Background(), nil, adresa, papka)
	if err != nil {
		t.Fatalf("вторая сверка: %v", err)
	}
	if itog.Obnovleno != 0 {
		t.Fatalf("вторая сверка сообщила об обновлении %d наборов, хотя на сервере ничего не менялось", itog.Obnovleno)
	}
	if n := atomic.LoadInt32(otdano); n != 1 {
		t.Fatalf("сервер отдал тело %d раза — условный запрос не сработал, качаем одно и то же", n)
	}
	if itog.Svereno != 1 {
		t.Fatalf("вторая сверка: сверено %d, ждали 1", itog.Svereno)
	}
}

// Свежесть считается по времени СВЕРКИ, а не по времени файла.
//
// Файл не меняется, когда сервер отвечает «не менялось», и не переживает
// переноса папки данных — клиент переезжал из папки пользователя в общую
// 08.09. Мерить возраст кеша по mtime значит либо качать заново без повода,
// либо считать протухший кеш свежим.
func TestVozrastSchitaetsyaPoVremeniSverki(t *testing.T) {
	papka := t.TempDir()
	server, _ := serverNaborov(t, `"proba-2"`)
	adresa := map[string]string{"ads": server.URL + "/ads.srs"}

	if _, err := Obnovit(context.Background(), nil, adresa, papka); err != nil {
		t.Fatalf("сверка: %v", err)
	}
	// Состариваем ФАЙЛ на неделю, сведения не трогаем.
	staro := time.Now().Add(-7 * 24 * time.Hour)
	put := filepath.Join(PutKesha(papka), "ads.srs")
	if err := os.Chtimes(put, staro, staro); err != nil {
		t.Fatalf("не состарил файл: %v", err)
	}

	if v := Vozrast(papka, []string{"ads"}); v > time.Minute {
		t.Fatalf("возраст кеша %s — считается по времени файла, а должен по времени сверки", v)
	}
	if Ustarel(papka, []string{"ads"}) {
		t.Fatal("кеш объявлен устаревшим сразу после сверки")
	}
}

// Условие не ставим, если самого набора на диске нет.
//
// Иначе 304 оставляет нас с пустотой: сервер говорит «не менялось», а ядру
// показывать нечего — и связь не поднимется вовсе.
func TestPropavshiyFaylKachaetsyaZanovoNesmotryaNaSvedeniya(t *testing.T) {
	papka := t.TempDir()
	server, otdano := serverNaborov(t, `"proba-3"`)
	adresa := map[string]string{"ads": server.URL + "/ads.srs"}

	if _, err := Obnovit(context.Background(), nil, adresa, papka); err != nil {
		t.Fatalf("первая сверка: %v", err)
	}
	if err := os.Remove(filepath.Join(PutKesha(papka), "ads.srs")); err != nil {
		t.Fatalf("не убрал набор: %v", err)
	}

	if _, err := Obnovit(context.Background(), nil, adresa, papka); err != nil {
		t.Fatalf("вторая сверка: %v", err)
	}
	if n := atomic.LoadInt32(otdano); n != 2 {
		t.Fatalf("сервер отдал тело %d раз(а): пропавший набор не перекачали, кеш остался дырявым", n)
	}
	if IzKesha(papka, []string{"ads"}) == nil {
		t.Fatal("после сверки набора всё равно нет на диске")
	}
}

// Пустой список нужных наборов — это «спрашивать было нечего», а не «кеш подошёл».
//
// Профиль не прочитан → tegiPravil пуст → раньше отсюда возвращалась пустая,
// но не nil карта, и в журнал уходило бодрое «беру правила из кеша (0
// наборов)» при полном их отсутствии.
func TestPustoySpisokNeSchitaetsyaGodnymKeshem(t *testing.T) {
	if m := IzKesha(t.TempDir(), nil); m != nil {
		t.Fatalf("пустой список дал карту %v вместо nil", m)
	}
}
