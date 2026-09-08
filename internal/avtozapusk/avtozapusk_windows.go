//go:build windows

package avtozapusk

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// vetka — пользовательская автозагрузка. Именно HKCU, а не HKLM: запись в
// машинную ветку требует прав администратора, а Kelevra запускается обычным
// пользователем, и просить у него UAC ради галочки — плохой размен.
const vetka = `Software\Microsoft\Windows\CurrentVersion\Run`

// stroka собирает то, что Windows выполнит при входе в систему.
// Путь в кавычках обязателен: у человека приложение лежит в папке с пробелами
// (%LOCALAPPDATA%\Programs\...), и без кавычек Windows запустит «C:\Program».
func stroka() (string, error) {
	put, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("не понял, где лежу сам: %w", err)
	}
	return `"` + put + `" ` + Nuzhnyy(sluzhbaEst()), nil
}

// Vklyuchen отвечает на вопрос «стартует ли Kelevra вместе с Windows».
//
// Отдельно возвращается ошибка чтения: «записи нет» и «не смог посмотреть» —
// разные вещи, и показывать второе как выключенный тумблер значит врать
// человеку про состояние его же компьютера.
func Vklyuchen() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, vetka, registry.QUERY_VALUE)
	if err != nil {
		return false, fmt.Errorf("не открыть ветку автозагрузки: %w", err)
	}
	defer k.Close()
	est, _, err := k.GetStringValue(Imya)
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("не прочитать запись автозагрузки: %w", err)
	}
	// Запись могла остаться от прошлой установки в другой папке — тогда
	// автозапуск включён, но запускает не тот файл. Для человека это
	// «включено», а чинится оно повторным включением (Vklyuchit перезапишет).
	return strings.TrimSpace(est) != "", nil
}

// Ustarela говорит, что запись есть, но ведёт на другой файл: так бывает после
// переустановки в другую папку или после переименования .exe.
func Ustarela() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, vetka, registry.QUERY_VALUE)
	if err != nil {
		return false, fmt.Errorf("не открыть ветку автозагрузки: %w", err)
	}
	defer k.Close()
	est, _, err := k.GetStringValue(Imya)
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("не прочитать запись автозагрузки: %w", err)
	}
	nado, err := stroka()
	if err != nil {
		return false, err
	}
	return !strings.EqualFold(strings.TrimSpace(est), nado), nil
}

// Vklyuchit прописывает запуск вместе с Windows. Повторный вызов не ломается
// и заодно лечит устаревший путь.
func Vklyuchit() error {
	nado, err := stroka()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, vetka, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("не открыть ветку автозагрузки на запись: %w", err)
	}
	defer k.Close()
	if err := k.SetStringValue(Imya, nado); err != nil {
		return fmt.Errorf("не записать автозагрузку: %w", err)
	}
	return nil
}

// Vyklyuchit убирает запись. Отсутствие записи — это уже нужный результат,
// а не ошибка: иначе повторное выключение краснеет на ровном месте.
func Vyklyuchit() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, vetka, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("не открыть ветку автозагрузки на запись: %w", err)
	}
	defer k.Close()
	if err := k.DeleteValue(Imya); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("не убрать автозагрузку: %w", err)
	}
	return nil
}

// sluzhbaEst — установлена ли служба Windows. Пакет автозапуска про службы
// ничего не знает, поэтому спрашивает системный реестр напрямую, а не тянет
// зависимость: иначе получился бы круг, ведь установка службы сама правит
// автозапуск.
//
// Ошибка чтения означает «не знаю» и трактуется как «службы нет»: старый
// режим автозапуска работает всегда, а новый — только когда служба точно есть.
func sluzhbaEst() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\KelevraSluzhba`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	k.Close()
	return true
}
