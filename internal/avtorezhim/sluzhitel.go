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

	// Primenit — приведение защиты к обстановке, зовётся ПОСЛЕ КАЖДОГО
	// зрячего захода, а не только при смене (см. privedenie.go). povtor
	// истинно, когда обстановка этим заходом НЕ менялась — то есть защиту
	// приводят к тому, на чём задвижка стояла и раньше.
	//
	// Зачем отдельно от Kolbek. Kolbek событийный: он говорит «обстановка
	// стала другой». Этого мало — защита может не отвечать обстановке и без
	// всякой смены (кнопка «Подключиться» дома завела задвижку сразу на
	// Doma; опускание не удалось с первого раза; человек поднял защиту
	// руками поверх работающего автомата). Ровно отсюда жалоба,
	// повторённая четырежды: вернулся домой — VPN не выключился. Телефонный
	// эталон зовёт apply() на каждом круге, в том числе repeat = true
	// (AutoMode.kt:1262-1275), и гасит туннель идемпотентно.
	//
	// Зовётся ТОЛЬКО когда заходу можно верить: слепой заход (мерили свой же
	// туннель, Nablyudeniye.ZondSlep) и обстановка Neizvestno приведения не
	// вызывают — броня против ложных срабатываний остаётся ровно та же.
	Primenit func(ctx context.Context, s Sostoyanie, povtor bool)

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
// Первый заход по событию сам по себе не гарантирует смену обстановки: либо
// сразу после NotifyAddrChange физический адаптер ещё не опознан и заход
// выходит слепым (Avtorezhim.Zahod, ZondSlep) — задвижке нечего предложить,
// либо адаптер опознан уверенно, но зонд застал старую/неверную картину
// (диагноз 30.08: боевая жалоба пережила первый фикс — VPN не
// гаснет при быстром возврате домой, потому что первый заход после события
// оказывался зрячим, но с устаревшей картиной, а добивающая пачка запускалась
// раньше только по признаку "слепой"). Поэтому Krutit запускает добивающую
// пачку быстрых опросов (povtorovBystrogo по pauzaBystrogo) по самому факту
// "заход по событию не подтвердил смену", независимо от slep — событие уже
// доказывает, что сеть шевельнулась, а pазобраться, как именно, дешевле
// быстрой перепроверкой, чем ждать страховочный тикер: тот доверенным не
// считается и требует Podtverzhdeniy (=3) заходов подряд, то есть до
// IntervalTikeraPoUmolchaniyu*3 (~6 минут).
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
			if izmenilos, _ := s.zahod(ctx, true); izmenilos {
				continue // обстановка сменилась, колбэк уже позвал — добивать нечего
			}
			// Обстановка не сменилась — либо заход вышел слепым (адаптер
			// ещё не опознан), либо зрячим, но подтвердил старую картину.
			// Второй случай раньше не добивался быстрой пачкой, хотя само
			// событие смены сети уже доказывает, что сеть шевельнулась —
			// картина могла оказаться устаревшей/неверной у уверенно
			// опознанного адаптера так же, как у неопознанного. Добиваем
			// по факту события, а не по признаку "слепой".
			bystryhOstalos = s.povtorovBystrogo()
			bystroyeOzhidaniye = s.posle()(s.pauzaBystrogo())
		case <-bystroyeOzhidaniye:
			izmenilos, _ := s.zahod(ctx, true)
			if izmenilos || bystryhOstalos <= 1 {
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
	if izm {
		log.Printf("авторежим: обстановка сменилась на %s", tekushcheye)
		if s.Kolbek != nil {
			s.Kolbek(ctx, tekushcheye)
		}
	}

	// Приведение защиты к обстановке — на КАЖДОМ зрячем заходе (см.
	// Sluzhitel.Primenit и privedenie.go). Два условия, при которых
	// приведения не будет, — это та самая броня, её тут не ослабили:
	//
	//   - nablyudeniye.ZondSlep: заход мерил наш собственный туннель, верить
	//     ему нельзя ни в одну сторону, а задвижка могла остаться на старом
	//     значении ещё с прошлой сети — приводить защиту к нему значило бы
	//     гасить VPN по догадке;
	//   - tekushcheye == Neizvestno: обстановки нет вовсе (холодный старт,
	//     сети нет) — Nuzhno на ней всё равно вернёт NeTrogat, но проверить
	//     тут дешевле, чем полагаться на дальний файл.
	//
	// Молчание резолверов приведения НЕ отменяет, и это намеренно, как на
	// телефоне: молчание даёт Neizvestno на уровне НАБЛЮДЕНИЯ, задвижка его
	// не пропускает (Zadvizhka.Predlozhit), и приводим мы защиту к той
	// обстановке, которая была принята РАНЬШЕ по зрячим заходам. Вернувшийся
	// домой человек с онемевшим на роуминге резолвером (авария 28.08) при
	// этом остаётся под поднятым VPN — задвижка всё ещё на VneDoma, — а не
	// теряет защиту по молчанию.
	if s.Primenit != nil && !nablyudeniye.ZondSlep && tekushcheye != Neizvestno {
		s.Primenit(ctx, tekushcheye, !izm)
	}

	if !izm {
		return false, nablyudeniye.ZondSlep
	}
	return true, false
}
