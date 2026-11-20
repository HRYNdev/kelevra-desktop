//go:build linux

package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"fyne.io/systray"
)

// Значок на Linux — StatusNotifierItem по DBus (fyne.io/systray, без CGO):
// его показывают панели KDE, XFCE (модуль «Status Notifier»), GNOME с
// расширением AppIndicator. Пузыря с кликом там нет — обновление ставится
// пунктом меню «Обновить до X», как второй путь на Windows.

//go:embed znachok-64.png
var znachokTreyaPNG []byte

var (
	treyLinuxZamok sync.Mutex
	treyLinuxGotov bool
	punktObnovit   *systray.MenuItem
)

func zapustitTrey(vyhod chan<- struct{}) {
	systray.Run(func() {
		systray.SetIcon(znachokTreyaPNG)
		systray.SetTitle("Kelevra")
		systray.SetTooltip(podskazkaTreya())
		systray.SetOnTapped(otkrytOknoIzTreya)
		obn := systray.AddMenuItem("Обновить", "")
		obn.Hide()
		otkr := systray.AddMenuItem("Открыть", "")
		systray.AddSeparator()
		vyh := systray.AddMenuItem("Выход", "")
		treyLinuxZamok.Lock()
		punktObnovit = obn
		treyLinuxGotov = true
		treyLinuxZamok.Unlock()
		obnovitPodskazkuTreya()
		log.Printf("трей: значок поставлен (StatusNotifierItem)")
		go func() {
			for {
				select {
				case <-obn.ClickedCh:
					tychokVObnovlenieLinux()
				case <-otkr.ClickedCh:
					otkrytOknoIzTreya()
				case <-vyh.ClickedCh:
					select {
					case vyhod <- struct{}{}:
					default:
					}
					systray.Quit()
					return
				}
			}
		}()
	}, func() {})
}

func pokazatOblachkoObnovleniya(versiya string) bool {
	zapomnitObnovlenie(versiya)
	obnovitPodskazkuTreya()
	puzyrOplatyPoslednim.Store(false)
	ok := uvedomit("Kelevra: доступно обновление",
		fmt.Sprintf("Вышла версия %s. Поставить — пункт «Обновить» в меню значка.", versiya))
	log.Printf("трей: уведомление про версию %s -> %v", versiya, ok)
	return ok
}

func pokazatOblachkoOplaty(tekst string) bool {
	puzyrOplatyPoslednim.Store(true)
	ok := uvedomit("Kelevra", tekst)
	log.Printf("трей: уведомление об оплате -> %v", ok)
	return ok
}

func obnovitPodskazkuTreya() {
	treyLinuxZamok.Lock()
	gotov, obn := treyLinuxGotov, punktObnovit
	treyLinuxZamok.Unlock()
	if !gotov {
		return
	}
	systray.SetTooltip(podskazkaTreya())
	if tekst, est := punktMenyuObnovleniya(); est {
		obn.SetTitle(tekst)
		obn.Show()
	} else {
		obn.Hide()
	}
}

func ubratZnachokPriZavershenii() {
	treyLinuxZamok.Lock()
	gotov := treyLinuxGotov
	treyLinuxGotov = false
	treyLinuxZamok.Unlock()
	if gotov {
		systray.Quit()
	}
}

func tychokVObnovlenieLinux() {
	if ustanovitObnovlenie == nil {
		log.Printf("трей: тычок в обновление — хук установки не подключён")
		return
	}
	versiya, err := ustanovitObnovlenie()
	if err != nil {
		log.Printf("трей: обновление до %s не встало: %v", versiya, err)
		uvedomit("Kelevra", "Обновление не установлено: "+err.Error())
		return
	}
	log.Printf("трей: обновление до %s поставлено по тычку", versiya)
}

// otkrytOknoIzTreya — окно отдельной копией, в своей сессии (Setsid), чтобы
// закрытие значка его не гасило. Файл — тот, что на диске сейчас
// (putDlyaNovoyKopii): после обновления свой путь значка — уже убранный
// прежний файл.
func otkrytOknoIzTreya() {
	sebya, err := os.Executable()
	if err != nil {
		log.Printf("трей: «Открыть» не понял свой путь: %v", err)
		return
	}
	if err := zapustitOknoOtdelno(putDlyaNovoyKopii(sebya)); err != nil {
		log.Printf("трей: «Открыть» не смог запустить окно: %v", err)
	}
}

// zapustitOknoOtdelno — окно (без аргументов) отдельным процессом в своей сессии.
func zapustitOknoOtdelno(put string) error {
	cmd := exec.Command(put)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
