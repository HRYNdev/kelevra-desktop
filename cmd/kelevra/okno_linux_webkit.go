//go:build linux && webkit

package main

import (
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	webview "github.com/webview/webview_go"
)

// Окно на Linux — WebKitGTK через webview (сборка с тегом webkit, CGO):
// тот же интерфейс службы, что WebView2 показывает на Windows.
// Без тега (проверки на сервере) — okno_other.go: адрес в журнал.

func otkrytOknoWebKit(zagolovok, url string, shirina, vysota int) {
	otkrytOknoWebKitSoStorozhem(zagolovok, url, shirina, vysota, false)
}

// otkrytOknoWebKitSoStorozhem — storozh: окно живёт, пока живёт служба, и
// переходит за ней, как окно Windows (okno_windows.go, storozh_okna.go).
// Без сторожа (до 0.6.112) окно на Linux держало адрес прежней копии службы
// навсегда: обновление и любой перезапуск службы меняют порт и ключ адреса,
// а окно, открытое в эти секунды, загружало оболочку страницы и больше ни
// одного ответа не получало — пустое окно (стенд 313, 20.11.2026: окно
// открыто за 1,4 с до перезапуска службы — пусто и через 20 с). Окно,
// открытое до обновления, висело на «Kelevra перезапускается…».
func otkrytOknoWebKitSoStorozhem(zagolovok, url string, shirina, vysota int, storozh bool) {
	// GTK живёт в одном потоке ОС от создания окна до выхода из цикла.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	w := webview.New(false)
	if w == nil {
		log.Printf("окно: WebKitGTK не поднялся (нет экрана или библиотеки) — интерфейс по адресу %s", url)
		return
	}
	var nadoPerezapustit atomic.Bool
	defer func() {
		w.Destroy()
		if nadoPerezapustit.Load() {
			zapustitOknoZanovoLinux()
		}
	}()
	w.SetTitle(zagolovok)
	w.SetSize(shirina, vysota, webview.HintNone)
	privyazat(w)
	if storozh {
		// GTK не терпит вызовов из чужого потока: всё — через Dispatch.
		zakrytOkno := func() { w.Dispatch(w.Terminate) }
		perejtiNaAdres := func(adres string) { w.Dispatch(func() { w.Navigate(adres) }) }
		perezapustitPriNovoySluzhbe := func(adres string) bool {
			v := versiyaSluzhby(adres)
			if !nuzhenPerezapuskOkna(podpiska.Versiya, v) {
				return false
			}
			log.Printf("сторож окна: служба уже версии %s, окно — %s; перезапускаю окно", v, podpiska.Versiya)
			nadoPerezapustit.Store(true)
			zakrytOkno()
			return true
		}
		// Первый же промах — искать новую копию: окно, открытое в секунды
		// перезапуска службы, иначе 6 с стоит с «Connection refused».
		go storozhitSluzhbu(url, hranenie.Papka(), shagStorozha, 1, srokOzhidaniyaZameny, zakrytOkno, perejtiNaAdres, perezapustitPriNovoySluzhbe)
	}
	w.Navigate(url)
	w.Run()
}

func pokazatOkno(url string) { otkrytOknoWebKitSoStorozhem("Kelevra", url, 420, 740, true) }

// zapustitOknoZanovoLinux — новая копия окна взамен этой (файл на диске уже
// новый, putDlyaNovoyKopii), своей сессией — как «Открыть» в значке.
func zapustitOknoZanovoLinux() {
	sebya, err := os.Executable()
	if err != nil {
		log.Printf("перезапуск окна: не понял свой путь: %v", err)
		return
	}
	put := putDlyaNovoyKopii(sebya)
	if err := zapustitOknoOtdelno(put); err != nil {
		log.Printf("перезапуск окна: не запустил %s: %v", put, err)
		return
	}
	log.Printf("перезапуск окна: запущена новая копия из %s", put)
}

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
