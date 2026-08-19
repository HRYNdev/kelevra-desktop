//go:build windows

package yadro

import (
	"os/exec"
	"syscall"
)

// spryatatOkno — ядро запускается без консольного окна.
func spryatatOkno(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// zavershit — на Windows у sing-box нет SIGTERM, гасим процесс напрямую.
func zavershit(cmd *exec.Cmd) error { return cmd.Process.Kill() }
