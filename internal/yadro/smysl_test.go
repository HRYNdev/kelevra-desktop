package yadro

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/tunnel"
	"github.com/HRYNdev/kelevra-desktop/internal/zapasnoy"
)

// bazaKonfiga — конфиг ядра в том виде, какой собирает служба (сокращён до
// разделов, по которым идёт сверка).
func bazaKonfiga() map[string]any {
	var d map[string]any
	if err := json.Unmarshal([]byte(`{
  "log": {"level": "info", "timestamp": true},
  "dns": {"final": "local", "servers": [
    {"type": "fakeip", "tag": "fakeip", "inet4_range": "198.19.128.0/17"},
    {"type": "tcp", "tag": "local", "server": "1.1.1.1"}],
    "rules": [{"server": "fakeip", "rule_set": ["youtube"]}]},
  "inbounds": [
    {"type": "tun", "tag": "tun-in", "interface_name": "tun125", "mtu": 1420, "stack": "mixed",
     "address": ["172.19.0.1/30", "fdfe:dcba:9876::1/126"], "auto_route": true, "strict_route": false,
     "route_exclude_address": ["192.168.0.0/16"]},
    {"type": "mixed", "tag": "` + zapasnoy.TegVhoda + `", "listen": "127.0.0.1", "listen_port": 20125}],
  "outbounds": [
    {"type": "selector", "tag": "Соединение", "outbounds": ["Нидерланды", "Финляндия"], "default": "Нидерланды"},
    {"type": "urltest", "tag": "Нидерланды", "outbounds": ["Нидерланды · прямой"]},
    {"type": "urltest", "tag": "Финляндия", "outbounds": ["Финляндия · прямой"]},
    {"type": "direct", "tag": "direct"},
    {"type": "vless", "tag": "Нидерланды · прямой", "server": "nl.example", "server_port": 443, "uuid": "u1"},
    {"type": "vless", "tag": "Финляндия · прямой", "server": "fi.example", "server_port": 443, "uuid": "u2"}],
  "route": {"final": "direct", "rules": [{"action": "sniff"}, {"outbound": "Соединение", "rule_set": ["youtube"]}],
    "rule_set": [{"tag": "youtube", "type": "remote", "format": "binary", "url": "https://example/youtube.srs", "update_interval": "1d"}]},
  "experimental": {"clash_api": {"external_controller": "127.0.0.1:9090", "secret": "s"},
    "cache_file": {"enabled": true, "path": "cache.db"}}
}`), &d); err != nil {
		panic(err)
	}
	return d
}

