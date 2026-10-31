package konfig

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/zapasnoy"
)

// probnyyDom — блок как его отдаст мозг, с пробными ключами (настоящих тут нет).
func probnyyDom() *Dom {
	return &Dom{
		PrivatnyyKlyuch: "YNbHIGLrMbKbcEiyJs2mBfgpamuOWX2/8bd9NmLXAEw=",
		Adres:           "100.77.0.5/32",
		UzelPub:         "fOm1kqQ0SnVBQ9EzlH6ErKu6S07zgwsNHrB2vFt3Ixw=",
		MostHost:        "100.76.0.1",
		MostPort:        51820,
		Seti:            []string{"100.77.0.0/16"},
		DNS:             "100.77.0.1",
		Zona:            "dom",
		MTU:             1280,
	}
}

func profilTelefona(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "profil_telefona.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sobrat(t *testing.T, syroy []byte, v Vybor) (map[string]any, Kartina) {
	t.Helper()
	gotovyy, k, err := Prigotovit(syroy, v)
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(gotovyy, &d); err != nil {
		t.Fatal(err)
	}
	return d, k
}

// Без блока dom конфиг ровно прежний, байт в байт; и вход домой — чистая
// добавка: вынуть эндпоинт, правило, DNS-сервер и DNS-правило — и останется
// тот же конфиг, что без блока.
func TestDomBezBlokaKonfigKakRanshe(t *testing.T) {
	for _, v := range []Vybor{
		{},
		{ClashSekret: "s", VhodSluzhby: &VhodSluzhby{Port: 20124, Parol: "p"}},
		{BezSetevyhPravil: true},
		{PravilaIzKomplekta: map[string]string{"ads": "C:/k/ads.srs"}},
	} {
		bez, kb, err := Prigotovit(profilTelefona(t), v)
		if err != nil {
			t.Fatal(err)
		}
		v2 := v
		v2.Dom = nil
		bez2, _, _ := Prigotovit(profilTelefona(t), v2)
		if !bytes.Equal(bez, bez2) {
			t.Fatal("сборка без блока не повторяется")
		}
		for _, chuzhoe := range []string{"wireguard", "endpoints", "100.77.", "100.76.", `"Дом"`, TegDnsDoma} {
			if strings.Contains(string(bez), chuzhoe) {
				t.Fatalf("без блока в конфиге %q", chuzhoe)
			}
		}
		if kb.Dom || kb.DomBeda != "" {
			t.Fatalf("без блока картина про дом: %+v", kb)
		}

		v.Dom = probnyyDom()
		s, ks := sobrat(t, profilTelefona(t), v)
		if !ks.Dom || ks.DomBeda != "" {
			t.Fatalf("дом не вписан: %+v", ks)
		}
		delete(s, "endpoints")
		r := s["route"].(map[string]any)
		var pravila []any
		for _, p := range r["rules"].([]any) {
			if pr := p.(map[string]any); pr["outbound"] == TegDoma || (pr["action"] == "reject" && pr["inbound"] == nil && reflect.DeepEqual(pr["ip_cidr"], []any{SvoyaSetDoma})) {
				continue
			}
			pravila = append(pravila, p)
		}
		r["rules"] = pravila
		dn := s["dns"].(map[string]any)
		var servery, dpravila []any
		for _, sv := range dn["servers"].([]any) {
			if sv.(map[string]any)["tag"] != TegDnsDoma {
				servery = append(servery, sv)
			}
		}
		for _, p := range dn["rules"].([]any) {
			if p.(map[string]any)["server"] != TegDnsDoma {
				dpravila = append(dpravila, p)
			}
		}
		dn["servers"], dn["rules"] = servery, dpravila
		var ozhidali map[string]any
		_ = json.Unmarshal(bez, &ozhidali)
		a, _ := json.MarshalIndent(s, "", " ")
		b, _ := json.MarshalIndent(ozhidali, "", " ")
		if !bytes.Equal(a, b) {
			t.Fatalf("вход домой задел что-то кроме своего (%+v):\n%s", v, a)
		}
	}
}

func TestDomEndpoint(t *testing.T) {
	d, _ := sobrat(t, profilTelefona(t), Vybor{Dom: probnyyDom()})
	eps, _ := d["endpoints"].([]any)
	if len(eps) != 1 {
		t.Fatalf("эндпоинтов %d, ждали 1", len(eps))
	}
	e := eps[0].(map[string]any)
	ozh := map[string]any{
		"type": "wireguard", "tag": "Дом", "mtu": float64(1280), "address": []any{"100.77.0.5/32"},
		"private_key": probnyyDom().PrivatnyyKlyuch, "detour": "Соединение",
	}
	for k, v := range ozh {
		if !reflect.DeepEqual(e[k], v) {
			t.Errorf("%s = %v, ждали %v", k, e[k], v)
		}
	}
	peers := e["peers"].([]any)
	if len(peers) != 1 {
		t.Fatalf("пиров %d", len(peers))
	}
	p := peers[0].(map[string]any)
	ozhP := map[string]any{
		"address": "100.76.0.1", "port": float64(51820), "public_key": probnyyDom().UzelPub,
		"allowed_ips": []any{"100.77.0.0/16"}, "persistent_keepalive_interval": float64(25),
	}
	for k, v := range ozhP {
		if !reflect.DeepEqual(p[k], v) {
			t.Errorf("peer.%s = %v, ждали %v", k, p[k], v)
		}
	}
}

