package sluzhba

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

// profilSOcheredyu — профиль телефона, в «Соединении» которого группы-узлы (urltest) с
// именами gruppy (каждая из одного выхода с тегом из листья) и комната.
func profilSOcheredyu(t *testing.T, gruppy, listya []string) []byte {
	t.Helper()
	syroy, err := os.ReadFile("../konfig/testdata/profil_telefona.json")
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(syroy, &d); err != nil {
		t.Fatal(err)
	}
	var vless, ostalnye []any
	for _, v := range d["outbounds"].([]any) {
		o := v.(map[string]any)
		switch o["type"] {
		case "vless":
			vless = append(vless, o)
		case "selector", "urltest":
		default:
			ostalnye = append(ostalnye, o)
		}
	}
	vybor := append(append([]any{}, toAny(gruppy)...), "Комната")
	out := []any{map[string]any{"type": "selector", "tag": "Соединение", "outbounds": vybor, "default": gruppy[0]}}
	for i, g := range gruppy {
		out = append(out, map[string]any{"type": "urltest", "tag": g, "outbounds": []any{listya[i]}})
		list := map[string]any{}
		for k, v := range vless[0].(map[string]any) {
			list[k] = v
		}
		list["tag"] = listya[i]
		out = append(out, list)
	}
	d["outbounds"] = append(out, ostalnye...)
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

var (
	staryeImena  = []string{"Финляндия", "Нидерланды"}
	staryeListya = []string{"Финляндия · прямой", "Нидерланды · прямой"}
	novyeImena   = []string{"Финляндия · XHTTP", "Финляндия · Vision", "Нидерланды · XHTTP"}
	novyeListya  = []string{"fi-1", "fi-1-vision", "vu-1"}
)

func vybratVOkne(t *testing.T, s *Sluzhba, uzel string) {
	t.Helper()
	telo, _ := json.Marshal(map[string]string{"gruppa": "Соединение", "uzel": uzel})
	r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/vybrat", bytes.NewReader(telo))
	w := httptest.NewRecorder()
	s.Obsluzhit().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("выбор «%s» не принят: %s", uzel, w.Body.String())
	}
}

func TestUstarevshiyVybor(t *testing.T) {
	novyy := []byte(`{"outbounds":[
		{"type":"selector","tag":"Соединение","outbounds":["Финляндия · XHTTP","Финляндия · Vision","Нидерланды · XHTTP","Комната"]},
		{"type":"urltest","tag":"Финляндия · XHTTP","outbounds":["fi-1"]},
		{"type":"vless","tag":"fi-1"}]}`)
	sluchai := []struct {
		imya string
		uzly map[string]string
		nado []string
	}{
		{"старое имя пункта", map[string]string{"Соединение": "Финляндия"}, []string{"Соединение"}},
		{"новое имя пункта", map[string]string{"Соединение": "Финляндия · Vision"}, nil},
		{"не узел, а комната", map[string]string{"Соединение": "Комната"}, nil},
		{"группы больше нет", map[string]string{"Старая группа": "Финляндия"}, []string{"Старая группа"}},
		{"пункт внутри urltest", map[string]string{"Финляндия · XHTTP": "fi-1"}, nil},
		{"ничего не выбрано", nil, nil},
	}
	for _, c := range sluchai {
		if got := ustarevshiyVybor(novyy, c.uzly); !reflect.DeepEqual(got, c.nado) {
			t.Errorf("%s: %v, нужно %v", c.imya, got, c.nado)
		}
	}
	// Мусор вместо конфига — не повод забывать выбор человека.
	if got := ustarevshiyVybor([]byte("не json"), map[string]string{"Соединение": "Финляндия"}); got != nil {
		t.Errorf("по мусору забыт выбор: %v", got)
	}
}

