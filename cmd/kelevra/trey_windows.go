//go:build windows

// Значок Kelevra в системном трее — обязательное завершение переделки из
// b924080 (окно и служба разведены на два процесса одного .exe).
//
// Беда без этого файла: человек закрывает окно, служба (ядро + прокси +
// HTTP) продолжает жить отдельным процессом — и понять, что защита включена,
// нечем, а выключить её нечем тоже. Трей делает службу видимой и даёт
// единственную кнопку «Выход», которая гасит её штатно (Ostanovit +
// Snyat — см. zapustitSluzhbu в main.go), а не через os.Exit, который
// оставил бы включённым системный прокси (та беда, про которую хозяин
// говорил 20.08, см. internal/proksi/proksi_windows.go).
//
// Чистый Go, без cgo и без сторонних библиотек трея — только syscall и
// golang.org/x/sys/windows, в том же стиле, что у skazat_windows.go,
// prava_windows.go и zapusk_windows.go: LazyDLL + Proc.Call.
package main

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:embed znachok.ico
var znachokIco []byte

var (
	user32TreyDLL   = syscall.NewLazyDLL("user32.dll")
	shell32TreyDLL  = syscall.NewLazyDLL("shell32.dll")
	kernel32TreyDLL = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW       = user32TreyDLL.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32TreyDLL.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32TreyDLL.NewProc("DefWindowProcW")
	procGetMessageW            = user32TreyDLL.NewProc("GetMessageW")
	procTranslateMessage       = user32TreyDLL.NewProc("TranslateMessage")
	procDispatchMessageW       = user32TreyDLL.NewProc("DispatchMessageW")
	procPostQuitMessage        = user32TreyDLL.NewProc("PostQuitMessage")
	procPostMessageW           = user32TreyDLL.NewProc("PostMessageW")
	procLoadIconW              = user32TreyDLL.NewProc("LoadIconW")
	procLoadCursorW            = user32TreyDLL.NewProc("LoadCursorW")
	procCreateIconFromResource = user32TreyDLL.NewProc("CreateIconFromResourceEx")
	procCreatePopupMenu        = user32TreyDLL.NewProc("CreatePopupMenu")
	procAppendMenuW            = user32TreyDLL.NewProc("AppendMenuW")
	procTrackPopupMenu         = user32TreyDLL.NewProc("TrackPopupMenu")
	procDestroyMenu            = user32TreyDLL.NewProc("DestroyMenu")
	procSetForegroundWindow    = user32TreyDLL.NewProc("SetForegroundWindow")
	procGetCursorPos           = user32TreyDLL.NewProc("GetCursorPos")
	procGetSystemMetrics       = user32TreyDLL.NewProc("GetSystemMetrics")

	procShellNotifyIconW = shell32TreyDLL.NewProc("Shell_NotifyIconW")

	procGetModuleHandleW = kernel32TreyDLL.NewProc("GetModuleHandleW")
)

// Win32-константы, которые нужны только здесь.
const (
	wmDestroy       = 0x0002
	wmCommand       = 0x0111
	wmRButtonUp     = 0x0205
	wmLButtonDblClk = 0x0203
	wmNull          = 0x0000
	wmApp           = 0x8000
	wmTreyIkonka    = wmApp + 1 // наше сообщение: несёт его Shell_NotifyIconW

	nimAdd    = 0
	nimDelete = 2

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString       = 0x00000000
	tpmLeftAlign   = 0x0000
	tpmRightButton = 0x0002

	idiApplication = 32512
	idcArrow       = 32512
	smCxSmIcon     = 49

	idMenuOtkryt = 1001
	idMenuVyhod  = 1002

	trayIconID = 1
)

// hwndMessage — HWND_MESSAGE, псевдо-родитель для окна, которое не рисуется
// вообще нигде: нам нужен только приёмник сообщений от Shell_NotifyIconW и
// от меню, а не видимое окно. ^uintptr(2) — это -3 в дополнительном коде,
// портируемо на любую разрядность uintptr (в этом репозитории — всегда 64-бит).
var hwndMessage = ^uintptr(2)

type pointT struct{ x, y int32 }

// msgT — Win32 MSG. Последнее поле — скрытое выравнивание, которое реально
// присутствует в MSG на 64-битной Windows (см. комментарий Raymond Chen про
// «invisible field» в MSG); без него размер структуры не совпал бы с тем,
// что ждут GetMessageW/DispatchMessageW.
type msgT struct {
	hwnd     syscall.Handle
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       pointT
	lPrivate uint32
}

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

