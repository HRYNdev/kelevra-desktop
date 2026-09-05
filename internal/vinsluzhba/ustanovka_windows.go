//go:build windows

package vinsluzhba

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

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
	return Ustanovit(putExe)
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
