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

// MakiShlyuzov — версия для стенда (продукт живёт только на Windows, та же
// граница, что и у [SetevoyAdapter]). Шлюзы берутся из /proc/net/route, их
// аппаратные номера — из /proc/net/arp. Не второй боевой механизм, а честный
// способ мерить живьём на линуксе.
//
// Список, а не один: причина та же, что на Windows (см. MakiShlyuzov там) —
// маршрутов по умолчанию на машине бывает несколько, и домашний среди них не
// обязан быть первым.
func MakiShlyuzov() ([]string, error) {
	shlyuzy, err := shlyuzyIzTablicyMarshrutov("/proc/net/route")
	if err != nil {
		return nil, err
	}
	var maki []string
	var bedy []string
	for _, sh := range shlyuzy {
		mak, err := makIzArp("/proc/net/arp", sh)
		if err != nil {
			bedy = append(bedy, fmt.Sprintf("шлюз %s: %v", sh, err))
			continue
		}
		maki = append(maki, mak)
	}
	if len(maki) == 0 {
		return nil, fmt.Errorf("ни один шлюз не опознан: %s", strings.Join(bedy, "; "))
	}
	return maki, nil
}

// shlyuzyIzTablicyMarshrutov — ВСЕ маршруты по умолчанию (Destination 0) не
// на туннельном интерфейсе. Перебор идёт до конца таблицы: ранний выход на
// первом найденном и был бедой 09.09.2026.
func shlyuzyIzTablicyMarshrutov(put string) ([]string, error) {
	f, err := os.Open(put)
	if err != nil {
		return nil, fmt.Errorf("не открыть %s: %w", put, err)
	}
	defer f.Close()

	var nayden []string
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
		nayden = append(nayden, ip.String())
	}
	if len(nayden) == 0 {
		return nil, fmt.Errorf("в %s нет маршрута по умолчанию на физическом интерфейсе", put)
	}
	return nayden, nil
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
