package konfig

import (
	"strings"
	"testing"
)

// Профиль без своей стратегии DNS: ядро не отдаёт программам AAAA — у
// туннеля нет выхода по IPv6, и каждый такой адрес кончался отказом
// «unreachable network» в журнале ядра.
func TestBezAdresovV6PoUmolchaniyu(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	dns := razobrat(t, gotovyy)["dns"].(map[string]any)
	if dns["strategy"] != "ipv4_only" {
		t.Fatalf("dns.strategy = %v, ждал ipv4_only", dns["strategy"])
	}
}

// Стратегию, присланную сервером, не перебиваем: появится выход IPv6 — сервер
// скажет это профилем.
func TestStrategiyuProfilyaNePerebivaem(t *testing.T) {
	syroy := strings.Replace(string(profil(t)), `"dns": {`, `"dns": {"strategy": "prefer_ipv6",`, 1)
	gotovyy, _, err := Prigotovit([]byte(syroy), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	dns := razobrat(t, gotovyy)["dns"].(map[string]any)
	if dns["strategy"] != "prefer_ipv6" {
		t.Fatalf("dns.strategy = %v, ждал присланное сервером prefer_ipv6", dns["strategy"])
	}
}
