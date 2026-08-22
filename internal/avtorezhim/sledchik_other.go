//go:build !windows

package avtorezhim

import (
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// IntervalPoUmolchaniyu — как часто опрашивается список интерфейсов на
// не-Windows сборке. Короткий и без притязаний на точность: см. комментарий
// у SledchikSeti — это не второй боевой вариант.
const IntervalPoUmolchaniyu = 3 * time.Second

// SledchikSeti — честный поллинг вместо события.
//
// Событийная версия ([sledchik_windows.go]) существует, потому что продукт
// живёт только на Windows (см. internal/avtozapusk: там та же граница). На
// остальных ОС ни NotifyAddrChange, ни его эквивалент без сторонних
// зависимостей недоступны — а сборка и тесты пакета на этой машине идут
// именно не на Windows. Поэтому здесь просто короткий опрос списка
// интерфейсов и их адресов: не второй боевой механизм, а честная заглушка
// ради go build/go test вне Windows.
type SledchikSeti struct {
	sobytiya chan struct{}
	stop     chan struct{}
	interval time.Duration
	stopOnce sync.Once
}

// NovySledchik запускает поллинг сразу и возвращает готовый к чтению канал.
func NovySledchik() *SledchikSeti {
	s := &SledchikSeti{
		sobytiya: make(chan struct{}, 1),
		stop:     make(chan struct{}),
		interval: IntervalPoUmolchaniyu,
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
	t := time.NewTicker(s.interval)
	defer t.Stop()

	proshlyy := snimokSetey()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
		}
		tekushchiy := snimokSetey()
		if tekushchiy != proshlyy {
			proshlyy = tekushchiy
			select {
			case s.sobytiya <- struct{}{}:
			default:
			}
		}
	}
}

// snimokSetey — грубый отпечаток сетевых интерфейсов: имена и адреса,
// отсортированные и склеенные в строку. Не пытается быть умным (это не
// продуктовый код) — только заметить, что список интерфейсов изменился.
func snimokSetey() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var stroki []string
	for _, ifc := range ifaces {
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		var adr []string
		for _, a := range addrs {
			adr = append(adr, a.String())
		}
		sort.Strings(adr)
		stroki = append(stroki, ifc.Name+":"+strings.Join(adr, ","))
	}
	sort.Strings(stroki)
	return strings.Join(stroki, "|")
}
