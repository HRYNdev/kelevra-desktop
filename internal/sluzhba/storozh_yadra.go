package sluzhba

// Сторож ядра: работавшее ядро ушло само — поднять его снова.
//
// Зачем. До 07.11 на падение ядра в службе не реагировал никто, кроме
// авторежима (событие сети → заход → подъём, 2–15 с напрямую мимо туннеля;
// событие потерялось — до 2 мин). Без авторежима человек оставался без
// туннеля до ручного «Подключиться», то есть бессрочно. Зависшее ядро (процесс
// жив, Clash API молчит) не замечал никто.
//
// Теперь: yadro замечает падение и зависание (yadro/slezhka.go; зависшее
// гасится и дожидается выхода процесса) и отдаёт Padenie сюда. Сторож,
// независимо от авторежима, поднимает ядро заново тем же подъёмом, что кнопка,
// под общим замком переходов и под запретом трафика мимо туннеля
// (zaslon_perezapuska.go) — с отсечкой частоты по лестнице lestnicaStorozha:
// ядро, падающее раз за разом, не крутится в петле. Каждое падение — строка
// журнала с причиной (код выхода и хвост yadro.log).
//
// Не поднимает: человек нажал «Отключить» после падения; авторежим считает,
// что человек дома (туннель не нужен, обход делает роутер); служба уходит;
// нет прав. Отказ, который повтором не лечится (нет конфига, нет прав, занят
// адаптер до перезагрузки), лестницу обрывает.

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// lestnicaStorozha — паузы перед попытками подряд: первая почти сразу,
// дальше реже; последняя ступень повторяется.
var lestnicaStorozha = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 60 * time.Second}

// stabilnoPosle — ядро после подъёма сторожем проработало столько — лестница
// начинается сначала.
var stabilnoPosle = 5 * time.Minute

// zakrytZaraneeDo — паузы не длиннее этой запрет ставится сразу при падении
// (секунды до подъёма трафик не уходит мимо туннеля); на долгих ступенях
// лестницы — только на время самого подъёма: ядро, падающее раз за разом, не
// должно держать человека без интернета минутами.
var zakrytZaraneeDo = 15 * time.Second

// predelPopytok — столько неудачных подъёмов подряд на одно падение, и сторож
// бросает до слова человека или авторежима: ядро, которое не стартует вовсе
// (сломанный профиль), не должно молотить раз в минуту сутками.
var predelPopytok = 10

// shagObrezkiZhurnala — как часто сторож смотрит размер журнала ядра.
var shagObrezkiZhurnala = time.Minute

// storozhLestnica — где сторож на лестнице.
type storozhLestnica struct {
	mu       sync.Mutex
	stupen   int       // попыток подряд
	podnyalV time.Time // последний удачный подъём сторожем
}

// nachatStorozh — из Novaya: падения ядра идут в очередь сторожа.
func (s *Sluzhba) nachatStorozh() {
	s.padeniya = make(chan yadro.Padenie, 8)
	s.Yadro.PriPadenii = s.zametitPadenie
}

// zametitPadenie — PriPadenii ядра: в очередь, не задерживая ядро.
func (s *Sluzhba) zametitPadenie(p yadro.Padenie) {
	select {
	case s.padeniya <- p:
	default:
		log.Printf("сторож: очередь падений полна — %s", p.Opisanie())
	}
}

// StorozhYadra крутится, пока жива служба (cmd/kelevra): падения ядра — по
// одному, раз в минуту — размер журнала ядра.
func (s *Sluzhba) StorozhYadra(ctx context.Context) {
	if s.padeniya == nil {
		return
	}
	tik := time.NewTicker(shagObrezkiZhurnala)
	defer tik.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tik.C:
			s.obrezatZhurnalYadra()
		case p := <-s.padeniya:
			s.podnyatPoslePadeniya(ctx, p)
		}
	}
}

