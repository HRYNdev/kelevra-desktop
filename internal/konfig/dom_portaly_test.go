package konfig

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Мост «Дом» идёт через входы VPS с порталом (блок dom, поле portaly), а не
// через выбор человека для интернета («Соединение»): при узле или входе без
// портала дома иначе не видно (живьём 26.10: Нидерланды без портала).

const (
	tegNL      = "Нидерланды · прямой" // vless primer.example:443 из profil_telefona.json
	tegFiX     = "Финляндия · XHTTP"
	tegFiV     = "Финляндия · Vision"
	tegFiHy    = "Финляндия · Hysteria2"
	tegKomnata = "Комната" // socks primer.example:443
)

// profilSPortalami — профиль телефона плюс узел fi-1 тремя входами: XHTTP
// (2053, портал), Vision (443) и Hysteria2 на том же 2053 (не VLESS — мост
// VLESS reverse на нём не живёт). chuzhoyMost — профиль сам принёс выход с
// тегом группы моста и правило в него.
func profilSPortalami(t *testing.T, chuzhoyMost bool) []byte {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal(profilTelefona(t), &d); err != nil {
		t.Fatal(err)
	}
	reality := func() map[string]any {
		return map[string]any{"enabled": true, "server_name": "fi.example.org",
			"utls":    map[string]any{"enabled": true, "fingerprint": "chrome"},
			"reality": map[string]any{"enabled": true, "public_key": "Q2KoN7mnqRpTxpBFBEKb7qQi3yJ9b6NLEOBdy8Thdng", "short_id": "ab"}}
	}
	vyhody := d["outbounds"].([]any)
	vyhody = append(vyhody,
		map[string]any{"type": "vless", "tag": tegFiX, "server": "fi.example.org", "server_port": 2053,
			"uuid": "11111111-1111-1111-1111-111111111111", "tls": reality()},
		map[string]any{"type": "vless", "tag": tegFiV, "server": "fi.example.org", "server_port": 443,
			"uuid": "11111111-1111-1111-1111-111111111111", "flow": "xtls-rprx-vision", "tls": reality()},
		map[string]any{"type": "hysteria2", "tag": tegFiHy, "server": "fi.example.org", "server_port": 2053,
			"password": "p", "tls": map[string]any{"enabled": true, "server_name": "fi.example.org"}},
	)
	if chuzhoyMost {
		vyhody = append(vyhody, map[string]any{"type": "direct", "tag": TegMostaDoma})
		r := d["route"].(map[string]any)
		r["rules"] = append([]any{map[string]any{"ip_cidr": []any{"203.0.113.0/24"}, "outbound": TegMostaDoma}}, r["rules"].([]any)...)
	}
	d["outbounds"] = vyhody
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func domSPortalami(p ...PortalDoma) *Dom {
	dm := probnyyDom()
	dm.Portaly = p
	return dm
}

func endpointDoma(t *testing.T, d map[string]any) map[string]any {
	t.Helper()
	eps, _ := d["endpoints"].([]any)
	if len(eps) != 1 {
		t.Fatalf("эндпоинтов %d, ждали 1", len(eps))
	}
	return eps[0].(map[string]any)
}

func vyhodPoTegu(d map[string]any, teg string) map[string]any {
	for _, v := range d["outbounds"].([]any) {
		if vh := v.(map[string]any); vh["tag"] == teg {
			return vh
		}
	}
	return nil
}

func TestMostDomaOdinPortal(t *testing.T) {
	d, k := sobrat(t, profilSPortalami(t, false), Vybor{Dom: domSPortalami(PortalDoma{Uzel: "fi-1", Server: "fi.example.org", Port: 2053})})
	if !k.Dom || k.DomBeda != "" {
		t.Fatalf("дом не вписан: %+v", k)
	}
	if got := endpointDoma(t, d)["detour"]; got != tegFiX {
		t.Fatalf("detour = %v, ждали %q (Hysteria2 на том же порту и Vision — не портал)", got, tegFiX)
	}
	if vyhodPoTegu(d, TegMostaDoma) != nil {
		t.Fatal("при одном портале группа моста не нужна")
	}
	if !strings.Contains(k.DomMost, tegFiX) {
		t.Fatalf("в журнал не попало, куда идёт мост: %q", k.DomMost)
	}
}

func TestMostDomaDvaPortala(t *testing.T) {
	d, k := sobrat(t, profilSPortalami(t, false), Vybor{Dom: domSPortalami(
		PortalDoma{Uzel: "fi-1", Server: "FI.example.org.", Port: 2053},
		PortalDoma{Uzel: "vu-1", Server: "primer.example", Port: 443},
		PortalDoma{Uzel: "fi-1", Server: "fi.example.org", Port: 2053}, // повтор — без дубля
	)})
	if !k.Dom || k.DomBeda != "" {
		t.Fatalf("дом не вписан: %+v", k)
	}
	if got := endpointDoma(t, d)["detour"]; got != TegMostaDoma {
		t.Fatalf("detour = %v, ждали группу %q", got, TegMostaDoma)
	}
	g := vyhodPoTegu(d, TegMostaDoma)
	if g == nil {
		t.Fatal("группы моста нет")
	}
	if g["type"] != "urltest" || g["interrupt_exist_connections"] != true {
		t.Fatalf("группа моста: %v", g)
	}
	// Socks «Комната» на том же primer.example:443 — не VLESS, в группу не идёт.
	if !reflect.DeepEqual(g["outbounds"], []any{tegFiX, tegNL}) {
		t.Fatalf("в группе моста %v, ждали [%s %s]", g["outbounds"], tegFiX, tegNL)
	}
	// Выбор человека для интернета не тронут: «Соединение» без группы моста.
	sel := vyhodPoTegu(d, "Соединение")
	for _, x := range sel["outbounds"].([]any) {
		if x == TegMostaDoma {
			t.Fatal("группа моста попала в выбор человека")
		}
	}
	// Правила маршрута в группу моста не ведут: её трафик — только мост «Дом».
	for _, p := range d["route"].(map[string]any)["rules"].([]any) {
		if p.(map[string]any)["outbound"] == TegMostaDoma {
			t.Fatalf("правило в группу моста: %v", p)
		}
	}
	// Белые списки на месте: один эндпоинт, reject своей подсети сразу за правилом дома.
	pravila := d["route"].(map[string]any)["rules"].([]any)
	for i, p := range pravila {
		if p.(map[string]any)["outbound"] == TegDoma {
			sled := pravila[i+1].(map[string]any)
			if sled["action"] != "reject" || !reflect.DeepEqual(sled["ip_cidr"], []any{SvoyaSetDoma}) {
				t.Fatalf("за правилом дома не reject своей подсети: %v", sled)
			}
		}
	}
}

func TestMostDomaBezPortalovVProfile(t *testing.T) {
	for imya, dm := range map[string]*Dom{
		"блок без поля (старый мозг)": probnyyDom(),
		"портал не в профиле":         domSPortalami(PortalDoma{Uzel: "xx-1", Server: "net.example", Port: 1}),
	} {
		d, k := sobrat(t, profilSPortalami(t, false), Vybor{Dom: dm})
		if !k.Dom {
			t.Fatalf("%s: дом не вписан: %+v", imya, k)
		}
		if got := endpointDoma(t, d)["detour"]; got != "Соединение" {
			t.Fatalf("%s: detour = %v, ждали «Соединение» (как раньше)", imya, got)
		}
		if vyhodPoTegu(d, TegMostaDoma) != nil {
			t.Fatalf("%s: группа моста без порталов", imya)
		}
	}
	_, k := sobrat(t, profilSPortalami(t, false), Vybor{Dom: domSPortalami(PortalDoma{Uzel: "xx-1", Server: "net.example", Port: 1})})
	if !strings.Contains(k.DomMost, "входов с порталом в профиле нет") {
		t.Fatalf("журнал молчит, что порталов в профиле нет: %q", k.DomMost)
	}
}

// Профиль не вправе принести свою «группу моста» (п.15): выход с этим тегом
// и правила в него выбрасываются, своя группа вписывается.
func TestMostDomaChuzhayaGruppaVyrezaetsya(t *testing.T) {
	d, k := sobrat(t, profilSPortalami(t, true), Vybor{Dom: domSPortalami(
		PortalDoma{Uzel: "fi-1", Server: "fi.example.org", Port: 2053},
		PortalDoma{Uzel: "vu-1", Server: "primer.example", Port: 443})})
	if !k.Dom || k.DomBeda != "" {
		t.Fatalf("дом не вписан: %+v", k)
	}
	g := vyhodPoTegu(d, TegMostaDoma)
	if g == nil || g["type"] != "urltest" {
		t.Fatalf("группа моста не своя: %v", g)
	}
	for _, p := range d["route"].(map[string]any)["rules"].([]any) {
		if p.(map[string]any)["outbound"] == TegMostaDoma {
			t.Fatalf("правило профиля в группу моста выжило: %v", p)
		}
	}
	// Без блока чужая группа тоже не выживает.
	d, _ = sobrat(t, profilSPortalami(t, true), Vybor{})
	if vyhodPoTegu(d, TegMostaDoma) != nil {
		t.Fatal("профиль протащил выход с тегом группы моста")
	}
}

// Служебный вход и упрощённый режим ведут в селектор, а не в группу моста.
func TestMostDomaNeTunnelnyyVyhod(t *testing.T) {
	d := map[string]any{"outbounds": []any{
		map[string]any{"type": "urltest", "tag": TegMostaDoma, "outbounds": []any{"a", "b"}},
		map[string]any{"type": "vless", "tag": "a"},
	}}
	if got := tunnelnyyVyhod(d); got != "a" {
		t.Fatalf("туннельный выход %q, ждали «a»", got)
	}
}

func TestYadroPrinimaetMostDoma(t *testing.T) {
	bin := putYadra(t)
	if bin == "" {
		t.Skipf("ядра нет: искал в %s", mestaPoiskaYadra())
	}
	gotovyy, k, err := Prigotovit(profilSPortalami(t, false), Vybor{Dom: domSPortalami(
		PortalDoma{Uzel: "fi-1", Server: "fi.example.org", Port: 2053},
		PortalDoma{Uzel: "vu-1", Server: "primer.example", Port: 443}), ClashSekret: "s"})
	if err != nil || !k.Dom {
		t.Fatalf("%v %+v", err, k)
	}
	put := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(put, gotovyy, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "check", "-c", put)
	cmd.Dir = t.TempDir()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ядро отвергло конфиг с группой моста: %v\n%s", err, out)
	}
}
