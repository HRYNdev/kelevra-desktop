// Пакет zaslon: запрет трафика мимо туннеля, которым владеет служба.
//
// Зачем. Ядро держит туннель (адаптер tun125, auto_route). Когда туннеля нет
// — перезапуск ядра, его падение, старт службы, уход из дома, — всё, что
// программы открывают в эти секунды, уходит напрямую через физический
// адаптер. Живой случай: ноут в Крыму, запросы Claude ушли через местного
// провайдера, и Anthropic ответил 403 «unsupported region» — ровно между
// «останавливаю ядро» 17:43:16 и «поднят туннель» 17:43:18.
//
// Как. Одна динамическая сессия WFP на весь процесс службы (wfp_windows.go):
// умерла служба — фильтры исчезли сами, вечной блокировки быть не может (это
// и есть «мягко», решение владельца 07.11.2026: не boot-time и не persistent).
// Какие правила стоят, решает чистая функция Vybrat (politika.go) от входа,
// который держит служба; смена правил — одна транзакция WFP (снять старые,
// поставить новые), промежутка «ничего не стоит» нет.
//
// Временный полный запрет на перезапуск ядра — как раньше, но это не
// отдельная сессия с таймером, а вход Perezapusk: предохранитель Srok (60 с)
// снимает его, если ядро не поднялось; у строгого режима полный запрет при
// этом остаётся своим правилом, у остальных остаётся уровень 1.
//
// Запрет не встал (WFP недоступен, ошибка) — работа идёт как раньше, строка
// в журнал и повтор через 5 с.
package zaslon

