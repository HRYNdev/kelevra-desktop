//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	user32      = syscall.NewLazyDLL("user32.dll")
	messageBoxW = user32.NewProc("MessageBoxW")
)

const (
	mbOK            = 0x00000000
	mbIconError     = 0x00000010
	mbSetForeground = 0x00010000
	mbTopMost       = 0x00040000
)

// skazat показывает пользователю системное окно с текстом.
//
// Это единственный способ что-то сказать до того, как поднялось своё окно:
// приложение оконное, консоли нет, stderr уходит в никуда. user32.dll есть
// в любой Windows, никаких зависимостей это не добавляет.
// Текст такого окна пользователь может скопировать целиком по Ctrl+C —
// значит, причину отказа он сможет переслать, а не пересказать.
func skazat(zagolovok, tekst string) {
	z, err := syscall.UTF16PtrFromString(zagolovok)
	if err != nil {
		return
	}
	t, err := syscall.UTF16PtrFromString(tekst)
	if err != nil {
		return
	}
	messageBoxW.Call(0,
		uintptr(unsafe.Pointer(t)),
		uintptr(unsafe.Pointer(z)),
		mbOK|mbIconError|mbSetForeground|mbTopMost)
}
