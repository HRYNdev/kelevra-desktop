package sluzhba

import (
	"context"
	"errors"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// Фоновые сетевые дела службы (обновление профиля, сверка правил, фоновая
// проверка обновления) и сон машины.
//
// Зачем. Разбор журналов 19.10.2026: все эти дела крутились на голых тикерах
// без повтора. Пока ноутбук спит, тикер копит пропущенный тик и отдаёт его в
// первую же секунду после пробуждения — Wi-Fi в этот момент ещё не поднялся,
// Windows отвечает «lookup …: no such host», ядро в те же секунды пишет
// «missing default interface». Следующая попытка была только на следующем
// тике (профиль — через час, обновление — через четыре), а к тому времени
// ноутбук снова спал. Так у человека неделями не менялись ни профиль, ни
// правила, хотя машина включалась каждый день.
//
// Лечится двумя вещами, обе здесь, общие на все три дела:
//   - сетевой отказ повторяется сам с нарастающей паузой (pauzaPovtora), а не
//     ждёт следующего тика;
//   - тик, выдающий сон (prosnulsya), сперва ждёт сеть (dozhdatsyaSeti) и
//     только потом идёт в неё.
//
// Это та же мысль, что у журналов (PovtorPosleOtkazaDoPeredachi: отказ до
// первого байта почти всегда значит «сеть ещё не поднялась после сна»), и тот
// же разбор ошибок по типам (otkazDoPeredachi). Журналы своего расписания не
// меняют: у них цена повтора — чужой трафик, и правило «не чаще тика» там
// оплачено отдельно.
//
// Обходного резолва мимо туннеля (DoH, зашитые адреса) здесь нет нарочно:
// это утечка запросов мимо защиты и не лечит причину — сети после сна просто
// ещё нет, и ни один резолвер её не поднимет.

// stupeniPovtora — паузы между повторами после сетевого отказа. Первая
// короткая: Wi-Fi после сна поднимается за секунды-десятки секунд. Дальше
// реже, чтобы машина без сети вовсе (самолёт, сломанный роутер) не долбила
// сервер каждые полминуты часами.
var stupeniPovtora = []time.Duration{
	15 * time.Second,
	30 * time.Second,
	time.Minute,
	2 * time.Minute,
	5 * time.Minute,
	10 * time.Minute,
	potolokPovtora,
}

// potolokPovtora — реже этого повтор не уходит: штатные тики дел — от
// получаса до четырёх часов, и пауза длиннее четверти часа снова оставила бы
// человека ждать до следующего тика.
const potolokPovtora = 15 * time.Minute

// razbrosPovtora — ±20% к паузе. Без разброса все машины, проснувшиеся после
// одного и того же сбоя (лёг сервер, лёг провайдер), стучались бы обратно
// одной волной в одну и ту же секунду.
const razbrosPovtora = 0.2

// srokOzhidaniyaSeti — сколько тик после сна ждёт сеть, прежде чем идти как
// есть. Минута с запасом покрывает подъём Wi-Fi после сна (обычно 3-15 с);
// не дождались — дальше работает обычный повтор, а не вечное ожидание.
var srokOzhidaniyaSeti = 60 * time.Second

// shagOzhidaniyaSeti — как часто смотреть, появилась ли сеть. Взгляд дешёвый
// (таблица адаптеров системы, без единого пакета в сеть).
var shagOzhidaniyaSeti = 2 * time.Second

// porogSna — опоздание, которое не объяснить ничем, кроме остановки
// процесса: планировщик Go и нагрузка дают миллисекунды, а не полминуты.
const porogSna = 30 * time.Second

// pauzaPovtora — пауза перед повтором номер stupen (с нуля). sluchay — число
// из [0, 1): 0.5 даёт ровно ступень, края — ±razbrosPovtora. Параметром, а не
// rand внутри: лестница проверяется таблицей без случайности.
func pauzaPovtora(stupen int, sluchay float64) time.Duration {
	if stupen < 0 {
		stupen = 0
	}
	if stupen >= len(stupeniPovtora) {
		stupen = len(stupeniPovtora) - 1
	}
	osnova := stupeniPovtora[stupen]
	pauza := time.Duration(float64(osnova) * (1 + razbrosPovtora*(2*sluchay-1)))
	if pauza > potolokPovtora {
		pauza = potolokPovtora
	}
	return pauza
}

// setevoyOtkaz — ответа не было вовсе: имя не разрешилось, соединение не
// установилось или оборвалось, истёк срок. Только такой отказ стоит
// повторять скоро: сеть после сна поднимется сама.
//
// Ответ сервера (4xx/5xx, незнакомый код доступа, непонятное тело) и
// отвергнутая подпись — НЕ сетевой отказ: повтор через 15 секунд получит то
// же самое, только лишний раз постучит в сервер. Им — штатный тик.
//
// Разбор по типам ошибок, а не по тексту — по той же причине, что в
// otkazDoPeredachi: текст у сетевых ошибок разный на разных системах и языках.
func setevoyOtkaz(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, obnovlenie.ErrPodpis) {
		return false
	}
	var kod *podpiska.OshibkaKoda
	if errors.As(err, &kod) {
		return false
	}
	var status *obnovlenie.OshibkaStatusa
	if errors.As(err, &status) {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false // служба гасится — это не сеть
	}
	if otkazDoPeredachi(err) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var setevaya net.Error
	if errors.As(err, &setevaya) && setevaya.Timeout() {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true // соединение закрыли, не ответив: туннель или сеть перестраивались
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) // соединение оборвалось на чтении/записи
}

