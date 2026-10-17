//go:build !windows

package prava

import (
	"errors"
	"os"
	"time"
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

// PoprositDlya — на не-Windows правами так не управляют: там либо запущено от
// root, либо нет, и окна с подтверждением не существует.
func PoprositDlya(argumenty string) error {
	return errors.New("запрос прав есть только в Windows")
}

// PoprositIZhdat — та же заглушка с ожиданием (см. prava_windows.go).
func PoprositIZhdat(okno uintptr, argumenty string, srok time.Duration) error {
	return errors.New("запрос прав есть только в Windows")
}
