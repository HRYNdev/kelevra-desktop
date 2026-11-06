package sluzhba

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Отложенный перезапуск ядра.
//
// Перезапуск, без которого связь работает (новый профиль с теми же узлами, но
// другими параметрами; новое ядро core-*; приём живого ядра со устаревшим не
// срочно конфигом — классификация в perezapusk_yadra.go), не делается сразу:
// каждый гасит TUN на секунды, а человек в этот миг может быть посреди звонка.
// Он ждёт ближайшего «естественного» момента:
//
//   - смена физической сети или пробуждение машины: соединения рвутся и так,
//     секунды перезапуска на этом фоне не видны. Ждём pauzaPosleSmenySeti —
//     сначала проверка «Дома» по смене сети (dom_smena_seti.go): она сама
//     может перезапустить ядро, и тогда отложенное уже выполнено;
//   - тихое окно: tishinaPoUmolchaniyu минут подряд через ядро прошло не
//     больше porogTishiny байт в минуту (счётчики Clash API /connections,
//     uploadTotal + downloadTotal) — человек не пользуется сетью;
//   - не позже srokPoUmolchaniyu с момента, когда перезапуск понадобился.
//
// Любой успешный подъём ядра (кнопка, авторежим, «Дом») берёт свежий конфиг и
// отложенное ядро сам — отложенное после него снимается (otlozhennyyVypolnen).
// Срочное (узел, на котором сидим, пропал; «Дом») идёт сразу, как раньше.

const (
	prichinaKonfig  = "konfig"
	prichinaYadro   = "yadro"
	prichinaPravila = "pravila" // наборы правил, которые ядро само не подхватит
)

// Пороги; пульт подстраивает (yadro.otlozhit_ne_dolshe_ch, yadro.tishina_min).
var (
	srokPoUmolchaniyu   = 6 * time.Hour
	tishinaPoUmolchaniyu = 10 * time.Minute
	// porogTishiny — байт в минуту, ниже которых минута считается тихой:
	// фон мессенджеров, проб «Дома» и замеров узлов — сотни байт и единицы
	// килобайт, страница сайта — сотни килобайт.
	porogTishiny int64 = 32 << 10
	// shagOtlozhennogo — шаг слежки (счётчики, срок, сон).
	shagOtlozhennogo = time.Minute
	// pauzaPosleSmenySeti — от смены сети до перезапуска.
	pauzaPosleSmenySeti = 20 * time.Second
)

// otlozhennyyPerezapusk — что ждёт перезапуска и с какого времени.
type otlozhennyyPerezapusk struct {
	mu sync.Mutex
	// zhdut — причина (prichinaKonfig, prichinaYadro) → с какого времени и
	// словами.
	zhdut map[string]zhdushchiy
	// otvergnutyy — отпечаток конфига, который ядро отвергло (check): тот же
	// конфиг снова не планируется.
	otvergnutyy string

	// Подмены для проверок; nil — настоящие.
	seychas        func() time.Time
	trafik         func() (int64, bool)
	sledchik       func() avtorezhim.Sledchik
	otpechatokSeti func() string
	perezapustit   func(prichina string) bool
	konfigVRabote  func() (yadro.KonfigRaboty, bool)
	// posleTika — тик обработан (проверки ждут его, прежде чем двигать часы).
	posleTika func()
}

type zhdushchiy struct {
	s       time.Time
	slovami string
}

func (o *otlozhennyyPerezapusk) chasy() time.Time {
	if o.seychas != nil {
		return o.seychas()
	}
	return time.Now()
}

func (o *otlozhennyyPerezapusk) snyat(prichina string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, est := o.zhdut[prichina]; est {
		delete(o.zhdut, prichina)
		log.Printf("отложенный перезапуск ядра: «%s» больше не нужен", map[string]string{prichinaKonfig: "новый конфиг", prichinaYadro: "новое ядро", prichinaPravila: "наборы правил"}[prichina])
	}
}

func (o *otlozhennyyPerezapusk) zapomnitOtvergnutyy(otpechatok string) {
	o.mu.Lock()
	o.otvergnutyy = otpechatok
	o.mu.Unlock()
}

// nuzhenS — ждёт ли что-нибудь и с какого времени (самое раннее); slovami —
// причины для журнала.
func (o *otlozhennyyPerezapusk) nuzhenS() (s time.Time, slovami string, nuzhen bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var chasti []string
	for _, p := range []string{prichinaKonfig, prichinaYadro, prichinaPravila} {
		z, est := o.zhdut[p]
		if !est {
			continue
		}
		if s.IsZero() || z.s.Before(s) {
			s = z.s
		}
		chasti = append(chasti, z.slovami)
	}
	return s, strings.Join(chasti, "; "), len(chasti) > 0
}

