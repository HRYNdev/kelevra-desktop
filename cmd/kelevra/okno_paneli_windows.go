//go:build windows

package main

import (
	"runtime"
	"syscall"

	"github.com/jchv/go-webview2"
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
	w.Navigate(ssylka)
	w.Run()
}