// notifyIconDataW — NOTIFYICONDATAW (версия с полями до guidItem/hBalloonIcon
// включительно, т.е. «V4», актуальная с Windows Vista — у нас Windows 10/11,
// тот же минимум, что и у WebView2 в okno_windows.go).
type notifyIconDataW struct {
	cbSize            uint32
	hWnd              syscall.Handle
	uID               uint32
	uFlags            uint32
	uCallbackMessage  uint32
	hIcon             syscall.Handle
	szTip             [128]uint16
	dwState           uint32
	dwStateMask       uint32
	szInfo            [256]uint16
	uTimeoutOrVersion uint32
	szInfoTitle       [64]uint16
	dwInfoFlags       uint32
	guidItem          [16]byte
	hBalloonIcon      syscall.Handle
}

// zapustitTrey поднимает значок Kelevra в системном трее и не возвращается,
// пока не придёт WM_QUIT (это происходит только из пункта меню «Выход»,
// см. WndProc ниже). Вызывается из main.go отдельной горутиной, обёрнутой в
// recover — сюда сознательно не добавляется свой recover поверх того, чтобы
// не было двух разных мест, решающих одну и ту же судьбу аварии.
//
// vyhod — канал, в который пишем ровно один раз при «Выход»: дальше
// zhdatSignal должна довести дело до s.Yadro.Ostanovit()+proksi.Snyat(),
// а не эта горутина через os.Exit.
func zapustitTrey(vyhod chan<- struct{}) {
	// У GUI-потока Windows свой цикл сообщений, привязанный к потоку ОС:
	// если дать планировщику Go увести горутину на другой поток, окно и его
	// сообщения перестанут доходить. Не Unlock — поток живёт с этим циклом
	// до конца процесса.
	runtime.LockOSThread()

	hInstanceR, _, _ := procGetModuleHandleW.Call(0)
	hInstance := syscall.Handle(hInstanceR)

	className, err := syscall.UTF16PtrFromString("KelevraTreyWnd")
	if err != nil {
		log.Printf("трей: не собрал имя класса окна: %v", err)
		return
	}
	windowName, _ := syscall.UTF16PtrFromString("Kelevra (служба)")

	var wndProc func(hwnd, msg, wparam, lparam uintptr) uintptr
	wndProc = func(hwnd, msg, wparam, lparam uintptr) uintptr {
		switch uint32(msg) {
		case wmTreyIkonka:
			switch uint32(lparam) {
			case wmRButtonUp:
				pokazatMenuTreya(syscall.Handle(hwnd))
			case wmLButtonDblClk:
				otkrytOknoIzTreya()
			}
			return 0
		case wmCommand:
			switch uint32(wparam) & 0xffff {
			case idMenuOtkryt:
				otkrytOknoIzTreya()
			case idMenuVyhod:
				log.Printf("трей: «Выход» из меню, снимаю значок и прошу службу остановиться")
				snyatZnachokTreya(syscall.Handle(hwnd))
				select {
				case vyhod <- struct{}{}:
				default:
				}
				procPostQuitMessage.Call(0)
			}
			return 0
		case wmDestroy:
			procPostQuitMessage.Call(0)
			return 0
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
		return r
	}
	callback := syscall.NewCallback(wndProc)

	var wc wndClassExW
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = callback
	wc.hInstance = hInstance
	wc.lpszClassName = className
	if r, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow)); r != 0 {
		wc.hCursor = syscall.Handle(r)
	}
	atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		log.Printf("трей: RegisterClassExW не удался: %v", err)
		return
	}

	hwndR, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		hwndMessage,
		0,
		uintptr(hInstance),
		0,
	)
	if hwndR == 0 {
		log.Printf("трей: CreateWindowExW не удался: %v", err)
		return
	}
	hwnd := syscall.Handle(hwndR)
	log.Printf("трей: поток встал, окно трея создано (HWND_MESSAGE), hwnd=%#x", hwnd)

	hIcon := sobratZnachokTreya()
	dobavitZnachokTreya(hwnd, hIcon)

	// Цикл сообщений: живёт, пока не придёт WM_QUIT (шлёт его только наш
	// собственный обработчик «Выход»/WM_DESTROY выше).
	var m msgT
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	log.Printf("трей: цикл сообщений завершён")
}

// sobratZnachokTreya делает HICON из znachok.ico настоящим
// CreateIconFromResourceEx. Если не получилось (битый/пустой файл значка) —
// честно логирует и возвращает системный IDI_APPLICATION: отказ трея не
// имеет права утащить за собой службу.
func sobratZnachokTreya() syscall.Handle {
	return sobratZnachokRazmera(zhelaemyyRazmerZnachka())
}

