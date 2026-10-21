package zapisi

import "testing"

// Разбор строк журнала ядра — таблицей, без файлов и без поднятого ядра.
//
// Строки взяты живьём из журнала ядра на рабочей машине 10.09.2026: именно на
// них строится ось «сайт», которой в разборе не хватало.
func TestRazborStrokYadra(t *testing.T) {
	sluchai := []struct {
		imya   string
		stroka string
		zhdem  Sobytie
	}{
		{
			"отказ по шестой версии адреса, прямым выходом",
			`+0500 2026-09-09 20:09:15 ERROR [3572860262 103ms] connection: open connection to [2a02:6b8:a::a]:443 using outbound/direct[direct]: dial wlan0: connect: network is unreachable`,
			Sobytie{Otkaz: true, Adres: "2a02:6b8:a::a", Port: 443, Vyhod: "direct", Kod: "set_nedostupna", V6: true, shumV6: true},
		},
		{
			"отказ по таймауту",
			`+0500 2026-09-09 20:09:16 ERROR [225491183 1.38s] connection: open connection to [2a02:6b8:a::a]:443 using outbound/direct[direct]: dial tcp: i/o timeout`,
			Sobytie{Otkaz: true, Adres: "2a02:6b8:a::a", Port: 443, Vyhod: "direct", Kod: "taymaut", V6: true},
		},
		{
			"успешное исходящее с ИМЕНЕМ — та самая ось «сайт»",
			`+0500 2026-09-10 22:49:37 INFO [1 5ms] outbound/vless[Нидерланды · прямой]: outbound connection to settings-win.data.microsoft.com:443`,
			Sobytie{Ishod: true, Imya: "settings-win.data.microsoft.com", Port: 443, Vyhod: "vless"},
		},
		{
			"успешное исходящее по адресу",
			`+0500 2026-09-10 22:49:37 INFO [2 4ms] outbound/vless[Нидерланды · прямой]: outbound connection to 77.239.102.44:2053`,
			Sobytie{Ishod: true, Adres: "77.239.102.44", Port: 2053, Vyhod: "vless"},
		},
		{
			"соединение заведено в туннель",
			`+0500 2026-09-10 22:49:37 INFO [3 0ms] inbound/tun[tun-in]: inbound connection to 142.250.150.95:443`,
			Sobytie{Vhod: true},
		},
		{
			"обычная строка — ничего не нашлось",
			`+0500 2026-09-10 22:49:34 INFO network: updated default interface Беспроводная сеть, index 13`,
			Sobytie{},
		},
	}
	for _, s := range sluchai {
		got := Razobrat(s.stroka)
		if got != s.zhdem {
			t.Errorf("%s:\n  получили %+v\n  ждали    %+v", s.imya, got, s.zhdem)
		}
	}
}

// Имя от адреса отличаем по составу: имя, начинающееся с цифр, адресом считаться
// не должно, иначе ось «сайт» молча наполнится мусором.
func TestImyaOtlichaetsyaOtAdresa(t *testing.T) {
	adresa := []string{"1.2.3.4", "77.239.102.44", "2a02:6b8:a::a", "::1"}
	for _, a := range adresa {
		if !pohozheNaAdres(a) {
			t.Errorf("%s должен считаться адресом", a)
		}
	}
	imena := []string{"ya.ru", "settings-win.data.microsoft.com", "1.2.3.4.example.com", "mc.yandex.ru"}
	for _, i := range imena {
		if pohozheNaAdres(i) {
			t.Errorf("%s должен считаться именем", i)
		}
	}
}

// Код причины — из закрытого списка; незнакомое идёт в «прочее», а не теряется.
func TestKodyPrichin(t *testing.T) {
	proby := map[string]string{
		"dial tcp: connect: network is unreachable":                                                      "set_nedostupna",
		"dial tcp [2a00::1]:443: connect: unreachable network":                                           "set_nedostupna",
		"dial tcp [2a00::1]:443: connectex: A socket operation was attempted to an unreachable network.": "set_nedostupna",
		"connectex: A socket operation was attempted to an unreachable host.":                            "marshruta_net",
		"dial tcp: i/o timeout":             "taymaut",
		"lookup subkv: no such host":        "imya_ne_reshilos",
		"connect: connection refused":       "otkaz_soedineniya",
		"tls: failed to verify certificate": "tls_ne_vyshel",
		"что-то совершенно новое":           "prochee",
	}
	for prichina, zhdem := range proby {
		if got := kodPrichiny(prichina); got != zhdem {
			t.Errorf("%q: получили %s, ждали %s", prichina, got, zhdem)
		}
	}
}

