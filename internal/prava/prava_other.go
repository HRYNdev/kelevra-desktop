//go:build !windows

package prava

import (
	"fmt"
	"os"
)

// Est — на не-Windows системах (сервер, где приложение проверяется) правами
// считается root: туннель там тоже без него не поднять.
//
// KELEVRA_PRAVA=net гасит права принудительно. Это нужно на стенде: сервер
// работает под root, а поднять там настоящий туннель с auto_route значит
// увести весь трафик машины в проверяемое ядро. Переменную задаёт только стенд.
func Est() bool {
	if os.Getenv("KELEVRA_PRAVA") == "net" {
		return false
	}
	return os.Geteuid() == 0
}

// Poprosit — окно UAC есть только на Windows.
func Poprosit() error {
	return fmt.Errorf("запросить права можно только на Windows")
}
