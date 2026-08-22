//go:build windows

package avtorezhim

import (
	"log"
	"sync"
	"syscall"
	"time"
)

// SledchikSeti — слежение за сменой сети СОБЫТИЕМ, а не поллингом: тот же
// принцип, что у телефонного клиента (DefaultNetworkListener в AutoMode.kt),
// только там колбэк даёт ConnectivityManager, а здесь — системный вызов
// NotifyAddrChange из iphlpapi.dll. Вызванный с обоими параметрами NULL, он
// синхронно блокирует поток до следующего изменения в таблице IP-адресов
// (адаптер поднялся или погас, сменился адрес — в том числе смена Wi-Fi
// сети с другим DHCP) и возвращается.
//
// Экзотических зависимостей не тянет: тот же приём (LoadLibrary +
// GetProcAddress через syscall), что уже применяется в internal/proksi
// (proksi_windows.go, soobshchitSisteme) — DLL грузим сами, без пакетов
// сверх уже используемого в модуле golang.org/x/sys.
//
// TODO: NotifyAddrChange реагирует на смену IP-адреса, а не на смену Wi-Fi
// сети с тем же адресом (тот же роутер раздал тот же адрес по DHCP заново).
// Телефонный клиент такое отдельно ловит отпечатком резолверов сети
// (networkKey в AutoMode.kt). Для первого среза этого достаточно: смена
// сети почти всегда меняет и адрес, а более точный отпечаток — задача
// следующего захода, когда пакет реально подключат к proksi/sluzhba.
type SledchikSeti struct {
	sobytiya chan struct{}
	stop     chan struct{}
	stopOnce sync.Once
}

// NovySledchik запускает слежение сразу и возвращает готовый к чтению канал.
func NovySledchik() *SledchikSeti {
	s := &SledchikSeti{
		sobytiya: make(chan struct{}, 1),
		stop:     make(chan struct{}),
	}
	go s.krutit()
	return s
}

func (s *SledchikSeti) Sobytiya() <-chan struct{} { return s.sobytiya }

func (s *SledchikSeti) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
}

func (s *SledchikSeti) krutit() {
	defer close(s.sobytiya)

	iphlpapi, err := syscall.LoadLibrary("iphlpapi.dll")
	if err != nil {
		log.Printf("авторежим: не загрузил iphlpapi.dll, слежение за сетью не работает: %v", err)
		return
	}
	defer syscall.FreeLibrary(iphlpapi)
	proc, err := syscall.GetProcAddress(iphlpapi, "NotifyAddrChange")
	if err != nil {
		log.Printf("авторежим: не нашёл NotifyAddrChange, слежение за сетью не работает: %v", err)
		return
	}

	for {
		if s.stopped() {
			return
		}
		// NotifyAddrChange(NULL, NULL): блокирует поток до следующего
		// изменения таблицы адресов. Оба параметра — указатели, за
		// неимением которых передаём 0 (NULL): синхронный режим вызова.
		r0, _, errno := syscall.Syscall(proc, 2, 0, 0, 0)
		if s.stopped() {
			return
		}
		if r0 != 0 { // не NO_ERROR
			log.Printf("авторежим: NotifyAddrChange отказал (%v), пробую снова через секунду", errno)
			select {
			case <-s.stop:
				return
			case <-time.After(time.Second):
			}
			continue
		}
		select {
		case s.sobytiya <- struct{}{}:
		default: // предыдущее событие ещё не забрали — новое не копим
		}
	}
}

func (s *SledchikSeti) stopped() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}
