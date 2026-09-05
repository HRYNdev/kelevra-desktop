package hranenie

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Perenesti копирует данные приложения из старой папки в новую.
//
// Копирует, а не перемещает, и намеренно. Старая папка остаётся нетронутой:
// если служба не встанет или человек откатится на прежнюю версию, его код
// доступа, настройки и профиль будут лежать там же, где лежали. Мусор на
// диске дешевле потерянного доступа.
//
// Уже существующие файлы в новой папке не трогаются: повторный вызов ничего
// не портит, а данные, которые служба успела написать сама, старше копии.
//
// Журнал и рабочие следы не переносятся: журнал начинается заново, а метка
// запуска и следы туннеля описывают процесс, которого уже нет.
func Perenesti(iz, v string) error {
	st, err := os.Stat(iz)
	if err != nil || !st.IsDir() {
		// Переносить нечего — это не беда: так выглядит установка на чистую
		// машину, где старой папки никогда и не было.
		return nil
	}
	if err := os.MkdirAll(v, 0o755); err != nil {
		return fmt.Errorf("не создать папку %s: %w", v, err)
	}
	return filepath.Walk(iz, func(put string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		otn, err := filepath.Rel(iz, put)
		if err != nil {
			return err
		}
		if otn == "." {
			return nil
		}
		if neNuzhno(otn, info) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		cel := filepath.Join(v, otn)
		if info.IsDir() {
			return os.MkdirAll(cel, 0o755)
		}
		if _, err := os.Stat(cel); err == nil {
			return nil // уже есть — своё новее
		}
		return kopirovat(put, cel, info.Mode())
	})
}

// neNuzhno — что в новой папке не нужно вовсе.
func neNuzhno(otn string, info os.FileInfo) bool {
	imya := filepath.Base(otn)
	switch imya {
	case "kelevra.log", "zapushcheno.json", "proksi.json", "tunnel.json":
		// Журналы прошлой жизни и следы процессов, которых давно нет.
		return true
	case "yadro":
		// Бинарь ядра весит десятки мегабайт и качается сам при первом же
		// подъёме. Тащить его копией — только время и место.
		return info.IsDir()
	}
	return false
}

func kopirovat(iz, v string, rezhim os.FileMode) error {
	src, err := os.Open(iz)
	if err != nil {
		return err
	}
	defer src.Close()
	// Через временный файл рядом с целью: оборванное копирование не оставит
	// половину файла под правильным именем, а «настройки наполовину» читаются
	// как испорченные и заменяются пустыми.
	vrem, err := os.CreateTemp(filepath.Dir(v), ".perenos-*")
	if err != nil {
		return err
	}
	putVrem := vrem.Name()
	defer os.Remove(putVrem)
	if _, err := io.Copy(vrem, src); err != nil {
		vrem.Close()
		return err
	}
	if err := vrem.Close(); err != nil {
		return err
	}
	if err := os.Chmod(putVrem, rezhim); err != nil {
		return err
	}
	return os.Rename(putVrem, v)
}
