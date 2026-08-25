//go:build !windows

package main

import (
	"os"
	"syscall"
)

// zhivProcess вне Windows нужен только затем, чтобы пакет собирался и
// проверялся на сервере без Windows (см. okno_other.go, zapusk_other.go):
// смена режима через UAC — вещь исключительно Windows. Сигнал 0 не убивает
// процесс, а лишь спрашивает ОС, жив ли PID.
func zhivProcess(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