// obrezatZhurnalYadra — журнал ядра больше предела: прежнее уезжает в
// yadro.log.proshlyy (yadro.ObrezatZhurnal).
func (s *Sluzhba) obrezatZhurnalYadra() {
	if s.Yadro == nil {
		return
	}
	if ok, err := yadro.ObrezatZhurnal(s.Yadro.Papka, yadro.PredelZhurnala); err != nil {
		log.Printf("журнал ядра: не обрезал (%v)", err)
	} else if ok {
		log.Printf("журнал ядра больше %d МБ — прежнее записанное уехало в yadro.log.proshlyy", yadro.PredelZhurnala>>20)
	}
}

// sleduyushchayaPauza — пауза перед очередной попыткой и её номер подряд.
func (s *Sluzhba) sleduyushchayaPauza() (time.Duration, int) {
	l := &s.storozh
	l.mu.Lock()
	defer l.mu.Unlock()
	i := l.stupen
	if i >= len(lestnicaStorozha) {
		i = len(lestnicaStorozha) - 1
	}
	l.stupen++
	return lestnicaStorozha[i], l.stupen
}

// podnyatPoslePadeniya — одно падение: попытки по лестнице, пока ядро не
// встанет, человек не скажет своё или отказ не окажется безнадёжным.
func (s *Sluzhba) podnyatPoslePadeniya(ctx context.Context, p yadro.Padenie) {
	otkl := s.chisloOtklyucheniy()
	l := &s.storozh
	l.mu.Lock()
	if !l.podnyalV.IsZero() && time.Since(l.podnyalV) >= stabilnoPosle {
		l.stupen = 0
	}
	l.mu.Unlock()
	pervaya := true
	for popytka := 1; ; popytka++ {
		if popytka > predelPopytok {
			log.Printf("сторож: ядро не поднялось %d раз подряд — бросаю до слова человека или авторежима", predelPopytok)
			return
		}
		if prichina := s.storozhNeNuzhen(otkl); prichina != "" {
			log.Printf("сторож: %s — не поднимаю: %s", p.Opisanie(), prichina)
			return
		}
		pauza, nomer := s.sleduyushchayaPauza()
		if pervaya {
			log.Printf("сторож: %s; причина из журнала ядра: %s — поднимаю заново через %s (попытка %d подряд)",
				p.Opisanie(), slovoPrichiny(p.Hvost), sekundyStorozha(pauza), nomer)
			pervaya = false
		} else {
			log.Printf("сторож: следующая попытка через %s (попытка %d подряд)", sekundyStorozha(pauza), nomer)
		}
		zaranee := pauza <= zakrytZaraneeDo
		if zaranee {
			// Туннеля уже нет: до подъёма трафик мимо туннеля закрыт.
			s.zakrytMimoTunnelya()
		}
		if !s.zhdatPauzuStorozha(ctx, pauza, otkl, zaranee) {
			return
		}
		dalshe, err := s.podnyatStorozhem(ctx, otkl)
		if err == nil || !dalshe {
			return
		}
	}
}

// zhdatPauzuStorozha — пауза лестницы. false — ждать больше нечего: служба
// уходит, человек нажал «Отключить» или связь уже поднял кто-то другой
// (авторежим, кнопка). Стоящий запрет при этом снимается по итогу.
func (s *Sluzhba) zhdatPauzuStorozha(ctx context.Context, pauza time.Duration, otkl uint64, zakryto bool) bool {
	konec := time.NewTimer(pauza)
	defer konec.Stop()
	shag := time.NewTicker(200 * time.Millisecond)
	defer shag.Stop()
	for {
		select {
		case <-ctx.Done():
			if zakryto {
				s.otkrytMimoTunnelya("служба останавливается")
			}
			return false
		case <-konec.C:
			return true
		case <-shag.C:
			if s.uhodit.Load() {
				return false
			}
			if s.chisloOtklyucheniy() != otkl {
				// «Отключить» само сняло запрет (OpustitZashchitu).
				log.Printf("сторож: нажато «Отключить» — ядро не поднимаю")
				return false
			}
			if s.Yadro.Sost() == yadro.Rabotaet {
				log.Printf("сторож: связь уже поднята другим подъёмом — мой не нужен")
				if zakryto {
					s.otkrytMimoTunnelyaPoItogu(nil)
				}
				return false
			}
		}
	}
}