// korotkoOSeti — причина сетевого отказа для строки журнала. Свои короткие
// слова, а не err.Error(): текст ошибки http-клиента несёт полный адрес
// запроса, а в адресе подписки — код доступа человека. Журнал уезжает
// разработчику, и код в нём — рабочий доступ к чужой подписке (см.
// podpiska.Maska и adresDlyaZhurnala — та же забота).
func korotkoOSeti(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.Name != "" {
			return "имя " + dnsErr.Name + " не разрешилось"
		}
		return "имя не разрешилось"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "истёк срок"
	}
	var setevaya net.Error
	if errors.As(err, &setevaya) && setevaya.Timeout() {
		return "истёк срок"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Op == "dial" {
			return "соединение не установилось"
		}
		return "соединение оборвалось"
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "соединение оборвалось"
	}
	return "нет ответа"
}

// prosnulsya — пришедший тик выдаёт, что машина спала.
//
// Три признака, потому что часы во сне ведут себя по-разному:
//   - между тиками по стенным часам прошло больше полутора периодов (и это
//     заметно больше porogSna — иначе разогнанный стендом тикер в
//     миллисекунды «просыпался» бы от каждой паузы сборщика мусора);
//   - тик доставлен намного позже, чем был назначен: монотонные часы шли во
//     сне, срок тика истёк, пока процесс стоял, и тикер отдал его в первую
//     секунду после пробуждения (tik — назначенное время, которое кладёт в
//     канал сам тикер);
//   - стенные часы ушли вперёд монотонных: монотонные во сне стояли.
func prosnulsya(proshlyy, tik, seychas time.Time, period time.Duration) bool {
	if proshlyy.IsZero() || period <= 0 {
		return false
	}
	stennye := seychas.Round(0).Sub(proshlyy.Round(0))
	if stennye > period*3/2 && stennye > period+porogSna {
		return true
	}
	if !tik.IsZero() && seychas.Sub(tik) > porogSna {
		return true
	}
	return stennye-seychas.Sub(proshlyy) > porogSna
}

// dozhdatsyaSeti ждёт, пока у машины появится сеть, не дольше srok.
//
// Признак сети — шлюз по умолчанию на физическом адаптере
// (avtorezhim.EstShlyuz): ровно его отсутствие ядро и называет «missing
// default interface», и ровно он появляется, когда Wi-Fi после сна получил
// адрес. Пробный резолв признаком не взят: при поднятом туннеле его ответ
// даёт наше же ядро, а без туннеля — домашний резолвер, который умеет
// молчать минутами (замер 28.08 в шапке avtorezhim.MakiShlyuzov).
//
// posle — чем ждать между взглядами (в проверках — без настоящего ожидания).
func dozhdatsyaSeti(ctx context.Context, est func() bool, srok, shag time.Duration, posle func(time.Duration) <-chan time.Time) bool {
	if shag <= 0 {
		shag = time.Second
	}
	for proshlo := time.Duration(0); ; proshlo += shag {
		if est() {
			return true
		}
		if proshlo >= srok {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-posle(shag):
		}
	}
}

