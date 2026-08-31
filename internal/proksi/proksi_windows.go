//go:build windows

// Пакет proksi снимает за собой системный прокси Windows.
//
// Зачем он есть. Ядро прописывает себя системным прокси само, а откатывает
// запись только при спокойном выходе. На Windows у sing-box нет мягкой
// остановки, приложение гасит его напрямую — и настройка остаётся висеть.
// Дословно от хозяина 20.08 10:23: «оно за собой при выключении не гасит прокси
// сервер и из-за этого после закрытия приложения перестают грузится сайты».
// То же самое случится при любой аварии ядра, поэтому чистить обязано
// приложение, а не расчёт на вежливый выход чужой программы.
package proksi

import (
	"log"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const putNastroek = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// tolkoSmotrim — KELEVRA_PROKSI=net: НИЧЕГО не менять в настройках сети этой
// машины, только читать. Той же природы переменная, что KELEVRA_PRAVA=net в
// internal/prava, и заведена по той же причине.
//
// Замер 31.08: проверки, собранные под Windows и запущенные на НАСТОЯЩЕЙ
// Windows (а не под wine, как на стенде), доходят до Postavit — и системный
// прокси машины уезжает на 127.0.0.1:2412, порт, за которым в этот момент
// никого нет. Это ровно та авария, из-за которой у человека дважды пропадал
// интернет, только устроенная не приложением, а его же проверками. Ставит
// переменную TestMain пакета internal/sluzhba; у пользователя её нет.
func tolkoSmotrim() bool { return os.Getenv("KELEVRA_PROKSI") == "net" }

// Snyat выключает системный прокси, если он включён.
// Возвращает true, если что-то действительно было выключено.
func Snyat() bool {
	if tolkoSmotrim() {
		log.Printf("системный прокси: KELEVRA_PROKSI=net — настройки сети машины не трогаю")
		UbratMetku()
		return false
	}
	// Метку читаем ДО всякого UbratMetku: в ней лежит адрес, который поставили
	// МЫ, и только он даёт право стереть строку ProxyServer (см. pribratSled).
	nash, estMetka := ProchestMetku()
	k, err := registry.OpenKey(registry.CURRENT_USER, putNastroek, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		log.Printf("системный прокси: не открыть настройки: %v", err)
		return false
	}
	defer k.Close()

	vkl, _, err := k.GetIntegerValue("ProxyEnable")
	if err == nil && vkl == 0 {
		if estMetka {
			pribratSled(k, nash) // галочка уже снята, а наша строка адреса могла остаться
		}
		UbratMetku() // уже снят (в т.ч. этим же Snyat() с прошлого раза) — метка стала лишней
		return false // никто ничего не включал — не трогаем чужие настройки
	}
	if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
		log.Printf("системный прокси: не выключить: %v", err)
		return false
	}
	// Снимать обязаны тем же путём, каким ставили. Если прокси был поставлен
	// через опцию 75, настоящий источник правды для WinINet — двоичный
	// DefaultConnectionSettings, а не наши две строки в реестре: погасив только
	// ProxyEnable, мы оставили бы человека ровно с той бедой 20.08, из-за
	// которой этот пакет и появился.
	if err := cherezOpciyu75("", flagPryamo); err != nil {
		log.Printf("системный прокси: опция 75 не сняла (%v) — сняли реестром", err)
	}
	// Свой адрес из реестра стираем. До 31.08 он оставался лежать: сайты
	// выключенная галочка не ломает, и казалось, что строка безобидна. Она не
	// безобидна — она врёт при разборе аварии. 31.08 по строке
	// «ProxyServer=http://127.0.0.1:2412» при ProxyEnable=0 дважды искали
	// причину пропавшего интернета, которой там не было. За собой прибираем
	// целиком, а не наполовину.
	if estMetka {
		pribratSled(k, nash)
	}
	soobshchitSisteme()
	UbratMetku()
	log.Printf("системный прокси снят")
	return true
}

