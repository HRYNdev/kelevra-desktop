// Пакет avtorezhim определяет, где сейчас ноутбук — дома или нет, — чтобы
// потом (не в этом срезе, см. ниже) переключать между лёгким и полным VPN.
//
// Дома обход блокировок уже делает роутер, конфликтовать полному туннелю
// не с чем, но держать его поднятым — лишняя обёртка поверх обёртки. Вне
// дома нужен полный туннель. Устройство перенесено с телефонного клиента
// (Kotlin, kbox_push/app/src/main/java/io/nekohasekai/sfa/bg/AutoMode.kt,
// AutoModeGate.kt, HomeSign.kt) — там оно обкатано и там же взяты все числа
// и решения ниже, с явной пометкой, где перенос упрощён.
//
// Три опоры устройства, все три перенесены:
//  1. DNS-отпечаток дома ([DnsZond]): свой роутер подменяет заблокированные
//     домены на адрес из диапазона 198.18.0.0/15, и подмена на ≥2 из 3
//     контрольных доменов — надёжный признак «мы за своим роутером».
//  2. Прямая проверка трафика ([PryamoyZond]): DNS-признак говорит только
//     «отвечает домашний резолвер», а не «наружу проходит трафик» — в сети
//     с белым списком (например, гостевой Wi-Fi с ограничениями) резолвер
//     может быть тот же, а трафик наружу не пройдёт. Домом считаем только
//     то, что доказано обоими признаками разом (см. [Reshit]).
//  3. Гистерезис на [Podtverzhdeniy] подтверждений подряд ([Zadvizhka]):
//     одиночное наблюдение не повод дёргать VPN — DNS на свежем Wi-Fi может
//     ответить через раз, прямая проба — потерять один пакет.
//
// Что сознательно НЕ перенесено в этот срез (см. TODO на местах):
//   - NetworkModeDetector / подсказка «белый список», которая в телефонном
//     клиенте отдельно запрещает признавать дом при определённых сетях;
//   - собственно переключение туннеля (internal/proksi, internal/sluzhba) —
//     задача прямо требует не трогать это в этом заходе, здесь только
//     распознавание обстановки и его тесты.
//
// Привязка DNS-запроса к конкретному сетевому адаптеру (на Android для
// этого есть Network-объект) на Windows тоже есть: [SetevoyAdapter] узнаёт
// у ОС (GetAdaptersAddresses) DNS-сервер и локальный IP физического
// адаптера, а [DnsZond] с заполненными AdresResolvera/LokalnyAdres спрашивает
// именно его, минуя системный путь. Это и снимает слепоту зондов в
// TUN-режиме — см. Nablyudeniye.ZondSlep и Avtorezhim.Zahod ниже.
package avtorezhim

import (
	"context"
	"net"
)

// Sostoyanie — в какой сети сейчас ноутбук с точки зрения авторежима.
type Sostoyanie int

const (
	// Neizvestno — ещё не смотрели, либо сети физически нет: решать не на чем.
	Neizvestno Sostoyanie = iota

	// Doma — за своим роутером: обход уже делает он, полный туннель не нужен.
	Doma

	// VneDoma — не дома (или не удалось доказать обратное): нужен полный туннель.
	VneDoma
)

func (s Sostoyanie) String() string {
	switch s {
	case Doma:
		return "дома"
	case VneDoma:
		return "вне дома"
	default:
		return "неизвестно"
	}
}

