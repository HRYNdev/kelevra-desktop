package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Окно на старте убирает системный прокси старой версии, только если рядом
// нет ЖИВОЙ копии старой версии (свой прокси она держит сама и снимет сама,
// уступая место). Живая копия версии без прокси уборке не мешает.
func TestKopiyaBezProksiPoVersii(t *testing.T) {
	sluchai := []struct {
		versiya string
		hotim   bool
	}{
		{"0.6.76", false},
		{"0.6.72", false},
		{versiyaBezProksi, true},
		{"0.6.80", true},
		{"", false}, // не ответила о версии — считаем старой
	}
	for _, s := range sluchai {
		t.Run(s.versiya, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if s.versiya == "" {
					http.Error(w, "нет", http.StatusNotFound)
					return
				}
				fmt.Fprintf(w, `{"tekushchaya":%q}`, s.versiya)
			}))
			defer srv.Close()
			if got := kopiyaBezProksi(srv.URL + "/"); got != s.hotim {
				t.Fatalf("копия версии %q: kopiyaBezProksi = %v, ждали %v", s.versiya, got, s.hotim)
			}
		})
	}
}