// nashSled отвечает на единственный вопрос, который даёт право стереть чужую
// с виду строку из реестра: «то, что лежит в ProxyServer, — это ровно тот
// адрес, который поставили МЫ?». Сравнение через Contains по той же причине,
// что и в Stoit: ядро пишет адрес со схемой («http://127.0.0.1:2412»), а в
// метке он лежит без неё («127.0.0.1:2412»).
//
// Пустая метка (её нет) или пустая строка в реестре — всегда «нет»: без
// доказательства чужую настройку не трогаем никогда, даже выключенную.
func nashSled(vReestre, nash string) bool {
	vReestre, nash = strings.TrimSpace(vReestre), strings.TrimSpace(nash)
	if vReestre == "" || nash == "" {
		return false
	}
	return strings.Contains(vReestre, nash)
}

// pribratSled стирает строку ProxyServer, если в ней стоит наш адрес nash.
// Чужой адрес оставляет как есть и говорит об этом в журнал.
func pribratSled(k registry.Key, nash string) {
	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil {
		return // строки нет вовсе — прибирать нечего
	}
	if !nashSled(server, nash) {
		log.Printf("системный прокси: в реестре не наш адрес (%q против нашего %q) — не трогаю", server, nash)
		return
	}
	if err := k.DeleteValue("ProxyServer"); err != nil {
		log.Printf("системный прокси: свою строку адреса стереть не вышло: %v", err)
		return
	}
	log.Printf("системный прокси: убрал за собой строку адреса (%s)", server)
}

// Stoit сообщает, стоит ли прямо сейчас в системе прокси на адрес adres.
// Ядро пишет адрес со схемой («http://127.0.0.1:2412»), а adres приходит без
// неё («127.0.0.1:2412») — поэтому сравниваем через strings.Contains, а не
// на равенство.
func Stoit(adres string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, putNastroek, registry.QUERY_VALUE)
	if err != nil {
		log.Printf("системный прокси: не открыть настройки: %v", err)
		return false
	}
	defer k.Close()

	vkl, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || vkl == 0 {
		return false
	}
	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil {
		return false
	}
	return strings.Contains(server, adres)
}

// Postavit прописывает системный прокси на adres сам — то же самое, что
// делают рабочие VPN-клиенты без прав администратора.
//
// Путей два, и порядок важен. Главный — InternetSetOption с опцией 75
// (INTERNET_PER_CONNECTION_OPTION): именно так ставят прокси sing-box и
// v2rayN, и именно его WinINet считает настоящим источником правды (он пишет
// двоичный DefaultConnectionSettings). Запасной — две строки в реестре;
// Microsoft про них прямым текстом: «Client applications should not use
// registry functions to change the default values of the Internet options».
// Реестр мы всё равно приводим в тот же вид: по нему читает Stoit() и его
// видит человек в Параметрах, так что расходиться им нельзя.
//
// Возвращает true, если хотя бы один из путей прошёл.
func Postavit(adres string) bool {
	if tolkoSmotrim() {
		log.Printf("системный прокси: KELEVRA_PROKSI=net — %s в настройки сети машины не пишу", adres)
		return false
	}
	poOpcii := cherezOpciyu75(adres, flagCherezProksi|flagPryamo)
	if poOpcii != nil {
		// Под wine (наш стенд) опция 75 не реализована и падает с 12009 —
		// это не признак того, что и на настоящей Windows будет отказ.
		log.Printf("системный прокси: опция 75 отказала (%v) — иду через реестр", poOpcii)
	}

	poReestru := vReestr(adres)
	if poReestru != nil {
		log.Printf("системный прокси: реестр отказал: %v", poReestru)
	}
	if poOpcii != nil && poReestru != nil {
		return false
	}
	soobshchitSisteme()
	log.Printf("системный прокси поставлен сам: %s (опция 75: %v, реестр: %v)",
		adres, poOpcii == nil, poReestru == nil)
	return true
}

func vReestr(adres string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, putNastroek, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetStringValue("ProxyServer", adres); err != nil {
		return err
	}
	return k.SetDWordValue("ProxyEnable", 1)
}

