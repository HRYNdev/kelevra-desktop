package konfig

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const chuzhoyProfil = `{
 "log":{"level":"warn"},
 "dns":{"servers":[{"type":"udp","tag":"dns-direct","server":"1.1.1.1"},
   {"type":"udp","tag":"dns-dom","server":"100.77.0.1"}],
   "rules":[{"domain_suffix":["dom"],"server":"dns-dom"}]},
 "inbounds":[%s],
 "outbounds":[{"type":"direct","tag":"direct"},{"type":"vless","tag":"Соединение","server":"1.2.3.4","server_port":443,"uuid":"11111111-1111-1111-1111-111111111111"}],
 "endpoints":[{"type":"wireguard","tag":"x","address":["100.77.0.9/32"],"private_key":"YNbHIGLrMbKbcEiyJs2mBfgpamuOWX2/8bd9NmLXAEw=",
   "peers":[{"address":"5.6.7.8","port":51820,"public_key":"fOm1kqQ0SnVBQ9EzlH6ErKu6S07zgwsNHrB2vFt3Ixw=","allowed_ips":["100.77.0.0/16"]}]}],
 "route":{"rules":[{"action":"sniff"},
   {"inbound":["x"],"outbound":"Дом"},
   {"ip_cidr":["100.77.0.0/16"],"outbound":"Соединение"},
   {"inbound":["x"],"outbound":"x"},
   {"protocol":"dns","action":"hijack-dns"}],"final":"Соединение"}}`

func sobratChuzhoy(t *testing.T, vhody string, v Vybor) (map[string]any, []byte) {
	t.Helper()
	b, _, err := Prigotovit([]byte(strings.Replace(chuzhoyProfil, "%s", vhody, 1)), v)
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	return d, b
}

func proverkaYadra(t *testing.T, b []byte) {
	bin := putYadra(t)
	if bin == "" {
		t.Logf("ядра нет — sing-box check пропущен: %s", mestaPoiskaYadra())
		return
	}
	put := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(put, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "check", "-c", put).CombinedOutput(); err != nil {
		t.Fatalf("ядро не приняло: %v\n%s", err, out)
	}
}

func TestChuzhoyEndpointIPraviloVyrezany(t *testing.T) {
	d, b := sobratChuzhoy(t, `{"type":"tun","tag":"tun-in","interface_name":"tun125","address":["172.19.0.1/30"]}`, Vybor{Dom: probnyyDom()})
	eps, _ := d["endpoints"].([]any)
	if len(eps) != 1 || eps[0].(map[string]any)["tag"] != TegDoma {
		t.Fatalf("endpoints = %v, нужен ровно «Дом»", eps)
	}
	pr := d["route"].(map[string]any)["rules"].([]any)
	// sniff, Дом (свой), reject, дальше ничего про дом и про x.
	if pr[1].(map[string]any)["outbound"] != TegDoma {
		t.Fatalf("после sniff не «Дом»: %v", pr)
	}
	rj := pr[2].(map[string]any)
	if rj["action"] != "reject" {
		t.Fatalf("reject не сразу за «Дом»: %v", pr)
	}
	if _, est := rj["inbound"]; est {
		t.Fatalf("у reject не должно быть inbound: %v", rj)
	}
	for _, p := range pr[3:] {
		if bs, _ := json.Marshal(p); strings.Contains(string(bs), "100.77.") || strings.Contains(string(bs), `"x"`) || strings.Contains(string(bs), TegDoma) {
			t.Fatalf("осталось чужое правило: %s", bs)
		}
	}
	proverkaYadra(t, b)
}

func TestChuzhoyZakrytyyRezhim(t *testing.T) {
	d, b := sobratChuzhoy(t, `{"type":"tun","tag":"tun-in","interface_name":"tun125","address":["172.19.0.1/30"]}`, Vybor{DomZakryt: true})
	if _, est := d["endpoints"]; est {
		t.Fatal("endpoints профиля остались")
	}
	pr := d["route"].(map[string]any)["rules"].([]any)
	rj := pr[1].(map[string]any)
	if rj["action"] != "reject" || rj["inbound"] != nil {
		t.Fatalf("закрытый режим: %v", pr)
	}
	proverkaYadra(t, b)
}

