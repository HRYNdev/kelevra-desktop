package zaslon

import (
	"net/netip"
)

// Набор фильтров по политике — чистые данные, без единого вызова WFP:
// состав проверяется тестом (filtry_test.go), а wfp_windows.go только
// переводит каждый Filtr в FWPM_FILTER0.
//
// Все фильтры живут в одном своём подслое с наибольшим весом (0xFFFF), на
// слоях ALE_AUTH_CONNECT_V4/V6 (новые исходящие соединения). Внутри подслоя
// решает совпавший фильтр с наибольшим весом, поэтому порядок весов и есть
// правило:
//
//	15 разрешить: к Anthropic из адреса туннеля
//	14 запретить: к Anthropic (всем, в том числе ядру и службе)
//	— дальше только при полном запрете —
//	13 разрешить: программы (ядро, служба) по ALE_APP_ID
//	12 разрешить: петля
//	11 разрешить: из адреса туннеля
//	10 разрешить: DHCP (68→67, 546→547)
//	 9 разрешить: NDP (ICMPv6 133, 135, 136)
//	 8 разрешить: локальная сеть
//	 0 запретить: всё остальное исходящее
//
// Между подслоями в WFP запрет сильнее разрешения: чужой подслой (второй
// процесс Kelevra при обновлении, антивирус) может только добавить запрет.

// Semeystvo — IPv4 или IPv6 (слой ALE_AUTH_CONNECT_V4 или _V6).
type Semeystvo uint8

const (
	V4 Semeystvo = 4
	V6 Semeystvo = 6
)

// Pole — что сравнивает условие.
type Pole uint8

const (
	PoleProgramma     Pole = iota + 1 // ALE_APP_ID по пути exe
	PoleUdalennyy                     // IP_REMOTE_ADDRESS в подсети
	PoleLokalnyy                      // IP_LOCAL_ADDRESS в подсети
	PolePetlya                        // FLAGS: IS_LOOPBACK
	PoleProtokol                      // IP_PROTOCOL
	PoleLokalnyyPort                  // IP_LOCAL_PORT (у ICMP — тип сообщения)
	PoleUdalennyyPort                 // IP_REMOTE_PORT
)

// Uslovie — одно условие фильтра. Условия с одним Pole в фильтре — «или»,
// с разными — «и» (так их сводит WFP).
type Uslovie struct {
	Pole   Pole
	Set    netip.Prefix // PoleUdalennyy, PoleLokalnyy
	Chislo uint16       // PoleProtokol, порты
	Put    string       // PoleProgramma
}

// Filtr — один фильтр WFP.
type Filtr struct {
	Imya      string
	Semeystvo Semeystvo
	Ves       uint8
	Zapret    bool
	Usloviya  []Uslovie
}

// Веса внутри подслоя (см. шапку).
const (
	VesAnthropicIzTunnelya = 15
	VesAnthropicZapret     = 14
	VesProgramm            = 13
	VesPetli               = 12
	VesTunnelya            = 11
	VesDhcp                = 10
	VesNdp                 = 9
	VesSeti                = 8
	VesZapreta             = 0
)

const (
	protokolUdp    = 17
	protokolIcmpV6 = 58
)

// LokalnyeSeti — локальная сеть: дом и шлюз при полном запрете не ломаются.
var LokalnyeSeti = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),     // link-local
	netip.MustParsePrefix("224.0.0.0/4"),        // мультикаст (mDNS, SSDP)
	netip.MustParsePrefix("255.255.255.255/32"), // широковещание
	netip.MustParsePrefix("fe80::/10"),          // link-local (в том числе NDP)
	netip.MustParsePrefix("fc00::/7"),           // ULA
	netip.MustParsePrefix("ff00::/8"),           // мультикаст (NDP, DHCPv6, mDNS)
}

func semeystvoSeti(p netip.Prefix) Semeystvo {
	if p.Addr().Is4() {
		return V4
	}
	return V6
}

// usloviyaSetey — условия pole по подсетям одного семейства.
func usloviyaSetey(pole Pole, seti []netip.Prefix, sem Semeystvo) []Uslovie {
	var out []Uslovie
	for _, p := range seti {
		if semeystvoSeti(p) == sem {
			out = append(out, Uslovie{Pole: pole, Set: p})
		}
	}
	return out
}

