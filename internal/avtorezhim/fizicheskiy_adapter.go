package avtorezhim

import "net"

// FizicheskiySetevoyAdapterPodnyat отвечает на грубый вопрос "есть ли вообще
// поднятый физический сетевой адаптер" — не какой из них домашний (это уже
// решает SetevoyAdapter при выборе DNS/IP), а стоит ли вообще пытаться
// зондировать сеть, или адаптеров попросту нет (самолётный режим, выключенный
// Wi-Fi, отключённый кабель).
//
// Хватает стандартной библиотеки: интерфейс поднят (net.FlagUp), не loopback,
// у него есть хотя бы один адрес. Платформенной специфики здесь не нужно —
// в отличие от SetevoyAdapter, вопрос не про конкретный DNS/IP, а про сам
// факт присутствия адаптера.
//
// Ошибка чтения списка интерфейсов — тоже "не узнали", и ответ тогда true:
// молчание этого спросчика не должно останавливать зонд, ошибаться можно
// только в сторону лишнего зонда, не в сторону молчания (см. Sluzhitel.zahod).
func FizicheskiySetevoyAdapterPodnyat() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return true
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}
		return true
	}
	return false
}
