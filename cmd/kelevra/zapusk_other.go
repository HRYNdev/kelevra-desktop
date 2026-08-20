//go:build !windows

package main

import (
	"os"
	"os/exec"
)

// zapustitOtdelnuyuSluzhbu вне Windows нужен только затем, чтобы пакет
// собирался и проверялся на сервере без Windows (см. okno_other.go,
// skazat_other.go). Настоящего отсоединения от родителя (DETACHED_PROCESS)
// тут нет — на этой платформе продукт не живёт, и точной беды тут нет.
func zapustitOtdelnuyuSluzhbu() error {
	sebya, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(sebya, argSluzhba)
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
