//go:build windows

// Пакет proksi убирает системный прокси Windows, оставленный старыми версиями.
//
// Зачем он есть. До 0.6.77 у Kelevra был прокси-режим: ядро прописывало себя
// системным прокси, а откатывало запись только при спокойном выходе. Жёсткая
// смерть процесса оставляла запись висеть — и после закрытия приложения
// переставали грузиться сайты (жалоба 20.08). Новые версии прокси не ставят
// никогда, но у того, кто обновился из прокси-режима, запись может остаться,
// и убрать её обязаны мы.
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
// Windows (а не под wine, как на стенде), доходили до Postavit (функция
// установки прокси, удалена в 0.6.77) — и системный прокси машины уезжал на
// 127.0.0.1:2412, порт, за которым в этот момент никого нет. Это ровно та авария, из-за которой у человека дважды пропадал
// интернет, только устроенная не приложением, а его же проверками. Ставит
// переменную TestMain пакета internal/sluzhba; у пользователя её нет.
func tolkoSmotrim() bool { return os.Getenv("KELEVRA_PROKSI") == "net" }

// UbratSled снимает системный прокси, оставленный СТАРОЙ версией Kelevra
// (до 0.6.77, прокси-режим), и стирает её метку. Возвращает true, если прокси
// действительно был включён и снят.
//
// Чужой прокси не трогает никогда: снимает, только если в ProxyServer стоит
// наш адрес — из метки старой версии или StaryyAdres. До 0.6.77 снятие гасило
// ЛЮБОЙ включённый прокси (ProxyEnable=0 без сверки адреса); теперь, когда
// Kelevra прокси не ставит вовсе, такой размах нечем оправдать — у человека
// может стоять свой прокси, и выключать его на каждом подключении нельзя.
//
// Зовут её: окно и значок на старте (пользовательский сеанс — ветка реестра
// человека), служебный процесс в сеансе человека при подъёме туннеля и на
// выходе. Из службы Windows она бесполезна, но безвредна: у системной
// учётной записи своя ветка HKCU, и нашего адреса там нет.
func UbratSled() bool {
	if tolkoSmotrim() {
		log.Printf("системный прокси: KELEVRA_PROKSI=net — настройки сети машины не трогаю")
		UbratMetku()
		return false
	}
	nashi := nashiAdresa()
	defer UbratMetku()
	k, err := registry.OpenKey(registry.CURRENT_USER, putNastroek, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		log.Printf("системный прокси: не открыть настройки: %v", err)
		return false
	}
	defer k.Close()

	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil || !nashSledIz(server, nashi) {
		// Строки нет или в ней чужой адрес — это не наш след, не трогаем.
		return false
	}
	snyali := false
	if vkl, _, err := k.GetIntegerValue("ProxyEnable"); err == nil && vkl != 0 {
		if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
			log.Printf("системный прокси старой версии: не выключить: %v", err)
			return false
		}
		// Снимать тем же путём, каким ставили: старая версия ставила прокси и
		// через опцию 75 (двоичный DefaultConnectionSettings — настоящий
		// источник правды для WinINet), и реестром.
		if err := snyatOpciey75(); err != nil {
			log.Printf("системный прокси: опция 75 не сняла (%v) — сняли реестром", err)
		}
		snyali = true
	}
	// Строку адреса стираем и при выключенной галочке: она врёт при разборе
	// аварии (31.08 по ней дважды искали причину пропавшего интернета).
	if err := k.DeleteValue("ProxyServer"); err != nil {
		log.Printf("системный прокси: свою строку адреса стереть не вышло: %v", err)
	}
	soobshchitSisteme()
	if snyali {
		log.Printf("системный прокси старой версии Kelevra снят (%s)", server)
	} else {
		log.Printf("системный прокси: убрал строку адреса старой версии Kelevra (%s)", server)
	}
	return snyali
}

// nashSled отвечает на единственный вопрос, который даёт право стереть чужую
// с виду строку из реестра: «то, что лежит в ProxyServer, — это ровно тот
// адрес, который ставила Kelevra?». Сравнение через Contains: ядро пишет адрес
// со схемой («http://127.0.0.1:2412»), а в метке он лежит без неё.
//
// Пустой адрес или пустая строка в реестре — всегда «нет»: без
// доказательства чужую настройку не трогаем никогда, даже выключенную.
func nashSled(vReestre, nash string) bool {
	vReestre, nash = strings.TrimSpace(vReestre), strings.TrimSpace(nash)
	if vReestre == "" || nash == "" {
		return false
	}
	return strings.Contains(vReestre, nash)
}

// nashSledIz — nashSled хотя бы для одного из наших адресов.
func nashSledIz(vReestre string, nashi []string) bool {
	for _, n := range nashi {
		if nashSled(vReestre, n) {
			return true
		}
	}
	return false
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

	flagPryamo = 1 // PROXY_TYPE_DIRECT
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

// snyatOpciey75 снимает системный прокси штатным вызовом WinINet: признак
// «напрямую», без адреса. Ставить прокси этим путём больше некому — адреса
// функция не принимает вовсе.
func snyatOpciey75() error {
	adres, priznaki := "", uintptr(flagPryamo)
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
