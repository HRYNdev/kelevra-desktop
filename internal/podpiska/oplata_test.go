package podpiska

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
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
	if !reflect.DeepEqual(*o, want) {
		t.Fatalf("%+v != %+v", *o, want)
	}
}

func TestRazobratOplatuIstoriya(t *testing.T) {
	o, err := RazobratOplatu([]byte(`{"platit": true, "cena": 249, "srok": "2026-12-12T07:00:00Z", "istoriya": [
		{"data": "2026-11-12T05:00:00Z", "summa": 249, "do": "2026-12-12T07:00:00Z"},
		{"data": "мусор", "summa": 249, "do": "2026-11-12T07:00:00Z"},
		"не объект",
		{"data": "2026-10-12T05:00:00Z", "summa": "249", "do": null},
		{"data": "2026-09-12"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []ZapisOplaty{
		{DataUnix: 1794459600, Summa: 249, DoUnix: 1797058800},
		{DataUnix: 1791781200},
		{DataUnix: 1789171200},
	}
	if !reflect.DeepEqual(o.Istoriya, want) {
		t.Fatalf("%+v != %+v", o.Istoriya, want)
	}
	for _, telo := range []string{`{"platit": true}`, `{"platit": true, "istoriya": null}`, `{"platit": true, "istoriya": {"a": 1}}`,
		`{"platit": true, "istoriya": []}`} {
		if o, err := RazobratOplatu([]byte(telo)); err != nil || o.Istoriya != nil {
			t.Fatalf("%s: %+v %v", telo, o, err)
		}
	}
	var mnogo strings.Builder
	mnogo.WriteString(`{"platit": true, "istoriya": [`)
	for i := 0; i < 40; i++ {
		if i > 0 {
			mnogo.WriteString(",")
		}
		mnogo.WriteString(`{"data": "2026-01-01T00:00:00Z", "summa": 249}`)
	}
	mnogo.WriteString(`]}`)
	if o, _ := RazobratOplatu([]byte(mnogo.String())); len(o.Istoriya) != potolokIstorii {
		t.Fatalf("потолок истории: %d", len(o.Istoriya))
	}
}

func TestNapominanieOplaty(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	den := int64(24 * 3600)
	srok := func(s int64) *Oplata { return &Oplata{Platit: true, Cena: 249, SrokUnix: s} }
	data := func(s int64) string { return time.Unix(s, 0).In(time.Local).Format("02.01") }

	skoro := now.Unix() + 2*den
	if k, tekst := NapominanieOplaty(srok(skoro), now); k != fmt.Sprintf("skoro:%d", skoro) || tekst != "Подписка до "+data(skoro)+" · 249 ₽" {
		t.Fatalf("скоро: %q %q", k, tekst)
	}
	vyshel := now.Unix() - 3600
	if k, tekst := NapominanieOplaty(srok(vyshel), now); k != fmt.Sprintf("konchilsya:%d", vyshel) || tekst != "Подписка закончилась · 249 ₽" {
		t.Fatalf("закончилась: %q %q", k, tekst)
	}
	bezCeny := &Oplata{Platit: true, SrokUnix: vyshel}
	if _, tekst := NapominanieOplaty(bezCeny, now); tekst != "Подписка закончилась" {
		t.Fatalf("без цены: %q", tekst)
	}
	zhdet := srok(skoro)
	zhdet.Zhdet = true
	for imya, o := range map[string]*Oplata{
		"nil": nil, "неплатящий": {SrokUnix: skoro}, "бессрочно": srok(0), "далеко": srok(now.Unix() + 10*den),
		"давно вышел": srok(now.Unix() - 4*den), "ждёт": zhdet,
	} {
		if k, tekst := NapominanieOplaty(o, now); k != "" || tekst != "" {
			t.Fatalf("%s: %q %q", imya, k, tekst)
		}
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
	if ShagOprosaOplaty(false) != time.Hour || ShagOprosaOplaty(true) != 30*time.Second {
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
