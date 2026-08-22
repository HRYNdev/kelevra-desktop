package avtorezhim

import (
	"bytes"
	"context"
	"fmt"
	"net"
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
// В отличие от телефонного эталона, зонд не умеет спрашивать резолвер
// конкретного сетевого адаптера (Android передаёт Network прямо в запрос) —
// на Windows такой привязки без экзотических зависимостей нет, поэтому
// спрашивается системный резолвер по умолчанию. Для ноутбука с одним
// активным адаптером (обычный случай для этого приложения) разницы нет;
// многосетевой случай — TODO на потом.
//
// KontrolnyyDomen (HOME_CONTROL в AutoMode.kt) должен резолвиться по
// настоящему адресу — иначе это не «выборочный» домашний обход, а
// перехватчик, подменяющий всё подряд, и признак дома снимается.
// Остальная логика HomeSign (память признака, ранний выход по бюджету) в
// этот срез не перенесена — TODO.
type DnsZond struct {
	Resolver        resolvat // nil — берётся net.DefaultResolver
	Domeny          []string
	Nuzhno          int
	KontrolnyyDomen string // "" — берётся KontrolnyyDomenPoUmolchaniyu
	Taimaut         time.Duration
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
func (z *DnsZond) DomaPoDns(ctx context.Context) (bool, error) {
	r := z.Resolver
	if r == nil {
		r = net.DefaultResolver
	}
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
			continue // молчание — не довод ни за, ни против
		}
		otvetili++
		for _, ip := range ips {
			if fakeIP(ip) {
				hits++
				break
			}
		}
	}
	if otvetili == 0 {
		return false, fmt.Errorf("резолвер не ответил ни на один из %d доменов", len(domeny))
	}

	kontrolnyyIps, err := r.LookupIP(cctx, "ip4", kontrolnyy)
	if err == nil && len(kontrolnyyIps) > 0 {
		for _, ip := range kontrolnyyIps {
			if fakeIP(ip) {
				return false, nil // контрольный домен тоже подменён — это перехватчик, не дом
			}
		}
	}

	return hits >= nuzhno, nil
}
