//go:build windows

package kopiya

import (
	"time"

	"golang.org/x/sys/windows"
)

// Vzyat берёт именованный мьютекс ОС — Windows-объект, а не файл: «занят
// или свободен» решает ядро одним системным вызовом, без временного окна
// между «прочитал» и «записал», в которое попадали два .exe, запущенных
// почти одновременно (см. zamok.go). CreateMutex создаёт объект без
// попытки завладеть им (initialOwner=false): владение всегда берётся
// явным WaitForSingleObject ниже, одинаково для «объект только что
// создан» и «объект уже существовал», — иначе пришлось бы отдельно
// разбирать ERROR_ALREADY_EXISTS, а этого мало: объект мог существовать и
// быть СВОБОДНЫМ (предыдущий владелец уже вызвал Otdat).
//
// `Local\` — объект виден только в сессии этого пользователя: то, что
// нужно для однопользовательского десктоп-приложения, и не требует прав
// администратора (`Global\` их требует на части систем).
//
// Если владелец мьютекса умер, не вызвав ReleaseMutex (жёсткий Kill,
// авария), ОС сама помечает его WAIT_ABANDONED при следующем ожидании —
// это тоже успешный захват: мёртвый процесс не должен вечно блокировать
// живых.
func Vzyat(timeout time.Duration) (*Zamok, bool) {
	imya, err := windows.UTF16PtrFromString(`Local\` + imyaZamka)
	if err != nil {
		return nil, false
	}
	handle, err := windows.CreateMutex(nil, false, imya)
	if err != nil || handle == 0 {
		return nil, false
	}
	sobytie, err := windows.WaitForSingleObject(handle, uint32(timeout.Milliseconds()))
	if err != nil || (sobytie != windows.WAIT_OBJECT_0 && sobytie != windows.WAIT_ABANDONED) {
		// Не завладели (таймаут — кто-то другой держит дольше нашего
		// бюджета ожидания) или сам вызов отказал: закрываем хендл здесь,
		// он больше не понадобится этому процессу.
		windows.CloseHandle(handle)
		return nil, false
	}
	return &Zamok{zakryt: func() {
		windows.ReleaseMutex(handle)
		windows.CloseHandle(handle)
	}}, true
}
