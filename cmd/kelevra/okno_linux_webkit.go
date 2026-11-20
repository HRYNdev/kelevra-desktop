//go:build linux && webkit

package main

import (
	"log"
	"runtime"

	webview "github.com/webview/webview_go"
)

// Окно на Linux — WebKitGTK через webview (сборка с тегом webkit, CGO):
// тот же интерфейс службы, что WebView2 показывает на Windows.
// Без тега (проверки на сервере) — okno_other.go: адрес в журнал.

func otkrytOknoWebKit(zagolovok, url string, shirina, vysota int) {
	// GTK живёт в одном потоке ОС от создания окна до выхода из цикла.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	w := webview.New(false)
	if w == nil {
		log.Printf("окно: WebKitGTK не поднялся (нет экрана или библиотеки) — интерфейс по адресу %s", url)
		return
	}
	defer w.Destroy()
	w.SetTitle(zagolovok)
	w.SetSize(shirina, vysota, webview.HintNone)
	w.Navigate(url)
	w.Run()
}

func pokazatOkno(url string) { otkrytOknoWebKit("Kelevra", url, 420, 740) }

// podnyatChuzheeOkno — поднять окно другой копии на Linux нечем (у
// оконного менеджера нет общего способа): открываем своё.
func podnyatChuzheeOkno() bool { return false }

func zakrytStaroeOkno() itogSmenyOkna { return itogSmenyOkna{} }

func pokazatOknoPaneli(ssylka string) {
	otkrytOknoWebKit("Kelevra — панель сети", ssylka, 1200, 820)
}

func soobshchitOPromahePoiska(kto string) {
	log.Printf("%s: окна других копий на Linux не ищутся", kto)
}
