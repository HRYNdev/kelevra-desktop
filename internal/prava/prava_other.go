//go:build !windows

package prava

import (
	"errors"
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

// Poprosit — окно UAC есть только на Windows. smenaPID сохраняет ту же
// сигнатуру, что и windows-версия (см. prava_windows.go), чтобы sluzhba.go
// собиралась одинаково на обеих платформах.
func Poprosit(smenaPID int) error {
	return fmt.Errorf("запросить права можно только на Windows")
}

// PoprositPriStarte — та же заглушка для запроса прав сразу при старте (см.
// prava_windows.go), нужна только чтобы cmd/kelevra собирался и на не-Windows.
func PoprositPriStarte(smenaPID int) error {
	return fmt.Errorf("запросить права можно только на Windows")
}

// PoprositDlya — на не-Windows правами так не управляют: там либо запущено от
// root, либо нет, и окна с подтверждением не существует.
func PoprositDlya(argumenty string) error {
	return errors.New("запрос прав есть только в Windows")
}
