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
		w.Write([]byte(`{"name":"Вова","active":true,"expires":1790000000,"used_bytes":123}`))
	}))
	s, err := k.Svedeniya(context.Background(), "ABC")
	if err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	if !s.Aktivna || s.Imya != "Вова" || s.SyedenoB != 123 {
		t.Fatalf("сведения разобраны неверно: %+v", s)
	}
}
