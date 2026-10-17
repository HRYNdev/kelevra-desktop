//go:build windows

package prava

import (
	"testing"
	"unsafe"
)

// SHELLEXECUTEINFOW на amd64 — 112 байт, hProcess по смещению 104. Разойдись
// раскладка с Windows — ShellExecuteExW отказал бы (неверный cbSize) или
// вернул описатель процесса не туда, и ожидание установки службы ждало бы
// мусор. Проверяем арифметикой: живой вызов показал бы окно UAC.
func TestRaskladkaShellExecuteInfo(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("раскладка проверяется для amd64")
	}
	if r := unsafe.Sizeof(shellExecuteInfo{}); r != 112 {
		t.Fatalf("SHELLEXECUTEINFOW: размер %d, а Windows ждёт 112", r)
	}
	if o := unsafe.Offsetof(shellExecuteInfo{}.hProcess); o != 104 {
		t.Fatalf("hProcess по смещению %d, а Windows ждёт 104", o)
	}
	if o := unsafe.Offsetof(shellExecuteInfo{}.nShow); o != 48 {
		t.Fatalf("nShow по смещению %d, а Windows ждёт 48", o)
	}
}
