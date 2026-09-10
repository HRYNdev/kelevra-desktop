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
			Sobytie{Otkaz: true, Adres: "2a02:6b8:a::a", Port: 443, Vyhod: "direct", Kod: "set_nedostupna"},
		},
		{
			"отказ по таймауту",
			`+0500 2026-09-09 20:09:16 ERROR [225491183 1.38s] connection: open connection to [2a02:6b8:a::a]:443 using outbound/direct[direct]: dial tcp: i/o timeout`,
			Sobytie{Otkaz: true, Adres: "2a02:6b8:a::a", Port: 443, Vyhod: "direct", Kod: "taymaut"},
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
		"dial tcp: connect: network is unreachable": "set_nedostupna",
		"dial tcp: i/o timeout":                     "taymaut",
		"lookup subkv: no such host":                "imya_ne_reshilos",
		"connect: connection refused":               "otkaz_soedineniya",
		"tls: failed to verify certificate":         "tls_ne_vyshel",
		"что-то совершенно новое":                   "prochee",
	}
	for prichina, zhdem := range proby {
		if got := kodPrichiny(prichina); got != zhdem {
			t.Errorf("%q: получили %s, ждали %s", prichina, got, zhdem)
		}
	}
}
