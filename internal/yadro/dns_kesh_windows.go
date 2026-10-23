//go:build windows

package yadro

import (
	"log"

	"golang.org/x/sys/windows"
)

// procDnsFlushResolverCache — то же, что делает «ipconfig /flushdns»: функция
// dnsapi.dll без описания в x/sys/windows, поэтому через LazyDLL, как и
// QueryFullProcessImageNameW в process_windows.go.
var procDnsFlushResolverCache = windows.NewLazySystemDLL("dnsapi.dll").NewProc("DnsFlushResolverCache")

// sbrositKeshDns очищает кеш DNS Windows (служба Dnscache) в момент, когда
// туннель встал или лёг.
//
// Зачем (разбор 23.10, журналы ноутбука). Кеш Windows держит ответ до
// конца его TTL, а ответы разных резолверов в нём живут вперемешку: подменный
// адрес нашего ядра (TTL 600), подменный адрес домашнего роутера (TTL 60) и
// настоящие адреса. Пока туннель поднят, чужой подменный адрес ядро не знает
// («router: missing fakeip record»), а после того как туннель погашен, наш
// подменный адрес не знает уже никто. Сброс на каждом переходе обрезает это
// окно до кешей самих программ (у браузеров — около минуты).
//
// Ничего не настраивает и не меняет — только забывает уже полученные ответы.
// Отказ не страшен: кеш дотечёт сам, поэтому ошибка только в журнал.
func sbrositKeshDns(pochemu string) {
	if err := procDnsFlushResolverCache.Find(); err != nil {
		log.Printf("кеш DNS Windows не сброшен (%s): %v", pochemu, err)
		return
	}
	if r, _, err := procDnsFlushResolverCache.Call(); r == 0 {
		log.Printf("кеш DNS Windows не сброшен (%s): %v", pochemu, err)
		return
	}
	log.Printf("кеш DNS Windows сброшен: %s", pochemu)
}
