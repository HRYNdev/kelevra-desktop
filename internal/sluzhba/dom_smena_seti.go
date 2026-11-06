package sluzhba

import (
	"context"
	"log"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
)

// Сброс сети ядра по смене физической сети — не дожидаясь трёх неудачных
// проб «Дома» (lechitDom).
//
// Зачем. Замер 06.11: Wi-Fi выключили на 45 с и включили, ядро смены сети не
// заметило (строки «network: updated default interface» нет), и соединение
// моста под WireGuard осталось молча мёртвым — «Дом» висел, пока его не
// перезапустили. Когда ядро смену замечает само (20:15 того же дня), оно
// зовёт InterfaceUpdated у эндпоинта, bind передиаливает, и «Дом» оживает
// через секунды. Служба узнаёт о смене сети тем же событием, что авторежим
// (avtorezhim.SledchikSeti: NotifyAddrChange — изменилась таблица IP-адресов
// машины), и делает за ядро то же самое: Clash API DELETE /connections
// (yadro.SbrositSet → ResetNetwork → InterfaceUpdated у эндпоинтов).
//
// Своя слежка, а не служитель авторежима: тот крутится только при
// включённом авторежиме, а «Дом» нужен и без него.
//
// Событие NotifyAddrChange приходит и от нашего TUN (подъём и перезапуск
// ядра), и от адресов IPv6, и пачкой на одну смену. Поэтому решает не само
// событие, а отпечаток физической сети (avtorezhim.OtpechatokSeti: адаптер,
// свой IPv4 и шлюз у физических адаптеров): сброс — только когда сеть есть и
// она другая, чем была, или между событиями пропадала. Сеть пропала (Wi-Fi
// выключен) — сбрасывать нечего: ждём, пока вернётся.

// srokMezhduSbrosami — сброс по смене сети не чаще этого (и не раньше этого
// после сброса самоизлечения): пачка событий одной смены — один сброс, а
// дребезжащий Wi-Fi не рвёт соединения людей каждые несколько секунд.
const srokMezhduSbrosami = 30 * time.Second

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
			s.sbrositSetPoSmeneSeti(chto)
		}
	}
}

// sbrositSetPoSmeneSeti — сброс сети ядра по смене физической сети, если
// туннель поднят с «Домом», лечение и подъём не идут и прошлый сброс был не
// ближе srokMezhduSbrosami. Удался — неудачные пробы до смены сети не в
// счёт (как после сна): у сброса есть три пробы, чтобы «Дом» ожил, дальше —
// обычное самоизлечение. Не удался — ничего: решат пробы.
func (s *Sluzhba) sbrositSetPoSmeneSeti(chto string) {
	d := s.dom
	if d.kesh.Blok() == nil {
		return
	}
	s.zamok.Lock()
	vKonfige, idet := s.kartina.Dom, s.podyomIdet
	s.zamok.Unlock()
	if !vKonfige || idet || !s.yadroRabotaetDlyaDoma() {
		return
	}
	t := s.chasyDoma()
	d.mu.Lock()
	if d.lechim {
		d.mu.Unlock()
		return
	}
	if !d.poslSbros.IsZero() && t.Sub(d.poslSbros) < srokMezhduSbrosami {
		proshlo := t.Sub(d.poslSbros).Round(time.Second)
		d.mu.Unlock()
		log.Printf("дом: сеть сменилась (%s), но сеть ядра сбрасывали %d с назад — не сбрасываю", chto, int(proshlo/time.Second))
		return
	}
	// Подъём или перезапуск ядра идёт — новое ядро и так начнёт с чистой сети.
	if !s.zamokPodyoma.TryLock() {
		d.mu.Unlock()
		return
	}
	d.lechim = true
	d.mu.Unlock()
	log.Printf("дом: сеть сменилась — сбрасываю сеть ядра, туннель не трогаю (%s)", chto)
	err := s.sbrositSetYadra()
	s.zamokPodyoma.Unlock()
	d.mu.Lock()
	d.lechim = false
	if err == nil {
		d.poslSbros, d.sbrosPoSmeneV, d.neudachPodryad = t, t, 0
	}
	d.mu.Unlock()
	if err != nil {
		log.Printf("дом: сеть ядра не сбросилась (%v) — дальше решат пробы", err)
	}
}
