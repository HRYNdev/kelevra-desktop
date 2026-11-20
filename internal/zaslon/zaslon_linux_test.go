//go:build linux

package zaslon

import (
	"strings"
	"testing"
)

// Уровень 1 в nftables: к Anthropic — только из адреса туннеля, иначе drop;
// разрешение раньше запрета (вес 15 выше 14).
func TestSkriptNftUroven1(t *testing.T) {
	s := SkriptNft(SobratFiltry(Politika{Anthropic: true, Seti: setiA, Tunnel: tunnelA}))
	for _, nado := range []string{
		"table inet kelevra_zaslon\ndelete table inet kelevra_zaslon\n",
		"type filter hook output priority filter; policy accept;",
		"meta nfproto ipv4 ip daddr { 160.79.104.0/23 } ip saddr { 172.19.0.0/30, 172.19.0.4/30 } accept",
		"meta nfproto ipv4 ip daddr { 160.79.104.0/23 } drop",
		"meta nfproto ipv6 ip6 daddr { 2607:6bc0::/48 } ip6 saddr { fdfe:dcba:9876::/126 } accept",
		"meta nfproto ipv6 ip6 daddr { 2607:6bc0::/48 } drop",
	} {
		if !strings.Contains(s, nado) {
			t.Errorf("в сценарии нет %q:\n%s", nado, s)
		}
	}
	if strings.Index(s, "160.79.104.0/23 } ip saddr") > strings.Index(s, "160.79.104.0/23 } drop") {
		t.Errorf("запрет стоит раньше разрешения из туннеля:\n%s", s)
	}
	if strings.Contains(s, "skuid") {
		t.Errorf("на уровне 1 разрешены программы (ядро к Anthropic напрямую = утечка):\n%s", s)
	}
}

// Полный запрет: последним правилом — drop всего, NDP — типами icmpv6.
func TestSkriptNftPolnyy(t *testing.T) {
	s := SkriptNft(SobratFiltry(Politika{Polnyy: true, Anthropic: true, Seti: setiA, Tunnel: tunnelA,
		Programmy: []string{"/opt/kelevra/yadro/sing-box"}}))
	stroki := strings.Split(strings.TrimSpace(s), "\n")
	posl := stroki[len(stroki)-3]
	if !strings.HasSuffix(strings.TrimSpace(strings.SplitN(posl, " comment", 2)[0]), "drop") {
		t.Errorf("последнее правило не запрет всего: %q\n%s", posl, s)
	}
	for _, nado := range []string{"meta skuid 0 accept", `oifname "lo"`, "icmpv6 type {"} {
		if !strings.Contains(s, nado) {
			t.Errorf("в полном запрете нет %q:\n%s", nado, s)
		}
	}
}
