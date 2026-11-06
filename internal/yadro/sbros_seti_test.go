package yadro

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Сброс сети ядра — DELETE /connections с паролем Clash API; 204 — сброшено,
// любой другой код и молчание — ошибка (служба тогда перезапускает ядро).
func TestSbrositSet(t *testing.T) {
	kod := http.StatusNoContent
	var metod, put, parol string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metod, put, parol = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(kod)
	}))
	defer srv.Close()
	y := &Yadro{Api: strings.TrimPrefix(srv.URL, "http://"), Sekret: "s3", Klient: srv.Client()}
	if err := y.SbrositSet(time.Second); err != nil {
		t.Fatal(err)
	}
	if metod != http.MethodDelete || put != "/connections" || parol != "Bearer s3" {
		t.Fatalf("запрос %s %s %q", metod, put, parol)
	}
	kod = http.StatusUnauthorized
	if err := y.SbrositSet(time.Second); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("401 принят за сброс: %v", err)
	}
	srv.Close()
	if err := y.SbrositSet(time.Second); err == nil {
		t.Fatal("ядро молчит, а сброс «вышел»")
	}
}
