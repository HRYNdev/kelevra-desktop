//go:build linux

package tunnel

import (
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
)

// NastroitDNS — на Linux системный DNS не знает про туннель: systemd-resolved
// продолжает спрашивать DNS локальной сети, а он исключён из туннеля
// (route_exclude_address) — запросы шли мимо ядра к провайдеру (стенд
// 20.11.2026: ответы 198.18.x от роутера при поднятом туннеле). На Windows
// эту роль играет DNS адаптера туннеля. Здесь — назначаем адаптеру туннеля
// DNS (адрес соседа по /30, его ловит hijack-dns ядра) для всех доменов
// (~.). Адаптер пропадает — resolved сам забывает его настройки.
func NastroitDNS(imya string) {
	if os.Geteuid() != 0 {
		return
	}
	rc, err := exec.LookPath("resolvectl")
	if err != nil {
		log.Printf("DNS туннеля: resolvectl нет (%v) — системный DNS не переключён", err)
		return
	}
	adres, err := sosedTunnelya(imya)
	if err != nil {
		log.Printf("DNS туннеля: %v — системный DNS не переключён", err)
		return
	}
	for _, args := range [][]string{
		{"dns", imya, adres},
		{"domain", imya, "~."},
		{"default-route", imya, "yes"},
		// DNS ядра — обычный: глобальные DNSOverTLS/DNSSEC человека здесь
		// только сломали бы разрешение имён.
		{"dnsovertls", imya, "no"},
		{"dnssec", imya, "no"},
	} {
		if out, err := exec.Command(rc, args...).CombinedOutput(); err != nil {
			log.Printf("DNS туннеля: resolvectl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			return
		}
	}
	log.Printf("DNS туннеля: системный DNS — через %s (%s), все домены", imya, adres)
}

// sosedTunnelya — адрес соседа по сети адаптера туннеля (свой .1 — сосед .2).
func sosedTunnelya(imya string) (string, error) {
	ifc, err := net.InterfaceByName(imya)
	if err != nil {
		return "", fmt.Errorf("адаптер %s не найден: %w", imya, err)
	}
	adresa, err := ifc.Addrs()
	if err != nil {
		return "", err
	}
	for _, a := range adresa {
		p, err := netip.ParsePrefix(a.String())
		if err != nil || !p.Addr().Is4() {
			continue
		}
		return p.Addr().Next().String(), nil
	}
	return "", fmt.Errorf("у адаптера %s нет IPv4", imya)
}
