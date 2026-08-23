//go:build !windows

package avtorezhim

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

// SetevoyAdapter — версия для стенда (продукт живёт только на Windows, см.
// internal/avtozapusk и sledchik_other.go — та же граница). Здесь нет
// GetAdaptersAddresses, поэтому DNS берётся первой строкой "nameserver" из
// /etc/resolv.conf, а локальный IPv4 — первым не-loopback up-адаптером из
// net.Interfaces(). Не второй боевой механизм, а честный способ мерить
// живьём на этой машине (stend/zond_doma.sh).
func SetevoyAdapter() (dnsAdres string, lokalnyIP string, err error) {
	dns, err := pervyyNameserver("/etc/resolv.conf")
	if err != nil {
		return "", "", err
	}
	ip, err := pervyyLokalnyIPv4()
	if err != nil {
		return "", "", err
	}
	return net.JoinHostPort(dns, "53"), ip, nil
}

func pervyyNameserver(put string) (string, error) {
	f, err := os.Open(put)
	if err != nil {
		return "", fmt.Errorf("не открыть %s: %w", put, err)
	}
	defer f.Close()

	skaner := bufio.NewScanner(f)
	for skaner.Scan() {
		stroka := strings.TrimSpace(skaner.Text())
		if !strings.HasPrefix(stroka, "nameserver") {
			continue
		}
		polya := strings.Fields(stroka)
		if len(polya) >= 2 {
			return polya[1], nil
		}
	}
	return "", fmt.Errorf("в %s нет строки nameserver", put)
}

func pervyyLokalnyIPv4() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("net.Interfaces: %w", err)
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if v4 := ipnet.IP.To4(); v4 != nil {
				return v4.String(), nil
			}
		}
	}
	return "", fmt.Errorf("не нашёл активный не-loopback адаптер с IPv4")
}