// Сервер переименовал пункты («Финляндия» → «Финляндия · XHTTP» и т.д.): сохранённое имя
// старого пункта забывается вместе с новым конфигом — и в памяти службы, и на диске.
// Без этого очередь узлов считала его ручным выбором и не вмешивалась никогда.
func TestUstarevshiyVyborZabytPriNovomKonfige(t *testing.T) {
	s := stend(t)
	if err := s.SohranitProfil(profilSOcheredyu(t, staryeImena, staryeListya)); err != nil {
		t.Fatalf("не сохранил профиль: %v", err)
	}
	vybratVOkne(t, s, "Нидерланды")
	if s.Nastroyki.Uzly["Соединение"] != "Нидерланды" {
		t.Fatalf("выбор второго узла обязан записаться: %+v", s.Nastroyki.Uzly)
	}

	if err := s.SohranitProfil(profilSOcheredyu(t, novyeImena, novyeListya)); err != nil {
		t.Fatalf("не сохранил новый профиль: %v", err)
	}
	if len(s.Nastroyki.Uzly) != 0 {
		t.Fatalf("выбор пункта, которого в конфиге нет, не забыт: %+v", s.Nastroyki.Uzly)
	}
	s2, err := Novaya()
	if err != nil {
		t.Fatalf("не поднял вторую службу: %v", err)
	}
	if len(s2.Nastroyki.Uzly) != 0 {
		t.Fatalf("забытый выбор остался на диске: %+v", s2.Nastroyki.Uzly)
	}

	// Выбор, который в новом конфиге есть, переживает пересборку.
	vybratVOkne(t, s, "Нидерланды · XHTTP")
	if err := s.PerestroitKonfig(); err != nil {
		t.Fatal(err)
	}
	if s.Nastroyki.Uzly["Соединение"] != "Нидерланды · XHTTP" {
		t.Fatalf("живой выбор забыт пересборкой: %+v", s.Nastroyki.Uzly)
	}

	// Откат признака: имена вернулись — новое имя на диске так же забывается.
	if err := s.SohranitProfil(profilSOcheredyu(t, staryeImena, staryeListya)); err != nil {
		t.Fatal(err)
	}
	if len(s.Nastroyki.Uzly) != 0 {
		t.Fatalf("после отката остался выбор несуществующего пункта: %+v", s.Nastroyki.Uzly)
	}
}

// Первый пункт очереди — это автоматика: клик по нему не записывается ручным выбором и
// снимает прежний ручной. Иначе после переименования пунктов имя первого перестало бы
// совпадать с первым пунктом и очередь сочла бы его ручным.
func TestPervyyPunktOcherediNeRuchnoyVybor(t *testing.T) {
	s := stend(t)
	if err := s.SohranitProfil(profilSOcheredyu(t, novyeImena, novyeListya)); err != nil {
		t.Fatalf("не сохранил профиль: %v", err)
	}
	vybratVOkne(t, s, "Финляндия · Vision")
	if s.Nastroyki.Uzly["Соединение"] != "Финляндия · Vision" {
		t.Fatalf("выбор не первого пункта обязан записаться: %+v", s.Nastroyki.Uzly)
	}
	vybratVOkne(t, s, "Финляндия · XHTTP")
	if len(s.Nastroyki.Uzly) != 0 {
		t.Fatalf("клик по первому пункту записан ручным выбором: %+v", s.Nastroyki.Uzly)
	}
	if avto, _ := vyhodDlyaOkna(s.Nastroyki.Uzly); !avto {
		t.Fatalf("после выбора первого пункта окно обязано показывать автоматику")
	}

	// Профиль без очереди (один узел): правило первого пункта не действует, выбор как раньше.
	s2 := stend(t)
	if err := s2.SohranitProfil(profilSOcheredyu(t, []string{"Нидерланды"}, []string{"Нидерланды · прямой"})); err != nil {
		t.Fatal(err)
	}
	vybratVOkne(t, s2, "Нидерланды")
	if s2.Nastroyki.Uzly["Соединение"] != "Нидерланды" {
		t.Fatalf("без очереди выбор обязан записываться как раньше: %+v", s2.Nastroyki.Uzly)
	}
}
