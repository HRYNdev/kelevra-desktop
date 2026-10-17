//go:build windows

// Пакет prava: есть ли у приложения права администратора и как их попросить.
//
// Без прав администратора ядро не поднимет туннель, а другого режима у
// Kelevra нет (0.6.77): права просятся один раз, на установку службы Windows.
package prava

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const tokenElevation = 20 // TOKEN_INFORMATION_CLASS.TokenElevation

// Est — запущено ли приложение с правами администратора.
func Est() bool {
	var token syscall.Token
	if err := syscall.OpenProcessToken(syscall.Handle(^uintptr(0)), syscall.TOKEN_QUERY, &token); err != nil {
		// ^0 — псевдодескриптор текущего процесса; если и это не вышло,
		// считаем, что прав нет: лучше честное «нужны права», чем ядро,
		// падающее на старте.
		return false
	}
	defer token.Close()
	var podnyat uint32
	var vernulos uint32
	err := syscall.GetTokenInformation(token, tokenElevation,
		(*byte)(unsafe.Pointer(&podnyat)), uint32(unsafe.Sizeof(podnyat)), &vernulos)
	if err != nil {
		return false
	}
	return podnyat != 0
}

// PoprositDlya просит права ради одного конкретного дела и передаёт новой
// копии ровно те аргументы, которые для него нужны.
//
// Появилась вместе со службой Windows: её установка — не «смена режима» и не
// «запрос при старте», это разовое действие, после которого копия уходит.
// Пропускать его через smenaPID было бы враньём: никакой смены копий тут нет.
func PoprositDlya(argumenty string) error { return zapustitSPravami(argumenty) }

// PoprositIZhdat — то же окно UAC, что PoprositDlya, но с ожиданием: копия с
// правами запускается, и вызывающий ждёт её конца (не дольше srok). Ошибка —
// отказ в окне UAC, ненулевой код выхода копии или истёкший срок.
//
// Нужна там, где за просьбой прав следует шаг, который без её итога делать
// нельзя: первый запуск ставит службу и только потом ищет её метку
// (cmd/kelevra: zaprositPravaNaStarte). Ожидание — по описателю процесса
// (ShellExecuteExW + SEE_MASK_NOCLOSEPROCESS), а не опросом наугад.
//
// okno — окно, поверх которого Windows покажет запрос (0 — без владельца):
// запрос от процесса без переднего плана Windows прячет мигающей кнопкой на
// панели задач, и человек его не видит.
func PoprositIZhdat(okno uintptr, argumenty string, srok time.Duration) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	fayl, _ := syscall.UTF16PtrFromString(exe)
	papka, _ := syscall.UTF16PtrFromString(katalog(exe))
	argy, _ := syscall.UTF16PtrFromString(argumenty)
	const (
		seeMaskNoCloseProcess = 0x00000040
		seeMaskNoAsync        = 0x00000100
		swShowNormal          = 1
		oshibkaOtmeneno       = 1223 // ERROR_CANCELLED — человек нажал «Нет»
	)
	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		hwnd:         okno,
		lpVerb:       verb,
		lpFile:       fayl,
		lpParameters: argy,
		lpDirectory:  papka,
		nShow:        swShowNormal,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	shell32 := syscall.NewLazyDLL("shell32.dll")
	r, _, e := shell32.NewProc("ShellExecuteExW").Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		if errno, ok := e.(syscall.Errno); ok && errno == oshibkaOtmeneno {
			return fmt.Errorf("права не выданы")
		}
		return fmt.Errorf("не удалось запросить права: %v", e)
	}
	if info.hProcess == 0 {
		return nil // процесс запущен, но описателя не дали — ждать нечем
	}
	defer syscall.CloseHandle(info.hProcess)
	sobytie, err := syscall.WaitForSingleObject(info.hProcess, uint32(srok/time.Millisecond))
	if err != nil {
		return fmt.Errorf("не дождался копии с правами: %w", err)
	}
	if sobytie != syscall.WAIT_OBJECT_0 {
		return fmt.Errorf("копия с правами не завершилась за %s", srok)
	}
	var kod uint32
	if err := syscall.GetExitCodeProcess(info.hProcess, &kod); err != nil {
		return fmt.Errorf("код выхода копии с правами не прочитан: %w", err)
	}
	if kod != 0 {
		return fmt.Errorf("копия с правами завершилась с кодом %d", kod)
	}
	return nil
}

// shellExecuteInfo — SHELLEXECUTEINFOW, раскладка amd64 (112 байт): поля
// идут в порядке структуры Windows, выравнивание Go совпадает с C.
type shellExecuteInfo struct {
	cbSize         uint32
	fMask          uint32
	hwnd           uintptr
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       uintptr
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      uintptr
	dwHotKey       uint32
	hIconOrMonitor uintptr
	hProcess       syscall.Handle
}

func zapustitSPravami(arg string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecute := shell32.NewProc("ShellExecuteW")

	verb, _ := syscall.UTF16PtrFromString("runas")
	fayl, _ := syscall.UTF16PtrFromString(exe)
	papka, _ := syscall.UTF16PtrFromString(katalog(exe))
	argy, _ := syscall.UTF16PtrFromString(arg)

	const swShowNormal = 1
	r, _, _ := shellExecute.Call(0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(fayl)),
		uintptr(unsafe.Pointer(argy)),
		uintptr(unsafe.Pointer(papka)),
		swShowNormal)
	// ShellExecuteW: значение > 32 — успех, всё остальное код ошибки.
	if r <= 32 {
		if r == 5 { // SE_ERR_ACCESSDENIED — человек нажал «Нет» в окне UAC
			return fmt.Errorf("права не выданы")
		}
		return fmt.Errorf("не удалось запросить права (код %d)", r)
	}
	return nil
}

func katalog(put string) string {
	if i := strings.LastIndexAny(put, `\/`); i > 0 {
		return put[:i]
	}
	return "."
}
