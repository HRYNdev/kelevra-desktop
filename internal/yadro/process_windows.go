//go:build windows

package yadro

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// spryatatOkno — ядро запускается без консольного окна.
func spryatatOkno(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// zavershit — на Windows у sing-box нет SIGTERM, гасим процесс напрямую.
func zavershit(cmd *exec.Cmd) error { return cmd.Process.Kill() }

// procQueryFullProcessImageName — самой функции нет в пришпиленной версии
// x/sys/windows (v0.0.0-20210218145245), но DLL и её адрес заводить можно
// напрямую через LazyDLL: вызываем без поднятия версии зависимости.
var procQueryFullProcessImageName = windows.NewLazySystemDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")

// putObrazaProcessa — путь к реальному образу процесса с данным PID, каким
// его знает ОС, а не строка из чужого конфига. Второе возвращаемое значение —
// «узнали ли»: false означает «ОС не ответила» (нет прав, процесс исчез между
// проверками и т.п.), а НЕ «это чужой процесс». Вызывающий обязан пропускать
// неузнанное, а не отвергать по нему: ложный обрыв туннеля хуже самой беды
// опознания (см. Prinyat).
func putObrazaProcessa(pid int) (string, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	bufer := make([]uint16, windows.MAX_PATH)
	razmer := uint32(len(bufer))
	r1, _, _ := procQueryFullProcessImageName.Call(
		uintptr(h),
		0,
		uintptr(unsafe.Pointer(&bufer[0])),
		uintptr(unsafe.Pointer(&razmer)),
	)
	if r1 == 0 {
		return "", false
	}
	return syscall.UTF16ToString(bufer[:razmer]), true
}
