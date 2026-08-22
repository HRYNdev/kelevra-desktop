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
//   - привязка DNS-запроса к конкретному сетевому адаптеру (на Android для
//     этого есть Network-объект, на Windows без экзотических зависимостей
//     такого нет — спрашивается системный резолвер по умолчанию);
//   - собственно переключение туннеля (internal/proksi, internal/sluzhba) —
//     задача прямо требует не трогать это в этом заходе, здесь только
//     распознавание обстановки и его тесты.
package avtorezhim

import "context"

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
}

// Novyy собирает авторежим с рабочими зондами по умолчанию.
func Novyy() *Avtorezhim {
	return &Avtorezhim{
		Dns:       NovyyDnsZond(),
		Trafik:    NovyyPryamoyZond(),
		Zadvizhka: NovayaZadvizhka(Neizvestno),
	}
}

// Zahod — один заход: спрашивает зонды и предлагает наблюдение задвижке.
//
// @param estSet — сеть физически есть (обычно спрашивается у ОС уровнем
// выше — в этом срезе такого спросчика ещё нет, поэтому параметр).
// Прямой трафик спрашивается, только если DNS уже дал признак дома: если
// DNS сразу говорит «не дома», трафик всё равно ничего не изменит (Reshit
// вернёт VneDoma при !DnsPriznakDoma независимо от TrafikPryamoy) — лишний
// TCP-запрос платить незачем.
func (a *Avtorezhim) Zahod(ctx context.Context, estSet bool) (nablyudeniye Nablyudeniye, izmenilos bool, tekushcheye Sostoyanie) {
	if !estSet {
		n := Nablyudeniye{EstSet: false}
		izm := a.Zadvizhka.Predlozhit(Reshit(n))
		return n, izm, a.Zadvizhka.Tekushcheye()
	}

	dnsDoma, err := a.Dns.DomaPoDns(ctx)
	if err != nil {
		dnsDoma = false // резолвер не ответил вовсе — не дома, безопасный дефолт
	}

	var trafik *bool
	if dnsDoma {
		if izmereno, proshel := a.Trafik.Proshel(ctx); izmereno {
			trafik = &proshel
		}
	}

	n := Nablyudeniye{EstSet: true, DnsPriznakDoma: dnsDoma, TrafikPryamoy: trafik}
	izm := a.Zadvizhka.Predlozhit(Reshit(n))
	return n, izm, a.Zadvizhka.Tekushcheye()
}
