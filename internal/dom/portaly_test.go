package dom

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// Поле portaly (входы VPS с порталом моста) — часть подписанного блока: мозг
// подписывает его тем же каноническим JSON (ключи по алфавиту на всех уровнях,
// json.dumps(..., sort_keys=True, separators=(",", ":"))).
func ploskiySPortalami(t *testing.T, priv ed25519.PrivateKey, port int) []byte {
	t.Helper()
	pub := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	kanon := fmt.Sprintf(`{"adres":"100.77.0.5/32","dns":"100.77.0.1","komu":"01-7","most":"100.76.0.1:51820","mtu":1280,"portaly":[{"port":%d,"server":"fi.example.org","uzel":"fi-1"},{"port":443,"server":"vu.example.org","uzel":"vu-1"}],"pub":"%s","seti":["100.77.0.0/16"],"uzel_pub":"%s","versiya":4,"zona":"dom"}`, port, svoyPubTest, pub)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, append([]byte(Metka), kanon...)))
	return []byte(fmt.Sprintf(`{"versiya": 4, "zona": "dom", "adres": "100.77.0.5/32", "uzel_pub": "%s", "most": "100.76.0.1:51820", "seti": ["100.77.0.0/16"], "dns": "100.77.0.1", "mtu": 1280, "komu": "01-7", "pub": "%s", "portaly": [{"uzel": "fi-1", "server": "fi.example.org", "port": 2053}, {"uzel": "vu-1", "server": "vu.example.org", "port": 443}], "podpis": "%s"}`, pub, svoyPubTest, sig))
}

func TestRazobratPortaly(t *testing.T) {
	priv := probnyyKlyuch(t)
	b, err := Razobrat(ploskiySPortalami(t, priv, 2053), otkrytye(priv), "01-7")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Portaly) != 2 || b.Portaly[0] != (Portal{Uzel: "fi-1", Server: "fi.example.org", Port: 2053}) ||
		b.Portaly[1] != (Portal{Uzel: "vu-1", Server: "vu.example.org", Port: 443}) {
		t.Fatalf("порталы: %+v", b.Portaly)
	}
	if s := b.String(); !strings.Contains(s, "fi-1 fi.example.org:2053") {
		t.Fatalf("журнал без порталов: %s", s)
	}
	// Порталы под подписью: подменённый порт — блок не принят.
	p := bytes.Replace(ploskiySPortalami(t, priv, 2053), []byte(`"port": 2053`), []byte(`"port": 2054`), 1)
	if _, err := Razobrat(p, otkrytye(priv), "01-7"); !otkaz(err, "podpis") {
		t.Fatalf("подменённый портал принят: %v", err)
	}
	// Старый блок без поля — как раньше, порталов нет.
	b, err = Razobrat(ploskiy(t, priv, 3), otkrytye(priv), "01-7")
	if err != nil || b.Portaly != nil {
		t.Fatalf("старый блок: %v %+v", err, b)
	}
}

func TestRazobratPortalyIsporcheny(t *testing.T) {
	priv := probnyyKlyuch(t)
	mnogo := make([]any, MaksPortalov+1)
	for i := range mnogo {
		mnogo[i] = map[string]any{"uzel": "fi-1", "server": "fi.example.org", "port": 1000 + i}
	}
	for imya, plohoe := range map[string]any{
		"порт 0":        []any{map[string]any{"uzel": "fi-1", "server": "fi.example.org", "port": 0}},
		"порт 70000":    []any{map[string]any{"uzel": "fi-1", "server": "fi.example.org", "port": 70000}},
		"пустой адрес":  []any{map[string]any{"uzel": "fi-1", "server": "", "port": 443}},
		"пробел":        []any{map[string]any{"uzel": "fi-1", "server": "fi example", "port": 443}},
		"слишком много": mnogo,
	} {
		d := dannyeBloka(1)
		d["portaly"] = plohoe
		if _, err := Razobrat(vKonverte(t, priv, d), otkrytye(priv), "01-7"); !otkaz(err, "portaly") {
			t.Errorf("%s: принято: %v", imya, err)
		}
	}
}
