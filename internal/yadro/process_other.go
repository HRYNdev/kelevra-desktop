//go:build !windows

package yadro

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func spryatatOkno(cmd *exec.Cmd) {}

// zavershit — на своём стенде даём ядру закрыться по-хорошему.
func zavershit(cmd *exec.Cmd) error { return cmd.Process.Signal(syscall.SIGTERM) }

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
