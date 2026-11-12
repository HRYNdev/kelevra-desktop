package zaslon

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"testing"
)

func pref(s ...string) []netip.Prefix {
	var out []netip.Prefix
	for _, x := range s {
		out = append(out, netip.MustParsePrefix(x))
	}
	return out
}

// opis — фильтр одной строкой: «v4 15 разрешить [уд 160.79.104.0/23 лок 172.19.0.0/30]».
func opis(f Filtr) string {
	d := "разрешить"
	if f.Zapret {
		d = "запретить"
	}
	var u []string
	for _, x := range f.Usloviya {
		switch x.Pole {
		case PoleProgramma:
			u = append(u, "прог "+x.Put)
		case PoleUdalennyy:
			u = append(u, "уд "+x.Set.String())
		case PoleLokalnyy:
			u = append(u, "лок "+x.Set.String())
		case PolePetlya:
			u = append(u, "петля")
		case PoleProtokol:
			u = append(u, fmt.Sprintf("прот %d", x.Chislo))
		case PoleLokalnyyPort:
			u = append(u, fmt.Sprintf("лпорт %d", x.Chislo))
		case PoleUdalennyyPort:
			u = append(u, fmt.Sprintf("упорт %d", x.Chislo))
		}
	}
	return fmt.Sprintf("v%d %d %s [%s]", f.Semeystvo, f.Ves, d, strings.Join(u, " "))
}

func opisi(ff []Filtr) []string {
	var out []string
	for _, f := range ff {
		out = append(out, opis(f))
	}
	sort.Strings(out)
	return out
}

func soderzhit(t *testing.T, ff []Filtr, nuzhno ...string) {
	t.Helper()
	est := strings.Join(opisi(ff), "\n")
	for _, n := range nuzhno {
		if !strings.Contains(est, n) {
			t.Errorf("нет фильтра «%s» среди:\n%s", n, est)
		}
	}
}

var (
	setiA   = pref("160.79.104.0/23", "2607:6bc0::/48")
	tunnelA = pref("172.19.0.0/30", "172.19.0.4/30", "fdfe:dcba:9876::/126")
)

// Уровень 1: два фильтра на семейство — разрешение из туннеля выше запрета.
// Ни одного «запрета всего», ни одной программы.
func TestFiltryUroven1(t *testing.T) {
	ff := SobratFiltry(Politika{Anthropic: true, Seti: setiA, Tunnel: tunnelA,
		Programmy: []string{`C:\k\sing-box.exe`}})
	if len(ff) != 4 {
		t.Fatalf("фильтров %d, ждали 4:\n%s", len(ff), strings.Join(opisi(ff), "\n"))
	}
	soderzhit(t, ff,
		"v4 15 разрешить [уд 160.79.104.0/23 лок 172.19.0.0/30 лок 172.19.0.4/30]",
		"v4 14 запретить [уд 160.79.104.0/23]",
		"v6 15 разрешить [уд 2607:6bc0::/48 лок fdfe:dcba:9876::/126]",
		"v6 14 запретить [уд 2607:6bc0::/48]",
	)
	for _, f := range ff {
		for _, u := range f.Usloviya {
			if u.Pole == PoleProgramma {
				t.Errorf("на уровне 1 разрешена программа (ядро к Anthropic напрямую = утечка): %s", opis(f))
			}
		}
	}
	if estZapretVsego(ff) {
		t.Error("на уровне 1 запрет всего")
	}
}

// Без своих адресов туннеля разрешения нет — только запрет (иначе фильтр без
// условия на локальный адрес открыл бы Anthropic всем).
func TestFiltryBezAdresaTunnelya(t *testing.T) {
	ff := SobratFiltry(Politika{Anthropic: true, Seti: setiA, Tunnel: pref("172.19.0.0/30")})
	for _, f := range ff {
		if !f.Zapret && f.Semeystvo == V6 {
			t.Errorf("v6 без адреса туннеля разрешён: %s", opis(f))
		}
		if !f.Zapret {
			lok := false
			for _, u := range f.Usloviya {
				lok = lok || u.Pole == PoleLokalnyy
			}
			if !lok {
				t.Errorf("разрешение Anthropic без условия на адрес туннеля: %s", opis(f))
			}
		}
	}
	soderzhit(t, ff, "v6 14 запретить [уд 2607:6bc0::/48]")
}

