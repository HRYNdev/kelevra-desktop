//go:build windows

package yadro

import (
	"os/exec"
	"strings"
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

// pidyPoImeni — номера процессов, чей исполняемый файл называется imya
// (без учёта регистра). Только грубый фильтр для NashiYadra: окончательно
// «наш ли это образ» решает ОС через putObrazaProcessa.
func pidyPoImeni(imya string) []int {
	snimok, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snimok)
	var zapis windows.ProcessEntry32
	zapis.Size = uint32(unsafe.Sizeof(zapis))
	var pidy []int
	for err = windows.Process32First(snimok, &zapis); err == nil; err = windows.Process32Next(snimok, &zapis) {
		if strings.EqualFold(windows.UTF16ToString(zapis.ExeFile[:]), imya) {
			pidy = append(pidy, int(zapis.ProcessID))
		}
	}
	return pidy
}

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
