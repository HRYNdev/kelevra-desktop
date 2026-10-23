package konfig

// Сторожа разведения нашего fakeip с fakeip домашнего роутера (dns.go,
// DiapazonFakeipKlienta). Разбор 23.10: ноутбук дома с поднятым туннелем
// получал подменные адреса роутера (198.18.5.170, 198.18.2.78…), а ядро,
// видя адрес из СВОЕГО диапазона без записи в своей карте, отказывало
// «router: missing fakeip record» раньше всех правил маршрутизации — 2777 раз
// за двое суток.

import (
	"encoding/json"
	"net"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Адреса, которые домашний роутер реально выдавал и на которые ломились
// программы ноутбука (журнал ядра, 21–22.10).
var adresaRoutera = []string{"198.18.5.170", "198.18.6.29", "198.18.2.78", "198.18.19.47", "198.18.0.159"}

func diapazonFakeip(t *testing.T, d map[string]any) string {
	t.Helper()
	sv := serveryDns(t, d)["fakeip"]
	if sv == nil {
		t.Fatal("в готовом конфиге нет сервера fakeip")
	}
	r, _ := sv["inet4_range"].(string)
	return r
}

// Боевой профиль присылает 198.18.0.0/15 — ровно то, что раздаёт роутер.
// Готовый конфиг обязан держать свой fakeip в DiapazonFakeipKlienta, а
// Kartina — сказать, что было в профиле (строка журнала «конфиг собран»).
func TestFakeipUhoditOtZonyRoutera(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	d := razobrat(t, gotovyy)
	if r := diapazonFakeip(t, d); r != DiapazonFakeipKlienta {
		t.Fatalf("inet4_range = %q, ждали %q: адреса роутера останутся «нашими без записи»", r, DiapazonFakeipKlienta)
	}
	if k.FakeipBylo != "198.18.0.0/15" {
		t.Fatalf("Kartina.FakeipBylo = %q, ждали диапазон профиля 198.18.0.0/15", k.FakeipBylo)
	}
	// IPv6 не трогаем: подменных AAAA домашний роутер не раздаёт.
	if r6, _ := serveryDns(t, d)["fakeip"]["inet6_range"].(string); r6 != "fc00::/18" {
		t.Fatalf("inet6_range = %q, ждали нетронутый fc00::/18", r6)
	}
}

// Сам выбранный диапазон: внутри 198.18.0.0/15 (на этом держатся авторежим
// и «слепота» зондов) и не задевает ни одного адреса, который роутер выдавал.
func TestDiapazonKlientaNePeresekaetsyaSRouterom(t *testing.T) {
	nash := netip.MustParsePrefix(DiapazonFakeipKlienta)
	if !netip.MustParsePrefix("198.18.0.0/15").Contains(nash.Addr()) || nash.Bits() < 15 {
		t.Fatalf("%s вышел за 198.18.0.0/15 — авторежим перестанет узнавать в нём подмену", nash)
	}
	if nash.Overlaps(zonaRoutera) {
		t.Fatalf("%s пересекается с зоной роутера %s", nash, zonaRoutera)
	}
	for _, a := range adresaRoutera {
		if nash.Contains(netip.MustParseAddr(a)) {
			t.Fatalf("адрес роутера %s попадает в наш диапазон %s", a, nash)
		}
	}
}

// Сервер вправе сам прислать другой диапазон (механизм обкатки
// PROBA_FAKEIP_INET4 на сервере). Не пересекается с зоной роутера — не
// перебиваем; повторная сборка уже разведённого конфига тоже ничего не меняет.
func TestFakeipNeZadevaetDiapazonBezPeresecheniya(t *testing.T) {
	for _, r := range []string{"240.0.0.0/12", DiapazonFakeipKlienta} {
		d := razobrat(t, profil(t))
		for _, sv := range d["dns"].(map[string]any)["servers"].([]any) {
			m := sv.(map[string]any)
			if m["type"] == "fakeip" {
				m["inet4_range"] = r
			}
		}
		syroy, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		gotovyy, k, err := Prigotovit(syroy, Vybor{})
		if err != nil {
			t.Fatal(err)
		}
		if got := diapazonFakeip(t, razobrat(t, gotovyy)); got != r {
			t.Fatalf("профиль прислал %s, в готовом %s — чужой выбор перебит", r, got)
		}
		if k.FakeipBylo != "" {
			t.Fatalf("FakeipBylo = %q при нетронутом диапазоне %s", k.FakeipBylo, r)
		}
	}
}

// Упрощённый режим (BezSetevyhPravil) выбрасывает fakeip целиком — переносить
// нечего, и журнал не должен врать про перенос.
func TestFakeipBezServeraNechegoPerenosit(t *testing.T) {
	_, k, err := Prigotovit(profil(t), Vybor{BezSetevyhPravil: true})
	if err != nil {
		t.Fatal(err)
	}
	if k.FakeipBylo != "" {
		t.Fatalf("FakeipBylo = %q, а сервера fakeip в конфиге нет", k.FakeipBylo)
	}
}

// Живой щуп на настоящем ядре (пропускается без бинаря, см.
// nastoyashchee_yadro_test.go). Конфиг с fakeip и правилом «198.18.0.0/15 —
// в выход-ловушку»: ловушка — http-прокси щупа на 127.0.0.1, наружу щуп не
// ходит. Через socks стучимся на адрес роутера 198.18.5.170:443 и смотрим,
// ЧТО решило ядро.
//
//   - готовый конфиг (наш fakeip в DiapazonFakeipKlienta): адрес роутера
//     обязан пройти проверку fakeip и дойти до правил маршрутизации — правило
//     по IP уводит его в ловушку («CONNECT 198.18.5.170:443»), в журнале нет
//     «missing fakeip record». Дома на месте ловушки — direct к роутеру,
//     который этот адрес и выдал;
//   - контроль порчей: тот же конфиг, но диапазон возвращён на 198.18.0.0/15,
//     обязан дать «missing fakeip record» и пустую ловушку. Без этого зелень
//     первого вердикта ничего бы не значила.
func TestZhivoeYadroPropuskaetAdresRoutera(t *testing.T) {
	bin := putYadra(t)
	if bin == "" {
		t.Skipf("настоящего ядра рядом нет — щуп пропущен. Искал: %s", mestaPoiskaYadra())
	}
	for _, sluchay := range []struct {
		imya       string
		diapazon   string // "" — как собрал Prigotovit
		zhdemOtkaz bool
	}{
		{"готовый конфиг", "", false},
		{"контроль: общий диапазон с роутером", "198.18.0.0/15", true},
	} {
		t.Run(sluchay.imya, func(t *testing.T) {
			dom := t.TempDir()
			mixedPort := svobodnyPort(t)
			lovushka, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer lovushka.Close()
			poymano := make(chan string, 1)
			go func() {
				c, err := lovushka.Accept()
				if err != nil {
					return
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(3 * time.Second))
				buf := make([]byte, 256)
				n, _ := c.Read(buf)
				poymano <- string(buf[:n])
			}()

			gotovyy, _, err := Prigotovit(profilSFakeip(t, dom, lovushka.Addr().(*net.TCPAddr).Port), Vybor{})
			if err != nil {
				t.Fatalf("Prigotovit: %v", err)
			}
			gotovyy = vstavitVhodShchupa(t, gotovyy, mixedPort)
			if sluchay.diapazon != "" {
				d := razobrat(t, gotovyy)
				serveryDns(t, d)["fakeip"]["inet4_range"] = sluchay.diapazon
				if gotovyy, err = json.Marshal(d); err != nil {
					t.Fatal(err)
				}
			}
			putKonfig := filepath.Join(dom, "config.json")
			if err := writeFile(t, putKonfig, gotovyy); err != nil {
				t.Fatal(err)
			}
			cmd, log := zapustitYadro(t, bin, putKonfig, mixedPort)
			defer ostanovitYadro(cmd)

			c, _, err := socks5Connect(t, "127.0.0.1:"+strconv.Itoa(mixedPort), "198.18.5.170", 443)
			if err == nil {
				defer c.Close()
			}
			var vLovushke string
			select {
			case vLovushke = <-poymano:
			case <-time.After(2 * time.Second):
			}
			// Ядро пишет решение в журнал асинхронно — даём ему дописать.
			time.Sleep(300 * time.Millisecond)
			zhurnal := log.String()
			if !strings.Contains(zhurnal, "198.18.5.170") {
				t.Fatalf("соединения к 198.18.5.170 в журнале ядра нет — щуп ничего не проверил\n%s", zhurnal)
			}
			for _, s := range strings.Split(zhurnal, "\n") {
				if strings.Contains(s, "198.18.5.170") || strings.Contains(s, "fakeip") || strings.Contains(s, "lovushka") {
					t.Log(s)
				}
			}
			t.Logf("ловушка получила: %q", strings.SplitN(vLovushke, "\r\n", 2)[0])

			otkaz := strings.Contains(zhurnal, "missing fakeip record")
			if otkaz != sluchay.zhdemOtkaz {
				t.Fatalf("«missing fakeip record» в журнале: %v, ждали %v\n%s", otkaz, sluchay.zhdemOtkaz, zhurnal)
			}
			doshlo := strings.Contains(vLovushke, "198.18.5.170:443")
			if doshlo == sluchay.zhdemOtkaz {
				t.Fatalf("до правила по IP дошло: %v, ждали %v (ловушка: %q)\n%s", doshlo, !sluchay.zhdemOtkaz, vLovushke, zhurnal)
			}
		})
	}
}

// profilSFakeip — минимальный профиль с живой связкой fakeip (сервер, правило
// dns.rules со ссылкой на него) и правилом, уводящим весь 198.18.0.0/15 в
// выход-ловушку (http-прокси щупа на 127.0.0.1:portLovushki): адрес роутера,
// прошедший проверку fakeip, придёт туда, а не в сеть.
func profilSFakeip(t *testing.T, dom string, portLovushki int) []byte {
	t.Helper()
	d := razobrat(t, profilDlyaZhivogo(0, spisokZablokirovannogo(t, dom)))
	d["dns"] = map[string]any{
		"servers": []any{
			map[string]any{"type": "fakeip", "tag": "fakeip", "inet4_range": "198.18.0.0/15"},
			map[string]any{"type": "local", "tag": "local"},
		},
		"rules": []any{map[string]any{"rule_set": []any{"zablokirovannoe"}, "server": "fakeip"}},
		"final": "local",
	}
	d["outbounds"] = append(d["outbounds"].([]any), map[string]any{
		"type": "http", "tag": "lovushka", "server": "127.0.0.1", "server_port": portLovushki,
	})
	r := d["route"].(map[string]any)
	// Ядро 1.14 без резолвера выхода не стартует вовсе (как и в боевом профиле).
	r["default_domain_resolver"] = "local"
	r["rules"] = append([]any{map[string]any{"ip_cidr": []any{"198.18.0.0/15"}, "outbound": "lovushka"}}, r["rules"].([]any)...)
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