// Nablyudeniye — что зонды увидели за один заход. Отдельный тип от
// [Sostoyanie]: наблюдение — это сырые факты, решение по ним считает [Reshit].
type Nablyudeniye struct {
	// EstSet — физическая сеть вообще есть (адаптер поднят, не «нет Wi-Fi и кабеля»).
	EstSet bool

	// ZondSlep — зонды в этот заход мерили не физическую сеть, а наш же
	// туннель, и верить им нельзя НИ В ОДНУ сторону.
	//
	// Почему так. Домашний отпечаток — это подмена контрольных доменов на
	// адрес из 198.18.0.0/15 (см. fakeIPPervyy/fakeIPPosledniy). Ровно этот
	// диапазон стоит в боевом профиле у нашего же ядра
	// (dns.servers[fakeip].inet4_range = 198.18.0.0/15), и youtube.com с
	// discord.com — два из трёх контрольных доменов при Nuzhno=2 — входят в
	// списки, которые ядро в fakeip и заворачивает (rule_set youtube,
	// discord). Значит при поднятом туннеле DNS-зонд, спрошенный системным
	// путём, видит подмену не роутера, а нашу собственную, а прямой зонд
	// ходит наружу ЧЕРЕЗ туннель и, понятно, проходит. Без лечения итог
	// такой: вне дома авторежим решает «дома» и САМ ОПУСКАЕТ защиту, потом
	// зонд (уже честный) говорит «вне дома», защита поднимается — и так по
	// кругу.
	//
	// Лечение — [SetevoyAdapter] плюс DnsZond.AdresResolvera: если у
	// физического адаптера получилось узнать приватный DNS-сервер, зонд
	// спрашивает ЕГО напрямую (в обход туннеля, см. dns_zond.go), и
	// ZondSlep не ставится — заход честный, см. Avtorezhim.Zahod. Слепота
	// остаётся только на случай, когда адрес адаптера узнать не вышло
	// (или он не приватный — подозрительно похоже на подмену) — тогда
	// решает даже не наш код, а чужой список правил с сервера подписки, и
	// довериться зонду нельзя.
	ZondSlep bool

	// DnsPriznakDoma — DNS-зонд насчитал подмену на ≥2 из 3 контрольных доменов.
	DnsPriznakDoma bool

	// TrafikPryamoy — прямая проверка трафика.
	// nil — не проверяли (в том числе: DNS уже сказал «не дома», проверять незачем)
	// или проверить не вышло (имя не резолвится — это тоже «не узнали», а не отказ).
	// *false — проверили, наружу не прошло. *true — проверили, прошло.
	TrafikPryamoy *bool
}

// Reshit — вердикт «дома ли» по одному заходу. Вынесена без единого
// обращения к сети или Windows нарочно: главное правило — «DNS-признак без
// подтверждённого трафика домом не считается» — обязано проверяться тестом,
// а не пересказом (тот же приём, что decide()/homeVerdict() в AutoMode.kt).
func Reshit(n Nablyudeniye) Sostoyanie {
	switch {
	case !n.EstSet:
		return Neizvestno
	case n.ZondSlep:
		// Мерили собственный туннель. Неизвестность здесь честнее любой
		// догадки: молчим и не трогаем защиту, которая уже поднята.
		return Neizvestno
	case !n.DnsPriznakDoma:
		return VneDoma
	case n.TrafikPryamoy != nil && *n.TrafikPryamoy:
		return Doma
	default:
		// DNS отвечает подменой, а трафик либо не подтверждён, либо не
		// проверялся — безопасный дефолт: полный туннель. Ошибиться в эту
		// сторону дёшево (туннель работает и там, где не обязателен),
		// ошибиться в другую значит выключить VPN там, где он нужен.
		return VneDoma
	}
}

// DnsProver — то, что умеет [DnsZond]. Интерфейс — чтобы [Avtorezhim] можно
// было прогнать тестом на подставном зонде, без единого сетевого запроса.
type DnsProver interface {
	DomaPoDns(ctx context.Context) (bool, error)
}

// TrafikProver — то, что умеет [PryamoyZond].
type TrafikProver interface {
	Proshel(ctx context.Context) (izmereno bool, proshel bool)
}

