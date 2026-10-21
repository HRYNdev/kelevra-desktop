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
//
// Переменная, а не константа, только ради проверок: тесты уводят её в свою
// ветку и не трогают настоящий автозапуск того, кто их гоняет.
var vetka = `Software\Microsoft\Windows\CurrentVersion\Run`

// strokaNa собирает для exe put то, что Windows выполнит при входе в систему.
// Путь в кавычках обязателен: у человека приложение лежит в папке с пробелами
// (%LOCALAPPDATA%\Programs\...), и без кавычек Windows запустит «C:\Program».
func strokaNa(put string) string {
	return StrokaDlya(put, Nuzhnyy(sluzhbaEst()))
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
	put, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("не понял, где лежу сам: %w", err)
	}
	return UstarelaDlya(put)
}

// UstarelaDlya — то же, что Ustarela, но сверяет с exe put (окно сверяет с
// копией в Program Files, а не с собой).
func UstarelaDlya(put string) (bool, error) {
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
	return !strings.EqualFold(strings.TrimSpace(est), strokaNa(put)), nil
}

// Vklyuchit прописывает запуск вместе с Windows. Повторный вызов не ломается
// и заодно лечит устаревший путь.
func Vklyuchit() error {
	put, err := os.Executable()
	if err != nil {
		return fmt.Errorf("не понял, где лежу сам: %w", err)
	}
	return VklyuchitNa(put)
}

// VklyuchitNa прописывает запуск вместе с Windows файла put (окно пишет
// копию в Program Files, даже если само запущено из «Загрузок»).
func VklyuchitNa(put string) error {
	nado := strokaNa(put)
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
//
// Переменная — точка подмены для проверок: на машине разработчика служба
// может стоять по-настоящему, и режим записи зависел бы от неё.
var sluzhbaEst = func() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\KelevraSluzhba`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	k.Close()
	return true
}

// PochinitNaProgramFiles сверяет запись автозапуска этого пользователя с
// копией в Program Files (putPF) и переписывает её, если она смотрит не туда
// (решение — Pochinka). Зовут окно и значок при каждом старте: это процессы
// в сеансе человека, их HKCU — его ветка. izmeneno=false и пустые bylo/stalo
// — запись верная, её нет или переписать не на что.
func PochinitNaProgramFiles(putPF string) (bylo, stalo string, izmeneno bool, err error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, vetka, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return "", "", false, fmt.Errorf("не открыть ветку автозагрузки: %w", err)
	}
	defer k.Close()
	est := true
	bylo, _, err = k.GetStringValue(Imya)
	if err == registry.ErrNotExist {
		est, err = false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("не прочитать запись автозагрузки: %w", err)
	}
	stalo, nado := Pochinka(bylo, est, putPF, Nuzhnyy(sluzhbaEst()))
	if !nado {
		return bylo, "", false, nil
	}
	if err := k.SetStringValue(Imya, stalo); err != nil {
		return bylo, stalo, false, fmt.Errorf("не переписать автозагрузку: %w", err)
	}
	return bylo, stalo, true, nil
}

// VseZapisi — записи автозапуска Kelevra у всех вошедших пользователей
// машины (для diagnostika пульта). Диагностику собирает служба от системы, а
// её HKCU — ветка самой системы, не человека; поэтому смотрим в HKEY_USERS:
// там загружены ветки всех, кто сейчас вошёл. S-1-5-21-… — обычные учётки,
// «…_Classes» — их ассоциации файлов, не автозапуск. Ветку, которую открыть
// не дали (служба в сеансе человека смотрит чужую), пропускаем молча.
func VseZapisi() ([]Zapis, error) {
	koren, err := registry.OpenKey(registry.USERS, "", registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil, fmt.Errorf("не открыть HKEY_USERS: %w", err)
	}
	imena, err := koren.ReadSubKeyNames(-1)
	koren.Close()
	if err != nil {
		return nil, fmt.Errorf("не прочитать HKEY_USERS: %w", err)
	}
	var zapisi []Zapis
	for _, sid := range imena {
		if !strings.HasPrefix(sid, "S-1-5-21-") || strings.HasSuffix(sid, "_Classes") {
			continue
		}
		k, err := registry.OpenKey(registry.USERS, sid+`\`+vetka, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		stroka, _, err := k.GetStringValue(Imya)
		k.Close()
		if err != nil || strings.TrimSpace(stroka) == "" {
			continue
		}
		z := Zapis{Stroka: stroka, Put: PutIzZapisi(stroka)}
		if st, err := os.Stat(z.Put); err == nil && !st.IsDir() {
			z.FaylEst = true
		}
		zapisi = append(zapisi, z)
	}
	return zapisi, nil
}
