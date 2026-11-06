package konfig

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Мост «Дом» всегда идёт через свой селектор щелчка (TegShchelchkaDoma):
// служба щёлкает его на заглушку и обратно вместо перезапуска ядра
// (sluzhba/dom.go, shchelknutMost).

// vyhodZaShchelchkom — detour эндпоинта «Дом» — селектор щелчка ровно нашей
// формы (прежний выход и заглушка block, default — выход, соединения людей
// не рвёт); возвращает выход за ним.
func vyhodZaShchelchkom(t *testing.T, d map[string]any) string {
	t.Helper()
	if got := endpointDoma(t, d)["detour"]; got != TegShchelchkaDoma {
		t.Fatalf("detour эндпоинта = %v, ждали селектор %q", got, TegShchelchkaDoma)
	}
	sel := vyhodPoTegu(d, TegShchelchkaDoma)
	if sel == nil {
		t.Fatal("селектора щелчка нет")
	}
	zagl := vyhodPoTegu(d, TegZaglushkiDoma)
	if !reflect.DeepEqual(zagl, map[string]any{"type": "block", "tag": TegZaglushkiDoma}) {
		t.Fatalf("заглушка: %v", zagl)
	}
	varianty, _ := sel["outbounds"].([]any)
	if sel["type"] != "selector" || len(varianty) != 2 || varianty[1] != TegZaglushkiDoma {
		t.Fatalf("селектор щелчка: %v", sel)
	}
	if sel["interrupt_exist_connections"] != false {
		t.Fatalf("селектор щелчка рвал бы соединения людей: %v", sel)
	}
	vyhod, _ := varianty[0].(string)
	if sel["default"] != vyhod || vyhod == "" || vyhodPoTegu(d, vyhod) == nil {
		t.Fatalf("default селектора %v, выход %q", sel["default"], vyhod)
	}
	return vyhod
}

// Три случая моста — один портал, несколько, ни одного: за щелчком прежний
// выход; конфиг ядра это же и говорит (RazobratMostDoma, MostDomaIzKonfiga);
// ни правила, ни выбор человека, ни служебный вход в щелчок не ведут.
func TestShchelchokMostaVoVsehSluchayah(t *testing.T) {
	for _, sl := range []struct {
		imya  string
		dom   *Dom
		vyhod string
	}{
		{"один портал", domSPortalami(PortalDoma{Uzel: "fi-1", Server: "fi.example.org", Port: 2053}), tegFiX},
		{"два портала", domSPortalami(
			PortalDoma{Uzel: "fi-1", Server: "fi.example.org", Port: 2053},
			PortalDoma{Uzel: "vu-1", Server: "primer.example", Port: 443}), TegMostaDoma},
		{"порталов нет", probnyyDom(), "Соединение"},
	} {
		t.Run(sl.imya, func(t *testing.T) {
			gotovyy, k, err := Prigotovit(profilSPortalami(t, false), Vybor{Dom: sl.dom, ClashSekret: "s",
				VhodSluzhby: &VhodSluzhby{Port: 20124, Parol: "p"}})
			if err != nil || !k.Dom || k.DomBeda != "" {
				t.Fatalf("дом не вписан: %v %+v", err, k)
			}
			var d map[string]any
			if err := json.Unmarshal(gotovyy, &d); err != nil {
				t.Fatal(err)
			}
			if got := vyhodZaShchelchkom(t, d); got != sl.vyhod {
				t.Fatalf("за щелчком %q, ждали %q", got, sl.vyhod)
			}
			if m := RazobratMostDoma(gotovyy); m != (MostDoma{Vyhod: sl.vyhod, Selektor: TegShchelchkaDoma, Zaglushka: TegZaglushkiDoma}) {
				t.Fatalf("мост из конфига: %+v", m)
			}
			if got := MostDomaIzKonfiga(gotovyy); got != sl.vyhod {
				t.Fatalf("MostDomaIzKonfiga = %q, ждали выход %q, не селектор и не заглушку", got, sl.vyhod)
			}
			for _, p := range d["route"].(map[string]any)["rules"].([]any) {
				o, _ := p.(map[string]any)["outbound"].(string)
				if o == TegShchelchkaDoma || o == TegZaglushkiDoma {
					t.Fatalf("правило маршрута в щелчок: %v", p)
				}
			}
			if f, _ := d["route"].(map[string]any)["final"].(string); f == TegShchelchkaDoma || f == TegZaglushkiDoma {
				t.Fatalf("route.final = %q", f)
			}
			for _, x := range vyhodPoTegu(d, "Соединение")["outbounds"].([]any) {
				if x == TegShchelchkaDoma || x == TegZaglushkiDoma {
					t.Fatal("щелчок попал в выбор человека")
				}
			}
			if got := tunnelnyyVyhod(d); got != "Соединение" {
				t.Fatalf("туннельный выход %q, ждали «Соединение»", got)
			}
		})
	}
}

// Профиль без своего селектора: служебный вход и упрощённый режим не уходят
// в селектор щелчка (он первый и единственный selector в конфиге).
func TestShchelchokNeTunnelnyyVyhod(t *testing.T) {
	d := map[string]any{"outbounds": []any{
		map[string]any{"type": "vless", "tag": "a"},
		map[string]any{"type": "block", "tag": TegZaglushkiDoma},
		map[string]any{"type": "selector", "tag": TegShchelchkaDoma, "outbounds": []any{"a", TegZaglushkiDoma}},
	}}
	if got := tunnelnyyVyhod(d); got != "a" {
		t.Fatalf("туннельный выход %q, ждали «a»", got)
	}
}

// Профиль не вправе принести выходы с тегами щелчка и заглушки (п.15): они
// и ссылки на них выбрасываются, свои вписываются.
func TestShchelchokChuzhoyVyrezaetsya(t *testing.T) {
	var p map[string]any
	if err := json.Unmarshal(profilSPortalami(t, false), &p); err != nil {
		t.Fatal(err)
	}
	p["outbounds"] = append(p["outbounds"].([]any),
		map[string]any{"type": "direct", "tag": TegShchelchkaDoma},
		map[string]any{"type": "direct", "tag": TegZaglushkiDoma})
	r := p["route"].(map[string]any)
	r["rules"] = append([]any{map[string]any{"ip_cidr": []any{"203.0.113.0/24"}, "outbound": TegShchelchkaDoma}}, r["rules"].([]any)...)
	syroy, _ := json.Marshal(p)

	d, k := sobrat(t, syroy, Vybor{Dom: probnyyDom()})
	if !k.Dom || k.DomBeda != "" {
		t.Fatalf("чужие теги сорвали вход домой: %+v", k)
	}
	if got := vyhodZaShchelchkom(t, d); got != "Соединение" {
		t.Fatalf("за щелчком %q", got)
	}
	for _, pr := range d["route"].(map[string]any)["rules"].([]any) {
		if pr.(map[string]any)["outbound"] == TegShchelchkaDoma {
			t.Fatalf("правило профиля в щелчок выжило: %v", pr)
		}
	}
	bez, _, err := Prigotovit(syroy, Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bez), TegShchelchkaDoma) || strings.Contains(string(bez), TegZaglushkiDoma) {
		t.Fatal("без блока профиль протащил теги щелчка")
	}
}
