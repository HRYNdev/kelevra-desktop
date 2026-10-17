package podpiska

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dver — подставная выдача: host без схемы и сам сервер.
func dver(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return strings.TrimPrefix(s.URL, "http://")
}

func klientDlyaDverey() *Klient {
	return &Klient{Shema: "http", DeviceID: "test-hwid", HTTP: &http.Client{}}
}

// Дверь своей сети знает вход устройства: отдаёт адрес /u/<ключ>, его и сохраняем.
func TestVoytiVhodUstroystva(t *testing.T) {
	var zagolovok string
	d := dver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/k/KOD1/ustroystvo" {
			zagolovok = r.Header.Get("X-Device-Id")
			w.Write([]byte(`{"profil":"https://node2kv.example/u/klyuch123456","slot":"01-6"}`))
			return
		}
		w.WriteHeader(404)
	})
	adres, err := klientDlyaDverey().Voyti(context.Background(), "KOD1", []string{d})
	if err != nil {
		t.Fatalf("ждал вход, получил: %v", err)
	}
	if adres != "https://node2kv.example/u/klyuch123456" {
		t.Fatalf("не тот адрес: %s", adres)
	}
	if zagolovok != "test-hwid" {
		t.Fatalf("устройство не назвало себя: %q", zagolovok)
	}
}

// Старый сервер семьи входа устройства не знает (404) — код берётся старым путём /k/,
// и это первая дверь: семья остаётся там же, где была.
func TestVoytiStaryyPutNaPervoyDveri(t *testing.T) {
	staraya := dver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/k/KOD2" {
			w.Write([]byte(primerKonfiga))
			return
		}
		w.WriteHeader(404)
	})
	novayaZvali := false
	novaya := dver(t, func(w http.ResponseWriter, r *http.Request) { novayaZvali = true; w.WriteHeader(404) })
	adres, err := klientDlyaDverey().Voyti(context.Background(), "KOD2", []string{staraya, novaya})
	if err != nil {
		t.Fatalf("ждал вход старым путём, получил: %v", err)
	}
	if adres != "http://"+staraya+"/k/KOD2" {
		t.Fatalf("не тот адрес: %s", adres)
	}
	if novayaZvali {
		t.Fatal("до второй двери дошли, хотя первая код знает")
	}
}

// Код своей сети: старая дверь его не знает, вторая выдаёт ключ.
func TestVoytiVtorayaDver(t *testing.T) {
	staraya := dver(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	novaya := dver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Write([]byte(`{"profil":"https://n/u/k2k2k2k2k2k2","slot":"01-3"}`))
			return
		}
		w.WriteHeader(404)
	})
	adres, err := klientDlyaDverey().Voyti(context.Background(), "KOD3", []string{staraya, novaya})
	if err != nil || adres != "https://n/u/k2k2k2k2k2k2" {
		t.Fatalf("ждал адрес второй двери, получил %q, %v", adres, err)
	}
}

// Лимит устройств: слова сервера доходят до человека, дальше по дверям не идём.
func TestVoytiLimit(t *testing.T) {
	d := dver(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"oshibka":"limit_ustroystv","tekst":"Достигнут лимит устройств"}`))
	})
	_, err := klientDlyaDverey().Voyti(context.Background(), "KOD4", []string{d})
	var o *OshibkaVhoda
	if !errors.As(err, &o) || o.Tekst != "Достигнут лимит устройств" {
		t.Fatalf("ждал отказ с текстом сервера, получил %v", err)
	}
}

// Ни одна дверь кода не знает — это «нет такого кода», а не «нет связи».
func TestVoytiNeizvestnyyKod(t *testing.T) {
	d1 := dver(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	d2 := dver(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	_, err := klientDlyaDverey().Voyti(context.Background(), "NETU", []string{d1, d2})
	var o *OshibkaKoda
	if !errors.As(err, &o) {
		t.Fatalf("ждал «код неизвестен», получил %v", err)
	}
}

// Первая дверь недоступна — идём ко второй, а не сдаёмся.
func TestVoytiPervayaDverLezhit(t *testing.T) {
	novaya := dver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Write([]byte(`{"profil":"https://n/u/k3k3k3k3k3k3"}`))
			return
		}
		w.WriteHeader(404)
	})
	adres, err := klientDlyaDverey().Voyti(context.Background(), "KOD5", []string{"127.0.0.1:1", novaya})
	if err != nil || adres != "https://n/u/k3k3k3k3k3k3" {
		t.Fatalf("ждал вторую дверь, получил %q, %v", adres, err)
	}
}

func TestRazobratSsylku(t *testing.T) {
	sluchai := []struct {
		vvod, adres, hvost string
		ok                 bool
	}{
		{"https://node2kv.chickenkiller.com/u/AbC_123-xyz", "https://node2kv.chickenkiller.com/u/AbC_123-xyz", "AbC_123-xyz", true},
		{"  https://subkv.chickenkiller.com/k/KOD12345/  ", "https://subkv.chickenkiller.com/k/KOD12345", "KOD12345", true},
		{"https://node2kv.chickenkiller.com/u/klyuch99/info?device=x", "https://node2kv.chickenkiller.com/u/klyuch99", "klyuch99", true},
		{"VS4EMH9PQUMX", "", "", false},
		{"https://example.com/panel/", "", "", false},
	}
	for _, s := range sluchai {
		a, h, ok := RazobratSsylku(s.vvod)
		if a != s.adres || h != s.hvost || ok != s.ok {
			t.Errorf("%q: получил (%q, %q, %v), ждал (%q, %q, %v)", s.vvod, a, h, ok, s.adres, s.hvost, s.ok)
		}
	}
}

// Профиль и сведения по сохранённому адресу: /info приклеивается к адресу, двери из сведений читаются.
func TestPoAdresu(t *testing.T) {
	d := dver(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/u/klyuch1234":
			w.Write([]byte(primerKonfiga))
		case "/u/klyuch1234/info":
			if r.URL.Query().Get("device") != "test-hwid" {
				w.WriteHeader(400)
				return
			}
			w.Write([]byte(`{"name":"owner","active":true,"dveri":["a.example","b.example"]}`))
		default:
			w.WriteHeader(404)
		}
	})
	k := klientDlyaDverey()
	adres := "http://" + d + "/u/klyuch1234"
	if _, err := k.KonfigPoAdresu(context.Background(), adres); err != nil {
		t.Fatalf("профиль по адресу: %v", err)
	}
	sv, err := k.SvedeniyaPoAdresu(context.Background(), adres)
	if err != nil || sv.Imya != "owner" || len(sv.Dveri) != 2 {
		t.Fatalf("сведения по адресу: %+v, %v", sv, err)
	}
	if _, err := k.KonfigPoAdresu(context.Background(), "http://"+d+"/u/chuzhoy0000"); !errors.As(err, new(*OshibkaKoda)) {
		t.Fatalf("неизвестный ключ должен дать OshibkaKoda, получил %v", err)
	}
}
