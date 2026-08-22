package avtorezhim

import (
	"context"
	"log"
	"time"
)

// IntervalTikeraPoUmolchaniyu — страховочный тикер служителя. Основной
// сигнал захода — событие [Sledchik.Sobytiya]; тикер только на случай, если
// событие почему-то не пришло (заснувший ноутбук пропустил его, драйвер
// смолчал и т.п.) — двух минут для подстраховки достаточно.
const IntervalTikeraPoUmolchaniyu = 2 * time.Minute

// PauzaDrebezga — сколько ждать после события смены сети, прежде чем
// зондировать. Сеть в первую секунду после события ещё не готова (адаптер
// поднимается, DHCP отвечает не мгновенно) — зонд, пущенный раньше срока,
// зря спишет наблюдение "не дома". Та же пауза схлопывает пачку событий,
// пришедших подряд (Windows шлёт несколько NotifyAddrChange на одну смену
// сети), в один заход: каждое новое событие просто отодвигает срабатывание.
const PauzaDrebezga = time.Second

// Sluzhitel — фоновый цикл, склеивающий [Sledchik] и [Avtorezhim.Zahod]:
// слушает событие смены сети и страховочный тикер, по каждому проверяет
// обстановку и зовёт Kolbek, только когда обстановка ДЕЙСТВИТЕЛЬНО сменилась
// (Avtorezhim.Zahod вернул izmenilos == true) — переключать защиту незачем
// на каждое дребезжащее подтверждение того же самого.
type Sluzhitel struct {
	Avtorezhim *Avtorezhim
	Sledchik   Sledchik

	// Interval — страховочный тикер; 0 значит IntervalTikeraPoUmolchaniyu.
	Interval time.Duration
	// Pauza — задержка схлопывания дребезга; 0 значит PauzaDrebezga.
	Pauza time.Duration
	// Kolbek зовётся только при реальной смене обстановки. Не блокируется
	// цикл слежения навечно тем, что колбэк идёт долго (перезапуск ядра —
	// до 45 с): пока он выполняется, Krutit не читает Sledchik.Sobytiya, а
	// обе рабочие реализации Sledchik сами не копят события в очередь —
	// шлют н***кирующей отправкой (select с default) и лишние роняют. Так
	// что события, пришедшие за время колбэка, схлопываются в один
	// следующий заход, а не выстраиваются в очередь.
	Kolbek func(ctx context.Context, s Sostoyanie)

	// Posle подменяется тестом, чтобы не спать по-настоящему во время
	// проверки схлопывания дребезга. По умолчанию — time.After.
	Posle func(d time.Duration) <-chan time.Time
}

func (s *Sluzhitel) interval() time.Duration {
	if s.Interval > 0 {
		return s.Interval
	}
	return IntervalTikeraPoUmolchaniyu
}

func (s *Sluzhitel) pauza() time.Duration {
	if s.Pauza > 0 {
		return s.Pauza
	}
	return PauzaDrebezga
}

func (s *Sluzhitel) posle() func(time.Duration) <-chan time.Time {
	if s.Posle != nil {
		return s.Posle
	}
	return time.After
}

// Krutit — блокирующий цикл слежения. Заходит один раз сразу на старте (окно
// приложения не должно ждать первого события или первого тика, чтобы узнать
// обстановку), дальше — по событию из Sledchik.Sobytiya (с паузой схлопывания
// дребезга) и по страховочному тикеру. Выходит по ctx.Done(), останавливая
// Sledchik за собой.
func (s *Sluzhitel) Krutit(ctx context.Context) {
	defer s.Sledchik.Stop()

	t := time.NewTicker(s.interval())
	defer t.Stop()

	s.zahod(ctx)

	sobytiya := s.Sledchik.Sobytiya()
	var ozhidaniye <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case _, otkryt := <-sobytiya:
			if !otkryt {
				sobytiya = nil // канал закрыт — не крутиться на нём вхолостую
				continue
			}
			// Каждое новое событие отодвигает срабатывание заново — пачка
			// событий подряд схлопывается в один заход после того, как они
			// перестали приходить.
			ozhidaniye = s.posle()(s.pauza())
		case <-ozhidaniye:
			ozhidaniye = nil
			s.zahod(ctx)
		case <-t.C:
			s.zahod(ctx)
		}
	}
}

// zahod — один проход: спрашивает Avtorezhim и зовёт Kolbek при смене обстановки.
func (s *Sluzhitel) zahod(ctx context.Context) {
	// TODO(упрощение среза): спросчика физической сети ("адаптер вообще
	// поднят") здесь нет — считаем сеть всегда физически присутствующей.
	// Ложь только в сторону лишнего зонда, не в сторону молчания; сам
	// спросчик — задача следующего захода (см. пакетный комментарий).
	const estSet = true
	_, izmenilos, tekushcheye := s.Avtorezhim.Zahod(ctx, estSet)
	if !izmenilos {
		return
	}
	log.Printf("авторежим: обстановка сменилась на %s", tekushcheye)
	if s.Kolbek != nil {
		s.Kolbek(ctx, tekushcheye)
	}
}