// otlozhitPerezapusk — перезапуск нужен, но не срочно. otpechatok — конфига,
// ради которого (пусто — не конфиг): отвергнутый ядром не планируется.
// Повтор той же причины время ожидания не сдвигает: срок считается с первой.
func (s *Sluzhba) otlozhitPerezapusk(prichina, slovami, otpechatok string) {
	o := &s.otlozh
	t := o.chasy()
	o.mu.Lock()
	if otpechatok != "" && otpechatok == o.otvergnutyy {
		o.mu.Unlock()
		return
	}
	if o.zhdut == nil {
		o.zhdut = map[string]zhdushchiy{}
	}
	z, bylo := o.zhdut[prichina]
	if !bylo {
		z.s = t
	}
	z.slovami = slovami
	o.zhdut[prichina] = z
	o.mu.Unlock()
	if !bylo {
		log.Printf("перезапуск ядра отложен (%s): при смене сети или пробуждении, в тихое окно (%d мин без трафика) или не позже %s",
			slovami, int(s.tishinaOtlozhennogo()/time.Minute), z.s.Add(s.srokOtlozhennogo()).Format("15:04"))
	}
}

// otlozhitDlyaYadra — новое ядро core-* готово, а ядро работает: замена при
// ближайшем перезапуске, но не позже срока.
func (s *Sluzhba) otlozhitDlyaYadra(teg string) {
	if s.Yadro == nil || s.Yadro.PID() == "" {
		return // ядро стоит — новое встанет при ближайшем подъёме само
	}
	s.otlozhitPerezapusk(prichinaYadro, "новое ядро "+teg, "")
}

// otlozhennyyVypolnen — ядро поднято на свежем конфиге: ждать нечего.
func (s *Sluzhba) otlozhennyyVypolnen() {
	s.otlozh.mu.Lock()
	n := len(s.otlozh.zhdut)
	s.otlozh.zhdut = nil
	s.otlozh.mu.Unlock()
	if n > 0 {
		log.Printf("отложенный перезапуск ядра выполнен попутно: ядро поднято на свежем конфиге")
	}
}

func (s *Sluzhba) srokOtlozhennogo() time.Duration {
	return s.pult.Dlitelnost("yadro.otlozhit_ne_dolshe_ch", time.Hour, srokPoUmolchaniyu)
}

func (s *Sluzhba) tishinaOtlozhennogo() time.Duration {
	return s.pult.Dlitelnost("yadro.tishina_min", time.Minute, tishinaPoUmolchaniyu)
}

// VestiOtlozhennyyPerezapusk — слежка за естественным моментом, пока жив ctx.
func (s *Sluzhba) VestiOtlozhennyyPerezapusk(ctx context.Context) {
	o := &s.otlozh
	sobrat := o.sledchik
	if sobrat == nil {
		sobrat = func() avtorezhim.Sledchik { return avtorezhim.NovySledchik() }
	}
	sl := sobrat()
	defer sl.Stop()
	t := time.NewTicker(shagOtlozhennogo)
	defer t.Stop()
	s.vestiOtlozhennyy(ctx, sl.Sobytiya(), t.C, time.After)
}

func (s *Sluzhba) otpechatokSetiOtlozh() string {
	if s.otlozh.otpechatokSeti != nil {
		return s.otlozh.otpechatokSeti()
	}
	return avtorezhim.OtpechatokSeti()
}

func (s *Sluzhba) trafikOtlozh() (int64, bool) {
	if s.otlozh.trafik != nil {
		return s.otlozh.trafik()
	}
	if s.Yadro == nil {
		return 0, false
	}
	t, err := s.Yadro.Trafik()
	if err != nil {
		return 0, false
	}
	return t.VverhBayt + t.VnizBayt, true
}

// slezhkaOtlozh — состояние слежки за естественным моментом.
type slezhkaOtlozh struct {
	s     *Sluzhba
	ctx   context.Context
	posle func(time.Duration) <-chan time.Time

	proshlayaSet string // отпечаток физической сети
	propadala    bool   // сеть пропадала с прошлого взгляда
	zhduSet      <-chan time.Time
	ostalos      int // ещё взглядов, пока сети нет
	moment       <-chan time.Time
	chtoZaMoment string

	proshlyyTik    time.Time
	proshlyyTrafik int64
	estTrafik      bool
	tiho           time.Duration // подряд тихо
}

