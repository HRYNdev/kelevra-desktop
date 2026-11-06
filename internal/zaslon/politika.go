package zaslon

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// Какие правила стоят — решает одна чистая функция Vybrat от того, что
// держит служба: намерение «подключено», обстановка автомата, строгий режим
// из пульта, идёт ли перезапуск ядра. Ни сети, ни Windows здесь нет: таблица
// проверяется тестом (politika_test.go).
//
// Два уровня (решение владельца 07.11.2026: «мне надо держать, чтоб по
// стандарту ничего на сервера Anthropic не уходило, а другим скорее не надо»;
// про загрузку Windows — «если мягко закрывать, типо только мне»):
//
//   - уровень 1, всем, по умолчанию: к адресам Anthropic исходящее только из
//     адреса туннеля. Интернет он не ломает, поэтому стоит всё время
//     «подключено», включая перезапуск и падение ядра. Самому ядру к Anthropic
//     напрямую тоже нельзя: направление direct для Anthropic — та же утечка;
//   - уровень 2, строгий, только по ключу пульта kanal.strogiy: всё исходящее
//     мимо туннеля закрыто всё время «подключено» вне дома, с момента старта
//     службы. Разрешены туннель, ядро и служба (к узлам и правилам direct),
//     петля, DHCP, NDP, локальная сеть.
//
// Дома (автомат решил «дома») не стоит ничего: трафик и так идёт через
// домашний шлюз, и запрет там только вредит. «Отключить» снимает всё.
// Временный полный запрет на перезапуск ядра — как раньше, но владелец один:
// служба, и снимается он сменой правил, а не закрытием отдельной сессии.

// Vhod — то, из чего выбираются правила.
type Vhod struct {
	// Namerenie — служба держит «подключено»: человек нажал «Подключиться»,
	// включён автомат или приняла живое ядро прошлой копии. «Отключить» его
	// снимает.
	Namerenie bool
	// Doma — автомат включён и решил «дома».
	Doma bool
	// Strogiy — ключ пульта kanal.strogiy (уровень 2).
	Strogiy bool
	// BezAnthropic — уровень 1 выключен пультом (vykl.zapret_anthropic).
	BezAnthropic bool
	// Perezapusk — служба сама перезапускает ядро, и срок временного запрета
	// не вышел.
	Perezapusk bool
}

// Reshenie — какие наборы правил ставить.
type Reshenie struct {
	// Anthropic — уровень 1: к адресам Anthropic только из туннеля.
	Anthropic bool
	// Polnyy — всё исходящее мимо туннеля закрыто (уровень 2 или перезапуск).
	Polnyy bool
	// Pochemu — для журнала.
	Pochemu string
}

// Vybrat — правила по входу. Порядок веток и есть договор:
//  1. перезапуск ядра — полный запрет (туннель был и должен вернуться);
//  2. нет намерения — ничего («Отключить» снимает всё сразу);
//  3. дома — ничего;
//  4. строгий — полный запрет;
//  5. иначе — уровень 1, если пульт его не выключил.
//
// Уровень 1 едет вместе с полным: в полном запрете ядру разрешено всё по
// его пути (к узлам), и без правил Anthropic оно могло бы уйти к Anthropic
// напрямую правилом direct.
func Vybrat(v Vhod) Reshenie {
	anthropic := !v.BezAnthropic
	switch {
	case v.Perezapusk:
		return Reshenie{Anthropic: anthropic, Polnyy: true, Pochemu: "перезапуск ядра"}
	case !v.Namerenie:
		return Reshenie{Pochemu: "связь отключена"}
	case v.Doma:
		return Reshenie{Pochemu: "дома"}
	case v.Strogiy:
		return Reshenie{Anthropic: anthropic, Polnyy: true, Pochemu: "строгий режим"}
	case !anthropic:
		return Reshenie{Pochemu: "запрет Anthropic выключен пультом"}
	default:
		return Reshenie{Anthropic: true, Pochemu: "подключено"}
	}
}