// fonovoeDelo — одно фоновое сетевое дело службы: штатный тик плюс повтор
// после сетевого отказа.
//
// Всё дело крутится в ОДНОЙ горутине (krutit), и попытка делается прямо в
// ней. Отсюда «одна попытка за раз» без замков: пока попытка идёт, ни тик,
// ни повтор не могут начать вторую, а тикер Go лишние тики не копит.
type fonovoeDelo struct {
	// imya — как дело называется в журнале: «обновление профиля» и т.п.
	imya   string
	period time.Duration
	// srazu — первая попытка при запуске, не дожидаясь тика.
	srazu bool
	// sdelat — одна попытка. Ошибку разбирает setevoyOtkaz: сетевая —
	// повтор, любая другая (или удача) — ждать штатного тика.
	sdelat func(ctx context.Context) error
	// zhdatSet — подождать сеть после сна; false — не дождались. nil — не ждать.
	zhdatSet func(ctx context.Context) (bool, time.Duration)

	// Подмены для проверок; nil — настоящие часы и случайность.
	tiki    <-chan time.Time
	posle   func(time.Duration) <-chan time.Time
	seychas func() time.Time
	sluchay func() float64
}

func (d fonovoeDelo) krutit(ctx context.Context) {
	posle, seychas, sluchay := d.posle, d.seychas, d.sluchay
	if posle == nil {
		posle = time.After
	}
	if seychas == nil {
		seychas = time.Now
	}
	if sluchay == nil {
		sluchay = rand.Float64
	}
	tiki := d.tiki
	if tiki == nil {
		t := time.NewTicker(d.period)
		defer t.Stop()
		tiki = t.C
	}

	stupen := 0
	var povtor <-chan time.Time // nil — повтора не назначено, select его не слушает
	popytka := func(posleSna bool) {
		if posleSna && d.zhdatSet != nil {
			if est, skolko := d.zhdatSet(ctx); est {
				log.Printf("%s: машина проснулась, сеть появилась через %s", d.imya, okruglit(skolko))
			} else {
				log.Printf("%s: машина проснулась, сети нет и через %s — пробую как есть", d.imya, okruglit(skolko))
			}
		}
		if ctx.Err() != nil {
			return
		}
		err := d.sdelat(ctx)
		if err == nil || ctx.Err() != nil || !setevoyOtkaz(err) {
			stupen, povtor = 0, nil
			return
		}
		pauza := pauzaPovtora(stupen, sluchay())
		stupen++
		log.Printf("%s: сеть не ответила (%s), повтор через %s", d.imya, korotkoOSeti(err), okruglit(pauza))
		povtor = posle(pauza)
	}

	proshlyy := seychas()
	if d.srazu {
		popytka(false)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case tik := <-tiki:
			s := seychas()
			posleSna := prosnulsya(proshlyy, tik, s, d.period)
			proshlyy = s
			// Штатный тик — свой заход: лестница повторов начинается заново,
			// а назначенный повтор снимается, чтобы не пойти в сеть второй
			// раз через минуту после тика.
			stupen, povtor = 0, nil
			popytka(posleSna)
		case <-povtor:
			povtor = nil
			popytka(false)
		}
	}
}

// delo собирает фоновое дело с ожиданием сети на настоящих (или стендовых)
// признаках этой службы.
func (s *Sluzhba) delo(imya string, period time.Duration, sdelat func(context.Context) error) fonovoeDelo {
	return fonovoeDelo{
		imya:   imya,
		period: period,
		sdelat: sdelat,
		zhdatSet: func(ctx context.Context) (bool, time.Duration) {
			est := s.estSetDlyaStenda
			if est == nil {
				est = avtorezhim.EstShlyuz
			}
			nachalo := time.Now()
			ok := dozhdatsyaSeti(ctx, est, srokOzhidaniyaSeti, shagOzhidaniyaSeti, time.After)
			return ok, time.Since(nachalo)
		},
	}
}
