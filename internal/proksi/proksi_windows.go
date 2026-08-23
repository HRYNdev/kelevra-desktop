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
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const putNastroek = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// Snyat выключает системный прокси, если он включён.
// Возвращает true, если что-то действительно было выключено.
func Snyat() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, putNastroek, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		log.Printf("системный прокси: не открыть настройки: %v", err)
		return false
	}
	defer k.Close()

	vkl, _, err := k.GetIntegerValue("ProxyEnable")
	if err == nil && vkl == 0 {
		return false // никто ничего не включал — не трогаем чужие настройки
	}
	if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
		log.Printf("системный прокси: не выключить: %v", err)
		return false
	}
	// Адрес не стираем: человек мог прописать свой прокси до нас, и тогда он
	// вернёт его одной галочкой. Выключенная галочка сайты уже не ломает.
	soobshchitSisteme()
	log.Printf("системный прокси снят")
	return true
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
// делают рабочие VPN-клиенты без прав администратора: ядро отказалось
// поставить системный прокси собой (winapi error #12009 под ограниченными
// правами), но запись в этот же ключ реестра ничего особенного не требует.
// Возвращает true, если запись прошла успешно.
func Postavit(adres string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, putNastroek, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		log.Printf("системный прокси: не открыть настройки: %v", err)
		return false
	}
	defer k.Close()

	if err := k.SetStringValue("ProxyServer", adres); err != nil {
		log.Printf("системный прокси: не прописать адрес: %v", err)
		return false
	}
	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		log.Printf("системный прокси: не включить: %v", err)
		return false
	}
	soobshchitSisteme()
	log.Printf("системный прокси поставлен сам: %s", adres)
	return true
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
