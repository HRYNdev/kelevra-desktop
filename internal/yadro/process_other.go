//go:build !windows

package yadro

import (
	"os/exec"
	"syscall"
)

func spryatatOkno(cmd *exec.Cmd) {}

// zavershit — на своём стенде даём ядру закрыться по-хорошему.
func zavershit(cmd *exec.Cmd) error { return cmd.Process.Signal(syscall.SIGTERM) }