// Avtorezhim собирает зонды и задвижку в один заход. Про смену сети наружу
// не спрашивает сам — вызывающая сторона зовёт [Avtorezhim.Zahod] по своему
// сигналу (ритм или [Sledchik.Sobytiya]); здесь же только логика одного захода.
type Avtorezhim struct {
	Dns       DnsProver
	Trafik    TrafikProver
	Zadvizhka *Zadvizhka

	// TunnelPodnyat — стоит ли сейчас наш туннель на пути зондов. nil значит
	// «не стоит» (так собран Novyy: сам по себе пакет про ядро ничего не
	// знает, признак приносит служба — internal/sluzhba).
	// Когда возвращает true, заход пробует узнать DNS-адрес физического
	// адаптера (см. SetevoyAdres) и спросить его напрямую — обычный Dns
	// (системный резолвер) в этот заход НЕ спрашивается вовсе, он видел бы
	// наш же туннель. Если адрес узнать не вышло или он не приватный, заход
	// целиком слепой (см. Nablyudeniye.ZondSlep) — платить сетевым запросом
	// за заведомо негодный ответ незачем.
	// Важно: в прокси-режиме признак должен быть false — системный прокси
	// зонды не уважают (net.Dialer и net.Resolver идут мимо него), так что
	// там они честны.
	TunnelPodnyat func() bool

	// SetevoyAdres — DNS-сервер и локальный IP физического адаптера (см.
	// [SetevoyAdapter]). nil — берётся SetevoyAdapter. Поле — ради теста:
	// на этой машине настоящих Windows-адаптеров нет, а слепой сценарий
	// (адрес неизвестен) должен проверяться и без него.
	SetevoyAdres func() (dnsAdres string, lokalnyIP string, err error)

	// DnsPryamoy — фабрика DNS-зонда, привязанного к конкретному резолверу
	// (см. DnsZond.AdresResolvera/LokalnyAdres). nil — NovyyDnsZond с
	// проставленными адресами. Поле — ради теста: настоящий сетевой запрос
	// на резолвер физического адаптера тесту не нужен.
	DnsPryamoy func(adresResolvera, lokalnyAdres string) DnsProver
}

// Novyy собирает авторежим с рабочими зондами по умолчанию.
func Novyy() *Avtorezhim {
	return &Avtorezhim{
		Dns:          NovyyDnsZond(),
		Trafik:       NovyyPryamoyZond(),
		Zadvizhka:    NovayaZadvizhka(Neizvestno),
		SetevoyAdres: SetevoyAdapter,
		DnsPryamoy:   novyyDnsZondPryamoy,
	}
}

// novyyDnsZondPryamoy — DnsZond с параметрами по умолчанию, привязанный к
// конкретному резолверу (см. DnsZond.AdresResolvera/LokalnyAdres).
func novyyDnsZondPryamoy(adresResolvera, lokalnyAdres string) DnsProver {
	z := NovyyDnsZond()
	z.AdresResolvera = adresResolvera
	z.LokalnyAdres = lokalnyAdres
	return z
}

// privatnyeSeti — 10/8, 172.16/12, 192.168/16 (RFC 1918): диапазон, в
// котором обязан лежать DNS-сервер физического адаптера дома. Публичный
// адрес на этом месте подозрителен (например, сама подмена перехватчика) —
// тот же осторожный выбор, что и «адрес неизвестен».
var privatnyeSeti = func() []*net.IPNet {
	var seti []*net.IPNet
	for _, s := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			panic(err) // константы, ошибиться тут можно только опечаткой
		}
		seti = append(seti, n)
	}
	return seti
}()