// vestiOtlozhennyy — тело слежки: события сети, тики шага, ожидание через
// posle (в проверках — без настоящего ожидания).
func (s *Sluzhba) vestiOtlozhennyy(ctx context.Context, sobytiya <-chan struct{}, tiki <-chan time.Time, posle func(time.Duration) <-chan time.Time) {
	sl := &slezhkaOtlozh{s: s, ctx: ctx, posle: posle, proshlyyTik: s.otlozh.chasy()}
	sl.proshlayaSet = s.otpechatokSetiOtlozh()
	sl.propadala = sl.proshlayaSet == ""
	for {
		select {
		case <-ctx.Done():
			return
		case _, otkryt := <-sobytiya:
			if !otkryt {
				sobytiya = nil // слежение за сетью не завелось — остаются тики
				continue
			}
			// Пачка событий одной смены — один взгляд после тишины.
			sl.zhduSet, sl.ostalos = posle(pauzaPosleSobytiyaSeti), popytokOzhidaniyaFizSeti
		case <-sl.zhduSet:
			sl.zhduSet = nil
			sl.posmotretSet()
		case <-sl.moment:
			sl.moment = nil
			if s.otpechatokSetiOtlozh() == "" {
				continue // сети нет — подождём её возврата (новое событие)
			}
			sl.popytka(sl.chtoZaMoment)
		case tik := <-tiki:
			sl.tik(tik)
			if s.otlozh.posleTika != nil {
				s.otlozh.posleTika()
			}
		}
	}
}

// naznachitMoment — естественный момент через pauzaPosleSmenySeti, если
// что-то ждёт и момент ещё не назначен.
func (sl *slezhkaOtlozh) naznachitMoment(chto string) {
	if _, _, nuzhen := sl.s.otlozh.nuzhenS(); nuzhen && sl.moment == nil {
		sl.moment, sl.chtoZaMoment = sl.posle(pauzaPosleSmenySeti), chto
	}
}

// posmotretSet — после событий: сменилась ли физическая сеть (свой TUN и
// IPv6 её не меняют) или вернулась после пропажи.
func (sl *slezhkaOtlozh) posmotretSet() {
	tek := sl.s.otpechatokSetiOtlozh()
	if tek == "" {
		sl.propadala = true
		if sl.ostalos > 0 {
			sl.ostalos--
			sl.zhduSet = sl.posle(shagOzhidaniyaFizSeti)
		}
		return
	}
	if tek == sl.proshlayaSet && !sl.propadala {
		return
	}
	sl.proshlayaSet, sl.propadala = tek, false
	sl.naznachitMoment("сеть сменилась")
}

// tik — шаг: пробуждение, срок, тихое окно.
func (sl *slezhkaOtlozh) tik(tik time.Time) {
	s, o := sl.s, &sl.s.otlozh
	seychas := o.chasy()
	if prosnulsya(sl.proshlyyTik, tik, seychas, shagOtlozhennogo) {
		sl.tiho, sl.estTrafik = 0, false
		sl.naznachitMoment("после сна")
	}
	interval := seychas.Sub(sl.proshlyyTik)
	sl.proshlyyTik = seychas
	nachalo, _, nuzhen := o.nuzhenS()
	if !nuzhen {
		sl.tiho, sl.estTrafik = 0, false
		return
	}
	if seychas.Sub(nachalo) >= s.srokOtlozhennogo() {
		sl.popytka(fmt.Sprintf("срок %s вышел", s.srokOtlozhennogo()))
		return
	}
	vsego, ok := s.trafikOtlozh()
	switch {
	case !ok:
		sl.tiho = 0
	case sl.estTrafik && vsego >= sl.proshlyyTrafik && interval > 0 &&
		(vsego-sl.proshlyyTrafik)*int64(time.Minute)/int64(interval) <= porogTishiny:
		sl.tiho += interval
	default:
		sl.tiho = 0
	}
	sl.proshlyyTrafik, sl.estTrafik = vsego, ok
	if sl.tiho >= s.tishinaOtlozhennogo() {
		sl.popytka(fmt.Sprintf("тихое окно: %d мин без трафика", int(sl.tiho/time.Minute)))
	}
}

// popytka — перезапустить, если что-то ждёт. Не вышло (занято) — ждёт
// дальше: следующий тик или момент попробует снова.
func (sl *slezhkaOtlozh) popytka(kogda string) {
	s, o := sl.s, &sl.s.otlozh
	_, slovami, nuzhen := o.nuzhenS()
	if !nuzhen {
		return
	}
	sdelat := o.perezapustit
	if sdelat == nil {
		sdelat = func(prichina string) bool {
			return s.perezapustitNaNovom(sl.ctx, prichina, s.pokolenieAvtorezhima())
		}
	}
	if sdelat(fmt.Sprintf("отложенный перезапуск (%s) — %s", slovami, kogda)) {
		sl.tiho, sl.estTrafik = 0, false
	}
}
