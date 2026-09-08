package tunnel

import (
	"fmt"
	"net/netip"
	"strings"
)

// Подбор адреса для сетевого адаптера туннеля.
//
// Зачем, если имя уже подбирается ([SvobodnoeImya]). Замер с машины человека
// 06.09, две записи одного вечера подряд:
//
//	18:54:27 ядро упало само: configure tun interface:
//	         Cannot create a file when that file already exists
//	18:54:33 откат удался: полный режим не вышел, работаю частично
//	18:54:57 ядро упало само: configure tun interface:
//	         set ipv4 address: The object already exists
//
// Читается так: первый раз упало по ИМЕНИ, подбор имени сработал и дал
// свободное — и через полминуты ядро упало снова, теперь уже по АДРЕСУ.
// Остаток прошлой попытки держит и то, и другое, а меняли мы только имя:
// новый адаптер приходил за тем же 172.19.0.1, что висит на старом.
//
// Отсюда правка: адрес едет за именем. Взяли соседнее имя — берём и соседний
// блок адресов, тем же сдвигом. Отдельно спрашивать систему, занят ли адрес,
// не нужно и вредно: занятость мы уже узнали по имени, а второй замер стоил
// бы человеку ещё секунд на кнопке.
//
// Чужие адреса при этом не трогаем и ничего в системе не сносим — ровно по
// той же причине, что и с именем (см. шапку imya.go): необратимое в чужой
// системе делается только по прямой просьбе.

// Sdvig — на сколько соседей ушло имя: «tun125» → «tun127» это 2. Ноль —
// имя не менялось или сравнивать нечего (разные основы, номеров нет).
//
// По этому числу двигается и блок адресов, поэтому считается оно из ИМЁН, а
// не задаётся отдельно: два числа, которые обязаны совпадать, разъезжаются
// рано или поздно, а одно разъехаться не может.
func Sdvig(ishodnoe, vzyatoe string) int {
	if ishodnoe == "" || vzyatoe == "" || ishodnoe == vzyatoe {
		return 0
	}
	oI, nI, estI := razobratImya(ishodnoe)
	oV, nV, estV := razobratImya(vzyatoe)
	if !estI || !estV || oI != oV || nV <= nI {
		return 0
	}
	return nV - nI
}

// SdvinutAdresa двигает каждый адрес на shag соседних блоков его же маски.
//
// Блок — это размер подсети из самой записи: у «172.19.0.1/30» блок в четыре
// адреса, и сосед начинается через четыре. Так адреса не наползают друг на
// друга, сколько бы копий ни висело.
//
// Что не по зубам — возвращаем как есть. Испорченная запись, адрес без маски,
// переполнение диапазона: пусть ядро запускается со своим прежним адресом
// (может, и поднимется), чем мы отменим человеку полный режим из-за строки,
// которую не разобрали.
func SdvinutAdresa(adresa []string, shag int) []string {
	if shag <= 0 || len(adresa) == 0 {
		return adresa
	}
	itog := make([]string, 0, len(adresa))
	for _, a := range adresa {
		itog = append(itog, sdvinutAdres(a, shag))
	}
	return itog
}

func sdvinutAdres(adres string, shag int) string {
	chistyy := strings.TrimSpace(adres)
	pref, err := netip.ParsePrefix(chistyy)
	if err != nil {
		return adres
	}
	razmer := pref.Addr().BitLen() - pref.Bits() // сколько битов под хосты
	if razmer <= 0 || razmer > 62 {
		// /32 и /128 двигать некуда, а слишком широкий блок (весь IPv6)
		// сдвигать смысла нет: адресов там столько, что столкнуться нельзя.
		return adres
	}
	novyy := pref.Addr()
	// Сдвиг ровно на shag блоков: прибавляем размер блока нужное число раз.
	// Побайтовое сложение, а не арифметика на числах: адрес может быть и
	// IPv4, и IPv6, а netip.Addr целым числом не представлен.
	for i := 0; i < shag; i++ {
		sled, ok := pribavit(novyy, uint64(1)<<uint(razmer))
		if !ok {
			return adres // упёрлись в край диапазона — оставляем как было
		}
		novyy = sled
	}
	return fmt.Sprintf("%s/%d", novyy, pref.Bits())
}

// pribavit прибавляет к адресу skolko, работая с ним как с большим числом в
// байтах. false — вышли за верхний край диапазона.
func pribavit(a netip.Addr, skolko uint64) (netip.Addr, bool) {
	b := a.AsSlice()
	perenos := skolko
	for i := len(b) - 1; i >= 0 && perenos > 0; i-- {
		summa := uint64(b[i]) + perenos&0xff
		b[i] = byte(summa & 0xff)
		perenos = perenos>>8 + summa>>8
	}
	if perenos > 0 {
		return netip.Addr{}, false
	}
	novyy, ok := netip.AddrFromSlice(b)
	return novyy, ok
}