// Опция 75 и её обвязка. Структуры — как в WinINet:
//
//	INTERNET_PER_CONN_OPTIONW { DWORD dwOption; union { DWORD; LPWSTR; FILETIME } }
//	INTERNET_PER_CONN_OPTION_LISTW { DWORD dwSize; LPWSTR pszConnection;
//	                                 DWORD dwOptionCount; DWORD dwOptionError;
//	                                 LPINTERNET_PER_CONN_OPTIONW pOptions }
//
// Пустое pszConnection значит «настройки по умолчанию» — то есть обычное
// проводное/Wi-Fi подключение, а не отдельный dial-up.
const (
	opciyaPerConn = 75 // INTERNET_OPTION_PER_CONNECTION_OPTION

	polePriznaki  = 1 // INTERNET_PER_CONN_FLAGS
	poleServer    = 2 // INTERNET_PER_CONN_PROXY_SERVER
	poleIsklyuchi = 3 // INTERNET_PER_CONN_PROXY_BYPASS

	flagPryamo       = 1 // PROXY_TYPE_DIRECT
	flagCherezProksi = 2 // PROXY_TYPE_PROXY
)

type perConnOpciya struct {
	pole uint32
	_    uint32  // выравнивание перед union (на amd64 union начинается с 8-го байта)
	znak uintptr // и DWORD, и указатель на строку влезают сюда
}

type perConnSpisok struct {
	razmer      uint32
	_           uint32
	podklyuchen *uint16
	skolko      uint32
	oshibkaPole uint32
	opcii       *perConnOpciya
}

// cherezOpciyu75 ставит (adres непустой) или снимает (adres пустой, priznaki =
// flagPryamo) системный прокси штатным вызовом WinINet.
func cherezOpciyu75(adres string, priznaki uintptr) error {
	wininet, err := syscall.LoadLibrary("wininet.dll")
	if err != nil {
		return err
	}
	defer syscall.FreeLibrary(wininet)
	proc, err := syscall.GetProcAddress(wininet, "InternetSetOptionW")
	if err != nil {
		return err
	}

	opcii := []perConnOpciya{{pole: polePriznaki, znak: priznaki}}
	var server, isklyuch *uint16
	if adres != "" {
		if server, err = syscall.UTF16PtrFromString(adres); err != nil {
			return err
		}
		if isklyuch, err = syscall.UTF16PtrFromString("<local>"); err != nil {
			return err
		}
		opcii = append(opcii,
			perConnOpciya{pole: poleServer, znak: uintptr(unsafe.Pointer(server))},
			perConnOpciya{pole: poleIsklyuchi, znak: uintptr(unsafe.Pointer(isklyuch))},
		)
	}
	spisok := perConnSpisok{
		razmer: uint32(unsafe.Sizeof(perConnSpisok{})),
		skolko: uint32(len(opcii)),
		opcii:  &opcii[0],
	}
	r, _, errno := syscall.Syscall6(proc, 4, 0, opciyaPerConn,
		uintptr(unsafe.Pointer(&spisok)), unsafe.Sizeof(spisok), 0, 0)
	runtime.KeepAlive(server)
	runtime.KeepAlive(isklyuch)
	runtime.KeepAlive(opcii)
	if r == 0 {
		if errno != 0 {
			return errno
		}
		return syscall.EINVAL
	}
	return nil
}

// soobshchitSisteme — без этого браузеры и сама Windows ещё какое-то время
// ходят по старой настройке: реестр меняется молча.
func soobshchitSisteme() {
	wininet, err := syscall.LoadLibrary("wininet.dll")
	if err != nil {
		return
	}
	defer syscall.FreeLibrary(wininet)
	proc, err := syscall.GetProcAddress(wininet, "InternetSetOptionW")
	if err != nil {
		return
	}
	const (
		nastroykiIzmenilis = 39 // INTERNET_OPTION_SETTINGS_CHANGED
		perechitat         = 37 // INTERNET_OPTION_REFRESH
	)
	for _, o := range []uintptr{nastroykiIzmenilis, perechitat} {
		syscall.Syscall6(proc, 4, 0, o, uintptr(unsafe.Pointer(nil)), 0, 0, 0)
	}
}
