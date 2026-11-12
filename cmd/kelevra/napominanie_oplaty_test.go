package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestOprosNapominaniy(t *testing.T) {
	otvety := []struct{ klyuch, tekst string }{
		{"", ""},
		{"skoro:1", "Подписка до 12.12 · 249 ₽"}, // пузырь не показался — не отмечаем
		{"skoro:1", "Подписка до 12.12 · 249 ₽"},
		{"konchilsya:1", "Подписка закончилась · 249 ₽"},
	}
	pokazy := []bool{false, true, true}
	var pokazano, otmecheno []string
	stop := make(chan struct{})
	gotovo := make(chan struct{})
	i := 0
	sprosit := func(string) (string, string) {
		o := otvety[i]
		i++
		if i >= len(otvety) {
			defer close(stop)
		}
		return o.klyuch, o.tekst
	}
	pokazat := func(tekst string) bool {
		ok := pokazy[len(pokazano)]
		pokazano = append(pokazano, tekst)
		return ok
	}
	go func() {
		defer close(gotovo)
		oprashivatNapominaniya(time.Millisecond, time.Millisecond, func() (string, bool) { return "http://x/", true },
			sprosit, pokazat, func(_, k string) { otmecheno = append(otmecheno, k) }, stop)
	}()
	select {
	case <-gotovo:
	case <-time.After(5 * time.Second):
		t.Fatal("опрос не вышел")
	}
	if want := []string{"skoro:1", "konchilsya:1"}; !reflect.DeepEqual(otmecheno, want) {
		t.Fatalf("отмечено %v, ждали %v", otmecheno, want)
	}
	if len(pokazano) != 3 {
		t.Fatalf("показано %v", pokazano)
	}
}

func TestNapominanieSluzhbyPoHTTP(t *testing.T) {
	var postKlyuch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/k/api/napominanie" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost {
			postKlyuch = r.URL.Query().Get("klyuch")
			w.Write([]byte(`{}`))
			return
		}
		w.Write([]byte(`{"klyuch": "skoro:1", "tekst": "Подписка до 12.12 · 249 ₽"}`))
	}))
	defer srv.Close()
	adres := srv.URL + "/k/"
	if k, tekst := napominanieSluzhby(adres); k != "skoro:1" || tekst != "Подписка до 12.12 · 249 ₽" {
		t.Fatalf("%q %q", k, tekst)
	}
	otmetitNapominanieSluzhby(adres, "skoro:1")
	if postKlyuch != "skoro:1" {
		t.Fatalf("отметка: %q", postKlyuch)
	}
	if k, _ := napominanieSluzhby(srv.URL + "/net/"); k != "" {
		t.Fatalf("404: %q", k)
	}
}
