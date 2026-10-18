package vinsluzhba

import (
	"path/filepath"
	"strings"
)

// Где служба Windows хранит свой exe.
//
// Служба работает от системы, поэтому файл, который она запускает, обязан лежать
// там, куда обычный пользователь писать не может: иначе подмена этого файла
// (вредная программа в сеансе человека, второй пользователь на общем ПК) даёт
// права системы. «Загрузки» и профиль человека для этого не годятся, а
// «Program Files» годится: писать туда могут только администраторы.
//
// Окно и значок в трее при этом могут жить откуда угодно, это обычные процессы
// человека. Служба — только из Program Files.

// ImyaKataloga — папка приложения внутри Program Files.
const ImyaKataloga = "Kelevra"

// ImyaExe — имя exe службы в этой папке.
const ImyaExe = "Kelevra.exe"

// Katalog — папка службы внутри каталога Program Files programFiles.
func Katalog(programFiles string) string {
	return filepath.Join(programFiles, ImyaKataloga)
}

// PutSluzhby — полный путь exe службы внутри programFiles.
func PutSluzhby(programFiles string) string {
	return filepath.Join(Katalog(programFiles), ImyaExe)
}

// VKataloge — лежит ли put внутри katalog (на любой глубине). Пути приводятся к
// чистому виду, регистр не различается: на Windows C:\Program Files и
// c:\program files одно и то же место. «..» после очистки уже не обманывает.
func VKataloge(put, katalog string) bool {
	if put == "" || katalog == "" {
		return false
	}
	p := strings.ToLower(filepath.Clean(put))
	k := strings.ToLower(filepath.Clean(katalog))
	sep := string(filepath.Separator)
	if !strings.HasSuffix(k, sep) {
		k += sep
	}
	return strings.HasPrefix(p, k)
}

// NuzhnoPerenosit — службу, запущенную из put, надо перерегистрировать на копию
// в Program Files. Решение, а не исполнение: его держит тест.
//
// Не переносим, когда файл уже в папке службы, и когда не знаем, где Program
// Files (пустой programFiles): лучше оставить как есть, чем сломать запуск.
func NuzhnoPerenosit(put, programFiles string) bool {
	if put == "" || programFiles == "" {
		return false
	}
	return !VKataloge(put, Katalog(programFiles))
}
