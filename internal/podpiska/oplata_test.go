package podpiska

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestRazobratOplatuPlatyashchemu(t *testing.T) {
	o, err := RazobratOplatu([]byte(`{"platit": true, "cena": 249, "ssylka": "https://t.tb.ru/c2c-qr-choose-bank?requisiteNumber=1&bankCode=2",
		"nomer": "+7 900 000-00-00", "srok": "2026-12-12T07:00:00Z", "zhdet": false}`))
	if err != nil {
		t.Fatal(err)
	}
	want := Oplata{Platit: true, Cena: 249, Ssylka: "https://t.tb.ru/c2c-qr-choose-bank?requisiteNumber=1&bankCode=2",
		Nomer: "+7 900 000-00-00", SrokUnix: 1797058800}
	if *o != want {
		t.Fatalf("%+v != %+v", *o, want)
	}
}

func TestRazobratOplatuNeplatyashchemuINeOplata(t *testing.T) {
	o, err := RazobratOplatu([]byte(`{"platit": false}`))
	if err != nil || o.Platit {
		t.Fatalf("неплатящий: %+v %v", o, err)
	}
	for _, telo := range []string{`{"outbounds": [], "route": {}}`, `<html>`, ``} {
		if _, err := RazobratOplatu([]byte(telo)); !errors.Is(err, ErrNeOplata) {
			t.Fatalf("%q: ждал ErrNeOplata, %v", telo, err)
		}
	}
}

func TestRazobratOplatuPustoeIChuzhoe(t *testing.T) {
	o, _ := RazobratOplatu([]byte(`{"platit": true, "cena": 249, "ssylka": "", "nomer": "", "srok": "", "zhdet": true}`))
	if o.Ssylka != "" || o.Nomer != "" || o.SrokUnix != 0 || !o.Zhdet {
		t.Fatalf("%+v", o)
	}
	for _, s := range []string{`"intent://x#Intent;end"`, `"http://x.ru"`, `"https://x.ru/a b"`, `null`, `5`, `"file:///c:/x"`} {
		o, err := RazobratOplatu([]byte(`{"platit": true, "ssylka": ` + s + `, "nomer": null, "srok": "завтра"}`))
		if err != nil || o.Ssylka != "" || o.Nomer != "" || o.SrokUnix != 0 {
			t.Fatalf("%s: %+v %v", s, o, err)
		}
	}
	if o, _ := RazobratOplatu([]byte(`{"platit": true, "srok": "2026-12-12"}`)); o.SrokUnix != 1797033600 {
		t.Fatalf("голая дата — полночь UTC: %d", o.SrokUnix)
	}
}

func TestSostoyanieSrokaIShag(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	den := int64(24 * 3600)
	for _, c := range []struct {
		srok int64
		want string
	}{{0, "obychnyj"}, {now.Unix() + 4*den, "obychnyj"}, {now.Unix() + 3*den, "skoro"}, {now.Unix() + 1, "skoro"},
		{now.Unix(), "konchilsya"}, {now.Unix() - den, "konchilsya"}} {
		if g := SostoyanieSroka(c.srok, now); g != c.want {
			t.Fatalf("%d: %s, ждал %s", c.srok-now.Unix(), g, c.want)
		}
	}
	if ShagOprosaOplaty(false) != time.Hour || ShagOprosaOplaty(true) != 5*time.Minute {
		t.Fatal("шаг опроса")
	}
}

func TestBazaOplaty(t *testing.T) {
	k := &Klient{}
	for _, c := range []struct{ adres, kod, want string }{
		{"https://subkv.chickenkiller.com/k/ABCD-12", "", "https://subkv.chickenkiller.com/k/ABCD-12"},
		{"https://node2kv.chickenkiller.com/u/klyuch_1234/", "", "https://node2kv.chickenkiller.com/u/klyuch_1234"},
		{"https://subkv.chickenkiller.com/s/ABCD", "", "https://subkv.chickenkiller.com/k/ABCD"},
		{"https://subkv.chickenkiller.com/x/ABCD", "", ""},
		{"", "AB CD", "https://subkv.chickenkiller.com/k/ABCD"},
		{"", "", ""},
	} {
		if g := k.BazaOplaty(c.adres, c.kod); g != c.want {
			t.Fatalf("%q/%q: %q, ждал %q", c.adres, c.kod, g, c.want)
		}
	}
}

func TestOplataIOplatilNaDveri(t *testing.T) {
	var metod, put, ustroystvo, idem string
	kod := http.StatusOK
	k := klientNa(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metod, put, ustroystvo, idem = r.Method, r.URL.Path, r.Header.Get("X-Device-Id"), r.Header.Get("Idempotency-Key")
		w.WriteHeader(kod)
		w.Write([]byte(`{"platit": true, "cena": 249, "zhdet": ` + map[bool]string{true: "true", false: "false"}[r.Method == "POST"] + `}`))
	}))
	baza := k.BazaOplaty("", "ABCD")
	o, err := k.Oplata(context.Background(), baza)
	if err != nil || metod != "GET" || put != "/k/ABCD/oplata" || ustroystvo != "test-hwid" || o.Zhdet {
		t.Fatalf("GET: %+v %v %s %s %q", o, err, metod, put, ustroystvo)
	}
	o, err = k.Oplatil(context.Background(), baza)
	if err != nil || metod != "POST" || put != "/k/ABCD/oplatil" || idem == "" || !o.Zhdet {
		t.Fatalf("POST: %+v %v %s %s", o, err, metod, put)
	}
	kod = http.StatusTooManyRequests
	if _, err := k.Oplatil(context.Background(), baza); !errors.Is(err, ErrChasto) {
		t.Fatalf("429: %v", err)
	}
	kod = http.StatusNotFound
	var ok *OshibkaKoda
	if _, err := k.Oplata(context.Background(), baza); !errors.As(err, &ok) {
		t.Fatalf("404: %v", err)
	}
	kod = http.StatusBadGateway
	if _, err := k.Oplata(context.Background(), baza); err == nil || errors.Is(err, ErrChasto) {
		t.Fatalf("502: %v", err)
	}
}
