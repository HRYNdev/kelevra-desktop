//go:build !windows

package avtorezhim

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// MakShlyuza — версия для стенда (продукт живёт только на Windows, та же
// граница, что и у [SetevoyAdapter]). Шлюз берётся из /proc/net/route, его
// аппаратный номер — из /proc/net/arp. Не второй боевой механизм, а честный
// способ мерить живьём на линуксе.
func MakShlyuza() (string, error) {
	shlyuz, err := shlyuzIzTablicyMarshrutov("/proc/net/route")
	if err != nil {
		return "", err
	}
	return makIzArp("/proc/net/arp", shlyuz)
}

// shlyuzIzTablicyMarshrutov — первый маршрут по умолчанию (Destination 0) не
// на туннельном интерфейсе.
func shlyuzIzTablicyMarshrutov(put string) (string, error) {
	f, err := os.Open(put)
	if err != nil {
		return "", fmt.Errorf("не открыть %s: %w", put, err)
	}
	defer f.Close()

	skaner := bufio.NewScanner(f)
	skaner.Scan() // шапка
	for skaner.Scan() {
		polya := strings.Fields(skaner.Text())
		if len(polya) < 3 {
			continue
		}
		imya, naznacheniye, shlyuz := polya[0], polya[1], polya[2]
		if naznacheniye != "00000000" {
			continue
		}
		if svoyPoImeni(imya) {
			continue
		}
		ip, err := ipIzHexLE(shlyuz)
		if err != nil || ip.IsUnspecified() {
			continue
		}
		return ip.String(), nil
	}
	return "", fmt.Errorf("в %s нет маршрута по умолчанию на физическом интерфейсе", put)
}

func svoyPoImeni(imya string) bool {
	n := strings.ToLower(imya)
	for _, slovo := range slovaSvoegoAdaptera {
		if strings.Contains(n, slovo) {
			return true
		}
	}
	return false
}

// ipIzHexLE — адрес в том виде, в каком его пишет ядро: восемь
// шестнадцатеричных цифр, порядок байтов обратный.
func ipIzHexLE(s string) (net.IP, error) {
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(v))
	return net.IPv4(b[0], b[1], b[2], b[3]), nil
}

// makIzArp — аппаратный номер соседа из /proc/net/arp.
func makIzArp(put, adres string) (string, error) {
	f, err := os.Open(put)
	if err != nil {
		return "", fmt.Errorf("не открыть %s: %w", put, err)
	}
	defer f.Close()

	skaner := bufio.NewScanner(f)
	skaner.Scan() // шапка
	for skaner.Scan() {
		polya := strings.Fields(skaner.Text())
		if len(polya) < 4 || polya[0] != adres {
			continue
		}
		mak := strings.ToLower(polya[3])
		if _, err := hex.DecodeString(strings.ReplaceAll(mak, ":", "")); err != nil {
			continue
		}
		if strings.Trim(mak, ":0") == "" {
			continue // 00:00:00:00:00:00 — записи ещё нет
		}
		return mak, nil
	}
	return "", fmt.Errorf("соседа %s в %s нет", adres, put)
}
