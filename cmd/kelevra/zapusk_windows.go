//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// zapustitOtdelnuyuSluzhbu запускает тот же .exe ещё раз, с флагом --sluzhba,
// полностью отсоединённым от текущего процесса: без своей консоли и вне
// группы процессов родителя. Раньше окно и служба были одним процессом, и
// закрытие окна крестиком убивало службу вместе с собой — это и есть беда,
// которую разводит вся эта переделка. DETACHED_PROCESS рвёт связь с консолью
// родителя, CREATE_NEW_PROCESS_GROUP — со своей группой (иначе Ctrl+C или
// падение окна дотягивается до службы через сигналы группы).
func zapustitOtdelnuyuSluzhbu() error {
	sebya, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(sebya, argSluzhba)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Release, а не Wait: этот процесс не нянька службе, он лишь её поднял.
	// Wait держал бы хендл и (что важнее по духу задачи) подразумевал бы, что
	// служба живёт, пока жив тот, кто её запустил, — а это и есть та самая дыра.
	return cmd.Process.Release()
}