// MostDomaIzKonfiga отдаёт detour эндпоинта «Дом» готового конфига; без
// входа домой — пусто.
func TestMostDomaIzKonfiga(t *testing.T) {
	gotovyy, _, err := Prigotovit(profilTelefona(t), Vybor{Dom: probnyyDom()})
	if err != nil {
		t.Fatal(err)
	}
	if got := MostDomaIzKonfiga(gotovyy); got != "Соединение" {
		t.Fatalf("мост %q, ждали «Соединение»", got)
	}
	bez, _, err := Prigotovit(profilTelefona(t), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	if got := MostDomaIzKonfiga(bez); got != "" {
		t.Fatalf("без входа домой мост %q", got)
	}
	if got := MostDomaIzKonfiga([]byte("не json")); got != "" {
		t.Fatalf("из мусора мост %q", got)
	}
}

// Правило дома — раньше hijack-dns, частных сетей, локальной сети, среза QUIC
// и rule_set; перед ним только снифф (и правило входа службы — оно по своему
// входу и сетей дома не касается).
func TestDomPraviloRanshe(t *testing.T) {
	for _, v := range []Vybor{
		{Dom: probnyyDom()},
		{Dom: probnyyDom(), VhodSluzhby: &VhodSluzhby{Port: 20124, Parol: "p"}},
		{Dom: probnyyDom(), BezSetevyhPravil: true},
	} {
		d, _ := sobrat(t, profilTelefona(t), v)
		pravila := d["route"].(map[string]any)["rules"].([]any)
		nashe := -1
		for i, p := range pravila {
			pr := p.(map[string]any)
			if pr["outbound"] == TegDoma {
				nashe = i
				if !reflect.DeepEqual(pr["ip_cidr"], []any{"100.77.0.0/16"}) || !reflect.DeepEqual(pr["inbound"], []any{"tun-in"}) || len(pr) != 3 {
					t.Fatalf("правило дома не то: %v", pr)
				}
				break
			}
			switch {
			case pr["action"] == "sniff" && len(pr) == 1:
			case reflect.DeepEqual(pr["inbound"], []any{zapasnoy.TegVhoda}) && len(pr) == 2:
			default:
				t.Fatalf("перед правилом дома стоит %v", pr)
			}
		}
		if nashe < 0 {
			t.Fatal("правила дома нет")
		}
	}
}

// 100.77.0.0/16 не частная ни для ядра, ни для наших исключений туннеля.
func TestDomNeVIsklyucheniyahINeChastnaya(t *testing.T) {
	svoya := netip.MustParsePrefix("100.77.0.0/16")
	for _, a := range []string{"100.77.0.1", "100.77.1.160", "100.77.10.1", "100.77.255.254"} {
		// sing common/network/addr.go IsPublicAddr — то, по чему ядро решает ip_is_private.
		ad := netip.MustParseAddr(a)
		if ad.IsPrivate() || ad.IsLoopback() || ad.IsMulticast() || ad.IsLinkLocalUnicast() || ad.IsUnspecified() {
			t.Fatalf("%s ядро сочло бы частным", a)
		}
	}
	for _, s := range LokalnyePodseti() {
		if netip.MustParsePrefix(s).Overlaps(svoya) {
			t.Fatalf("локальная подсеть %s задевает сеть дома", s)
		}
	}
	d, _ := sobrat(t, profilTelefona(t), Vybor{Dom: probnyyDom()})
	if z := SetiDomaVIsklyucheniyah(d, []string{"100.77.0.0/16"}); len(z) > 0 {
		t.Fatalf("в route_exclude_address сеть дома: %v", z)
	}
	// Чужой профиль исключил CGNAT целиком и узкий кусок своей подсети, и
	// задал route_address без неё: исключения вырезаются (остаток CGNAT
	// сохраняется), своя подсеть дописывается — с блоком и без.
	var p map[string]any
	_ = json.Unmarshal(profilTelefona(t), &p)
	tun := p["inbounds"].([]any)[0].(map[string]any)
	tun["route_exclude_address"] = []any{"100.64.0.0/10", "100.77.1.0/24", "8.8.8.8/32"}
	tun["route_address"] = []any{"10.0.0.0/8", "172.16.0.0/12"}
	syroy, _ := json.Marshal(p)
	for _, v := range []Vybor{{Dom: probnyyDom()}, {DomZakryt: true}, {}} {
		gotovo, k := sobrat(t, syroy, v)
		if len(k.SvoyaSetVyrezana) != 3 || k.DomBeda != "" {
			t.Fatalf("вырезано: %+v", k)
		}
		if z := SetiDomaVIsklyucheniyah(gotovo, []string{"100.77.0.0/16"}); len(z) > 0 {
			t.Fatalf("исключения после вырезания задевают сеть дома: %v", z)
		}
		vh := gotovo["inbounds"].([]any)[0].(map[string]any)
		isk := spisokStrok(vh["route_exclude_address"])
		est := map[string]bool{}
		for _, s := range isk {
			est[s] = true
		}
		for _, s := range []string{"8.8.8.8/32", "100.96.0.0/11", "100.80.0.0/12", "100.64.0.0/13", "100.72.0.0/14", "100.78.0.0/15", "100.76.0.0/16"} {
			if !est[s] {
				t.Fatalf("в исключениях нет %s: %v", s, isk)
			}
		}
		if ra := spisokStrok(vh["route_address"]); len(ra) != 3 || ra[2] != "100.77.0.0/16" {
			t.Fatalf("route_address: %v", ra)
		}
	}
}

func TestDomDns(t *testing.T) {
	d, _ := sobrat(t, profilTelefona(t), Vybor{Dom: probnyyDom()})
	dn := d["dns"].(map[string]any)
	pervoe := dn["rules"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(pervoe, map[string]any{"domain_suffix": []any{"dom"}, "server": TegDnsDoma, "inbound": []any{"tun-in"}}) {
		t.Fatalf("первое правило DNS: %v", pervoe)
	}
	var nash map[string]any
	for _, sv := range dn["servers"].([]any) {
		if m := sv.(map[string]any); m["tag"] == TegDnsDoma {
			nash = m
		}
	}
	if !reflect.DeepEqual(nash, map[string]any{"type": "udp", "tag": TegDnsDoma, "server": "100.77.0.1", "server_port": float64(53), "detour": "Дом"}) {
		t.Fatalf("сервер DNS дома: %v", nash)
	}
	if dn["final"] != "local" {
		t.Fatalf("dns.final сменился: %v", dn["final"])
	}
}

func TestDomNeVpisyvaetsyaBezOsnovy(t *testing.T) {
	plohoy := probnyyDom()
	plohoy.PrivatnyyKlyuch = ""
	d, k := sobrat(t, profilTelefona(t), Vybor{Dom: plohoy})
	if k.Dom || k.DomBeda == "" || d["endpoints"] != nil {
		t.Fatalf("неполный блок вписан: %+v", k)
	}
	var p map[string]any
	_ = json.Unmarshal(profilTelefona(t), &p)
	p["outbounds"] = append(p["outbounds"].([]any), map[string]any{"type": "direct", "tag": "Дом"})
	syroy, _ := json.Marshal(p)
	// Выход «Дом» из профиля вырезается — тег свободен, свой вход добавлен.
	d, k = sobrat(t, syroy, Vybor{Dom: probnyyDom()})
	if !k.Dom || k.DomBeda != "" {
		t.Fatalf("выход «Дом» профиля сорвал вход домой: %+v", k)
	}
	for _, o := range d["outbounds"].([]any) {
		if o.(map[string]any)["tag"] == TegDoma {
			t.Fatalf("выход «Дом» профиля остался: %v", o)
		}
	}
}

// Настоящее ядро принимает конфиг с входом домой (sing-box 1.14.1, теги
// with_wireguard и with_gvisor, как в core.yml). Ядра нет — пропуск.
func TestYadroPrinimaetKonfigSDomom(t *testing.T) {
	bin := putYadra(t)
	if bin == "" {
		t.Skipf("ядра нет: искал в %s", mestaPoiskaYadra())
	}
	for _, v := range []Vybor{
		{Dom: probnyyDom(), ClashSekret: "s", VhodSluzhby: &VhodSluzhby{Port: 20124, Parol: "p"}},
		{Dom: probnyyDom(), BezSetevyhPravil: true},
	} {
		gotovyy, _, err := Prigotovit(profilTelefona(t), v)
		if err != nil {
			t.Fatal(err)
		}
		put := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(put, gotovyy, 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bin, "check", "-c", put)
		cmd.Dir = t.TempDir()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("ядро отвергло конфиг с домом: %v\n%s", err, out)
		}
	}
	// Контроль: check действительно разбирает эндпоинт — битый ключ он отвергает.
	plohoy := probnyyDom()
	plohoy.PrivatnyyKlyuch = "ne-klyuch"
	gotovyy, _, _ := Prigotovit(profilTelefona(t), Vybor{Dom: plohoy})
	put := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(put, gotovyy, 0o600)
	cmd := exec.Command(bin, "check", "-c", put)
	cmd.Dir = t.TempDir()
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("ядро приняло эндпоинт с битым ключом — щуп ничего не проверяет:\n%s", out)
	}
}

