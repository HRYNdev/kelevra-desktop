package avtorezhim

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// DomenyPoUmolchaniyu — контрольные домены: те же три, что в телефонном
// клиенте (AutoMode.kt, HOME_DOMAINS) — домашний роутер их гарантированно
// заворачивает через фейковый адрес, поэтому именно они и служат отпечатком.
var DomenyPoUmolchaniyu = []string{"youtube.com", "discord.com", "rutracker.org"}

// KontrolnyyDomenPoUmolchaniyu — HOME_CONTROL в AutoMode.kt: домен, который
// домашний роутер НЕ подменяет (отвечает настоящим адресом). Живой замер:
// дома youtube.com/discord.com/rutracker.org уходят в fake-IP, а
// gosuslugi.ru резолвится в 213.59.254.7. Сеть с тотальным перехватом DNS
// (публичный Wi-Fi с резолвером, который подменяет вообще всё) даст 3 из 3
// по обычным доменам и без этой проверки ложно опознается как дом —
// контрольный домен отличает выборочную подмену (дом) от тотальной
// (перехватчик).
const KontrolnyyDomenPoUmolchaniyu = "gosuslugi.ru"

// fakeIPPervyy, fakeIPPosledniy — границы диапазона подменных адресов
// домашнего роутера: 198.18.0.0 .. 198.19.255.255 — тот же диапазон, что
// FAKE_IP_FIRST/FAKE_IP_LAST в AutoMode.kt.
var (
	fakeIPPervyy    = net.IPv4(198, 18, 0, 0).To4()
	fakeIPPosledniy = net.IPv4(198, 19, 255, 255).To4()
)

func fakeIP(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	return bytes.Compare(v4, fakeIPPervyy) >= 0 && bytes.Compare(v4, fakeIPPosledniy) <= 0
}

// resolvat — то немногое от *net.Resolver, что нужно зонду. Отдельный
// интерфейс, чтобы гонять тесты без единого реального DNS-запроса
// (см. dns_zond_test.go).
type resolvat interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

// DnsZond — DNS-отпечаток дома: спрашивает контрольные домены и смотрит,
// сколько из них резолвер подменил на фейковый адрес. Совпадений нужно
// не меньше Nuzhno — одиночное совпадение может быть случайным (чужой
// CDN тоже иногда сидит в приватном диапазоне), см. HOME_HITS в AutoMode.kt.
//
// Теперь зонд умеет то же, что телефонный эталон (Android передаёт Network
// прямо в запрос): спрашивать резолвер конкретного сетевого адаптера
// напрямую, минуя системный. Для этого заданы AdresResolvera и
// LokalnyAdres (см. SetevoyAdapter) — актуально в первую очередь для
// TUN-режима, где системный резолвер перехвачен нашим же ядром (см.
// avtorezhim.go, ZondSlep). Если AdresResolvera пуст, поведение прежнее —
// спрашивается системный резолвер по умолчанию, это путь отката.
//
// KontrolnyyDomen (HOME_CONTROL в AutoMode.kt) должен резолвиться по
// настоящему адресу — иначе это не «выборочный» домашний обход, а
// перехватчик, подменяющий всё подряд, и признак дома снимается.
// Остальная логика HomeSign (память признака, ранний выход по бюджету) в
// этот срез не перенесена — TODO.
type DnsZond struct {
	Resolver resolvat // nil — берётся AdresResolvera, а если и он пуст — net.DefaultResolver

	// AdresResolvera — "IP:port" конкретного резолвера (например, DNS
	// физического адаптера, "192.168.1.192:53"), которого спрашиваем
	// напрямую по UDP, минуя системный резолвер. Пусто — используется
	// системный резолвер (net.DefaultResolver), как раньше.
	AdresResolvera string

	// LokalnyAdres — IP физического адаптера, с которого привязывается
	// исходящий сокет к AdresResolvera (net.Dialer.LocalAddr). Пусто —
	// сокет уходит с адреса, который выберет ОС сама.
	LokalnyAdres string

	Domeny          []string
	Nuzhno          int
	KontrolnyyDomen string // "" — берётся KontrolnyyDomenPoUmolchaniyu
	Taimaut         time.Duration

	// otchet — что зонд увидел в последний заход, для журнала (см. OtchetZonda).
	zamok  sync.Mutex
	otchet OtchetZonda
}

// rezolverPryamoy собирает *net.Resolver, который спрашивает не системный
// путь, а AdresResolvera напрямую: Dial игнорирует адрес, который ему даёт
// сам резолвер (на unix это был бы путь из /etc/resolv.conf), и всегда
// стучится в AdresResolvera. PreferGo обязателен — без него на Windows
// LookupIP уходит через cgo/системный резолвер мимо этого Dial целиком.
func rezolverPryamoy(adresResolvera, lokalnyAdres string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := &net.Dialer{}
			if laddr := lokalnyAdresDlyaSeti(network, lokalnyAdres); laddr != nil {
				d.LocalAddr = laddr
			}
			return d.DialContext(ctx, network, adresResolvera)
		},
	}
}

