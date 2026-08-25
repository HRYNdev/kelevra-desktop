//go:build windows

package main

import "golang.org/x/sys/windows"

// stillActive — код STILL_ACTIVE (259), которым Windows отвечает на
// GetExitCodeProcess, пока процесс ещё не завершился. Своё имя, а не
// windows.STATUS_PENDING: то же число, но константа из x/sys/windows про
// другое (коды NTSTATUS), совпадение значений — не повод занимать чужое имя.
const stillActive = 259

// zhivProcess — жив ли прямо сейчас процесс с этим PID. Открываем хендл
// системным вызовом и спрашиваем код завершения: сам факт открытия хендла
// ничего не значит (Windows отдаёт хендлы и на только что умерший процесс),
// а вот STILL_ACTIVE — надёжный признак жизни, тот же, каким пользуется
// Диспетчер задач.
func zhivProcess(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var kod uint32
	if err := windows.GetExitCodeProcess(h, &kod); err != nil {
		return false
	}
	return kod == stillActive
}
