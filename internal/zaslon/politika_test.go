package zaslon

import (
	"net/netip"
	"strings"
	"testing"
)

// Таблица выбора правил: намерение × дома × строгий × перезапуск × пульт.
func TestVybratTablica(t *testing.T) {
	type itog struct{ anthropic, polnyy bool }
	sluchai := []struct {
		imya   string
		v      Vhod
		nuzhno itog
	}{
		{"отключено", Vhod{}, itog{}},
		{"отключено, строгий", Vhod{Strogiy: true}, itog{}},
		{"подключено", Vhod{Namerenie: true}, itog{anthropic: true}},
		{"подключено, дома", Vhod{Namerenie: true, Doma: true}, itog{}},
		{"подключено, дома, строгий", Vhod{Namerenie: true, Doma: true, Strogiy: true}, itog{}},
		{"подключено, строгий", Vhod{Namerenie: true, Strogiy: true}, itog{anthropic: true, polnyy: true}},
		{"подключено, уровень 1 выключен пультом", Vhod{Namerenie: true, BezAnthropic: true}, itog{}},
		{"строгий, уровень 1 выключен пультом", Vhod{Namerenie: true, Strogiy: true, BezAnthropic: true}, itog{polnyy: true}},
		{"перезапуск", Vhod{Perezapusk: true}, itog{anthropic: true, polnyy: true}},
		{"перезапуск, подключено", Vhod{Namerenie: true, Perezapusk: true}, itog{anthropic: true, polnyy: true}},
		{"перезапуск, уровень 1 выключен", Vhod{Namerenie: true, Perezapusk: true, BezAnthropic: true}, itog{polnyy: true}},
		{"перезапуск, строгий", Vhod{Namerenie: true, Strogiy: true, Perezapusk: true}, itog{anthropic: true, polnyy: true}},
	}
	for _, s := range sluchai {
		r := Vybrat(s.v)
		if r.Anthropic != s.nuzhno.anthropic || r.Polnyy != s.nuzhno.polnyy {
			t.Errorf("%s: %s, ждали anthropic=%v polnyy=%v", s.imya, r, s.nuzhno.anthropic, s.nuzhno.polnyy)
		}
		if r.Pochemu == "" {
			t.Errorf("%s: нет причины для журнала", s.imya)
		}
	}
}

func TestRazobratSeti(t *testing.T) {
	seti, negodnye := RazobratSeti([]string{" 160.79.104.1/23 ", "2607:6bc0::/48", "1.1.1.1", "не адрес", "160.79.104.0/23"})
	nuzhno := []string{"160.79.104.0/23", "2607:6bc0::/48", "1.1.1.1/32"}
	if len(seti) != len(nuzhno) {
		t.Fatalf("разобрано %v, ждали %v", seti, nuzhno)
	}
	for i := range nuzhno {
		if seti[i].String() != nuzhno[i] {
			t.Errorf("[%d] %s, ждали %s", i, seti[i], nuzhno[i])
		}
	}
	if len(negodnye) != 1 || negodnye[0] != "не адрес" {
		t.Errorf("негодные %v", negodnye)
	}
}

// Свои адреса туннеля с запасом на сдвиг tun125 → tun126…: сдвиг тот же,
// что у адаптера (здесь — простой сдвиг на блок /30).
func TestSetiTunnelyaSoSdvigom(t *testing.T) {
	sdvig := func(adresa []string, shag int) []string {
		var out []string
		for _, a := range adresa {
			p := netip.MustParsePrefix(a)
			if !p.Addr().Is4() {
				out = append(out, a)
				continue
			}
			b := p.Addr().As4()
			b[3] += byte(4 * shag)
			out = append(out, netip.PrefixFrom(netip.AddrFrom4(b), p.Bits()).String())
		}
		return out
	}
	seti := SetiTunnelya([]string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"}, 2, sdvig)
	var stroki []string
	for _, p := range seti {
		stroki = append(stroki, p.String())
	}
	tekst := strings.Join(stroki, " ")
	for _, nuzhno := range []string{"172.19.0.0/30", "172.19.0.4/30", "172.19.0.8/30", "fdfe:dcba:9876::/126"} {
		if !strings.Contains(tekst, nuzhno) {
			t.Errorf("нет %s в %s", nuzhno, tekst)
		}
	}
	if len(seti) != 4 {
		t.Errorf("повторы не убраны: %s", tekst)
	}
	if len(SetiTunnelya(nil, 8, sdvig)) != 0 {
		t.Error("без адресов профиля — пусто")
	}
}

func TestPolitikaRavna(t *testing.T) {
	a := Politika{Anthropic: true, Seti: []netip.Prefix{netip.MustParsePrefix("1.0.0.0/8")}}
	b := a
	b.Seti = []netip.Prefix{netip.MustParsePrefix("1.0.0.0/8")}
	if !a.Ravna(b) {
		t.Error("одинаковые политики не равны")
	}
	b.Tunnel = []netip.Prefix{netip.MustParsePrefix("172.19.0.0/30")}
	if a.Ravna(b) {
		t.Error("сменились адреса туннеля — политика та же")
	}
	if !(Politika{Seti: a.Seti}).Ravna(Politika{}) {
		t.Error("пустые политики с разными данными не равны")
	}
}