// Уровень 2: полный набор. Anthropic запрещён весом выше программ — ядро не
// уйдёт к Anthropic напрямую и при полном запрете.
func TestFiltryPolnyy(t *testing.T) {
	ff := SobratFiltry(Politika{Anthropic: true, Polnyy: true, Seti: setiA, Tunnel: tunnelA,
		Programmy: []string{`C:\k\sing-box.exe`, `C:\P\Kelevra.exe`}})
	soderzhit(t, ff,
		"v4 15 разрешить [уд 160.79.104.0/23 лок 172.19.0.0/30 лок 172.19.0.4/30]",
		"v4 14 запретить [уд 160.79.104.0/23]",
		`v4 13 разрешить [прог C:\k\sing-box.exe]`,
		`v6 13 разрешить [прог C:\P\Kelevra.exe]`,
		"v4 12 разрешить [петля]",
		"v6 12 разрешить [петля]",
		"v4 11 разрешить [лок 172.19.0.0/30 лок 172.19.0.4/30]",
		"v6 11 разрешить [лок fdfe:dcba:9876::/126]",
		"v4 10 разрешить [прот 17 лпорт 68 упорт 67]",
		"v6 10 разрешить [прот 17 лпорт 546 упорт 547]",
		"v6 9 разрешить [прот 58 лпорт 133 лпорт 135 лпорт 136]",
		"v4 8 разрешить [уд 10.0.0.0/8 уд 172.16.0.0/12 уд 192.168.0.0/16 уд 169.254.0.0/16 уд 224.0.0.0/4 уд 255.255.255.255/32 уд 26.0.0.0/8 уд 100.64.0.0/10]",
		"v6 8 разрешить [уд fe80::/10 уд fc00::/7 уд ff00::/8]",
		"v4 0 запретить []",
		"v6 0 запретить []",
	)
	if VesAnthropicZapret <= VesProgramm {
		t.Error("запрет Anthropic не выше разрешения программ")
	}
	if VesAnthropicIzTunnelya <= VesAnthropicZapret {
		t.Error("разрешение из туннеля не выше запрета Anthropic")
	}
	// Ничего лишнего: 4 Anthropic + 4 программы + 2 петли + 2 туннеля + 2 DHCP + NDP + 2 сети + 2 запрета.
	if len(ff) != 19 {
		t.Errorf("фильтров %d, ждали 19:\n%s", len(ff), strings.Join(opisi(ff), "\n"))
	}
}

// Перезапуск при выключенном уровне 1 — полный набор без правил Anthropic.
func TestFiltryPolnyyBezAnthropic(t *testing.T) {
	ff := SobratFiltry(Politika{Polnyy: true, Seti: setiA, Tunnel: tunnelA})
	for _, f := range ff {
		if f.Ves == VesAnthropicZapret || f.Ves == VesAnthropicIzTunnelya {
			t.Errorf("лишний фильтр Anthropic: %s", opis(f))
		}
	}
	if !estZapretVsego(ff) {
		t.Error("нет запрета всего")
	}
}

func TestFiltryPusto(t *testing.T) {
	if ff := SobratFiltry(Politika{Seti: setiA, Tunnel: tunnelA, Programmy: []string{"x"}}); len(ff) != 0 {
		t.Fatalf("пустая политика дала %d фильтров", len(ff))
	}
}

// Все веса укладываются в FWP_UINT8 и условия у фильтров со своим смыслом.
func TestFiltryVesa(t *testing.T) {
	ff := SobratFiltry(Politika{Anthropic: true, Polnyy: true, Seti: setiA, Tunnel: tunnelA, Programmy: []string{"a"}})
	for _, f := range ff {
		if f.Ves > 15 {
			t.Errorf("вес %d больше 15: %s", f.Ves, opis(f))
		}
		if f.Semeystvo != V4 && f.Semeystvo != V6 {
			t.Errorf("нет семейства: %s", opis(f))
		}
		for _, u := range f.Usloviya {
			if (u.Pole == PoleUdalennyy || u.Pole == PoleLokalnyy) && semeystvoSeti(u.Set) != f.Semeystvo {
				t.Errorf("адрес чужого семейства: %s", opis(f))
			}
		}
	}
}
