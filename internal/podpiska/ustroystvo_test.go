package podpiska

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Устройство обязано называть себя на ОБОИХ запросах к серверу — и за
// конфигом, и за сведениями: точка правки в zapros одна как раз затем, чтобы
// один из двух не остался безымянным.
func TestUstroystvoNazyvaetSebyaNaOboihZaprosah(t *testing.T) {
	zagolovki := map[string]http.Header{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zagolovki[r.URL.Path] = r.Header.Clone()
		if strings.HasSuffix(r.URL.Path, "/info") {
			_, _ = w.Write([]byte(`{"name":"Егор","active":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"outbounds":[{"type":"direct"}]}`))
	}))
	defer s.Close()

	k := &Klient{Host: strings.TrimPrefix(s.URL, "http://"), Shema: "http", DeviceID: "0123456789abcdef"}
	if _, err := k.Konfig(context.Background(), "kod"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Svedeniya(context.Background(), "kod"); err != nil {
		t.Fatal(err)
	}
	if len(zagolovki) < 2 {
		t.Fatalf("сервер видел только %d путей: %v", len(zagolovki), zagolovki)
	}
	for put, h := range zagolovki {
		for _, z := range []string{"X-Device-Id", "X-Device-Model", "X-Device-Platform", "X-App-Version"} {
			if h.Get(z) == "" {
				t.Errorf("%s: заголовок %s не доехал", put, z)
			}
		}
		if h.Get("X-Device-Id") != "0123456789abcdef" {
			t.Errorf("%s: X-Device-Id = %q — идентификатор не тот, что на диске", put, h.Get("X-Device-Id"))
		}
		// Старые заголовки остаются запасным путём для сервера.
		if h.Get("x-hwid") == "" {
			t.Errorf("%s:x-hwid потерян, а он ещё нужен как фолбэк", put)
		}
	}
}

// person/device в /info — необязательные поля. Старый сервер их не шлёт, и
// разбор ответа не имеет права ни упасть, ни выдумать имя.
func TestImenaChelovekaIUstroystvaNeobyazatelny(t *testing.T) {
	sluchai := []struct {
		imya                 string
		telo                 string
		chelovek, ustroystvo string
	}{
		{"новый сервер",
			`{"name":"ключ","person":{"name":"Егор"},"device":{"name":"ASUS TUF Gaming B550"}}`,
			"Егор", "ASUS TUF Gaming B550"},
		{"старый сервер — полей нет вовсе", `{"name":"ключ","active":true}`, "", ""},
		{"поля есть, но пустые", `{"person":{"name":"  "},"device":{}}`, "", ""},
		{"знает человека, но не устройство", `{"person":{"name":"Егор"}}`, "Егор", ""},
	}
	for _, s := range sluchai {
		sv := &Svedeniya{}
		if err := json.Unmarshal([]byte(s.telo), sv); err != nil {
			t.Fatalf("%s: %v", s.imya, err)
		}
		if got := sv.ImyaCheloveka(); got != s.chelovek {
			t.Errorf("%s: человек = %q, ждали %q", s.imya, got, s.chelovek)
		}
		if got := sv.ImyaUstroystva(); got != s.ustroystvo {
			t.Errorf("%s: устройство = %q, ждали %q", s.imya, got, s.ustroystvo)
		}
	}
	// nil-Svedeniya тоже не имеет права уронить окно: сведений может не быть,
	// пока код доступа ещё не введён.
	var net *Svedeniya
	if net.ImyaCheloveka() != "" || net.ImyaUstroystva() != "" {
		t.Error("nil-сведения выдумали имена")
	}
}