func vBayty(t *testing.T, d map[string]any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(d, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func vyhod(d map[string]any, teg string) map[string]any {
	for _, o := range d["outbounds"].([]any) {
		if o.(map[string]any)["tag"] == teg {
			return o.(map[string]any)
		}
	}
	return nil
}

func tun(d map[string]any) map[string]any { return d["inbounds"].([]any)[0].(map[string]any) }

func ubratVyhod(d map[string]any, teg string) {
	var ost []any
	for _, o := range d["outbounds"].([]any) {
		if o.(map[string]any)["tag"] != teg {
			ost = append(ost, o)
		}
	}
	d["outbounds"] = ost
}

func sdvinut(t *testing.T, d map[string]any, imya string, shag int) {
	t.Helper()
	tn := tun(d)
	tn["interface_name"] = imya
	var a []string
	for _, x := range tn["address"].([]any) {
		a = append(a, x.(string))
	}
	stali := tunnel.SdvinutAdresa(a, shag)
	if stali[0] == a[0] {
		t.Fatalf("сдвиг адресов не сработал: %v", stali)
	}
	tn["address"] = []any{stali[0], stali[1]}
}

// Таблица правила: (живой конфиг, новый конфиг) → перезапуск сразу /
// отложить / не нужен. Одно правило на приём ядра при обновлении и на
// ежечасный профиль.
func TestSravnitKonfigiTablica(t *testing.T) {
	vybranoNL := map[string]string{"Соединение": "Нидерланды", "Нидерланды": "Нидерланды · прямой"}
	sluchai := []struct {
		imya   string
		staryy func(d map[string]any)
		novyy  func(d map[string]any)
		vybor  map[string]string
		zhdem  Nuzhda
		slovo  string
	}{
		{imya: "тот же конфиг", zhdem: NeNuzhen},
		{imya: "живое ядро на tun126 со сдвигом адресов, новый — tun125 из профиля",
			staryy: func(d map[string]any) { sdvinut(t, d, "tun126", 1) }, zhdem: NeNuzhen},
		{imya: "живое ядро на tun125, новый собран на tun127",
			novyy: func(d map[string]any) { sdvinut(t, d, "tun127", 2) }, zhdem: NeNuzhen},
		{imya: "наборы правил: путь из кеша вместо адреса",
			novyy: func(d map[string]any) {
				d["route"].(map[string]any)["rule_set"] = []any{map[string]any{"tag": "youtube", "type": "local", "format": "binary", "path": "C:/kesh/youtube.srs"}}
			}, zhdem: NeNuzhen},
		{imya: "набор правил: тот же файл кеша (содержимое ядро перечитает само)",
			staryy: func(d map[string]any) {
				d["route"].(map[string]any)["rule_set"] = []any{map[string]any{"tag": "youtube", "type": "local", "format": "binary", "path": "C:/kesh/youtube.srs"}}
			},
			novyy: func(d map[string]any) {
				d["route"].(map[string]any)["rule_set"] = []any{map[string]any{"tag": "youtube", "type": "local", "format": "binary", "path": `c:\kesh\youtube.srs`}}
			}, zhdem: NeNuzhen},
		{imya: "набор правил: ядро читает встроенный комплект, свежий набор лёг в кеш",
			staryy: func(d map[string]any) {
				d["route"].(map[string]any)["rule_set"] = []any{map[string]any{"tag": "youtube", "type": "local", "format": "binary", "path": "C:/yadro/pravila/youtube.srs"}}
			},
			novyy: func(d map[string]any) {
				d["route"].(map[string]any)["rule_set"] = []any{map[string]any{"tag": "youtube", "type": "local", "format": "binary", "path": "C:/kesh/youtube.srs"}}
			}, zhdem: Otlozhit, slovo: "youtube"},
		{imya: "вход службы на другом порту",
			novyy: func(d map[string]any) { d["inbounds"].([]any)[1].(map[string]any)["listen_port"] = 20131 }, zhdem: NeNuzhen},
		{imya: "журнал ядра другой",
			novyy: func(d map[string]any) { d["log"] = map[string]any{"level": "warn"} }, zhdem: NeNuzhen},
		{imya: "выходы в другом порядке, члены группы с default в другом порядке",
			novyy: func(d map[string]any) {
				o := d["outbounds"].([]any)
				o[0], o[5] = o[5], o[0]
				vyhod(d, "Соединение")["outbounds"] = []any{"Финляндия", "Нидерланды"}
			}, zhdem: NeNuzhen},
		{imya: "селектор без default: первым стал другой узел",
			staryy: func(d map[string]any) { delete(vyhod(d, "Соединение"), "default") },
			novyy: func(d map[string]any) {
				delete(vyhod(d, "Соединение"), "default")
				vyhod(d, "Соединение")["outbounds"] = []any{"Финляндия", "Нидерланды"}
			}, zhdem: Otlozhit, slovo: "~Соединение"},
		{imya: "те же узлы, другие параметры",
			novyy: func(d map[string]any) { vyhod(d, "Финляндия · прямой")["server_port"] = 8443.0 }, vybor: vybranoNL,
			zhdem: Otlozhit, slovo: "~Финляндия · прямой"},
		{imya: "параметры узла, на котором сидим (узел остался)",
			novyy: func(d map[string]any) { vyhod(d, "Нидерланды · прямой")["uuid"] = "u9" }, vybor: vybranoNL,
			zhdem: Otlozhit},
		{imya: "новый узел",
			novyy: func(d map[string]any) {
				d["outbounds"] = append(d["outbounds"].([]any), map[string]any{"type": "vless", "tag": "Германия · прямой", "server": "de.example"})
			}, zhdem: Otlozhit, slovo: "+Германия · прямой"},
		{imya: "убран узел, на котором НЕ сидим",
			novyy: func(d map[string]any) {
				ubratVyhod(d, "Финляндия · прямой")
				ubratVyhod(d, "Финляндия")
				vyhod(d, "Соединение")["outbounds"] = []any{"Нидерланды"}
			}, vybor: vybranoNL, zhdem: Otlozhit, slovo: "-Финляндия"},
		{imya: "убран узел, на котором сидим",
			novyy: func(d map[string]any) {
				ubratVyhod(d, "Нидерланды · прямой")
				ubratVyhod(d, "Нидерланды")
				vyhod(d, "Соединение")["outbounds"] = []any{"Финляндия"}
				vyhod(d, "Соединение")["default"] = "Финляндия"
			}, vybor: vybranoNL, zhdem: Srazu, slovo: "из профиля пропал"},
		{imya: "узел, на котором сидим, вынут из группы, но выход остался",
			novyy: func(d map[string]any) { vyhod(d, "Соединение")["outbounds"] = []any{"Финляндия"} },
			vybor: vybranoNL, zhdem: Srazu, slovo: "«Нидерланды»"},
		{imya: "убран узел, на котором сидим, но выбор неизвестен (ядро не ответило)",
			novyy: func(d map[string]any) {
				ubratVyhod(d, "Нидерланды · прямой")
				ubratVyhod(d, "Нидерланды")
				vyhod(d, "Соединение")["outbounds"] = []any{"Финляндия"}
			}, zhdem: Otlozhit},
		{imya: "вход tun: MTU", novyy: func(d map[string]any) { tun(d)["mtu"] = 1280.0 }, zhdem: Otlozhit, slovo: "~tun-in"},
		{imya: "вход tun: стек", novyy: func(d map[string]any) { tun(d)["stack"] = "gvisor" }, zhdem: Otlozhit},
		{imya: "вход tun: маршруты", novyy: func(d map[string]any) { tun(d)["route_exclude_address"] = []any{"10.0.0.0/8"} }, zhdem: Otlozhit},
		{imya: "вход tun: адрес сменён не сдвигом (имя то же)",
			novyy: func(d map[string]any) { tun(d)["address"] = []any{"172.20.0.1/30", "fdfe:dcba:9876::1/126"} }, zhdem: Otlozhit},
		{imya: "DNS-сервер", novyy: func(d map[string]any) {
			d["dns"].(map[string]any)["servers"].([]any)[1].(map[string]any)["server"] = "8.8.8.8"
		}, zhdem: Otlozhit, slovo: "DNS"},
		{imya: "правила маршрутизации", novyy: func(d map[string]any) { d["route"].(map[string]any)["final"] = "Соединение" },
			zhdem: Otlozhit, slovo: "правила маршрутизации"},
		{imya: "кеш ядра", novyy: func(d map[string]any) {
			d["experimental"].(map[string]any)["cache_file"] = map[string]any{"enabled": false}
		}, zhdem: Otlozhit},
		{imya: "вход домой появился", novyy: func(d map[string]any) {
			d["endpoints"] = []any{map[string]any{"type": "wireguard", "tag": "Дом", "detour": "Соединение"}}
		}, zhdem: Srazu, slovo: "вход домой"},
		{imya: "служебный порт ядра", novyy: func(d map[string]any) {
			d["experimental"].(map[string]any)["clash_api"] = map[string]any{"external_controller": "127.0.0.1:9091", "secret": "s"}
		}, zhdem: Srazu, slovo: "clash_api"},
	}
	for _, sl := range sluchai {
		t.Run(sl.imya, func(t *testing.T) {
			a, b := bazaKonfiga(), bazaKonfiga()
			if sl.staryy != nil {
				sl.staryy(a)
			}
			if sl.novyy != nil {
				sl.novyy(b)
			}
			r := SravnitKonfigi(vBayty(t, a), vBayty(t, b), sl.vybor)
			if r.Nuzhda != sl.zhdem {
				t.Fatalf("перезапуск %q, ждали %q (%s)", r.Nuzhda, sl.zhdem, r.Slovami())
			}
			if sl.slovo != "" && !strings.Contains(r.Slovami(), sl.slovo) {
				t.Fatalf("в причинах нет %q: %s", sl.slovo, r.Slovami())
			}
		})
	}
}

// Сломанный конфиг — не «не нужен» и не «сразу»: отложить (судить нечем).
func TestSravnitKonfigiNeRazobrat(t *testing.T) {
	if r := SravnitKonfigi([]byte("{"), vBayty(t, bazaKonfiga()), nil); r.Nuzhda != Otlozhit {
		t.Fatalf("живой не разобрать: %v", r.Nuzhda)
	}
	if r := SravnitKonfigi(vBayty(t, bazaKonfiga()), []byte("мусор"), nil); r.Nuzhda != Otlozhit {
		t.Fatalf("новый не разобрать: %v", r.Nuzhda)
	}
}

// Ядро без снимка (поднято прежней версией): совпал отпечаток — не нужен,
// иначе — отложить, а не менять сразу, как было до 07.11.
func TestSravnitPoOtpechatku(t *testing.T) {
	b := vBayty(t, bazaKonfiga())
	if r := SravnitPoOtpechatku(OtpechatokKonfigaBayt(b), b); r.Nuzhda != NeNuzhen {
		t.Fatalf("тот же отпечаток: %v", r.Nuzhda)
	}
	if r := SravnitPoOtpechatku("", b); r.Nuzhda != NeNuzhen {
		t.Fatalf("отпечатка нет: %v", r.Nuzhda)
	}
	if r := SravnitPoOtpechatku("0123456789abcdef", b); r.Nuzhda != Otlozhit {
		t.Fatalf("другой отпечаток: %v", r.Nuzhda)
	}
}

// Сверка не портит переданные байты и группы находит по конфигу.
func TestGruppyKonfiga(t *testing.T) {
	g := GruppyKonfiga(vBayty(t, bazaKonfiga()))
	if strings.Join(g, ",") != "Соединение,Нидерланды,Финляндия" {
		t.Fatalf("группы %v", g)
	}
}
