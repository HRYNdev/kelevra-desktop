package yadro

import (
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
