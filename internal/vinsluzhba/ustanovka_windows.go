//go:build windows

package vinsluzhba

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// UstanovitPolnostyu — всё, ради чего человека спрашивают об администраторе.
// Один вызов, одно подтверждение, дальше служба живёт сама.
//
// Порядок шагов не произволен. Папка и права заводятся ДО регистрации службы:
// служба стартует сразу после создания, и к этому моменту ей должно быть куда
// писать. Перенос данных идёт до старта по той же причине — иначе первая же
// секунда работы застанет пустой профиль, служба посчитает, что кода доступа
// нет, и человек увидит приглашение ввести его заново.
func UstanovitPolnostyu(putExe string) error {
	obshchaya := hranenie.PapkaObshchaya()
	if obshchaya == "" {
		return fmt.Errorf("не понять, где общая папка данных: пуста переменная PROGRAMDATA")
	}
	if err := os.MkdirAll(obshchaya, 0o755); err != nil {
		return fmt.Errorf("не создать общую папку %s: %w", obshchaya, err)
	}
	if err := zakrytPapku(obshchaya); err != nil {
		// Не приговор: служба работать будет. Но записать это надо громко —
		// папка с открытой записью означает, что подменить конфиг ядра может
		// любой на машине, а его читает процесс с правами системы.
		log.Printf("ВНИМАНИЕ: права на общую папку не выставлены: %v", err)
	}
	if staraya := staraya(); staraya != "" {
		if err := hranenie.Perenesti(staraya, obshchaya); err != nil {
			// Тоже не приговор: человек введёт код заново. Рушить установку
			// из-за этого хуже, чем остаться без переноса.
			log.Printf("ВНИМАНИЕ: данные не перенеслись из %s: %v", staraya, err)
		}
	}
	// Флаг «подключиться после установки» — до регистрации: служба стартует
	// сразу после неё и на первом же старте его читает
	// (sluzhba.PodklyuchitPosleUstanovki).
	if err := hranenie.OtmetitPodklyuchitPosleUstanovki(); err != nil {
		log.Printf("флаг подключения после установки не записан: %v", err)
	}
	// Служба запускается от системы, поэтому её exe кладём в Program Files
	// (туда пользователь писать не может), а не оставляем там, откуда запущена
	// установка (Загрузки, профиль). Регистрируем уже ЭТУ копию.
	est, _ := Ustanovlena()
	if est {
		// Служба уже есть (перенос со старого пути или повторное «Дать права»):
		// останавливаем, чтобы заменить занятый ею файл, и перенацеливаем.
		_ = Ostanovit()
	}
	kopiya, err := KopirovatVProgramFiles(putExe)
	if err != nil {
		return fmt.Errorf("не положить exe службы в Program Files: %w", err)
	}
	if est {
		if err := PerenastroitSluzhbu(kopiya); err != nil {
			return fmt.Errorf("не перенацелить службу на %s: %w", kopiya, err)
		}
		return Zapustit()
	}
	return Ustanovit(kopiya)
}

// KatalogProgramFiles — настоящий каталог Program Files у системы. Спрашиваем
// у оболочки, а не берём из переменной окружения: переменную процесса можно
// подменить из сеанса человека, а папка, куда потом пойдёт exe службы от
// системы, обязана быть той самой защищённой.
func KatalogProgramFiles() (string, error) {
	p, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFilesX64, 0)
	if err != nil {
		return "", fmt.Errorf("не узнать, где Program Files: %w", err)
	}
	return p, nil
}

// PutUstanovlennoy — exe службы в Program Files, если он там лежит. Пусто —
// службы там нет (или не узнать). Нужен окну и значку: когда служба новее них,
// перезапускаться надо из её файла, а не из старой копии в Загрузках.
func PutUstanovlennoy() string {
	pf, err := KatalogProgramFiles()
	if err != nil {
		return ""
	}
	p := PutSluzhby(pf)
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return ""
	}
	return p
}

