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

// PauzaBystrogoOprosa — шаг между быстрыми опросами обстановки сразу после
// события смены сети.
//
// Первый заход по событию иногда приходится на момент, когда физический
// адаптер ещё не опознан (см. Avtorezhim.Zahod: adresFizicheskogoAdaptera
// возвращает uznali=false) — заход выходит слепым и НИЧЕГО не предлагает
// задвижке. До этой правки следующий шанс подтвердить обстановку был только
// у страховочного тикера — c Podtverzhdeniy=3 и IntervalTikeraPoUmolchaniyu=
// 2 мин это до ~6 минут висящего VPN после прихода домой (боевая жалоба
// хозяина 29.08, воспроизведена в bystryy_opros_test.go). Пока идёт
// пачка быстрых опросов, каждый всё ещё доверенный (dovereno=true) — та же
// логика needed=1, что и у самого первого захода по событию.
const PauzaBystrogoOprosa = 5 * time.Second

// PovtorovBystrogoOprosa — сколько быстрых опросов сделать после события
// смены сети, прежде чем вернуться к обычному темпу (страховочный тикер +
// ожидание следующего события). 6 попыток по PauzaBystrogoOprosa — это
// около 30 с, что кратно короче страховочного тикера.
const PovtorovBystrogoOprosa = 6

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
	// PauzaBystrogo — шаг быстрых опросов после события; 0 значит
	// PauzaBystrogoOprosa.
	PauzaBystrogo time.Duration
	// PovtorovBystrogo — сколько быстрых опросов сделать после события; 0
	// значит PovtorovBystrogoOprosa.
	PovtorovBystrogo int
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

func (s *Sluzhitel) pauzaBystrogo() time.Duration {
	if s.PauzaBystrogo > 0 {
		return s.PauzaBystrogo
	}
	return PauzaBystrogoOprosa
}

func (s *Sluzhitel) povtorovBystrogo() int {
	if s.PovtorovBystrogo > 0 {
		return s.PovtorovBystrogo
	}
	return PovtorovBystrogoOprosa
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
//
// Заход на старте и заход по событию Sledchik помечаются доверенными (см.
// Zadvizhka.Predlozhit): холодный старт не обязан сидеть в Neizvestno
// IntervalTikeraPoUmolchaniyu, пока наберутся Podtverzhdeniy заходов, а
// событие смены сети — уже сигнал от системы, ему верим с первого раза (тот
// же перенос AutoModeGate.offer(trust=true), что и в Zadvizhka). Заход по
// страховочному тикеру доверенным не помечается — тикер ничего не доказывает,
// это просто периодический опрос на случай, если событие потерялось.
//
// Первый заход по событию сам по себе не гарантирует смену обстановки:
// сразу после NotifyAddrChange физический адаптер иногда ещё не опознан, и
// заход выходит слепым (Avtorezhim.Zahod, ZondSlep) — задвижке нечего
// предложить. Поэтому пока заход по событию не подтвердил смену, Krutit
// добивает обстановку короткой пачкой быстрых опросов (povtorovBystrogo по
// pauzaBystrogo) вместо того, чтобы ждать следующего подтверждения от
// страховочного тикера — тот доверенным не считается и требует Podtverzhdeniy
// (=3) заходов подряд, то есть до IntervalTikeraPoUmolchaniyu*3 (~6 минут).
func (s *Sluzhitel) Krutit(ctx context.Context) {
	defer s.Sledchik.Stop()

	t := time.NewTicker(s.interval())
	defer t.Stop()

	s.zahod(ctx, true)

	sobytiya := s.Sledchik.Sobytiya()
	var ozhidaniye <-chan time.Time
	var bystroyeOzhidaniye <-chan time.Time
	var bystryhOstalos int

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
			// перестали приходить. Новое событие также отменяет незавершённую
			// пачку быстрых опросов предыдущего события — у него будет своя.
			ozhidaniye = s.posle()(s.pauza())
			bystroyeOzhidaniye = nil
			bystryhOstalos = 0
		case <-ozhidaniye:
			ozhidaniye = nil
			if _, slep := s.zahod(ctx, true); !slep {
				continue // заход зрячий: либо сменил обстановку, либо честно
				// подтвердил то, на чём и так стоим — добивать нечего
			}
			bystryhOstalos = s.povtorovBystrogo()
			bystroyeOzhidaniye = s.posle()(s.pauzaBystrogo())
		case <-bystroyeOzhidaniye:
			_, slep := s.zahod(ctx, true)
			if !slep || bystryhOstalos <= 1 {
				bystroyeOzhidaniye = nil
				bystryhOstalos = 0
				continue
			}
			bystryhOstalos--
			bystroyeOzhidaniye = s.posle()(s.pauzaBystrogo())
		case <-t.C:
			s.zahod(ctx, false)
		}
	}
}

// zahod — один проход: спрашивает Avtorezhim и зовёт Kolbek при смене
// обстановки. poSobytiyu — заход случился по доказанному сигналу (старт или
// Sledchik), а не по страховочному тикеру — прокидывается в
// Avtorezhim.Zahod как dovereno.
//
// Возвращает izmenilos (обстановка сменилась этим заходом) и slep —
// заход вышел слепым (Nablyudeniye.ZondSlep, см. Avtorezhim.Zahod), то есть
// задвижке не было что предложить. Пачка быстрых опросов после события
// добивает именно slep-заходы — заход, честно подтвердивший то, на чём и так
// стоим (izmenilos=false, slep=false), добивать не нужно.
func (s *Sluzhitel) zahod(ctx context.Context, poSobytiyu bool) (izmenilos bool, slep bool) {
	// TODO(упрощение среза): спросчика физической сети ("адаптер вообще
	// поднят") здесь нет — считаем сеть всегда физически присутствующей.
	// Ложь только в сторону лишнего зонда, не в сторону молчания; сам
	// спросчик — задача следующего захода (см. пакетный комментарий).
	const estSet = true
	nablyudeniye, izm, tekushcheye := s.Avtorezhim.Zahod(ctx, estSet, poSobytiyu)
	if !izm {
		return false, nablyudeniye.ZondSlep
	}
	log.Printf("авторежим: обстановка сменилась на %s", tekushcheye)
	if s.Kolbek != nil {
		s.Kolbek(ctx, tekushcheye)
	}
	return true, false
}