// sobratZnachokRazmera — то же самое, что sobratZnachokTreya, но под
// произвольный желаемый размер образа. Вынесена отдельно, потому что тем же
// znachok.ico и той же CreateIconFromResourceEx пользуется ещё окно
// WebView2 (см. ustanovitZnachokOkna в okno_windows.go) — там нужны сразу
// два размера (ICON_SMALL и ICON_BIG), а не один, как в трее.
func sobratZnachokRazmera(zhelaemyy int) syscall.Handle {
	data := znachokIco
	obraz, err := vybratObrazIzIco(data, zhelaemyy)
	if err != nil {
		log.Printf("значок: не удалось собрать значок из znachok.ico (%v), беру системный значок", err)
		return sistemnyyZnachokTreya()
	}
	presbits := data[obraz.smeshchenie : obraz.smeshchenie+obraz.razmer]
	r, _, err := procCreateIconFromResource.Call(
		uintptr(unsafe.Pointer(&presbits[0])),
		uintptr(len(presbits)),
		1,          // fIcon = TRUE
		0x00030000, // dwVer — обязательное значение для этого формата ресурса
		uintptr(obraz.shirina),
		uintptr(obraz.vysota),
		0, // LR_DEFAULTCOLOR
	)
	if r == 0 {
		log.Printf("значок: CreateIconFromResourceEx вернул 0 для образа %dx%d (%v), беру системный значок", obraz.shirina, obraz.vysota, err)
		return sistemnyyZnachokTreya()
	}
	hIcon := syscall.Handle(r)
	log.Printf("значок: собран из znachok.ico (%dx%d, %d байт образа) под желаемый размер %d, HICON=%#x", obraz.shirina, obraz.vysota, len(presbits), zhelaemyy, hIcon)
	return hIcon
}

func sistemnyyZnachokTreya() syscall.Handle {
	r, _, _ := procLoadIconW.Call(0, uintptr(idiApplication))
	return syscall.Handle(r)
}

func zhelaemyyRazmerZnachka() int {
	if r, _, _ := procGetSystemMetrics.Call(smCxSmIcon); r > 0 {
		return int(r)
	}
	return 16
}

// obrazIco — один образ внутри .ico: его размер в файле и координаты, взятые
// из ICONDIRENTRY, а не из констант.
type obrazIco struct {
	shirina, vysota int
	razmer          uint32
	smeshchenie     uint32
}

// razobratIco читает ICONDIR + ICONDIRENTRY[] в начале .ico-файла.
// Формат: [0:2] reserved(=0), [2:4] type(=1 — значок), [4:6] count,
// затем count записей по 16 байт: width,height,colorCount,reserved,
// planes(2),bitCount(2),bytesInRes(4),imageOffset(4).
func razobratIco(data []byte) ([]obrazIco, error) {
	if len(data) < 6 {
		return nil, fmt.Errorf("файл короче заголовка ICONDIR (%d байт)", len(data))
	}
	if binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return nil, fmt.Errorf("не .ico (тип %d, ждали 1)", binary.LittleEndian.Uint16(data[2:4]))
	}
	kolichestvo := int(binary.LittleEndian.Uint16(data[4:6]))
	if kolichestvo == 0 {
		return nil, fmt.Errorf("в ICONDIR ноль образов")
	}
	zapisi := make([]obrazIco, 0, kolichestvo)
	for i := 0; i < kolichestvo; i++ {
		off := 6 + i*16
		if off+16 > len(data) {
			return nil, fmt.Errorf("ICONDIRENTRY %d обрезан", i)
		}
		w := int(data[off])
		if w == 0 {
			w = 256
		}
		h := int(data[off+1])
		if h == 0 {
			h = 256
		}
		razmer := binary.LittleEndian.Uint32(data[off+8 : off+12])
		smeshchenie := binary.LittleEndian.Uint32(data[off+12 : off+16])
		if razmer == 0 {
			return nil, fmt.Errorf("образ %d нулевого размера", i)
		}
		if uint64(smeshchenie)+uint64(razmer) > uint64(len(data)) {
			return nil, fmt.Errorf("образ %d выходит за пределы файла", i)
		}
		zapisi = append(zapisi, obrazIco{w, h, razmer, smeshchenie})
	}
	return zapisi, nil
}

