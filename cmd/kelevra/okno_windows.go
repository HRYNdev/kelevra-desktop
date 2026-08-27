//go:build windows

package main

import (
	"log"
	"os"
	"syscall"
	"unsafe"

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

// classWebview / titleOkna — то, чем само окно себя объявляет системе:
// класс регистрирует go-webview2 (webview.go: className = "webview",
// см. jchv/go-webview2), заголовок — наш собственный Title из
// webview2.WindowOptions{Title: "Kelevra"} выше в pokazatOkno.
const (
	classWebview = "webview"
	titleOkna    = "Kelevra"
	swRestore    = 9 // SW_RESTORE: развернуть, если окно свёрнуто
)

// procFindWindowW/procShowWindow — свои проки этого файла; procSetForegroundWindow
// уже объявлен в trey_windows.go (тот же пакет, тот же user32TreyDLL).
var (
	procFindWindowW = user32TreyDLL.NewProc("FindWindowW")
	procShowWindow  = user32TreyDLL.NewProc("ShowWindow")
)

// podnyatChuzheeOkno ищет уже открытое окно чужой копии Kelevra по классу и
// заголовку и выводит его на передний план вместо того, чтобы main плодил
// второе окно поверх первого. Беда 23.08: два независимых окна опрашивали
// /api/sostoyanie каждое по своему локальному состоянию, и второе слало
// podklyuchit на уже работающее ядро (см. симметричную правку Zapustit в
// internal/yadro/yadro.go). Не нашло окно (например, оно ещё не успело
// появиться) — возвращает false, и вызывающий код обязан откатиться на
// pokazatOkno, как было раньше.
func podnyatChuzheeOkno() bool {
	hwnd, est := naytiOkno()
	if !est {
		return false
	}
	procShowWindow.Call(uintptr(hwnd), swRestore)
	procSetForegroundWindow.Call(uintptr(hwnd))
	log.Printf("поднятие чужого окна: нашёл hwnd=%#x, развернул и вывел на передний план", hwnd)
	return true
}

// wmClose — Win32 WM_CLOSE: то же сообщение, что система шлёт окну по
// нажатию крестика. UIPI (User Interface Privilege Isolation) пускает
// сообщение от процесса С правами к окну БЕЗ прав (обратное запрещено), а
// смена режима всегда идёт именно в эту сторону — от уже повышенной копии к
// ещё не повышенному окну старой, — так что послать его отсюда можно всегда.
// wmClose использует procPostMessageW — тот же прок, что уже объявлен в
// trey_windows.go (общий на пакет, тот же build-тег windows).
const wmClose = 0x0010

// naytiOkno ищет HWND окна Kelevra по классу и заголовку — общий поиск для
// podnyatChuzheeOkno (поднять чужое окно на передний план) и
// zakrytStaroeOkno (закрыть его). Оба ищут один и тот же (и единственный по
// конструкции приложения — см. kopiya.Vzyat) экземпляр окна, отличается
// только то, что с ним делают дальше.
func naytiOkno() (syscall.Handle, bool) {
	classPtr, err := syscall.UTF16PtrFromString(classWebview)
	if err != nil {
		log.Printf("поиск окна: не собрал имя класса: %v", err)
		return 0, false
	}
	titlePtr, err := syscall.UTF16PtrFromString(titleOkna)
	if err != nil {
		log.Printf("поиск окна: не собрал заголовок: %v", err)
		return 0, false
	}
	hwndR, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(classPtr)), uintptr(unsafe.Pointer(titlePtr)))
	if hwndR == 0 {
		log.Printf("поиск окна: FindWindowW не нашёл окно (класс %q, заголовок %q)", classWebview, titleOkna)
		return 0, false
	}
	return syscall.Handle(hwndR), true
}

// zakrytStaroeOkno закрывает окно ПРЕДЫДУЩЕЙ копии сразу после смены режима
// (--smena, см. main.go: adresKopii), не дожидаясь, пока это заметит его
// собственный сторож (storozh_okna.go: 3 промаха по 2с, ≈6с — нарочно
// небыстро, чтобы не закрыть окно на разовую заминку ЖИВОЙ службы). К
// моменту вызова этой функции старая служба уже подтверждённо мертва
// (zhdatSmenu), поэтому ждать чужой таймаут незачем — не закрой мы его сами,
// человек увидел бы старое (уже неживое) окно рядом с новым ещё несколько
// секунд, ровно ту беду 25.08 «2 нахуй открыто», которую эта смена и обязана
// не повторить.
func zakrytStaroeOkno() bool {
	hwnd, est := naytiOkno()
	if !est {
		// Окна не было (автозапуск, значок в трее без окна) — закрывать нечего.
		return false
	}
	procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
	log.Printf("смена режима: отправил WM_CLOSE окну прошлой копии (hwnd=%#x)", hwnd)
	return true
}