// KopirovatVProgramFiles кладёт копию exe istochnik в <Program Files>\Kelevra\Kelevra.exe
// и возвращает путь копии. Требует прав администратора (у службы они есть всегда,
// у установки после подтверждения).
//
// Копия пишется во временный файл рядом и становится на место переименованием.
// Прежний файл на месте (в том числе занятый запущенным процессом) отодвигается
// в .old: переименовать запущенный exe Windows позволяет, а перезаписать нет.
// Хвост .old убирает обычная уборка (obnovlenie.UbratHvost).
func KopirovatVProgramFiles(istochnik string) (string, error) {
	pf, err := KatalogProgramFiles()
	if err != nil {
		return "", err
	}
	kat, cel := Katalog(pf), PutSluzhby(pf)
	if err := os.MkdirAll(kat, 0o755); err != nil {
		return "", fmt.Errorf("не создать %s: %w", kat, err)
	}
	if err := zakrytPapku(kat); err != nil {
		// Под Program Files права и так закрыты наследованием; говорим вслух, но
		// не рушим установку.
		log.Printf("ВНИМАНИЕ: права на %s не выставлены явно: %v", kat, err)
	}
	if a, errA := os.Stat(istochnik); errA == nil {
		if b, errB := os.Stat(cel); errB == nil && os.SameFile(a, b) {
			return cel, nil // запущены уже оттуда
		}
	}
	vhod, err := os.Open(istochnik)
	if err != nil {
		return "", fmt.Errorf("не открыть %s: %w", istochnik, err)
	}
	defer vhod.Close()
	vremenny, err := os.CreateTemp(kat, ".kelevra-*.new")
	if err != nil {
		return "", fmt.Errorf("не создать файл в %s: %w", kat, err)
	}
	imya := vremenny.Name()
	gotovo := false
	defer func() {
		if !gotovo {
			vremenny.Close()
			os.Remove(imya)
		}
	}()
	if _, err := io.Copy(vremenny, vhod); err != nil {
		return "", fmt.Errorf("не скопировать в %s: %w", imya, err)
	}
	if err := vremenny.Sync(); err != nil {
		return "", err
	}
	if err := vremenny.Close(); err != nil {
		return "", err
	}
	if _, err := os.Stat(cel); err == nil {
		staryy := cel + ".old"
		_ = os.Remove(staryy)
		if err := os.Rename(cel, staryy); err != nil {
			staryy = fmt.Sprintf("%s.old%d", cel, os.Getpid())
			if err := os.Rename(cel, staryy); err != nil {
				return "", fmt.Errorf("не отодвинуть прежний %s: %w", cel, err)
			}
		}
	}
	if err := os.Rename(imya, cel); err != nil {
		return "", fmt.Errorf("не поставить %s: %w", cel, err)
	}
	gotovo = true
	return cel, nil
}

// PerenestiVProgramFiles зовёт сама служба при старте. Если её exe лежит вне
// Program Files (так ставили до этой версии: из Загрузок, из профиля), копирует
// себя туда и перерегистрирует службу на копию. true: перерегистрировано, и
// процессу пора уйти с отказом, диспетчер поднимет службу уже из новой папки.
//
// Права на это у службы есть по построению (она работает от системы), окно
// подтверждения не нужно. Любая неудача возвращается ошибкой, и служба
// продолжает работать как работала: сломать запуск из-за переноса хуже, чем
// остаться на старом месте до следующего старта.
func PerenestiVProgramFiles() (bool, error) {
	pf, err := KatalogProgramFiles()
	if err != nil {
		return false, err
	}
	put, err := os.Executable()
	if err != nil {
		return false, err
	}
	if nastoyashchiy, err := filepath.EvalSymlinks(put); err == nil {
		put = nastoyashchiy
	}
	if !NuzhnoPerenosit(put, pf) {
		return false, nil
	}
	kopiya, err := KopirovatVProgramFiles(put)
	if err != nil {
		return false, err
	}
	if err := PerenastroitSluzhbu(kopiya); err != nil {
		return false, err
	}
	return true, nil
}

// staraya — папка данных в профиле человека, откуда переезжаем. Пустая строка
// означает, что переезжать неоткуда.
func staraya() string {
	d := os.Getenv("LOCALAPPDATA")
	if d == "" {
		return ""
	}
	p := filepath.Join(d, "Kelevra")
	if st, err := os.Stat(p); err != nil || !st.IsDir() {
		return ""
	}
	return p
}

// zakrytPapku оставляет запись только системе и администраторам, а обычным
// пользователям — чтение.
//
// Почему это важно именно здесь: в папке лежит конфиг ядра, а ядро запускает
// служба с правами системы. Папка, открытая на запись всем, означает, что
// любая программа в сеансе человека может переписать этот конфиг и увести
// через себя весь трафик машины.
//
// Делается штатным icacls, а не своей вознёй с дескрипторами: правило читается
// глазами, ошибается реже и разбирается любым, кто откроет свойства папки.
func zakrytPapku(put string) error {
	// /inheritance:r — снять унаследованные разрешения, иначе к нашим правилам
	// добавятся права родителя, где запись обычно разрешена всем.
	komandy := [][]string{
		{put, "/inheritance:r"},
		{put, "/grant:r", "*S-1-5-18:(OI)(CI)F"},      // система
		{put, "/grant:r", "*S-1-5-32-544:(OI)(CI)F"},  // администраторы
		{put, "/grant:r", "*S-1-5-32-545:(OI)(CI)RX"}, // пользователи: чтение
	}
	for _, args := range komandy {
		cmd := exec.Command("icacls", args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("icacls %v: %w: %s", args[1:], err, out)
		}
	}
	return nil
}

// UdalitPolnostyu снимает службу. Данные не трогает: они пригодятся и после
// удаления, а вернуть их человеку будет неоткуда.
func UdalitPolnostyu() error { return Udalit() }
