//go:build windows

package yadro

import (
	"syscall"
	"unsafe"
)

// Предопределённый хэндл HKEY_CURRENT_USER (winreg.h) — не открывается,
// подставляется как есть.
const hkeyCurrentUser = 0x80000001

const (
	regDword = 4

	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

var (
	advapi32           = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW  = advapi32.NewProc("RegOpenKeyExW")
	procRegSetValueExW = advapi32.NewProc("RegSetValueExW")
	procRegCloseKey    = advapi32.NewProc("RegCloseKey")

	wininet               = syscall.NewLazyDLL("wininet.dll")
	procInternetSetOption = wininet.NewProc("InternetSetOptionW")
)

// sbrositSistemnyProksi выключает системный прокси в реестре пользователя и
// говорит системе, что настройки поменялись (иначе уже открытые программы
// узнают об этом нескоро).
//
// Тихо уходит при любой ошибке: это уборка за собой, а не операция, ради
// которой стоит валить остановку ядра.
func sbrositSistemnyProksi() {
	put, err := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Internet Settings`)
	if err != nil {
		return
	}
	var hkey syscall.Handle
	r, _, _ := procRegOpenKeyExW.Call(
		uintptr(hkeyCurrentUser),
		uintptr(unsafe.Pointer(put)),
		0,
		uintptr(syscall.KEY_SET_VALUE),
		uintptr(unsafe.Pointer(&hkey)),
	)
	if r != 0 {
		return
	}
	defer procRegCloseKey.Call(uintptr(hkey))

	imya, err := syscall.UTF16PtrFromString("ProxyEnable")
	if err != nil {
		return
	}
	var znachenie uint32
	procRegSetValueExW.Call(
		uintptr(hkey),
		uintptr(unsafe.Pointer(imya)),
		0,
		regDword,
		uintptr(unsafe.Pointer(&znachenie)),
		4,
	)

	procInternetSetOption.Call(0, internetOptionSettingsChanged, 0, 0)
	procInternetSetOption.Call(0, internetOptionRefresh, 0, 0)
}
