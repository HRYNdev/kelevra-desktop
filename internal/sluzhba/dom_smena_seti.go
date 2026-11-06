package sluzhba

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
)

// Проверка «Дома» по смене физической сети — не дожидаясь трёх неудачных
// проб слежки (lechitDom).
//
// Зачем. Замер 06.11: Wi-Fi выключили на 45 с и включили, ядро смены сети не
// заметило (строки «network: updated default interface» нет), и соединение
// моста под WireGuard осталось молча мёртвым — «Дом» висел, пока его не
// перезапустили. Сброс сети ядра (Clash API DELETE /connections) живьём
// «Дом» не оживил: чтение моста XHTTP после молчаливого обрыва не
// просыпается, а второй сброс завис по сроку (06.11, 0.6.100). Оживил
// перезапуск ядра — с 0.6.98 он идёт под запретом трафика мимо туннеля, так
// что мимо туннеля ничего не уходит. Поэтому по смене сети: пауза
// srokPoslePodyomaYadra — сколько после подъёма ядра проверка по смене сети
// молчит: новый адаптер туннеля сам рождает событие смены адресов, а «Дом»
// поднимается ~10 с (живьём 06.11: перезапуск по пробам в 22:35:18 и тут же
// второй — по событию от своего же адаптера в 22:35:22).
var srokPoslePodyomaYadra = 30 * time.Second

// pauzaPeredProboyPoSmene (мосту дать время на рукопожатие), одна проба
// «Дома»; ответил — ядро не трогаем, не ответил — перезапуск ядра сразу.
//
// О смене сети служба узнаёт тем же событием, что авторежим
// (avtorezhim.SledchikSeti: NotifyAddrChange — изменилась таблица IP-адресов
// машины). Своя слежка, а не служитель авторежима: тот крутится только при
// включённом авторежиме, а «Дом» нужен и без него.
//
// Событие NotifyAddrChange приходит и от нашего TUN (подъём и перезапуск
// ядра), и от адресов IPv6, и пачкой на одну смену. Поэтому решает не само
// событие, а отпечаток физической сети (avtorezhim.OtpechatokSeti: адаптер,
// свой IPv4 и шлюз у физических адаптеров): проверка — только когда сеть
// есть и она другая, чем была, или между событиями пропадала. Сеть пропала
// (Wi-Fi выключен) — проверять нечего: ждём, пока вернётся.
//
// Проверка идёт в самом цикле слежки (до ~12 с: пауза и две попытки пробы).
// События за это время не теряются и не копятся: следчик шлёт их без
// очереди (канал на одно событие, лишние роняет), так что смены за время
// проверки цикл увидит одним событием сразу после неё. Второй перезапуск
// рядом не начнётся: на время проверки и перезапуска стоит d.lechim, а
// между проверками — отсечка srokMezhduProverkamiPoSmene.

// srokMezhduProverkamiPoSmene — проверка по смене сети не чаще этого:
// пачка событий одной смены — одна проверка, а дребезжащий Wi-Fi не
// перезапускает ядро каждые несколько секунд.
const srokMezhduProverkamiPoSmene = 30 * time.Second

// pauzaPeredProboyPoSmene — сколько дать мосту «Дома» после смены сети до
// пробы: рукопожатие WireGuard идёт раз в 5 с. Подмена для проверок.
var pauzaPeredProboyPoSmene = 6 * time.Second

// pauzaPosleSobytiyaSeti — тишина после последнего события, прежде чем
// смотреть сеть: пачка событий одной смены схлопывается в одну проверку,
// а адаптер успевает получить адрес и шлюз.
var pauzaPosleSobytiyaSeti = 3 * time.Second

// shagOzhidaniyaFizSeti, popytokOzhidaniyaFizSeti — сети после события нет
// (адрес уже выдан, шлюза ещё нет; Wi-Fi только что выключили): смотреть ещё
// столько раз с таким шагом. Новых событий может и не прийти — шлюз
// появляется без смены адреса.
var (
	shagOzhidaniyaFizSeti    = 2 * time.Second
	popytokOzhidaniyaFizSeti = 15
)

// sleditZaSmenoySeti — слежка за сменой физической сети ради «Дома»;
// крутится, пока жив ctx (SleditZaDomom).
func (s *Sluzhba) sleditZaSmenoySeti(ctx context.Context) {
	sobrat := s.dom.sledchik
	if sobrat == nil {
		sobrat = func() avtorezhim.Sledchik { return avtorezhim.NovySledchik() }
	}
	sl := sobrat()
	defer sl.Stop()
	s.sleditZaSobytiyamiSeti(ctx, sl.Sobytiya(), time.After)
}

// otpechatokFizSeti — отпечаток физической сети (подмена d.otpechatokSeti).
func (s *Sluzhba) otpechatokFizSeti() string {
	if s.dom.otpechatokSeti != nil {
		return s.dom.otpechatokSeti()
	}
	return avtorezhim.OtpechatokSeti()
}

// sleditZaSobytiyamiSeti — тело слежки: события из sobytiya, ожидание через
// posle (в проверках — без настоящего ожидания). Канал закрыт (слежение в
// системе не завелось) — выходит: тогда «Дом» лечат одни пробы.
func (s *Sluzhba) sleditZaSobytiyamiSeti(ctx context.Context, sobytiya <-chan struct{}, posle func(time.Duration) <-chan time.Time) {
	proshlyy := s.otpechatokFizSeti()
	propadala := proshlyy == ""
	var zhdu <-chan time.Time
	ostalos := 0
	for {
		select {
		case <-ctx.Done():
			return
		case _, otkryt := <-sobytiya:
			if !otkryt {
				return
			}
			// Каждое новое событие отодвигает проверку: пачка — одна проверка.
			zhdu, ostalos = posle(pauzaPosleSobytiyaSeti), popytokOzhidaniyaFizSeti
		case <-zhdu:
			zhdu = nil
			tekushchiy := s.otpechatokFizSeti()
			if tekushchiy == "" {
				propadala = true
				if ostalos > 0 {
					ostalos--
					zhdu = posle(shagOzhidaniyaFizSeti)
				}
				continue
			}
			if tekushchiy == proshlyy && !propadala {
				continue // свой TUN, IPv6, повтор той же сети — физическая сеть та же
			}
			chto := "сеть вернулась"
			if !propadala {
				chto = "другая сеть"
			}
			proshlyy, propadala = tekushchiy, false
			s.proveritDomPoSmeneSeti(ctx, chto)
		}
	}
}

