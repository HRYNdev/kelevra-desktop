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

// zapustitSebyaZanovo вне Windows — та же копия себя без отсоединения (см.
// zapustitOtdelnuyuSluzhbu выше и perezapusk_treya.go).
func zapustitSebyaZanovo(args ...string) (string, error) {
	sebya, err := os.Executable()
	if err != nil {
		return "", err
	}
	put := putDlyaPerezapuska(sebya)
	cmd := exec.Command(put, args...)
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return put, err
	}
	return put, cmd.Process.Release()
}
