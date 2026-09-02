//go:build windows

// Пакет prava: есть ли у приложения права администратора и как их попросить.
//
// От прав зависит не удобство, а режим работы: без них ядро не поднимет
// туннель и защищены будут только программы, уважающие системный прокси.
package prava

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const tokenElevation = 20 // TOKEN_INFORMATION_CLASS.TokenElevation

// Est — запущено ли приложение с правами администратора.
func Est() bool {
	var token syscall.Token
	if err := syscall.OpenProcessToken(syscall.Handle(^uintptr(0)), syscall.TOKEN_QUERY, &token); err != nil {
		// ^0 — псевдодескриптор текущего процесса; если и это не вышло,
		// считаем, что прав нет: лучше прокси-режим, чем ядро, падающее на старте.
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

// Poprosit перезапускает приложение с правами администратора (окно UAC).
// Возвращается только при отказе: при согласии старый процесс должен уйти,
// иначе на машине окажутся две копии.
//
// smenaPID — pid ЭТОЙ, ещё не повышенной копии. Передаётся новой копии
// аргументом --smena, чтобы та знала: она не первый запуск, а смена режима,
// и старая копия может быть ещё жива (см. cmd/kelevra/main.go: zhdatSmenu).
// Раньше метка единственного экземпляра снималась ДО этого вызова, и новая
// копия, ничего не зная о старой, стартовала как первая — обе оказывались
// живы разом (беда 25.08: открывалось два окна). Теперь метка живёт у старой
// копии до её смерти, а связь между копиями — явный аргумент, а не гонка.
func Poprosit(smenaPID int) error {
	return poprosit(smenaPID, false)
}

// PoprositPriStarte — то же самое окно UAC, но при запросе прав сразу на
// обычном старте программы (заказ 29.08), а не по кнопке «Включить для
// всех программ». Разница — аргумент --pri-starte новой копии: main.go по
// нему не зовёт vosstanovitPolnuyuZashchitu (человек ещё ничего не подключал,
// он просто открыл приложение), в отличие от --smena после кнопки.
func PoprositPriStarte(smenaPID int) error {
	return poprosit(smenaPID, true)
}

func poprosit(smenaPID int, priStarte bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecute := shell32.NewProc("ShellExecuteW")

	verb, _ := syscall.UTF16PtrFromString("runas")
	fayl, _ := syscall.UTF16PtrFromString(exe)
	papka, _ := syscall.UTF16PtrFromString(katalog(exe))
	arg := fmt.Sprintf("--smena %d", smenaPID)
	if priStarte {
		arg += " --pri-starte"
	}
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