// «Закрыто по умолчанию»: без блока при DomZakryt своя подсеть и зона dom —
// reject, раньше hijack-dns и всего прочего; с блоком DomZakryt ничего не
// добавляет; ядро конфиг принимает.
func TestDomZakryt(t *testing.T) {
	d, k := sobrat(t, profilTelefona(t), Vybor{DomZakryt: true})
	if !k.DomZakryt || k.Dom {
		t.Fatalf("картина: %+v", k)
	}
	pravila := d["route"].(map[string]any)["rules"].([]any)
	if !reflect.DeepEqual(pravila[1], map[string]any{"ip_cidr": []any{"100.77.0.0/16"}, "action": "reject"}) {
		t.Fatalf("второе правило (после сниффа): %v", pravila[1])
	}
	pervoe := d["dns"].(map[string]any)["rules"].([]any)[0]
	if !reflect.DeepEqual(pervoe, map[string]any{"domain_suffix": []any{"dom"}, "action": "reject", "inbound": []any{"tun-in"}}) {
		t.Fatalf("первое правило DNS: %v", pervoe)
	}
	_, k2 := sobrat(t, profilTelefona(t), Vybor{Dom: probnyyDom(), DomZakryt: true})
	if !k2.Dom || k2.DomZakryt {
		t.Fatalf("с блоком закрыто: %+v", k2)
	}
	bin := putYadra(t)
	if bin == "" {
		t.Skipf("ядра нет: искал в %s", mestaPoiskaYadra())
	}
	gotovyy, _, _ := Prigotovit(profilTelefona(t), Vybor{DomZakryt: true})
	put := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(put, gotovyy, 0o600)
	cmd := exec.Command(bin, "check", "-c", put)
	cmd.Dir = t.TempDir()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ядро отвергло закрытый конфиг: %v\n%s", err, out)
	}
}