// lokalnyAdresDlyaSeti — net.Addr нужного типа (TCP/UDP) для net.Dialer.LocalAddr,
// собранный из голого IP. Резолвер сам решает, UDP или TCP (truncated-ответ
// пересылается по TCP), поэтому тип адреса подбирается по network из Dial.
func lokalnyAdresDlyaSeti(network, ip string) net.Addr {
	if ip == "" {
		return nil
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil
	}
	if strings.HasPrefix(network, "tcp") {
		return &net.TCPAddr{IP: parsed}
	}
	return &net.UDPAddr{IP: parsed}
}

// NovyyDnsZond — зонд с параметрами по умолчанию (те же 3 домена, нужно 2 из 3).
func NovyyDnsZond() *DnsZond {
	return &DnsZond{
		Domeny:          append([]string(nil), DomenyPoUmolchaniyu...),
		Nuzhno:          2,
		KontrolnyyDomen: KontrolnyyDomenPoUmolchaniyu,
		Taimaut:         3 * time.Second,
	}
}

// DomaPoDns — есть ли DNS-признак дома.
//
// Ошибка возвращается, только если не удалось спросить ни одного домена
// (например, резолвер целиком не отвечает): единичные молчащие домены на
// признак не влияют — так же, как HostAnswer.Silent в AutoMode.kt не
// считается ни за, ни против, а не превращается в «не дома».
//
// Признак дома снимается, если ответил контрольный домен и оказался
// подменён: подмена ВСЕГО подряд (в том числе контрольного, который дома
// всегда настоящий) — это тотальный перехватчик (публичный Wi-Fi), а не
// выборочный домашний обход. Молчание контрольного домена, как и молчание
// остальных, ни за, ни против не считается.
// OtchetZonda — что зонд увидел за один заход, словами для журнала.
//
// Заведён 09.09 по разбору живой беды: у человека вне дома авторежим через
// две секунды после подъёма туннеля объявлял «дома» и гасил защиту, а в
// журнале от всего захода была одна строка — сколько миллисекунд занял
// DNS-подэтап. Ни кого спрашивали, ни что ответили, ни почему вышло «дома»,
// узнать было нельзя, и разбор упирался в догадки.
type OtchetZonda struct {
	Rezolver   string   // кого спрашивали: адрес резолвера или «системный»
	Cherez     string   // с какого локального адреса, если привязывались
	Otvetili   int      // сколько контрольных доменов вообще ответили
	Podmen     int      // на скольких из них увидели подменный адрес
	Kontrolnyy string   // что ответил контрольный домен: настоящий/подменён/молчит
	Otvety     []string // домен=адрес, как есть
}

// Otchet — отчёт последнего захода зонда.
func (z *DnsZond) Otchet() OtchetZonda {
	z.zamok.Lock()
	defer z.zamok.Unlock()
	return z.otchet
}

func (z *DnsZond) zapomnit(o OtchetZonda) {
	z.zamok.Lock()
	z.otchet = o
	z.zamok.Unlock()
}

func (z *DnsZond) DomaPoDns(ctx context.Context) (bool, error) {
	otchet := OtchetZonda{Rezolver: "системный", Cherez: z.LokalnyAdres}
	r := z.Resolver
	if r == nil {
		if z.AdresResolvera != "" {
			r = rezolverPryamoy(z.AdresResolvera, z.LokalnyAdres)
			otchet.Rezolver = z.AdresResolvera
		} else {
			r = net.DefaultResolver
		}
	} else if z.AdresResolvera != "" {
		otchet.Rezolver = z.AdresResolvera
	}
	defer func() { z.zapomnit(otchet) }()
	taimaut := z.Taimaut
	if taimaut <= 0 {
		taimaut = 3 * time.Second
	}
	nuzhno := z.Nuzhno
	if nuzhno <= 0 {
		nuzhno = 2
	}
	domeny := z.Domeny
	if len(domeny) == 0 {
		domeny = DomenyPoUmolchaniyu
	}
	kontrolnyy := z.KontrolnyyDomen
	if kontrolnyy == "" {
		kontrolnyy = KontrolnyyDomenPoUmolchaniyu
	}

	cctx, otmena := context.WithTimeout(ctx, taimaut)
	defer otmena()

	hits := 0
	otvetili := 0
	for _, host := range domeny {
		ips, err := r.LookupIP(cctx, "ip4", host)
		if err != nil || len(ips) == 0 {
			otchet.Otvety = append(otchet.Otvety, host+"=молчит")
			continue // молчание — не довод ни за, ни против
		}
		otvetili++
		otchet.Otvety = append(otchet.Otvety, host+"="+ips[0].String())
		for _, ip := range ips {
			if fakeIP(ip) {
				hits++
				break
			}
		}
	}
	otchet.Otvetili, otchet.Podmen = otvetili, hits
	if otvetili == 0 {
		otchet.Kontrolnyy = "не спрашивали"
		return false, fmt.Errorf("резолвер не ответил ни на один из %d доменов", len(domeny))
	}

	otchet.Kontrolnyy = "молчит"
	kontrolnyyIps, err := r.LookupIP(cctx, "ip4", kontrolnyy)
	if err == nil && len(kontrolnyyIps) > 0 {
		otchet.Kontrolnyy = "настоящий " + kontrolnyyIps[0].String()
		for _, ip := range kontrolnyyIps {
			if fakeIP(ip) {
				otchet.Kontrolnyy = "подменён " + ip.String()
				return false, nil // контрольный домен тоже подменён — это перехватчик, не дом
			}
		}
	}

	return hits >= nuzhno, nil
}