// podnyatStorozhem — сама попытка, под общим замком переходов. dalshe —
// стоит ли пробовать снова по лестнице.
func (s *Sluzhba) podnyatStorozhem(ctx context.Context, otkl uint64) (dalshe bool, err error) {
	if _, err := s.vzyatZamokPodyoma(ctx); err != nil {
		log.Printf("сторож: очередь переходов не дождался (%v)", err)
		s.otkrytMimoTunnelya("сторож не дождался очереди")
		return ctx.Err() == nil, err
	}
	defer s.zamokPodyoma.Unlock()
	if prichina := s.storozhNeNuzhen(otkl); prichina != "" {
		log.Printf("сторож: ядро не поднимаю: %s", prichina)
		if s.chisloOtklyucheniy() == otkl {
			s.otkrytMimoTunnelya(prichina)
		}
		return false, nil
	}
	if s.zashchitaPodnyata() {
		log.Printf("сторож: связь уже поднята другим подъёмом — мой не нужен")
		s.otkrytMimoTunnelyaPoItogu(nil)
		return false, nil
	}
	log.Printf("сторож: поднимаю ядро заново")
	s.zakrytMimoTunnelya()
	_ = s.Yadro.Ostanovit() // ядра нет; на случай гонки — не два рядом
	err = s.podnyatZashchituPodZamkom(ctx)
	s.otkrytMimoTunnelyaPoItogu(err)
	if err == nil {
		l := &s.storozh
		l.mu.Lock()
		l.podnyalV = time.Now()
		l.mu.Unlock()
		log.Printf("сторож: ядро снова работает")
		return false, nil
	}
	if bezNadezhdy(ctx, err) {
		log.Printf("сторож: ядро не поднялось (%v) — повтор не поможет, жду слова человека или авторежима", err)
		return false, err
	}
	log.Printf("сторож: ядро не поднялось: %v", err)
	return true, err
}

// storozhNeNuzhen — почему поднимать ядро сейчас не надо; пусто — надо.
func (s *Sluzhba) storozhNeNuzhen(otkl uint64) string {
	switch {
	case s.uhodit.Load():
		return "служба уходит"
	case s.chisloOtklyucheniy() != otkl:
		return "нажато «Отключить»"
	case !s.estPrava():
		return "нет прав администратора"
	case s.avtomatSchitaetDoma():
		return "авторежим: дома, туннель не нужен"
	}
	return ""
}

// avtomatSchitaetDoma — автомат включён и его обстановка «дома».
func (s *Sluzhba) avtomatSchitaetDoma() bool {
	s.avtorezhimZamok.Lock()
	defer s.avtorezhimZamok.Unlock()
	if !s.Nastroyki.Avtorezhim || s.avtorezhimEkz == nil || s.avtorezhimEkz.Zadvizhka == nil {
		return false
	}
	return s.avtorezhimEkz.Zadvizhka.Tekushcheye() == avtorezhim.Doma
}

// bezNadezhdy — отказ, который повтор по лестнице не вылечит.
func bezNadezhdy(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	for _, e := range []error{OshibkaNetPrav, OshibkaAdapterZanyat, OshibkaReshenieSmenilos,
		OshibkaRezhimSmenilsya, yadro.ErrNeZapuskalos, yadro.ErrOstanovleno} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

func slovoPrichiny(hvost string) string {
	if hvost == "" {
		return "журнал ядра пуст"
	}
	return hvost
}

func sekundyStorozha(d time.Duration) string {
	return d.String()
}
