//go:build windows

package fizset

import (
	"encoding/binary"
	"syscall"

	"golang.org/x/sys/windows"
)

// ipUnicastIf — IP_UNICAST_IF (ws2ipdef.h): исходящий одноадресный трафик
// сокета идёт через этот интерфейс, что бы ни говорила таблица маршрутов
// (а она при поднятом TUN говорит «в tun125»). Тем же способом ядро
// привязывает свои соединения к физическому адаптеру.
const ipUnicastIf = 31

// privyazat — Control для net.Dialer: сокет к интерфейсу indeks. Для IPv4
// индекс передаётся в сетевом порядке байт (документация IP_UNICAST_IF).
func privyazat(indeks uint32) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], indeks)
		znachenie := binary.LittleEndian.Uint32(b[:])
		var oshibka error
		if err := c.Control(func(fd uintptr) {
			oshibka = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, ipUnicastIf, int(int32(znachenie)))
		}); err != nil {
			return err
		}
		return oshibka
	}
}