func TestChuzhoyDvaTunIBezTega(t *testing.T) {
	d, b := sobratChuzhoy(t, `{"type":"tun","interface_name":"tun125","address":["172.19.0.1/30"]},{"type":"tun","tag":"vtoroy","interface_name":"tun126","address":["172.20.0.1/30"]}`, Vybor{Dom: probnyyDom()})
	vh := d["inbounds"].([]any)
	if len(vh) != 1 || vh[0].(map[string]any)["tag"] != TegTuna {
		t.Fatalf("inbounds = %v", vh)
	}
	pr := d["route"].(map[string]any)["rules"].([]any)
	in, _ := pr[1].(map[string]any)["inbound"].([]any)
	if len(in) != 1 || in[0] != TegTuna {
		t.Fatalf("правило «Дом» без тега клиента: %v", pr[1])
	}
	proverkaYadra(t, b)
}

func TestChuzhoyNeObektVoVhodah(t *testing.T) {
	d, b := sobratChuzhoy(t, `"mixed-in",42,null,{"type":"tun","tag":"t","interface_name":"tun125","address":["172.19.0.1/30"]}`, Vybor{})
	vh := d["inbounds"].([]any)
	if len(vh) != 1 {
		t.Fatalf("не-объекты остались: %v", vh)
	}
	proverkaYadra(t, b)
}

const profilSVyhodomDom = `{
 "log":{"level":"warn"},
 "dns":{"servers":[{"type":"udp","tag":"dns-direct","server":"1.1.1.1"},
   {"type":"udp","tag":"dns-dom","server":"8.8.8.8"}],"final":"dns-dom"},
 "inbounds":[{"type":"tun","tag":"tun-in","interface_name":"tun125","address":["172.19.0.1/30"]}],
 "outbounds":[{"type":"direct","tag":"Дом"},{"type":"vless","tag":"Соединение","server":"1.2.3.4","server_port":443,"uuid":"11111111-1111-1111-1111-111111111111"},
   {"type":"selector","tag":"sel","outbounds":["Соединение","Дом"],"default":"Дом"}],
 "route":{"rules":[{"action":"sniff"},{"protocol":"dns","action":"hijack-dns"}],"final":"Дом"}}`

func imeyutReject(d map[string]any) bool {
	pr := d["route"].(map[string]any)["rules"].([]any)
	for _, p := range pr {
		if m, ok := p.(map[string]any); ok && m["action"] == "reject" && strings.Contains(fmtJSON(m), "100.77.0.0/16") {
			return true
		}
	}
	return false
}

func fmtJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestChuzhoyVyhodDomVyrezan(t *testing.T) {
	b, k, err := Prigotovit([]byte(profilSVyhodomDom), Vybor{Dom: probnyyDom()})
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if !k.Dom || k.DomBeda != "" {
		t.Fatalf("свой «Дом» не добавлен: %+v", k)
	}
	eps, _ := d["endpoints"].([]any)
	if len(eps) != 1 || eps[0].(map[string]any)["tag"] != TegDoma {
		t.Fatalf("endpoints = %v", eps)
	}
	for _, o := range d["outbounds"].([]any) {
		if o.(map[string]any)["tag"] == TegDoma {
			t.Fatalf("выход «Дом» профиля остался: %v", o)
		}
	}
	if !imeyutReject(d) {
		t.Fatal("нет reject 100.77.0.0/16")
	}
	if f := d["route"].(map[string]any)["final"]; f == TegDoma {
		t.Fatal("route.final = Дом")
	}
	if f := d["dns"].(map[string]any)["final"]; f == TegDnsDoma {
		t.Fatal("dns.final = dns-dom")
	}
	proverkaYadra(t, b)
}

func TestDomBedaZakryvaet(t *testing.T) {
	dm := probnyyDom()
	dm.Seti = nil // блок неполон → dobavitDom откажет
	b, k, err := Prigotovit([]byte(profilSVyhodomDom), Vybor{Dom: dm})
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if k.Dom || k.DomBeda == "" || !k.DomZakryt {
		t.Fatalf("ждали беду и закрытие: %+v", k)
	}
	if !imeyutReject(d) {
		t.Fatal("нет reject 100.77.0.0/16")
	}
	dp := d["dns"].(map[string]any)["rules"].([]any)
	if len(dp) == 0 || dp[0].(map[string]any)["action"] != "reject" {
		t.Fatalf("dns зоны dom не reject: %v", dp)
	}
	proverkaYadra(t, b)
}
