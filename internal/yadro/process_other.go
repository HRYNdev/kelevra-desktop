//go:build !windows

package yadro

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

func spryatatOkno(cmd *exec.Cmd) {}

// zavershit — на своём стенде даём ядру закрыться по-хорошему.
func zavershit(cmd *exec.Cmd) error { return cmd.Process.Signal(syscall.SIGTERM) }

// pidyPoImeni — номера процессов, чей образ называется imya, по /proc.
// Только грубый фильтр для NashiYadra, см. одноимённую функцию для Windows.
func pidyPoImeni(imya string) []int {
	zapisi, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pidy []int
	for _, z := range zapisi {
		pid, err := strconv.Atoi(z.Name())
		if err != nil {
			continue
		}
		obraz, znaem := putObrazaProcessa(pid)
		if znaem && filepath.Base(obraz) == imya {
			pidy = append(pidy, pid)
		}
	}
	return pidy
}

// putObrazaProcessa — путь к реальному образу процесса с данным PID через
// /proc. Второе значение — «узнали ли»: false значит «не смогли спросить ОС»
// (нет /proc, процесс исчез, нет прав), а НЕ «это чужой процесс» — см.
// комментарий к одноимённой функции в process_windows.go.
func putObrazaProcessa(pid int) (string, bool) {
	put, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return "", false
	}
	return put, true
}
