package yadro

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
)

// Группа моста «Дом» (urltest между входами с порталом) выбирает сама и к
// выбору выхода человеком отношения не имеет: в окне её нет.
func TestGruppyStatikBezMostaDoma(t *testing.T) {
	syroy := []byte(`{"outbounds":[
		{"type":"selector","tag":"Соединение","outbounds":["Нидерланды","Финляндия"],"default":"Нидерланды"},
		{"type":"urltest","tag":"` + konfig.TegMostaDoma + `","outbounds":["Нидерланды","Финляндия"]},
		{"type":"vless","tag":"Нидерланды"},{"type":"vless","tag":"Финляндия"}]}`)
	g, err := GruppyStatik(syroy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 1 || g[0].Imya != "Соединение" {
		t.Fatalf("группы в окне: %+v", g)
	}
}

// Селектором щелчка моста «Дом» правит служба: в окне его нет ни у
// работающего ядра (подставной Clash API), ни в конфиге на диске.
func TestGruppyBezShchelchkaDoma(t *testing.T) {
	syroy := []byte(`{"outbounds":[
		{"type":"selector","tag":"Соединение","outbounds":["Нидерланды","Финляндия"],"default":"Нидерланды"},
		{"type":"block","tag":"` + konfig.TegZaglushkiDoma + `"},
		{"type":"selector","tag":"` + konfig.TegShchelchkaDoma + `","outbounds":["Соединение","` + konfig.TegZaglushkiDoma + `"],"default":"Соединение"},
		{"type":"vless","tag":"Нидерланды"},{"type":"vless","tag":"Финляндия"}]}`)
	g, err := GruppyStatik(syroy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 1 || g[0].Imya != "Соединение" {
		t.Fatalf("группы в окне (с диска): %+v", g)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"proxies":{
			"Соединение":{"type":"Selector","now":"Нидерланды","all":["Нидерланды","Финляндия"]},
			"` + konfig.TegShchelchkaDoma + `":{"type":"Selector","now":"Соединение","all":["Соединение","` + konfig.TegZaglushkiDoma + `"]},
			"` + konfig.TegZaglushkiDoma + `":{"type":"Reject"},
			"Нидерланды":{"type":"VLESS"},"Финляндия":{"type":"VLESS"}}}`))
	}))
	defer srv.Close()
	y := &Yadro{Api: strings.TrimPrefix(srv.URL, "http://"), Klient: srv.Client()}
	g, err = y.Gruppy()
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 1 || g[0].Imya != "Соединение" {
		t.Fatalf("группы в окне (живое ядро): %+v", g)
	}
}
