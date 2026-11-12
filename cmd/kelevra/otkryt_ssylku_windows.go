//go:build windows

package main

import (
	"log"
	"syscall"
	"unsafe"

	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

var procShellExecuteW = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")

// otkrytSsylkuOplaty — кнопка «Открыть ссылку» шторки оплаты: ссылка на
// перевод — в системный браузер человека. Открывает процесс окна (он от
// имени человека, служба — от системы). Только https (podpiska.SsylkaOplatyGodna).
func otkrytSsylkuOplaty(ssylka string) bool {
	if !podpiska.SsylkaOplatyGodna(ssylka) {
		log.Printf("оплата: ссылка не https — не открываю")
		return false
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	fayl, err := syscall.UTF16PtrFromString(ssylka)
	if err != nil {
		return false
	}
	const swShowNormal = 1
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(fayl)), 0, 0, swShowNormal)
	// ShellExecuteW: значение > 32 — успех.
	if r <= 32 {
		log.Printf("оплата: ссылка не открылась (код %d)", r)
		return false
	}
	return true
}
