//go:build windows

package main

import (
	"fmt"
	"log"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

// pokazatOknoPaneli — окно панели своей сети: большое, своё, в отдельном процессе (см. okno_paneli.go).
// Возвращается, когда человек закрыл окно.
func pokazatOknoPaneli(ssylka string) {
	runtime.LockOSThread() // очередь окна живёт в одном потоке ОС — см. pokazatOkno
	defer runtime.UnlockOSThread()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "Kelevra — панель сети",
			Width:  1200,
			Height: 820,
			Center: true,
		},
	})
	if w == nil {
		skazat("Панель не открылась", "В этой Windows нет компонента WebView2:\n"+gdeWebView2)
		return
	}
	defer w.Destroy()
	hwnd := syscall.Handle(uintptr(w.Window()))
	ustanovitZnachokOkna(hwnd)
	vklyuchitTemnyyZagolovok(hwnd)
	if err := dobavitKelevraAppVUserAgent(w); err != nil {
		log.Printf("окно панели: user agent не дополнен (%v), открываю как есть", err)
	}
	w.Navigate(ssylka)
	w.Run()
}

// metkaPrilozheniya — дописывается к user agent окна панели: так панель
// узнаёт, что открыта в приложении, а не в браузере (как на телефоне). Без
// «tema=…»: на компьютере панель берёт тему по системе.
const metkaPrilozheniya = "KelevraApp"

// IID_ICoreWebView2Settings2 (WebView2.h) — в нём живут get_/put_UserAgent.
var iidSettings2 = windows.GUID{Data1: 0xee9a0f68, Data2: 0xf46c, Data3: 0x4e32,
	Data4: [8]byte{0xac, 0x23, 0xef, 0x8c, 0xac, 0x22, 0x4d, 0x2a}}

// Места методов в таблице ICoreWebView2Settings2: IUnknown (3) + девять
// свойств ICoreWebView2Settings парами get/put (18) — дальше UserAgent.
const (
	mestoQueryInterface = 0
	mestoRelease        = 2
	mestoGetUserAgent   = 21
	mestoPutUserAgent   = 22
)

// dobavitKelevraAppVUserAgent дописывает « KelevraApp» к user agent окна.
//
// go-webview2 своего Chromium наружу не отдаёт (поле browser не экспортировано),
// а его GetUserAgent сломан (передаёт nil вместо адреса под ответ) — поэтому
// Chromium достаём отражением, а Settings2 зовём сами по таблице COM. Всё с
// проверками: поле не нашлось, тип не тот, интерфейса нет — ошибка, и окно
// открывается с обычным user agent, ничего не падает.
func dobavitKelevraAppVUserAgent(w webview2.WebView) error {
	v := reflect.ValueOf(w)
	if v.Kind() != reflect.Ptr || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("окно не того вида: %T", w)
	}
	pole := v.Elem().FieldByName("browser")
	if !pole.IsValid() || !pole.CanAddr() {
		return fmt.Errorf("в %T нет поля browser", w)
	}
	br := reflect.NewAt(pole.Type(), unsafe.Pointer(pole.UnsafeAddr())).Elem().Interface()
	ch, ok := br.(*edge.Chromium)
	if !ok || ch == nil {
		return fmt.Errorf("browser не Chromium: %T", br)
	}
	nastroyki, err := ch.GetSettings()
	if err != nil || nastroyki == nil {
		return fmt.Errorf("нет настроек WebView2: %v", err)
	}
	var s2 unsafe.Pointer
	if hr := comVyzov(unsafe.Pointer(nastroyki), mestoQueryInterface,
		uintptr(unsafe.Pointer(&iidSettings2)), uintptr(unsafe.Pointer(&s2))); hr != 0 || s2 == nil {
		return fmt.Errorf("нет ICoreWebView2Settings2 (hr=%#x)", hr)
	}
	defer comVyzov(s2, mestoRelease)
	var staryy *uint16
	if hr := comVyzov(s2, mestoGetUserAgent, uintptr(unsafe.Pointer(&staryy))); hr != 0 || staryy == nil {
		return fmt.Errorf("get_UserAgent: hr=%#x", hr)
	}
	ua := windows.UTF16PtrToString(staryy)
	windows.CoTaskMemFree(unsafe.Pointer(staryy))
	if strings.Contains(ua, metkaPrilozheniya) {
		return nil
	}
	novyy, err := windows.UTF16PtrFromString(ua + " " + metkaPrilozheniya)
	if err != nil {
		return err
	}
	if hr := comVyzov(s2, mestoPutUserAgent, uintptr(unsafe.Pointer(novyy))); hr != 0 {
		return fmt.Errorf("put_UserAgent: hr=%#x", hr)
	}
	log.Printf("окно панели: user agent «%s %s»", ua, metkaPrilozheniya)
	return nil
}

// comVyzov — метод COM по месту в таблице: первый аргумент всегда сам объект.
// Возвращает HRESULT (0 — S_OK).
func comVyzov(obekt unsafe.Pointer, mesto int, args ...uintptr) uintptr {
	tablica := *(*unsafe.Pointer)(obekt)
	metod := *(*uintptr)(unsafe.Add(tablica, mesto*int(unsafe.Sizeof(uintptr(0)))))
	hr, _, _ := syscall.SyscallN(metod, append([]uintptr{uintptr(obekt)}, args...)...)
	return hr
}
