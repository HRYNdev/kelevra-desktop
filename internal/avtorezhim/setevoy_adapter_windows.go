//go:build windows

package avtorezhim

import (
	"fmt"
	"net"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// propVirtual — IANA ifType 53 (IF_TYPE_PROPVIRTUAL), которым Windows чаще
// всего размечает виртуальные туннельные адаптеры (wintun/WireGuard в том
// числе) — golang.org/x/sys/windows её не заводит, поэтому своя константа.
const propVirtual = 53

// slovaSvoegoAdaptera — куски описания/имени, по которым отсеивается НАШ
// собственный TUN-адаптер (или чужой VPN): профиль не всегда даёт нам
// заранее знать точное системное имя интерфейса (internal/konfig задаёт
// interface_name динамически), поэтому фильтр — по типу и по подстроке в
// описании, как и обычные VPN-клиенты друг у друга.
var slovaSvoegoAdaptera = []string{"wireguard", "wintun", "sing-box", "tun"}

// SetevoyAdapter — DNS-сервер и локальный IPv4 первого подходящего
// физического сетевого адаптера: поднят (IfOperStatusUp), не loopback, не
// туннельный/наш собственный TUN, с непустым списком DNS-серверов и
// непустым unicast IPv4. Нужно [DnsZond], чтобы в TUN-режиме спрашивать
// резолвер физической сети напрямую, а не системный (см. dns_zond.go,
// avtorezhim.go — ZondSlep).
func SetevoyAdapter() (dnsAdres string, lokalnyIP string, err error) {
	adaptery, err := poluchitAdaptery()
	if err != nil {
		return "", "", err
	}

	for a := adaptery; a != nil; a = a.Next {
		if a.OperStatus != windows.IfOperStatusUp {
			continue
		}
		if a.IfType == windows.IF_TYPE_SOFTWARE_LOOPBACK || a.IfType == windows.IF_TYPE_TUNNEL || a.IfType == propVirtual {
			continue
		}
		if svoyAdapter(a) {
			continue
		}

		dns := pervyyDnsIPv4(a)
		if dns == "" {
			continue
		}
		ip := pervyyUnicastIPv4(a)
		if ip == "" {
			continue
		}
		return dns, ip, nil
	}
	return "", "", fmt.Errorf("не нашёл подходящий физический адаптер (up, не loopback, не туннель, с DNS и IPv4)")
}

func svoyAdapter(a *windows.IpAdapterAddresses) bool {
	opisaniye := strings.ToLower(windows.UTF16PtrToString(a.Description))
	imya := strings.ToLower(windows.UTF16PtrToString(a.FriendlyName))
	for _, slovo := range slovaSvoegoAdaptera {
		if strings.Contains(opisaniye, slovo) || strings.Contains(imya, slovo) {
			return true
		}
	}
	return false
}

// pervyyDnsIPv4 — "IP:53" первого DNS-сервера адаптера с IPv4-адресом:
// DnsZond.AdresResolvera ждёт именно "host:port" (см. dns_zond.go).
func pervyyDnsIPv4(a *windows.IpAdapterAddresses) string {
	for d := a.FirstDnsServerAddress; d != nil; d = d.Next {
		if ip := d.Address.IP(); ip != nil {
			if v4 := ip.To4(); v4 != nil {
				return net.JoinHostPort(v4.String(), "53")
			}
		}
	}
	return ""
}

func pervyyUnicastIPv4(a *windows.IpAdapterAddresses) string {
	for u := a.FirstUnicastAddress; u != nil; u = u.Next {
		if ip := u.Address.IP(); ip != nil {
			if v4 := ip.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
}

// poluchitAdaptery — обёртка над GetAdaptersAddresses с ростом буфера:
// первый вызов даёт нужный размер через ERROR_BUFFER_OVERFLOW (обычный
// протокол этого API — размер таблицы адаптеров заранее не известен).
func poluchitAdaptery() (*windows.IpAdapterAddresses, error) {
	razmer := uint32(15 * 1024) // с запасом, как в типовых примерах MSDN
	for popytka := 0; popytka < 3; popytka++ {
		bufer := make([]byte, razmer)
		adaptery := (*windows.IpAdapterAddresses)(unsafe.Pointer(&bufer[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, 0, 0, adaptery, &razmer)
		if err == nil {
			return adaptery, nil
		}
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue // razmer уже обновлён вызовом — пробуем снова
		}
		return nil, fmt.Errorf("GetAdaptersAddresses: %w", err)
	}
	return nil, fmt.Errorf("GetAdaptersAddresses: буфер не подошёл за 3 попытки")
}