// Договор, «Безопасность» п.10: в итоговом конфиге нет ни reverse у
// выходов, ни входов профиля, кроме tun (и служебного, который ставит клиент).
func TestBelyySpisokVhodovIBezReverse(t *testing.T) {
	var p map[string]any
	_ = json.Unmarshal(profilTelefona(t), &p)
	p["inbounds"] = append(p["inbounds"].([]any),
		map[string]any{"type": "shadowsocks", "tag": "ss-in", "listen": "0.0.0.0", "listen_port": 8388, "method": "2022-blake3-aes-128-gcm", "password": "AAAAAAAAAAAAAAAAAAAAAA=="},
		map[string]any{"type": "direct", "tag": "direct-in", "listen": "0.0.0.0", "listen_port": 5353},
	)
	vyhody := p["outbounds"].([]any)
	for _, v := range vyhody {
		if vh := v.(map[string]any); vh["type"] == "vless" {
			vh["reverse"] = map[string]any{"tag": "dom-most"}
		}
	}
	p["route"].(map[string]any)["rules"] = append(p["route"].(map[string]any)["rules"].([]any),
		map[string]any{"inbound": []any{"ss-in"}, "outbound": "direct"})
	syroy, _ := json.Marshal(p)
	for _, v := range []Vybor{{}, {Dom: probnyyDom(), VhodSluzhby: &VhodSluzhby{Port: 20124, Parol: "p"}}} {
		gotovyy, _, err := Prigotovit(syroy, v)
		if err != nil {
			t.Fatal(err)
		}
		s := string(gotovyy)
		for _, chuzhoe := range []string{`"reverse"`, `"ss-in"`, `"direct-in"`, `"shadowsocks"`, `"0.0.0.0"`, `"mixed-in"`} {
			if strings.Contains(s, chuzhoe) {
				t.Fatalf("в конфиге осталось %s", chuzhoe)
			}
		}
		var d map[string]any
		_ = json.Unmarshal(gotovyy, &d)
		for _, vh := range d["inbounds"].([]any) {
			m := vh.(map[string]any)
			if m["type"] != "tun" && m["tag"] != zapasnoy.TegVhoda {
				t.Fatalf("лишний вход: %v", m)
			}
		}
		if bin := putYadra(t); bin != "" {
			put := filepath.Join(t.TempDir(), "config.json")
			_ = os.WriteFile(put, gotovyy, 0o600)
			cmd := exec.Command(bin, "check", "-c", put)
			cmd.Dir = t.TempDir()
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("ядро отвергло конфиг: %v\n%s", err, out)
			}
		}
	}
}