// Politika — решение плюс всё, что нужно для фильтров: адреса Anthropic,
// свои адреса туннеля и программы, которым можно наружу при полном запрете.
// Ровно это уходит держателю запрета при обновлении (peredacha.go).
type Politika struct {
	Anthropic bool           `json:"anthropic,omitempty"`
	Polnyy    bool           `json:"polnyy,omitempty"`
	Seti      []netip.Prefix `json:"seti,omitempty"`
	Tunnel    []netip.Prefix `json:"tunnel,omitempty"`
	Programmy []string       `json:"programmy,omitempty"`
}

// Pusto — правил нет вовсе.
func (p Politika) Pusto() bool { return !p.Anthropic && !p.Polnyy }

// Ravna — тот же набор правил (данные сравниваются целиком: сменились адреса
// туннеля или путь ядра — фильтры другие).
func (p Politika) Ravna(q Politika) bool {
	if p.Pusto() && q.Pusto() {
		return true
	}
	return p.Anthropic == q.Anthropic && p.Polnyy == q.Polnyy &&
		slices.Equal(p.Seti, q.Seti) && slices.Equal(p.Tunnel, q.Tunnel) &&
		slices.Equal(p.Programmy, q.Programmy)
}

// Slovami — для журнала и диагностики.
func (p Politika) Slovami() string {
	switch {
	case p.Polnyy:
		return "всё мимо туннеля закрыто"
	case p.Anthropic:
		return "Anthropic только через туннель"
	default:
		return "нет"
	}
}

// Kratko — одно слово для /api/sostoyanie и диагностики (не для человека).
func (p Politika) Kratko() string {
	switch {
	case p.Polnyy:
		return "polnyy"
	case p.Anthropic:
		return "anthropic"
	default:
		return ""
	}
}

// AnthropicPoUmolchaniyu — адреса Anthropic, если пульт не задал своих
// (kanal.anthropic_seti). Источник — анонсы AS399358 «ANTHROPIC - Anthropic, PBC»
// по RIPEstat (announced-prefixes), сверено 07.11.2026; сменятся — править пультом.
var AnthropicPoUmolchaniyu = []string{"160.79.104.0/23", "2607:6bc0::/48", "2607:6bc0:11::/48"}

// RazobratSeti — строки вида «адрес/маска» в подсети. Негодные строки не
// принимаются и возвращаются отдельно (для журнала): лучше узкий запрет по
// понятым строкам, чем никакого.
func RazobratSeti(stroki []string) (seti []netip.Prefix, negodnye []string) {
	for _, s := range stroki {
		s = strings.TrimSpace(s)
		p, err := netip.ParsePrefix(s)
		if err != nil {
			if a, errA := netip.ParseAddr(s); errA == nil {
				p = netip.PrefixFrom(a, a.BitLen())
			} else {
				negodnye = append(negodnye, s)
				continue
			}
		}
		p = p.Masked()
		if !slices.Contains(seti, p) {
			seti = append(seti, p)
		}
	}
	return seti, negodnye
}

// SetiTunnelya — свои адреса туннеля из записей профиля («172.19.0.1/30») с
// запасом на сдвиг: при занятом имени адаптера служба берёт соседнее имя
// (tun126…) и двигает адрес на столько же блоков (internal/tunnel/adres.go).
// sdvinut — тот самый сдвиг (tunnel.SdvinutAdresa), maksSdvig — сколько
// соседей бывает. Так правило «из туннеля» верно и для ядра на tun126, и для
// ядра, принятого у прошлой копии, без замера живого адаптера.
func SetiTunnelya(zapisi []string, maksSdvig int, sdvinut func([]string, int) []string) []netip.Prefix {
	var out []netip.Prefix
	for shag := 0; shag <= maksSdvig; shag++ {
		adresa := zapisi
		if shag > 0 {
			if sdvinut == nil {
				break
			}
			adresa = sdvinut(zapisi, shag)
		}
		seti, _ := RazobratSeti(adresa)
		for _, p := range seti {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

func (r Reshenie) String() string {
	return fmt.Sprintf("anthropic=%v polnyy=%v (%s)", r.Anthropic, r.Polnyy, r.Pochemu)
}
