package podpiska

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const primerKonfiga = `{"log":{"level":"info"},"outbounds":[{"type":"direct","tag":"direct"}]}`

func klientNa(t *testing.T, h http.Handler) *Klient {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return &Klient{Host: strings.TrimPrefix(s.URL, "http://"), Shema: "http", HTTP: s.Client(), DeviceID: "test-hwid"}
}

// Сервер отвечает по мобильному пути /k/ — берём его.
func TestKonfigPoPutiK(t *testing.T) {
	k := klientNa(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/k/ABC" {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte(primerKonfiga))
	}))
	b, err := k.Konfig(context.Background(), "ABC")
	if err != nil {
		t.Fatalf("ждал конфиг, получил ошибку: %v", err)
	}
	if !strings.Contains(string(b), "outbounds") {
		t.Fatalf("вернулось не то: %s", b)
	}
}

// Сервер знает только старый десктопный путь /s/ — клиент обязан дойти вторым заходом.
func TestKonfigOtkatNaPutS(t *testing.T) {
	k := klientNa(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/s/ABC" {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte(primerKonfiga))
	}))
	if _, err := k.Konfig(context.Background(), "ABC"); err != nil {
		t.Fatalf("откат на /s/ не сработал: %v", err)
	}
}

// Неизвестный код — понятная ошибка, а не «сервер ответил 404».
func TestNeizvestnyyKod(t *testing.T) {
	k := klientNa(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	_, err := k.Konfig(context.Background(), "XXX")
	if err == nil {
		t.Fatal("ждал ошибку")
	}
	if _, то := err.(*OshibkaKoda); !то {
		t.Fatalf("ждал OshibkaKoda, получил %T: %v", err, err)
	}
}

// Сервер отдал HTML вместо конфига — приложение не должно нести это в ядро.
func TestMusorVmestoKonfiga(t *testing.T) {
	k := klientNa(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>заглушка провайдера</html>"))
	}))
	if _, err := k.Konfig(context.Background(), "ABC"); err == nil {
		t.Fatal("мусор прошёл проверку конфига")
	}
}

// Конфиг без узлов — тоже мусор: ядро на нём не поднимет соединение.
func TestKonfigBezUzlov(t *testing.T) {
	k := klientNa(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"log":{"level":"info"}}`))
	}))
	if _, err := k.Konfig(context.Background(), "ABC"); err == nil {
		t.Fatal("конфиг без outbounds прошёл проверку")
	}
}

func TestSvedeniya(t *testing.T) {
	k := klientNa(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/k/ABC/info" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("device") != "test-hwid" {
			t.Errorf("устройство не доехало: %s", r.URL.RawQuery)
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "Kelevra-Desktop/") {
			t.Errorf("чужой User-Agent: %s", r.Header.Get("User-Agent"))
		}
		w.Write([]byte(`{"name":"хозяин","active":true,"expires":1790000000,"used_bytes":123}`))
	}))
	s, err := k.Svedeniya(context.Background(), "ABC")
	if err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	if !s.Aktivna || s.Imya != "хозяин" || s.SyedenoB != 123 {
		t.Fatalf("сведения разобраны неверно: %+v", s)
	}
}

// TestMaskaNikogdaNeOtdaetKodTselikom — гейт на утечку ключа. Маска живёт
// ради одного: окно с открытым кодом человек снимает на телефон и шлёт в
// поддержку, и вместе со снимком уезжает рабочий доступ к подписке. Поэтому
// проверяется не «красиво ли», а свойство: в маске НЕТ ни одного знака кода,
// кроме последних двух, и её длина не растёт с длиной ключа.
func TestMaskaNikogdaNeOtdaetKodTselikom(t *testing.T) {
	for _, kod := range []string{
		"Hgh-QXAH8_8HQ_Et",
		"короткий",
		"abcdefghijklmnopqrstuvwxyz0123456789",
		"  Hgh-QXAH8_8HQ_Et  ", // пробелы по краям сервер и человек шлют регулярно
	} {
		m := Maska(kod)
		golyy := strings.TrimSpace(kod)
		if m == golyy {
			t.Errorf("Maska(%q) вернула сам код", kod)
		}
		if !strings.HasPrefix(m, "***") {
			t.Errorf("Maska(%q) = %q — маска обязана начинаться со звёздочек", kod, m)
		}
		if len([]rune(m)) > 5 {
			t.Errorf("Maska(%q) = %q — открыто больше двух знаков", kod, m)
		}
		// Хвост обязан совпасть: маска бесполезна, если по ней нельзя узнать
		// СВОЙ ключ (ради этого она в окне и стоит).
		if hvost := []rune(golyy); len(hvost) >= 3 && !strings.HasSuffix(m, string(hvost[len(hvost)-2:])) {
			t.Errorf("Maska(%q) = %q — по ней не узнать свой ключ", kod, m)
		}
	}
	if m := Maska("   "); m != "" {
		t.Errorf("Maska(пустой код) = %q, ждали пустую строку: строки «Код доступа» в окне тогда нет вовсе", m)
	}
	// Код в один-два знака маскировать нечем — отдаём звёздочки без хвоста, а
	// не сам код: пусть лучше строка бесполезна, чем выдаёт ключ целиком.
	if m := Maska("ab"); m != "***" {
		t.Errorf("Maska(%q) = %q, ждали ***", "ab", m)
	}
}

// Расход обязан уехать на ОБА запроса к серверу: и за конфигом, и за
// сведениями. Точка сборки заголовков одна (zapros), но проверяем оба пути —
// иначе правка, разведшая их, останется незамеченной.
func TestRashodEdetNaObaZaprosa(t *testing.T) {
	vidno := map[string]string{}
	k := klientNa(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vidno[r.URL.Path] = r.Header.Get("X-Device-Traffic")
		if strings.HasSuffix(r.URL.Path, "/info") {
			w.Write([]byte(`{"name":"n","active":true}`))
			return
		}
		w.Write([]byte(primerKonfiga))
	}))
	k.Trafik = func() int64 { return 987654321 }

	if _, err := k.Konfig(context.Background(), "ABC"); err != nil {
		t.Fatalf("конфиг не взялся: %v", err)
	}
	if _, err := k.Svedeniya(context.Background(), "ABC"); err != nil {
		t.Fatalf("сведения не взялись: %v", err)
	}
	for _, put := range []string{"/k/ABC", "/k/ABC/info"} {
		if vidno[put] != "987654321" {
			t.Fatalf("на %s расход пришёл как %q, ждали 987654321", put, vidno[put])
		}
	}
}

// Источника расхода может не быть вовсе (стенд, ранний старт) — это не повод
// падать и не повод слать пустой или нулевой заголовок.
func TestBezIstochnikaRashodaZagolovkaNet(t *testing.T) {
	var byl bool
	k := klientNa(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, byl = r.Header["X-Device-Traffic"]
		w.Write([]byte(primerKonfiga))
	}))
	if _, err := k.Konfig(context.Background(), "ABC"); err != nil {
		t.Fatalf("конфиг не взялся: %v", err)
	}
	if byl {
		t.Fatalf("клиент без источника расхода всё же поставил X-Device-Traffic")
	}
}
