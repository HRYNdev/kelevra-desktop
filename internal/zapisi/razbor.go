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
	kod   string
	obraz string
}{
	{"set_nedostupna", "network is unreachable"},
	// Та же причина словами Windows: «A socket operation was attempted to an
	// unreachable network» (и укороченное «connect: unreachable network»).
	{"set_nedostupna", "unreachable network"},
	{"marshruta_net", "no route to host"},
	{"marshruta_net", "unreachable host"},
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

// Адрес шестой версии в тексте причины: «dial tcp [2a00:1450::5]:443: …».
// Ядро пишет в «open connection to» имя, если его знает, а адрес, куда на
// самом деле шёл набор, остаётся только в причине.
var rxAdresV6VPrichine = regexp.MustCompile(`\[[0-9a-fA-F]*:[0-9a-fA-F:.%a-zA-Z]*\]:\d+`)

// Соединение заведено в туннель.
var rxVhod = regexp.MustCompile(`inbound connection to `)

// Успешное исходящее с именем или адресом.
var rxIshod = regexp.MustCompile(
	`outbound/([a-zA-Z0-9_.\-]+)\[[^\]]*\]: outbound connection to \[?([^\]\s]+?)\]?:(\d+)`)

// Sobytie — что нашлось в одной строке журнала ядра.
type Sobytie struct {
	Otkaz bool
	Vhod  bool
	Ishod bool
	Imya  string // имя сайта, если ядро его назвало
	Adres string // адрес, если вместо имени стоял он
	Port  int
	Vyhod string
	Kod   string
	// V6 — отказ по адресу шестой версии (адрес в «куда» или в причине).
	V6 bool
	// shumV6 — каждая часть причины — недоступность по IPv6 (см. ShumV6).
	shumV6 bool
}

// ShumV6 — отказ, который говорит не о беде, а о том, что у машины нет пути
// по шестой версии: программа получила адрес AAAA, постучалась по нему, сеть
// ответила «недоступна». Программа после этого сама идёт по четвёртой версии
// (так устроен выбор адреса в системе), и человек ничего не замечает. На
// ноутбуке 07.10 таких было 1023 из 1031 отказа за утро — и каждое окно
// выходило «бедой», хотя по четвёртой версии ошибок не было ни одной.
//
// Причина бывает составной: ядро пробует все адреса имени и склеивает отказы
// через « | » («dial tcp [v6]:443: … unreachable network | dial tcp
// 1.2.3.4:443: i/o timeout»). Шум — только если КАЖДАЯ часть — недоступность
// по IPv6; хоть одна часть по IPv4 или с другой причиной — обычный отказ.
func (s Sobytie) ShumV6() bool {
	return s.Otkaz && s.shumV6
}

// rxAdresV4VChasti — адрес четвёртой версии с портом в части причины.
var rxAdresV4VChasti = regexp.MustCompile(`(^|[^0-9.])\d{1,3}(\.\d{1,3}){3}:\d+`)

// razobratPrichinu — код причины и признак шума IPv6 по частям причины.
//
// Код — первой части, которая НЕ недоступность по IPv6: у составной причины
// из примера выше это «taymaut» по IPv4, а не «set_nedostupna» по IPv6, ради
// которой человек ничего не заметил бы. Все части — шум: код первой части.
func razobratPrichinu(kuda, prichina string) (kod string, shum bool) {
	chasti := strings.Split(prichina, " | ")
	kudaV6 := strings.Contains(kuda, ":")
	shum = true
	for _, chast := range chasti {
		k := kodPrichiny(chast)
		v6 := rxAdresV6VPrichine.MatchString(chast)
		v4 := rxAdresV4VChasti.MatchString(chast)
		// Части без адреса («dial wlan0: connect: network is unreachable»)
		// принадлежат адресу из «куда».
		if !v6 && !v4 {
			v6 = kudaV6
		}
		nedostupnostV6 := v6 && !v4 && (k == "set_nedostupna" || k == "marshruta_net")
		if !nedostupnostV6 {
			if shum {
				kod = k
			}
			shum = false
		}
	}
	if shum {
		kod = kodPrichiny(chasti[0])
	}
	return kod, shum
}

// Razobrat — одна строка журнала ядра. Ничего не нашлось — нулевое событие.
func Razobrat(stroka string) Sobytie {
	stroka = strings.TrimRight(stroka, "\r\n")
	if m := rxOtkaz.FindStringSubmatch(stroka); m != nil && strings.Contains(stroka, "ERROR") {
		kuda, port, vyhod, prichina := m[1], m[2], m[3], m[4]
		s := Sobytie{Otkaz: true, Vyhod: vyhod}
		s.Kod, s.shumV6 = razobratPrichinu(kuda, prichina)
		s.V6 = strings.Contains(kuda, ":") || rxAdresV6VPrichine.MatchString(prichina)
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
