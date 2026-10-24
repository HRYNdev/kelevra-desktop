//go:build windows

package main

import (
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// imyaZamkaZnachka — замок значка в сеансе человека (`Local\`): значок свой
// у каждого вошедшего пользователя, прав администратора не требует. Имя
// отдельное от замка запуска (internal/kopiya): тот держится секунды на
// время решения «чья копия», а этот — всю жизнь значка.
var imyaZamkaZnachka = `Local\KelevraZnachokTreya` // var: тест берёт своё имя

// klassOknaTreya — класс скрытого окна значка (trey_windows.go: zapustitTrey).
// По нему видно и значок версии ДО замка: у тех замка нет.
const klassOknaTreya = "KelevraTreyWnd"

var procFindWindowExW = user32TreyDLL.NewProc("FindWindowExW")

// zanyatMestoZnachka — берёт замок значка на всю жизнь процесса. false —
// замок держит другой живой значок. Примитив недоступен — не мешаем
// значку встать (лучше редкий второй значок, чем ни одного).
//
// Мьютекс Windows принадлежит ПОТОКУ, а не процессу: уйди поток-владелец, и
// замок станет «брошенным» — следующий значок счёл бы его свободным. Поэтому
// берёт и держит его своя горутина, прибитая к потоку ОС до конца процесса.
func zanyatMestoZnachka(srok time.Duration) bool {
	itog := make(chan bool, 1)
	go func() {
		runtime.LockOSThread() // не Unlock: поток держит замок до выхода
		imya, err := windows.UTF16PtrFromString(imyaZamkaZnachka)
		if err != nil {
			itog <- true
			return
		}
		h, err := windows.CreateMutex(nil, false, imya)
		if err != nil || h == 0 {
			itog <- true
			return
		}
		sobytie, err := windows.WaitForSingleObject(h, uint32(srok.Milliseconds()))
		if err == nil && (sobytie == windows.WAIT_OBJECT_0 || sobytie == windows.WAIT_ABANDONED) {
			itog <- true
			select {} // держим замок, пока жив процесс; ОС снимет его при выходе
		}
		windows.CloseHandle(h)
		itog <- false
	}()
	return <-itog
}

// zamokZnachkaZanyat — держит ли замок значка кто-то живой. Объект может
// существовать и свободным (его хендл как раз сейчас держит соседнее окно с
// той же проверкой), поэтому смотрим не на «есть ли объект», а на «можно ли
// им завладеть»: можно — значка нет, тут же отдаём.
func zamokZnachkaZanyat() bool {
	// Завладеть и отдать — в ОДНОМ потоке ОС: ReleaseMutex из другого потока
	// отказывает, и окно незаметно держало бы замок значка у себя.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	imya, err := windows.UTF16PtrFromString(imyaZamkaZnachka)
	if err != nil {
		return false
	}
	h, err := windows.CreateMutex(nil, false, imya)
	if err != nil || h == 0 {
		return false
	}
	defer windows.CloseHandle(h)
	sobytie, err := windows.WaitForSingleObject(h, 0)
	if err == nil && (sobytie == windows.WAIT_OBJECT_0 || sobytie == windows.WAIT_ABANDONED) {
		windows.ReleaseMutex(h)
		return false
	}
	return true
}

// oknoTreyaEst — есть ли в сеансе скрытое окно значка (HWND_MESSAGE).
// FindWindowW такие окна не видит, нужен FindWindowExW с родителем
// HWND_MESSAGE.
func oknoTreyaEst() bool {
	klass, err := windows.UTF16PtrFromString(klassOknaTreya)
	if err != nil {
		return false
	}
	r, _, _ := procFindWindowExW.Call(hwndMessage, 0, uintptr(unsafe.Pointer(klass)), 0)
	return r != 0
}

// znachokZhivOS — жив ли значок: замок занят (значок этой версии и новее)
// или есть его окно (значок версии до замка).
func znachokZhivOS() bool {
	return zamokZnachkaZanyat() || oknoTreyaEst()
}