// domMozhnoProveryat — туннель поднят с «Домом» (блок есть, дом в конфиге,
// ядро работает) и подъём связи не идёт.
func (s *Sluzhba) domMozhnoProveryat() bool {
	if s.dom.kesh.Blok() == nil {
		return false
	}
	s.zamok.Lock()
	vKonfige, idet := s.kartina.Dom, s.podyomIdet
	s.zamok.Unlock()
	return vKonfige && !idet && s.yadroRabotaetDlyaDoma()
}

// proveritDomPoSmeneSeti — по смене физической сети: если туннель поднят с
// «Домом», лечение, подъём и перезапуск не идут и прошлая проверка была не
// ближе srokMezhduProverkamiPoSmene — пауза pauzaPeredProboyPoSmene, проба
// «Дома»; ответил — ядро не трогаем, не ответил — перезапуск ядра
// (lechitPerezapuskom). Неудачные пробы до смены сети не в счёт: дальше
// обычное самоизлечение с начала счёта. ctx отменён (служба
// останавливается) — выходит сразу, ничего не трогая.
func (s *Sluzhba) proveritDomPoSmeneSeti(ctx context.Context, chto string) {
	d := s.dom
	if !s.domMozhnoProveryat() {
		return
	}
	// Подъём или перезапуск ядра идёт — новое ядро и так начнёт с нового
	// соединения моста.
	if !s.zamokPodyoma.TryLock() {
		return
	}
	s.zamokPodyoma.Unlock()
	t := s.chasyDoma()
	d.mu.Lock()
	if d.lechim {
		d.mu.Unlock()
		return
	}
	if !d.yadroPodnyatoV.IsZero() && t.Sub(d.yadroPodnyatoV) < srokPoslePodyomaYadra {
		proshlo := t.Sub(d.yadroPodnyatoV).Round(time.Second)
		d.mu.Unlock()
		log.Printf("дом: сеть сменилась (%s), но ядро поднято %d с назад — «Дом» ещё поднимается, не трогаю", chto, int(proshlo/time.Second))
		return
	}
	if !d.proverkaPoSmene.IsZero() && t.Sub(d.proverkaPoSmene) < srokMezhduProverkamiPoSmene {
		proshlo := t.Sub(d.proverkaPoSmene).Round(time.Second)
		d.mu.Unlock()
		log.Printf("дом: сеть сменилась (%s), но дом проверяли %d с назад — не трогаю", chto, int(proshlo/time.Second))
		return
	}
	d.lechim, d.proverkaPoSmene = true, t
	proba := d.proba
	d.mu.Unlock()
	otboy := func() {
		d.mu.Lock()
		d.lechim = false
		d.mu.Unlock()
	}

	taymer := time.NewTimer(pauzaPeredProboyPoSmene)
	select {
	case <-ctx.Done():
		taymer.Stop()
		otboy()
		return
	case <-taymer.C:
	}
	// За паузу человек мог нажать «Отключить», ядро — упасть, подъём — начаться.
	b := d.kesh.Blok()
	if b == nil || !s.domMozhnoProveryat() {
		otboy()
		return
	}
	if proba == nil {
		proba = probaDnsDoma
	}
	zhiv := false
	for i := 0; i < 2 && !zhiv && ctx.Err() == nil; i++ {
		pctx, otmena := context.WithTimeout(ctx, 3*time.Second)
		zhiv = proba(pctx, b.DNS, b.Zona) == nil
		otmena()
	}
	if ctx.Err() != nil {
		otboy()
		return
	}
	d.mu.Lock()
	d.neudachPodryad = 0
	if zhiv {
		d.lechim = false
		d.mu.Unlock()
		log.Printf("дом: сеть сменилась (%s), дом на связи — ядро не трогаю", chto)
		return
	}
	perezapustit := d.perezapustit
	most := d.most
	d.mu.Unlock()
	// Выход моста сам не отвечает (сеть ещё не ожила, узел лёг) — перезапуск
	// ядра «Дом» не вернёт, а дребезг Wi-Fi дёргал бы ядро каждые 30 с. Как в
	// lechitDom: не трогаем, дальше решат пробы и лестница.
	if most == nil {
		most = s.mostOtvechaet
	}
	if zhivMost, teg := most(ctx); !zhivMost {
		otboy()
		log.Printf("дом: сеть сменилась (%s), дом не отвечает, но и мост «%s» молчит — ядро не трогаю", chto, teg)
		return
	}
	// d.lechim снимет lechitPerezapuskom по окончании перезапуска.
	s.lechitPerezapuskom(perezapustit, fmt.Sprintf("сеть сменилась (%s), дом не отвечает — перезапускаю ядро", chto))
}

// otmetitPodyomYadra — служба только что подняла ядро (см. yadroPodnyatoV).
func (s *Sluzhba) otmetitPodyomYadra() {
	if s.dom == nil {
		return
	}
	t := s.chasyDoma()
	s.dom.mu.Lock()
	s.dom.yadroPodnyatoV = t
	s.dom.mu.Unlock()
}