// privatnyyAdres — host из "host:port" (или голый host) лежит в одной из
// privatnyeSeti.
func privatnyyAdres(dnsAdres string) bool {
	host, _, err := net.SplitHostPort(dnsAdres)
	if err != nil {
		host = dnsAdres // на случай голого IP без порта
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, set := range privatnyeSeti {
		if set.Contains(ip) {
			return true
		}
	}
	return false
}

// Zahod — один заход: спрашивает зонды и предлагает наблюдение задвижке.
//
// @param estSet — сеть физически есть (обычно спрашивается у ОС уровнем
// выше — в этом срезе такого спросчика ещё нет, поэтому параметр).
// @param dovereno — наблюдение доверенное (см. [Zadvizhka.Predlozhit]):
// заход случился по уже доказанному сигналу смены сети ([Sledchik]), а не
// по страховочному тикеру или холостому опросу — решать это вызывающей
// стороне ([Sluzhitel]).
// Прямой трафик спрашивается, только если DNS уже дал признак дома: если
// DNS сразу говорит «не дома», трафик всё равно ничего не изменит (Reshit
// вернёт VneDoma при !DnsPriznakDoma независимо от TrafikPryamoy) — лишний
// TCP-запрос платить незачем.
func (a *Avtorezhim) Zahod(ctx context.Context, estSet bool, dovereno bool) (nablyudeniye Nablyudeniye, izmenilos bool, tekushcheye Sostoyanie) {
	if !estSet {
		n := Nablyudeniye{EstSet: false}
		izm := a.Zadvizhka.Predlozhit(Reshit(n), dovereno)
		return n, izm, a.Zadvizhka.Tekushcheye()
	}

	dns := a.Dns
	if a.TunnelPodnyat != nil && a.TunnelPodnyat() {
		dnsAdres, lokalnyIP, uznali := a.adresFizicheskogoAdaptera()
		if !uznali {
			// Задвижке не предлагаем НИЧЕГО, даже Neizvestno: слепой заход —
			// это отсутствие наблюдения, а не наблюдение «не знаю». Предложи
			// мы Neizvestno — три слепых захода подряд (шесть минут по
			// страховочному тикеру) сдвинули бы обстановку, окно показало бы
			// человеку «неизвестно» вместо честного «вне дома», а из этой
			// ямы авторежим уже не выбрался бы: туннель-то поднят, следующий
			// заход тоже слепой.
			return Nablyudeniye{EstSet: true, ZondSlep: true}, false, a.Zadvizhka.Tekushcheye()
		}
		// Адрес физического адаптера известен и приватен — DnsZond
		// спрашивает ЕГО напрямую (DnsZond.AdresResolvera), в обход
		// туннеля: заход больше не слепой, ZondSlep не ставится.
		tvorec := a.DnsPryamoy
		if tvorec == nil {
			tvorec = novyyDnsZondPryamoy
		}
		dns = tvorec(dnsAdres, lokalnyIP)
	}

	dnsDoma, err := dns.DomaPoDns(ctx)
	if err != nil {
		dnsDoma = false // резолвер не ответил вовсе — не дома, безопасный дефолт
	}

	var trafik *bool
	if dnsDoma {
		// PryamoyZond в TUN-режиме всё равно ходит наружу ЧЕРЕЗ туннель и
		// потому почти всегда «проходит» — это ложноположительный ВТОРОЙ
		// признак, а не первый. Опасности в этом нет: dnsDoma здесь уже
		// посчитан ПРЯМЫМ запросом к резолверу физического адаптера (или
		// системным вне туннеля, тоже честным), а Reshit при
		// !DnsPriznakDoma в любом случае вернёт VneDoma независимо от
		// TrafikPryamoy — трафик только подтверждает уже доказанное DNS,
		// самостоятельно защиту не снимет.
		if izmereno, proshel := a.Trafik.Proshel(ctx); izmereno {
			trafik = &proshel
		}
	}

	n := Nablyudeniye{EstSet: true, DnsPriznakDoma: dnsDoma, TrafikPryamoy: trafik}
	izm := a.Zadvizhka.Predlozhit(Reshit(n), dovereno)
	return n, izm, a.Zadvizhka.Tekushcheye()
}

// adresFizicheskogoAdaptera — DNS-адрес и локальный IP физического
// адаптера, если удалось узнать (SetevoyAdres) и адрес лежит в приватном
// диапазоне (privatnyyAdres). SetevoyAdres == nil (не собран через Novyy) —
// тоже «не узнали», тот же безопасный слепой путь.
func (a *Avtorezhim) adresFizicheskogoAdaptera() (dnsAdres string, lokalnyIP string, uznali bool) {
	if a.SetevoyAdres == nil {
		return "", "", false
	}
	dnsAdres, lokalnyIP, err := a.SetevoyAdres()
	if err != nil || dnsAdres == "" || !privatnyyAdres(dnsAdres) {
		return "", "", false
	}
	return dnsAdres, lokalnyIP, true
}
