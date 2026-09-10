package zapisi

import (
	"regexp"
	"strconv"
	"strings"
)

// Разбор строк журнала ядра в события. Отдельным файлом от записи, потому что
// это чистые функции: они проверяются таблицей строк, без файлов и без ядра.

// Коды причин — закрытый список, тот же, что на телефоне и на сервере. Порядок
// важен: первое совпадение и есть код.
var prichiny = []struct {
	kod    string
	obraz  string
}{
	{"set_nedostupna", "network is unreachable"},
	{"marshruta_net", "no route to host"},
	{"taymaut", "i/o timeout"},
	{"taymaut", "context deadline exceeded"},
	{"imya_ne_reshilos", "no such host"},
	{"otkaz_soedineniya", "connection refused"},
	{"sbros", "connection reset"},
	{"tls_ne_vyshel", "tls: "},
}

// Отказ исходящего: «ERROR … open connection to <куда>:<порт> using outbound/<тег>…: <причина>».
//
// Куда — это либо адрес, либо ИМЯ: на компьютере ядро пишет имя, когда его знает
// («outbound connection to settings-win.data.microsoft.com:443»), и это ровно та
// ось «сайт», которой на телефоне приходится добывать отдельно.
var rxOtkaz = regexp.MustCompile(
	`open connection to \[?([^\]\s]+?)\]?:(\d+)(?: using outbound/([a-zA-Z0-9_.\-]+))?[^:]*: (.+)$`)

// Соединение заведено в туннель.
var rxVhod = regexp.MustCompile(`inbound connection to `)

// Успешное исходящее с именем или адресом.
var rxIshod = regexp.MustCompile(
	`outbound/([a-zA-Z0-9_.\-]+)\[[^\]]*\]: outbound connection to \[?([^\]\s]+?)\]?:(\d+)`)

// Sobytie — что нашлось в одной строке журнала ядра.
type Sobytie struct {
	Otkaz  bool
	Vhod   bool
	Ishod  bool
	Imya   string // имя сайта, если ядро его назвало
	Adres  string // адрес, если вместо имени стоял он
	Port   int
	Vyhod  string
	Kod    string
}

// Razobrat — одна строка журнала ядра. Ничего не нашлось — нулевое событие.
func Razobrat(stroka string) Sobytie {
	stroka = strings.TrimRight(stroka, "\r\n")
	if m := rxOtkaz.FindStringSubmatch(stroka); m != nil && strings.Contains(stroka, "ERROR") {
		kuda, port, vyhod, prichina := m[1], m[2], m[3], m[4]
		s := Sobytie{Otkaz: true, Vyhod: vyhod, Kod: kodPrichiny(prichina)}
		s.Port, _ = strconv.Atoi(port)
		if pohozheNaAdres(kuda) {
			s.Adres = kuda
		} else {
			s.Imya = kuda
		}
		return s
	}
	if m := rxIshod.FindStringSubmatch(stroka); m != nil {
		s := Sobytie{Ishod: true, Vyhod: m[1]}
		s.Port, _ = strconv.Atoi(m[3])
		if pohozheNaAdres(m[2]) {
			s.Adres = m[2]
		} else {
			s.Imya = m[2]
		}
		return s
	}
	if rxVhod.MatchString(stroka) {
		return Sobytie{Vhod: true}
	}
	return Sobytie{}
}

// kodPrichiny — причина отказа кодом из закрытого списка.
func kodPrichiny(prichina string) string {
	n := strings.ToLower(prichina)
	for _, p := range prichiny {
		if strings.Contains(n, p.obraz) {
			return p.kod
		}
	}
	return "prochee"
}

// pohozheNaAdres — адрес это или имя. Простая проверка по составу: у адреса
// четвёртой версии только цифры и точки, у шестой — двоеточия.
//
// Именно по составу, а не по попытке разобрать: имя вида «1.2.3.4.example.com»
// тоже состоит из цифр и точек, но заканчивается буквами, и разбор адреса на
// нём споткнётся ровно там, где надо.
func pohozheNaAdres(kuda string) bool {
	if kuda == "" {
		return false
	}
	if strings.Contains(kuda, ":") {
		return true
	}
	for _, c := range kuda {
		if (c < '0' || c > '9') && c != '.' {
			return false
		}
	}
	return true
}
