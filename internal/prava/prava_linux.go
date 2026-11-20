//go:build linux

package prava

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Est — права есть у root. KELEVRA_PRAVA=net гасит их на стенде (см. prava_other.go).
func Est() bool {
	if os.Getenv("KELEVRA_PRAVA") == "net" {
		return false
	}
	return os.Geteuid() == 0
}

// PoprositDlya — запрос прав без ожидания итога.
func PoprositDlya(argumenty string) error {
	cmd, err := komandaPkexec(argumenty)
	if err != nil {
		return err
	}
	return cmd.Start()
}

// PoprositIZhdat — аналог UAC: pkexec показывает системное окно пароля
// администратора (агент polkit рабочего стола) и запускает эту же программу
// от root с аргументами; ждём её итога не дольше srok.
func PoprositIZhdat(okno uintptr, argumenty string, srok time.Duration) error {
	ctx, otmena := context.WithTimeout(context.Background(), srok)
	defer otmena()
	cmd, err := komandaPkexec(argumenty)
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...).CombinedOutput()
	if err == nil {
		return nil
	}
	var kod *exec.ExitError
	if errors.As(err, &kod) {
		switch kod.ExitCode() {
		case 126, 127: // pkexec: человек отказал или не прошёл проверку
			return fmt.Errorf("права администратора не даны")
		}
	}
	return fmt.Errorf("установка с правами не удалась: %v: %s", err, strings.TrimSpace(string(out)))
}

func komandaPkexec(argumenty string) (*exec.Cmd, error) {
	pk, err := exec.LookPath("pkexec")
	if err != nil {
		return nil, fmt.Errorf("нет pkexec — выполните в терминале: sudo kelevra %s", argumenty)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return exec.Command(pk, append([]string{exe}, strings.Fields(argumenty)...)...), nil
}
