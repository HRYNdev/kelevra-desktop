package konfig

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// profilSvoiImena — профиль телефона (testdata) с hosts-сервером и
// выходами узлов на любой вкус; hosts — nil, если hosts-сервера нет.
func profilSvoiImena(t *testing.T, hosts []any, vyhody ...map[string]any) []byte {
	t.Helper()
	d := razobrat(t, profil(t))
	dn := d["dns"].(map[string]any)
	dn["servers"] = append(dn["servers"].([]any), hosts...)
	for _, v := range vyhody {
		d["outbounds"] = append(d["outbounds"].([]any), v)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func svoiImena(pred map[string]any) map[string]any {
	return map[string]any{"type": "hosts", "tag": TegSvoihImen, "predefined": pred}
}

// vyhodyPoTegu — выходы готового конфига по тегу.
func vyhodyPoTegu(t *testing.T, gotovyy []byte) map[string]map[string]any {
	t.Helper()
	itog := map[string]map[string]any{}
	for _, v := range razobrat(t, gotovyy)["outbounds"].([]any) {
		m := v.(map[string]any)
		itog[m["tag"].(string)] = m
	}
	return itog
}

var vyhodyUzlov = []map[string]any{
	{"type": "vless", "tag": "Финляндия · XHTTP", "server": "node2kv.chickenkiller.com", "server_port": 2053, "uuid": "00000000-0000-0000-0000-000000000001"},
	{"type": "hysteria2", "tag": "Финляндия · Hysteria2", "server": "Node2KV.chickenkiller.com.", "server_port": 8443, "password": "p", "tls": map[string]any{"enabled": true, "server_name": "node2kv.chickenkiller.com"}},
	{"type": "vless", "tag": "Чужое имя", "server": "drugoy.example", "server_port": 443, "uuid": "00000000-0000-0000-0000-000000000002"},
	{"type": "vless", "tag": "По адресу", "server": "203.0.113.9", "server_port": 443, "uuid": "00000000-0000-0000-0000-000000000003"},
	{"type": "vless", "tag": "Свой резолвер", "server": "node2kv.chickenkiller.com", "server_port": 443, "uuid": "00000000-0000-0000-0000-000000000004", "domain_resolver": "local"},
	{"type": "vless", "tag": "Через посредника", "server": "node2kv.chickenkiller.com", "server_port": 443, "uuid": "00000000-0000-0000-0000-000000000005", "detour": "direct"},
	{"type": "vless", "tag": "Заблокированное", "server": "blok.example", "server_port": 443, "uuid": "00000000-0000-0000-0000-000000000006"},
}

// svoi-imena есть и знает имя узла — выходу узла ставится резолвер hosts;
// имя не из hosts, адрес, свой резолвер, detour, запись-блокировка — как было.
func TestUzlyBezDnsSvoiImena(t *testing.T) {
	hosts := []any{svoiImena(map[string]any{
		"node2kv.chickenkiller.com": []any{"203.0.113.10", "2001:db8::10"},
		"blok.example":              "0.0.0.0",
	})}
	gotovyy, k, err := Prigotovit(profilSvoiImena(t, hosts, vyhodyUzlov...), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	v := vyhodyPoTegu(t, gotovyy)
	for teg, zhdali := range map[string]any{
		"Финляндия · XHTTP":     TegSvoihImen,
		"Финляндия · Hysteria2": TegSvoihImen, // регистр и точка в конце — то же имя
		"Чужое имя":             nil,
		"По адресу":             nil,
		"Свой резолвер":         "local",
		"Через посредника":      nil,
		"Заблокированное":       nil,
		"Нидерланды · прямой":   nil, // primer.example — не в hosts
		"Соединение":            nil,
		"direct":                "local", // так в профиле; direct не узел
	} {
		if bylo := v[teg]["domain_resolver"]; bylo != zhdali {
			t.Errorf("%s: domain_resolver %v, ждали %v", teg, bylo, zhdali)
		}
	}
	if k.UzlovBezDns != 2 {
		t.Fatalf("UzlovBezDns = %d, ждали 2", k.UzlovBezDns)
	}
	// Резолвер по умолчанию (для direct) не тронут.
	r := razobrat(t, gotovyy)["route"].(map[string]any)
	if fmt.Sprint(r["default_domain_resolver"]) != fmt.Sprint(map[string]any{"server": "local"}) {
		t.Fatalf("default_domain_resolver тронут: %v", r["default_domain_resolver"])
	}
}

// hosts-сервера нет — выходы как были, ни одного domain_resolver не
// появилось.
func TestUzlyBezDnsNetSvoihImen(t *testing.T) {
	gotovyy, k, err := Prigotovit(profilSvoiImena(t, nil, vyhodyUzlov...), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	for teg, v := range vyhodyPoTegu(t, gotovyy) {
		if r, est := v["domain_resolver"]; est && teg != "Свой резолвер" && teg != "direct" {
			t.Errorf("%s: появился domain_resolver %v без hosts-сервера", teg, r)
		}
	}
	if k.UzlovBezDns != 0 {
		t.Fatalf("UzlovBezDns = %d без hosts-сервера", k.UzlovBezDns)
	}
}

// Имя узла не в hosts (там другие имена) — выход как был.
func TestUzlyBezDnsImyaNeVHosts(t *testing.T) {
	hosts := []any{svoiImena(map[string]any{"subkv.chickenkiller.com": "203.0.113.11"})}
	gotovyy, k, err := Prigotovit(profilSvoiImena(t, hosts, vyhodyUzlov[0]), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	if r, est := vyhodyPoTegu(t, gotovyy)["Финляндия · XHTTP"]["domain_resolver"]; est {
		t.Fatalf("имени нет в hosts, а резолвер поставлен: %v", r)
	}
	if k.UzlovBezDns != 0 {
		t.Fatalf("UzlovBezDns = %d", k.UzlovBezDns)
	}
}

// Два hosts-сервера знают одно имя — берётся svoi-imena; имя знает только
// другой hosts — берётся он.
func TestUzlyBezDnsDvaHosts(t *testing.T) {
	hosts := []any{
		map[string]any{"type": "hosts", "tag": "drugie", "predefined": map[string]any{
			"node2kv.chickenkiller.com": "198.51.100.1", "drugoy.example": "198.51.100.2"}},
		svoiImena(map[string]any{"node2kv.chickenkiller.com": "203.0.113.10"}),
	}
	gotovyy, _, err := Prigotovit(profilSvoiImena(t, hosts, vyhodyUzlov[0], vyhodyUzlov[2]), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	v := vyhodyPoTegu(t, gotovyy)
	if v["Финляндия · XHTTP"]["domain_resolver"] != TegSvoihImen || v["Чужое имя"]["domain_resolver"] != "drugie" {
		t.Fatalf("выбор hosts: %v / %v", v["Финляндия · XHTTP"]["domain_resolver"], v["Чужое имя"]["domain_resolver"])
	}
}

// Настоящее ядро (sing-box check): конфиг с резолвером hosts у vless и
// hysteria2 принимается.
func TestUzlyBezDnsYadroPrinimaet(t *testing.T) {
	bin := putYadra(t)
	if bin == "" {
		t.Skipf("настоящего ядра рядом нет — щуп пропущен. Искал: %s", mestaPoiskaYadra())
	}
	hosts := []any{svoiImena(map[string]any{"node2kv.chickenkiller.com": []any{"203.0.113.10"}})}
	gotovyy, k, err := Prigotovit(profilSvoiImena(t, hosts, vyhodyUzlov[0], vyhodyUzlov[1]), Vybor{})
	if err != nil || k.UzlovBezDns != 2 {
		t.Fatalf("Prigotovit: %v, узлов без DNS %d", err, k.UzlovBezDns)
	}
	put := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(put, gotovyy, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "check", "-c", put).CombinedOutput(); err != nil {
		t.Fatalf("ядро не приняло конфиг: %v\n%s", err, out)
	}
}

// Живой щуп: два настоящих ядра на петле. «Узел» — второе ядро (вход mixed
// на 127.0.0.1); у «ноутбука» выход-узел socks на ИМЯ uzel.kelevra.test,
// которое знает только svoi-imena (→ 127.0.0.1), а резолвер по умолчанию
// "local" — молчащий порт петли (как застрявший системный DNS после смены
// сети). С резолвером hosts соединение до эха через узел проходит; контроль
// порчей — тот же конфиг без domain_resolver: имя узла не разрешается, эха
// нет.
func TestUzlyBezDnsZhivoeYadro(t *testing.T) {
	bin := putYadra(t)
	if bin == "" {
		t.Skipf("настоящего ядра рядом нет — щуп пропущен. Искал: %s", mestaPoiskaYadra())
	}
	eho := echoTCP(t, "127.0.0.1", svobodnyPort(t))
	defer eho.Close()
	ehoPort := eho.Addr().(*net.TCPAddr).Port
	// Адрес назначения — не петля и не частная сеть (их профиль ведёт мимо
	// туннеля); «узел» подменяет его на эхо петли.
	const cel = "198.51.100.77"

	papka := t.TempDir()
	uzelPort := svobodnyPort(t)
	uzel := map[string]any{
		"log":       map[string]any{"level": "info"},
		"inbounds":  []any{map[string]any{"type": "mixed", "tag": "vhod", "listen": "127.0.0.1", "listen_port": uzelPort}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route": map[string]any{"final": "direct", "rules": []any{
			map[string]any{"ip_cidr": []any{cel + "/32"}, "action": "route-options", "override_address": "127.0.0.1"},
		}},
	}
	putUzla := filepath.Join(papka, "uzel", "config.json")
	if err := writeFileVPapku(t, putUzla, uzel); err != nil {
		t.Fatal(err)
	}
	cmdUzla, logUzla := zapustitYadro(t, bin, putUzla, uzelPort)
	defer ostanovitYadro(cmdUzla)

	d := razobrat(t, profilDlyaZhivogoSDns(t))
	d["outbounds"] = []any{
		map[string]any{"type": "direct", "tag": "direct"},
		map[string]any{"type": "socks", "tag": "Узел", "server": "uzel.kelevra.test", "server_port": uzelPort},
		map[string]any{"type": "selector", "tag": "Соединение", "outbounds": []any{"Узел"}, "default": "Узел"},
	}
	d["dns"].(map[string]any)["servers"] = append(d["dns"].(map[string]any)["servers"].([]any),
		svoiImena(map[string]any{"uzel.kelevra.test": []any{"127.0.0.1"}}))
	d["route"].(map[string]any)["final"] = "Соединение"
	syroy, _ := json.Marshal(d)
	gotovyy, k, err := Prigotovit(syroy, Vybor{})
	if err != nil || k.UzlovBezDns != 1 {
		t.Fatalf("Prigotovit: %v, узлов без DNS %d", err, k.UzlovBezDns)
	}

	mixedPort := svobodnyPort(t)
	proba := func(t *testing.T, imya string, konf []byte) (doshlo bool, log string) {
		konf = vstavitVhodShchupa(t, konf, mixedPort)
		put := filepath.Join(papka, imya, "config.json")
		if err := writeFileVPapku(t, put, razobrat(t, konf)); err != nil {
			t.Fatal(err)
		}
		cmd, zh := zapustitYadro(t, bin, put, mixedPort)
		defer ostanovitYadro(cmd)
		c, kod, err := socks5Connect(t, fmt.Sprintf("127.0.0.1:%d", mixedPort), cel, ehoPort)
		if err != nil || kod != 0 {
			return false, zh.String()
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(8 * time.Second))
		c.Write([]byte("proba-uzla"))
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		return string(buf[:n]) == "proba-uzla", zh.String()
	}

	t.Run("имя узла из svoi-imena — без DNS", func(t *testing.T) {
		doshlo, log := proba(t, "s-hosts", gotovyy)
		if !doshlo {
			t.Fatalf("эхо через узел не дошло\nноутбук:\n%s\nузел:\n%s", log, logUzla.String())
		}
	})
	t.Run("контроль: без резолвера hosts не доходит", func(t *testing.T) {
		isporchen := razobrat(t, gotovyy)
		for _, v := range isporchen["outbounds"].([]any) {
			delete(v.(map[string]any), "domain_resolver")
		}
		b, _ := json.Marshal(isporchen)
		doshlo, log := proba(t, "bez-hosts", b)
		if doshlo {
			t.Fatal("без резолвера hosts эхо всё равно дошло — щуп ничего не проверяет")
		}
		if !strings.Contains(log, "uzel.kelevra.test") {
			t.Logf("в журнале ядра нет имени узла (не страшно):\n%s", log)
		}
	})
}
