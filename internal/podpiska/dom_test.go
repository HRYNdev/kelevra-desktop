package podpiska

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVoytiSoSlotom(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/k/K7QM2XRT9PLA/ustroystvo" {
			w.Write([]byte(`{"profil":"` + srv.URL + `/u/klyuch123456","slot":"01-6"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	k := &Klient{Shema: "http", DeviceID: "dev-1"}
	adres, slot, err := k.VoytiSoSlotom(context.Background(), "K7QM 2XRT 9PLA", []string{strings.TrimPrefix(srv.URL, "http://")})
	if err != nil || slot != "01-6" || !strings.HasSuffix(adres, "/u/klyuch123456") {
		t.Fatalf("вход: %q %q %v", adres, slot, err)
	}
}

func TestOtpravitKlyuchDoma(t *testing.T) {
	var telo map[string]string
	var h http.Header
	kod := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/k/K7QM2XRT9PLA/dom/klyuch" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		h = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&telo)
		w.WriteHeader(kod)
		w.Write([]byte(`{"sostoyanie":"zhdet","dom":false}`))
	}))
	defer srv.Close()
	k := &Klient{Shema: "http", DeviceID: "dev-1"}
	dver := strings.TrimPrefix(srv.URL, "http://")
	o, err := k.OtpravitKlyuchDoma(context.Background(), dver, "K7QM2XRT9PLA", "01-2", "PUB=", "KLYUCH")
	if err != nil || o.Sostoyanie != "zhdet" || o.Dom == nil || *o.Dom {
		t.Fatalf("ответ: %+v %v", o, err)
	}
	if len(telo) != 2 || telo["slot"] != "01-2" || telo["pub"] != "PUB=" {
		t.Fatalf("тело: %v", telo)
	}
	if h.Get("X-Device-Id") != "dev-1" || h.Get("X-Device-Key") != "KLYUCH" || h.Get("Content-Type") != "application/json" {
		t.Fatalf("заголовки: %v", h)
	}
	kod = http.StatusNotFound
	_, err = k.OtpravitKlyuchDoma(context.Background(), dver, "K7QM2XRT9PLA", "01-2", "PUB=", "")
	var o404 *OshibkaKlyuchaDoma
	if !errors.As(err, &o404) || o404.Status != http.StatusNotFound {
		t.Fatalf("404: %v", err)
	}
	if strings.Contains(err.Error(), "K7QM2XRT9PLA") {
		t.Fatalf("код в тексте ошибки: %v", err)
	}
}