// Шум шестой версии: машина без пути по IPv6 получила AAAA и постучалась по
// нему. Узнаём и когда адрес стоит в «куда», и когда там имя, а адрес — только
// в причине (так ядро пишет, если знает домен). По четвёртой версии и по
// другим причинам — не шум, а настоящий отказ.
func TestShumV6(t *testing.T) {
	shum := []string{
		`+0500 2026-10-07 09:48:00 ERROR [1 10ms] connection: open connection to [2a00::1]:443 using outbound/direct[direct]: dial tcp [2a00::1]:443: connect: unreachable network`,
		`+0500 2026-10-07 09:48:00 ERROR [2 10ms] connection: open connection to www.example.com:443 using outbound/direct[direct]: dial tcp [2a00:1450:4010:c05::5e]:443: connectex: A socket operation was attempted to an unreachable network.`,
		`+0500 2026-09-09 20:09:15 ERROR [3 103ms] connection: open connection to [2a02:6b8:a::a]:443 using outbound/direct[direct]: dial wlan0: connect: network is unreachable`,
		`+0500 2026-10-07 09:48:00 ERROR [4 10ms] connection: open connection to [2a00::1]:443 using outbound/direct[direct]: dial tcp [2a00::1]:443: connect: no route to host`,
	}
	for _, s := range shum {
		if !Razobrat(s).ShumV6() {
			t.Errorf("не узнан шум IPv6: %s (%+v)", s, Razobrat(s))
		}
	}
	nastoyashchie := []string{
		`+0500 2026-10-07 09:48:00 ERROR [5 10ms] connection: open connection to [1.2.3.4]:443 using outbound/direct[direct]: dial tcp 1.2.3.4:443: connect: network is unreachable`,
		`+0500 2026-10-07 09:48:00 ERROR [6 10ms] connection: open connection to www.example.com:443 using outbound/direct[direct]: dial tcp 93.184.216.34:443: i/o timeout`,
		`+0500 2026-10-07 09:48:00 ERROR [7 10ms] connection: open connection to [2a00::1]:443 using outbound/direct[direct]: dial tcp [2a00::1]:443: i/o timeout`,
		`+0500 2026-10-07 09:48:00 ERROR [8 10ms] connection: open connection to www.example.com:443 using outbound/vless[node]: EOF`,
		`+0500 2026-09-10 22:49:37 INFO [3 0ms] inbound/tun[tun-in]: inbound connection to [2a00::1]:443`,
	}
	for _, s := range nastoyashchie {
		if Razobrat(s).ShumV6() {
			t.Errorf("принят за шум IPv6, а это не он: %s", s)
		}
	}
}

// Составная причина (ядро перебрало адреса имени): шум — только если все
// части — недоступность по IPv6. Часть по IPv4 делает отказ настоящим, и код
// у него — код этой части, а не первой по списку.
func TestSostavnayaPrichina(t *testing.T) {
	sluchai := []struct {
		stroka string
		shum   bool
		kod    string
	}{
		{`+0500 2026-10-21 21:00:00 ERROR [1 3s] connection: open connection to www.example.com:443 using outbound/direct[direct]: dial tcp [2a00::1]:443: connect: unreachable network | dial tcp 1.2.3.4:443: i/o timeout`, false, "taymaut"},
		{`+0500 2026-10-21 21:00:00 ERROR [2 3s] connection: open connection to www.example.com:443 using outbound/direct[direct]: dial tcp 1.2.3.4:443: i/o timeout | dial tcp [2a00::1]:443: connect: unreachable network`, false, "taymaut"},
		{`+0500 2026-10-21 21:00:00 ERROR [3 3s] connection: open connection to www.example.com:443 using outbound/direct[direct]: dial tcp [2a00::1]:443: connect: unreachable network | dial tcp [2a00::2]:443: connect: no route to host`, true, "set_nedostupna"},
		{`+0500 2026-10-21 21:00:00 ERROR [4 3s] connection: open connection to www.example.com:443 using outbound/direct[direct]: dial tcp [2a00::1]:443: connect: unreachable network | dial tcp [2a00::2]:443: i/o timeout`, false, "taymaut"},
		{`+0500 2026-10-21 21:00:00 ERROR [5 3s] connection: open connection to www.example.com:443 using outbound/direct[direct]: dial tcp [2a00::1]:443: connect: unreachable network | dial tcp 1.2.3.4:443: connect: network is unreachable`, false, "set_nedostupna"},
	}
	for _, sl := range sluchai {
		s := Razobrat(sl.stroka)
		if s.ShumV6() != sl.shum || s.Kod != sl.kod {
			t.Errorf("%s:\n  шум %v код %s, ждал шум %v код %s", sl.stroka, s.ShumV6(), s.Kod, sl.shum, sl.kod)
		}
	}
}
