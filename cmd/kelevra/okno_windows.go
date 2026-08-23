//go:build windows

package main

import (
	"log"
	"os"
	"syscall"

	"github.com/jchv/go-webview2"
)

// gdeWebView2 — официальный установщик компонента от Microsoft.
const gdeWebView2 = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

// Win32-константы для WM_SETICON — своего окна с таким сообщением в
// приложении больше нет (у трея в trey_windows.go — HWND_MESSAGE без
// видимой рамки, значку взяться неоткуда своим SendMessage), поэтому не
// заводим под них отдельный файл.
const (
	wmSetIcon = 0x0080
	iconSmall = 0
	iconBig   = 1
	smCxIcon  = 11 // GetSystemMetrics: ширина «большого» системного значка
)

// procSendMessageW — единственный прок в этом файле; user32TreyDLL общий
// на пакет (объявлен в trey_windows.go, тот же build-тег windows).
var procSendMessageW = user32TreyDLL.NewProc("SendMessageW")

// pokazatOkno открывает окно приложения на встроенном WebView2 (он есть в
// Windows 10/11 из коробки) и не возвращается, пока пользователь его не закроет.
func pokazatOkno(url string) {
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "Kelevra",
			Width:  420,
			Height: 660,
			Center: true,
		},
	})
	if w == nil {
		// Не log.Fatal: в оконной сборке это молчание, а человек видит,
		// что двойной щелчок не сделал ничего. Компонент ставится бесплатно
		// и в одну кнопку, поэтому говорим прямо, что именно поставить.
		log.Printf("ОТКАЗ: нет компонента WebView2, окно открыть нечем")
		skazat("Kelevra не запустилась",
			"В этой Windows нет компонента WebView2 — окно приложения рисовать нечем.\n\n"+
				"Он бесплатный, от Microsoft. Поставьте его отсюда и запустите Kelevra снова:\n"+
				gdeWebView2+"\n\n(этот текст можно скопировать целиком: Ctrl+C)")
		os.Exit(1)
	}
	defer w.Destroy()
	// go-webview2 не ставит окну свой значок — HWND у него класса
	// webview2 без RegisterClassExW.hIcon (в отличие от трея, который
	// регистрирует класс сам, см. trey_windows.go). Window() отдаёт HWND
	// напрямую (common.go: «when using Win32 backend the pointer is HWND
	// pointer»), поэтому достаём его и ставим значок вручную.
	ustanovitZnachokOkna(syscall.Handle(uintptr(w.Window())))
	// Окно живёт ровно столько, сколько живёт его служба: без неё показывать
	// нечего, а осиротевшее окно человек принимает за вторую копию приложения
	// (см. storozh_okna.go). Terminate по документации go-webview2 можно звать
	// из чужого потока, поэтому сторож работает своей горутиной.
	go storozhitSluzhbu(url, shagStorozha, molchaniyDoZakrytiya, w.Terminate)
	w.Navigate(url)
	w.Run()
}

// ustanovitZnachokOkna вешает на окно WebView2 значок из znachok.ico:
// маленький — для заголовка окна и Alt-Tab, большой — для панели задач.
// Тем же CreateIconFromResourceEx, что и трей (sobratZnachokRazmera в
// trey_windows.go); отказ не фатален — окно просто останется с системным
// значком, как было до этой правки.
func ustanovitZnachokOkna(hwnd syscall.Handle) {
	if hwnd == 0 {
		log.Printf("значок окна: HWND нулевой, пропускаю")
		return
	}
	hIconMalyy := sobratZnachokRazmera(zhelaemyyRazmerZnachka())
	procSendMessageW.Call(uintptr(hwnd), wmSetIcon, iconSmall, uintptr(hIconMalyy))

	razmerBolshoy := 32 // запасное значение, если GetSystemMetrics вернёт 0
	if r, _, _ := procGetSystemMetrics.Call(smCxIcon); r > 0 {
		razmerBolshoy = int(r)
	}
	hIconBolshoy := sobratZnachokRazmera(razmerBolshoy)
	procSendMessageW.Call(uintptr(hwnd), wmSetIcon, iconBig, uintptr(hIconBolshoy))

	log.Printf("значок окна: WM_SETICON отправлен (small HICON=%#x, big HICON=%#x)", hIconMalyy, hIconBolshoy)
}