// vybratObrazIzIco выбирает запись, ближайшую по ширине к желаемой (обычно
// 16 — размер значка в трее).
func vybratObrazIzIco(data []byte, zhelaemyy int) (obrazIco, error) {
	zapisi, err := razobratIco(data)
	if err != nil {
		return obrazIco{}, err
	}
	luchshiy := zapisi[0]
	for _, z := range zapisi {
		if z.shirina == zhelaemyy {
			return z, nil
		}
		if abs(z.shirina-zhelaemyy) < abs(luchshiy.shirina-zhelaemyy) {
			luchshiy = z
		}
	}
	return luchshiy, nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// dobavitZnachokTreya вешает значок (NIM_ADD). Результат Shell_NotifyIconW
// логируется честно: под wine без explorer.exe он законно может быть 0 —
// это не значит, что вызов не случился.
func dobavitZnachokTreya(hwnd syscall.Handle, hIcon syscall.Handle) {
	var d notifyIconDataW
	d.cbSize = uint32(unsafe.Sizeof(d))
	d.hWnd = hwnd
	d.uID = trayIconID
	d.uFlags = nifMessage | nifIcon | nifTip
	d.uCallbackMessage = wmTreyIkonka
	d.hIcon = hIcon
	kopirovatStrokuUTF16(d.szTip[:], "Kelevra: VPN включён")
	r, _, _ := procShellNotifyIconW.Call(uintptr(nimAdd), uintptr(unsafe.Pointer(&d)))
	log.Printf("трей: Shell_NotifyIconW(NIM_ADD) -> %v", r != 0)
}

func snyatZnachokTreya(hwnd syscall.Handle) {
	var d notifyIconDataW
	d.cbSize = uint32(unsafe.Sizeof(d))
	d.hWnd = hwnd
	d.uID = trayIconID
	r, _, _ := procShellNotifyIconW.Call(uintptr(nimDelete), uintptr(unsafe.Pointer(&d)))
	log.Printf("трей: Shell_NotifyIconW(NIM_DELETE) -> %v", r != 0)
}

func kopirovatStrokuUTF16(dst []uint16, s string) {
	u := syscall.StringToUTF16(s)
	n := copy(dst, u)
	if n == len(dst) {
		dst[len(dst)-1] = 0
	}
}

// pokazatMenuTreya — меню по правому клику: «Открыть» и «Выход».
// SetForegroundWindow перед TrackPopupMenu и PostMessage(WM_NULL) после —
// обязательные грабли WinAPI (см. заметку Raymond Chen про TrackPopupMenu):
// без первого меню не получает фокус и не гаснет само по клику мимо, без
// второго иногда остаётся невидимым перехватчиком следующего клика.
func pokazatMenuTreya(hwnd syscall.Handle) {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		log.Printf("трей: не смог создать меню")
		return
	}
	defer procDestroyMenu.Call(hMenu)

	otkr, _ := syscall.UTF16PtrFromString("Открыть")
	vyh, _ := syscall.UTF16PtrFromString("Выход")
	procAppendMenuW.Call(hMenu, mfString, uintptr(idMenuOtkryt), uintptr(unsafe.Pointer(otkr)))
	procAppendMenuW.Call(hMenu, mfString, uintptr(idMenuVyhod), uintptr(unsafe.Pointer(vyh)))

	var pt pointT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(uintptr(hwnd))
	procTrackPopupMenu.Call(hMenu, uintptr(tpmLeftAlign|tpmRightButton), uintptr(pt.x), uintptr(pt.y), 0, uintptr(hwnd), 0)
	procPostMessageW.Call(uintptr(hwnd), wmNull, 0, 0)
}

// otkrytOknoIzTreya — «Открыть» (пункт меню и двойной клик): запускает
// САМ СЕБЯ без аргументов отдельным процессом, тем же способом, что
// zapustitOtdelnuyuSluzhbu в zapusk_windows.go. Запущенная копия найдёт уже
// работающую службу через internal/kopiya и покажет окно — второе ядро не
// поднимется (см. main.go: kopiya.Nayti).
func otkrytOknoIzTreya() {
	sebya, err := os.Executable()
	if err != nil {
		log.Printf("трей: «Открыть» не понял свой путь: %v", err)
		return
	}
	cmd := exec.Command(sebya)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		log.Printf("трей: «Открыть» не смог запустить окно: %v", err)
		return
	}
	if err := cmd.Process.Release(); err != nil {
		log.Printf("трей: «Открыть» не отпустил процесс: %v", err)
	}
	log.Printf("трей: «Открыть» запустил отдельную копию без аргументов")
}
