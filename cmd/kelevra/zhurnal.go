package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// predelZhurnala — при каком размере журнал уезжает в «прошлый». Приложение
// живёт месяцами, а строк пишет мало; полмегабайта — это надолго.
const predelZhurnala = 512 * 1024

// otkrytZhurnal направляет весь вывод log в файл рядом с данными приложения.
//
// Зачем: Kelevra.exe собирается оконным (-H=windowsgui), консоли у него нет,
// и всё, что уходит в stderr, у пользователя не видит никто. Без файла
// единственным следом отказа остаётся то, что приложение «не запустилось»,
// а по такому описанию причину не найти.
//
// Возвращает путь к журналу (пустой, если писать некуда) и функцию закрытия.
func otkrytZhurnal(papka string) (string, func()) {
	put, f := otkrytFayl(papka)
	if f == nil {
		// Запасной путь: если своя папка недоступна (нет прав, антивирус),
		// журнал всё равно нужен — именно этот случай и надо будет разбирать.
		put, f = otkrytFayl(filepath.Join(os.TempDir(), "Kelevra"))
	}
	if f == nil {
		return "", func() {}
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.SetFlags(log.LstdFlags)
	return put, func() { _ = f.Close() }
}

func otkrytFayl(papka string) (string, *os.File) {
	if err := os.MkdirAll(papka, 0o755); err != nil {
		return "", nil
	}
	put := filepath.Join(papka, "kelevra.log")
	if st, err := os.Stat(put); err == nil && st.Size() > predelZhurnala {
		_ = os.Rename(put, put+".proshlyy")
	}
	f, err := os.OpenFile(put, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", nil
	}
	return put, f
}