import (
	"fmt"
	"log"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// SrokPoUmolchaniyu — дольше этого временный полный запрет перезапуска не
// держится.
const SrokPoUmolchaniyu = 60 * time.Second

// PovtorPosleOshibki — через сколько повторить, если WFP отказал.
const PovtorPosleOshibki = 5 * time.Second

// Sistema — где живёт запрет. Настоящая — WFP (wfp_windows.go); проверки
// подставляют свою: на машине, где идут проверки, сеть не трогается.
type Sistema interface {
	// Primenit — поставить ровно эти фильтры вместо прежних, одной
	// транзакцией: не вышло — стоят прежние.
	Primenit(filtry []Filtr) error
	// Snyat — снять всё своё (закрыть сессию). Повтор на снятом — не ошибка.
	Snyat() error
}

// Dannye — адреса и программы для фильтров; берутся на каждой смене правил.
type Dannye struct {
	Seti      []string       // адреса Anthropic (строки пульта); пусто — AnthropicPoUmolchaniyu
	Tunnel    []netip.Prefix // свои адреса туннеля с запасом на сдвиг (SetiTunnelya)
	Programmy []string       // кому наружу можно при полном запрете (ядро, служба)
}

// Zaslon — владелец запрета. Нулевой Sys (и nil *Zaslon) — ничего не делать:
// не Windows, стенд, проверки.
//
// Методы нельзя звать из-под замков службы: Vhod и Dannye берут их сами.
type Zaslon struct {
	Sys Sistema
	// Vhod — намерение, обстановка, строгий режим (без Perezapusk: его
	// держит Zaslon). nil — нет намерения.
	Vhod func() Vhod
	// Dannye — адреса и программы. nil — пусто.
	Dannye func() Dannye
	// Srok — предохранитель временного запрета перезапуска; 0 — SrokPoUmolchaniyu.
	Srok time.Duration

	mu         sync.Mutex
	perezapusk bool
	nachalo    time.Time
	nomer      uint64 // растёт на каждом перезапуске: старый таймер не трогает новый
	taymer     *time.Timer
	primeneno  Politika // что стоит в системе
	slomano    bool     // последняя смена не встала — повторить
	povtor     *time.Timer
}

func (z *Zaslon) srok() time.Duration {
	if z.Srok > 0 {
		return z.Srok
	}
	return SrokPoUmolchaniyu
}

func (z *Zaslon) aktiven() bool { return z != nil && z.Sys != nil }

// Obnovit — вход сменился (намерение, обстановка, пульт, адреса туннеля):
// поставить правила, которые ему отвечают. Совпадают со стоящими — ничего.
func (z *Zaslon) Obnovit(prichina string) error {
	if !z.aktiven() {
		return nil
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.primenit(prichina)
}

// politikaSeychas — под z.mu.
func (z *Zaslon) politikaSeychas() (Politika, Reshenie) {
	var v Vhod
	if z.Vhod != nil {
		v = z.Vhod()
	}
	v.Perezapusk = z.perezapusk
	r := Vybrat(v)
	p := Politika{Anthropic: r.Anthropic, Polnyy: r.Polnyy}
	if p.Pusto() {
		return Politika{}, r
	}
	var d Dannye
	if z.Dannye != nil {
		d = z.Dannye()
	}
	if p.Anthropic {
		stroki := d.Seti
		if len(stroki) == 0 {
			stroki = AnthropicPoUmolchaniyu
		}
		seti, negodnye := RazobratSeti(stroki)
		if len(negodnye) > 0 {
			log.Printf("запрет мимо туннеля: адреса Anthropic не разобраны (%s) — беру остальные", strings.Join(negodnye, ", "))
		}
		if len(seti) == 0 {
			seti, _ = RazobratSeti(AnthropicPoUmolchaniyu)
		}
		p.Seti = seti
	}
	p.Tunnel = append([]netip.Prefix(nil), d.Tunnel...)
	if p.Polnyy {
		p.Programmy = append([]string(nil), d.Programmy...)
	}
	return p, r
}

// primenit — под z.mu.
func (z *Zaslon) primenit(prichina string) error {
	p, r := z.politikaSeychas()
	if !z.slomano && p.Ravna(z.primeneno) {
		return nil
	}
	var err error
	if p.Pusto() {
		err = z.Sys.Snyat()
	} else {
		err = z.Sys.Primenit(SobratFiltry(p))
	}
	if err != nil {
		z.slomano = true
		log.Printf("запрет мимо туннеля: не встал «%s» (%v) — стоит прежнее «%s», повторю через %s",
			p.Slovami(), err, z.primeneno.Slovami(), sekundy(PovtorPosleOshibki))
		z.zaplanirovatPovtor()
		return err
	}
	bylo := z.primeneno
	z.primeneno, z.slomano = p, false
	if z.povtor != nil {
		z.povtor.Stop()
		z.povtor = nil
	}
	if !bylo.Ravna(p) {
		pochemu := r.Pochemu
		if prichina != "" && prichina != r.Pochemu {
			pochemu += "; " + prichina
		}
		log.Printf("запрет мимо туннеля: %s (%s)", p.Slovami(), pochemu)
	}
	return nil
}

// zaplanirovatPovtor — под z.mu.
func (z *Zaslon) zaplanirovatPovtor() {
	if z.povtor != nil {
		return
	}
	z.povtor = time.AfterFunc(PovtorPosleOshibki, func() {
		z.mu.Lock()
		defer z.mu.Unlock()
		z.povtor = nil
		if z.slomano {
			_ = z.primenit("повтор")
		}
	})
}

// Zakryt ставит временный полный запрет перед тем, как служба сама погасит
// ядро ради подъёма. true — полный запрет стоит; false — нет системы или не
// встал (строка в журнал, перезапуск идёт как раньше). Повтор при идущем
// перезапуске срок не продлевает.
func (z *Zaslon) Zakryt() bool {
	if !z.aktiven() {
		return false
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.perezapusk {
		return z.primeneno.Polnyy
	}
	z.perezapusk = true
	if err := z.primenit("перезапуск ядра"); err != nil || !z.primeneno.Polnyy {
		z.perezapusk = false
		if err != nil {
			log.Printf("перезапуск ядра: закрыть трафик мимо туннеля не вышло (%v) — перезапускаю как раньше", err)
		}
		return false
	}
	z.nachalo = time.Now()
	z.nomer++
	nomer, srok := z.nomer, z.srok()
	z.taymer = time.AfterFunc(srok, func() { z.poSroku(nomer, srok) })
	log.Printf("перезапуск ядра: трафик мимо туннеля закрыт до подъёма (не дольше %s)", sekundy(srok))
	return true
}

// poSroku — предохранитель: перезапуск того же номера всё ещё идёт.
func (z *Zaslon) poSroku(nomer uint64, srok time.Duration) {
	z.mu.Lock()
	defer z.mu.Unlock()
	if !z.perezapusk || z.nomer != nomer {
		return
	}
	z.konchitPerezapusk()
	_ = z.primenit("срок временного запрета")
	z.skazatItog(fmt.Sprintf("по сроку %s, туннель не поднялся", sekundy(srok)), "открыт ")
}

// OtkrytPodnyat — туннель поднят: временный запрет перезапуска снять.
func (z *Zaslon) OtkrytPodnyat() {
	if !z.aktiven() {
		return
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if !z.perezapusk {
		return
	}
	proshlo := time.Since(z.nachalo)
	z.konchitPerezapusk()
	_ = z.primenit("туннель поднят")
	z.skazatItog("туннель поднят за "+sekundy(proshlo), "открыт: ")
}

// Otkryt — перезапуск кончился без туннеля (подъём не удался, решение о
// связи сменилось, связь опущена): временный запрет снять, prichina — в журнал.
func (z *Zaslon) Otkryt(prichina string) {
	if !z.aktiven() {
		return
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if !z.perezapusk {
		return
	}
	proshlo := time.Since(z.nachalo)
	z.konchitPerezapusk()
	_ = z.primenit(prichina)
	z.skazatItog(prichina, "открыт через "+sekundy(proshlo)+": ")
}

// skazatItog — под z.mu: чем кончился временный запрет. Строки «открыт…»
// прежние (по ним ищут в kelevra.log); если после перезапуска остался свой
// запрет (строгий, уровень 1), это сказано рядом.
func (z *Zaslon) skazatItog(chto, kak string) {
	if z.primeneno.Polnyy {
		log.Printf("перезапуск ядра: временный запрет кончился (%s), полный запрет остаётся: %s", chto, z.primeneno.Slovami())
		return
	}
	hvost := ""
	if z.primeneno.Anthropic {
		hvost = " (" + z.primeneno.Slovami() + ")"
	}
	log.Printf("перезапуск ядра: трафик мимо туннеля %s%s%s", kak, chto, hvost)
}

// konchitPerezapusk — под z.mu.
func (z *Zaslon) konchitPerezapusk() {
	z.perezapusk = false
	if z.taymer != nil {
		z.taymer.Stop()
		z.taymer = nil
	}
}

// Zakryto — стоит ли сейчас полный запрет (перезапуск или строгий).
func (z *Zaslon) Zakryto() bool {
	if z == nil {
		return false
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.primeneno.Polnyy
}

// Perezapusk — идёт ли временный запрет перезапуска.
func (z *Zaslon) Perezapusk() bool {
	if z == nil {
		return false
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.perezapusk
}

// Primeneno — что стоит в системе (копия).
func (z *Zaslon) Primeneno() Politika {
	if z == nil {
		return Politika{}
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	p := z.primeneno
	p.Seti = append([]netip.Prefix(nil), p.Seti...)
	p.Tunnel = append([]netip.Prefix(nil), p.Tunnel...)
	p.Programmy = append([]string(nil), p.Programmy...)
	return p
}

// sekundy — «1,8 с», «60 с»: десятичная запятая, как пишут люди.
func sekundy(d time.Duration) string {
	if d%time.Second == 0 {
		return fmt.Sprintf("%d с", int64(d/time.Second))
	}
	return strings.Replace(fmt.Sprintf("%.1f с", d.Seconds()), ".", ",", 1)
}
