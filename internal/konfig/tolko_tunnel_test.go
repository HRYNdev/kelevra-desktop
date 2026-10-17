package konfig

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Неполного режима нет (решение владельца 17.10: «нет никаких неполных
// режимов и не рабочих»). Сборка конфига обязана всегда давать туннель и
// никогда — системный прокси: ни входа mixed, ни set_system_proxy, ни
// заметки «только браузеры».
func TestKonfigVsegdaTunnel(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	if k.Rezhim != Tunnel || !k.EstTunnel {
		t.Fatalf("режим %q (туннель в профиле %v) — ждали туннель", k.Rezhim, k.EstTunnel)
	}
	if k.Zametka != ZametkaVes {
		t.Fatalf("заметка %q, ждали %q", k.Zametka, ZametkaVes)
	}
	estTun := false
	for _, vh := range vhody(razobrat(t, gotovyy)) {
		switch vh["type"] {
		case "tun":
			estTun = true
		case "mixed", "http", "socks":
			t.Fatalf("в конфиге остался вход %v — локальный прокси на компьютере не нужен никогда", vh["type"])
		}
	}
	if !estTun {
		t.Fatal("в конфиге нет туннеля")
	}
	if strings.Contains(string(gotovyy), "set_system_proxy") {
		t.Fatal("в конфиге есть set_system_proxy — системный прокси Kelevra больше не ставит")
	}
}

// Каждая подстраховка (правила с диска, упрощённый режим, другое имя адаптера)
// тоже остаётся туннелем.
func TestPodstrahovkiNeUvodyatVProksi(t *testing.T) {
	sluchai := map[string]Vybor{
		"без сетевых правил":  {BezSetevyhPravil: true},
		"другое имя адаптера": {TunImya: "tun127", TunSdvigAdresa: 2},
		"обычный":             {},
	}
	for imya, v := range sluchai {
		gotovyy, k, err := Prigotovit(profil(t), v)
		if err != nil {
			t.Fatalf("%s: %v", imya, err)
		}
		if k.Rezhim != Tunnel {
			t.Fatalf("%s: режим %q", imya, k.Rezhim)
		}
		if strings.Contains(string(gotovyy), "set_system_proxy") {
			t.Fatalf("%s: в конфиге set_system_proxy", imya)
		}
		for _, vh := range vhody(razobrat(t, gotovyy)) {
			if vh["type"] != "tun" {
				t.Fatalf("%s: в конфиге вход %v", imya, vh["type"])
			}
		}
	}
}

// Профиль без туннеля — не повод поднять «хотя бы прокси»: подключаться
// нечем, и это ошибка, а не ступень ниже.
func TestBezTunnelyaVProfileOshibka(t *testing.T) {
	var d map[string]any
	if err := json.Unmarshal(profil(t), &d); err != nil {
		t.Fatal(err)
	}
	var ostalis []any
	for _, v := range d["inbounds"].([]any) {
		if vh, _ := v.(map[string]any); vh["type"] != "tun" {
			ostalis = append(ostalis, v)
		}
	}
	d["inbounds"] = ostalis
	syroy, _ := json.Marshal(d)
	gotovyy, _, err := Prigotovit(syroy, Vybor{})
	if !errors.Is(err, OshibkaBezTunnelya) {
		t.Fatalf("ждали OshibkaBezTunnelya, получили %v", err)
	}
	if gotovyy != nil {
		t.Fatal("конфиг без туннеля всё-таки собран")
	}
}