// SobratFiltry — фильтры политики. Пустая политика — ни одного.
//
// Разрешение «к Anthropic из туннеля» ставится, только когда известны и
// адреса Anthropic, и свои адреса туннеля этого семейства: фильтр без
// условия на локальный адрес разрешил бы Anthropic всем — лучше закрыто
// совсем, чем открыто мимо туннеля.
func SobratFiltry(p Politika) []Filtr {
	var out []Filtr
	dobavit := func(f Filtr) { out = append(out, f) }
	oba := func(imya string, ves uint8, zapret bool, usl []Uslovie) {
		dobavit(Filtr{Imya: imya, Semeystvo: V4, Ves: ves, Zapret: zapret, Usloviya: usl})
		dobavit(Filtr{Imya: imya, Semeystvo: V6, Ves: ves, Zapret: zapret, Usloviya: usl})
	}

	if p.Anthropic {
		for _, sem := range []Semeystvo{V4, V6} {
			k := usloviyaSetey(PoleUdalennyy, p.Seti, sem)
			if len(k) == 0 {
				continue
			}
			if iz := usloviyaSetey(PoleLokalnyy, p.Tunnel, sem); len(iz) > 0 {
				dobavit(Filtr{Imya: "Anthropic через туннель", Semeystvo: sem, Ves: VesAnthropicIzTunnelya,
					Usloviya: append(append([]Uslovie{}, k...), iz...)})
			}
			dobavit(Filtr{Imya: "Anthropic мимо туннеля", Semeystvo: sem, Ves: VesAnthropicZapret, Zapret: true, Usloviya: k})
		}
	}

	if !p.Polnyy {
		return out
	}
	for _, put := range p.Programmy {
		oba("программа "+put, VesProgramm, false, []Uslovie{{Pole: PoleProgramma, Put: put}})
	}
	oba("петля", VesPetli, false, []Uslovie{{Pole: PolePetlya}})
	for _, sem := range []Semeystvo{V4, V6} {
		if iz := usloviyaSetey(PoleLokalnyy, p.Tunnel, sem); len(iz) > 0 {
			dobavit(Filtr{Imya: "туннель", Semeystvo: sem, Ves: VesTunnelya, Usloviya: iz})
		}
	}
	// DHCP: запрос клиента 68 → 67 (IPv4) и 546 → 547 (IPv6) на любой адрес —
	// сервер DHCP в гостинице или у провайдера бывает и не в частной сети.
	dobavit(Filtr{Imya: "DHCP", Semeystvo: V4, Ves: VesDhcp, Usloviya: portyUdp(68, 67)})
	dobavit(Filtr{Imya: "DHCPv6", Semeystvo: V6, Ves: VesDhcp, Usloviya: portyUdp(546, 547)})
	// NDP: просьба к роутеру (133), соседи (135, 136) — на любой адрес, иначе
	// при смене сети теряется маршрутизатор (так у WireGuard и Mullvad).
	dobavit(Filtr{Imya: "NDP", Semeystvo: V6, Ves: VesNdp, Usloviya: []Uslovie{
		{Pole: PoleProtokol, Chislo: protokolIcmpV6},
		{Pole: PoleLokalnyyPort, Chislo: 133},
		{Pole: PoleLokalnyyPort, Chislo: 135},
		{Pole: PoleLokalnyyPort, Chislo: 136},
	}})
	for _, sem := range []Semeystvo{V4, V6} {
		dobavit(Filtr{Imya: "локальная сеть", Semeystvo: sem, Ves: VesSeti, Usloviya: usloviyaSetey(PoleUdalennyy, LokalnyeSeti, sem)})
	}
	oba("остальное исходящее", VesZapreta, true, nil)
	return out
}

func portyUdp(svoy, chuzhoy uint16) []Uslovie {
	return []Uslovie{
		{Pole: PoleProtokol, Chislo: protokolUdp},
		{Pole: PoleLokalnyyPort, Chislo: svoy},
		{Pole: PoleUdalennyyPort, Chislo: chuzhoy},
	}
}
