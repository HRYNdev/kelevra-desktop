//go:build linux && webkit

package main

import (
	"log"
	"os/exec"
	"runtime"
	"sync/atomic"

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
	privyazat(w)
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

// privyazat — те же привязки страницы, что у окна Windows (okno_windows.go).
func privyazat(w webview.WebView) {
	if err := w.Bind("kelevraPanel", zapustitOknoPaneli); err != nil {
		log.Printf("кнопка панели сети не привязана: %v", err)
	}
	if err := w.Bind("kelevraOtkrytSsylku", otkrytSsylkuBrauzerom); err != nil {
		log.Printf("кнопка ссылки оплаты не привязана: %v", err)
	}
	var idetUstanovka atomic.Bool
	if err := w.Bind("kelevraDatPrava", func() bool {
		if !idetUstanovka.CompareAndSwap(false, true) {
			return false
		}
		go func() {
			defer idetUstanovka.Store(false)
			log.Printf("«Дать права»: прошу пароль администратора и ставлю службу")
			if err := poprositUstanovkuSluzhby(0); err != nil {
				log.Printf("«Дать права»: служба не поставлена: %v", err)
				return
			}
			log.Printf("«Дать права»: служба поставлена")
		}()
		return true
	}); err != nil {
		log.Printf("кнопка «Дать права» не привязана: %v", err)
	}
	if err := w.Bind("kelevraAvtozapuskSostoyanie", func() sostoyanieAvtozapuska {
		return avtozapuskOknaNastoyashchiy.sostoyanie()
	}); err != nil {
		log.Printf("состояние автозапуска не привязано: %v", err)
	}
	if err := w.Bind("kelevraAvtozapusk", avtozapuskOknaNastoyashchiy.pereklyuchit); err != nil {
		log.Printf("тумблер автозапуска не привязан: %v", err)
	}
}

// otkrytSsylkuBrauzerom — ссылка (оплата) в браузере человека по умолчанию.
func otkrytSsylkuBrauzerom(ssylka string) bool {
	if err := exec.Command("xdg-open", ssylka).Start(); err != nil {
		log.Printf("ссылка не открылась (xdg-open): %v", err)
		return false
	}
	return true
}
