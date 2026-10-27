//go:build windows

package nrpt

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

// NovayaSistema — настоящая система этой ОС: powershell.exe из System32.
// Нужны права системы или администратора (служба Kelevra работает от системы).
func NovayaSistema() Sistema { return &PowerShell{Zapusk: zapustitPowerShell} }

func zapustitPowerShell(ctx context.Context, scenariy string) (string, error) {
	koren := os.Getenv("SystemRoot")
	if koren == "" {
		koren = `C:\Windows`
	}
	exe := filepath.Join(koren, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	u := utf16.Encode([]rune(scenariy))
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	cmd := exec.CommandContext(ctx, exe, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("powershell: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
