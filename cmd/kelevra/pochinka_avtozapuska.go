package main

import (
	"log"

	"github.com/HRYNdev/kelevra-desktop/internal/avtozapusk"
	"github.com/HRYNdev/kelevra-desktop/internal/vinsluzhba"
)

// pochinitAvtozapusk — запись автозапуска на копию в Program Files.
//
// Зовут окно и значок при каждом старте (это процессы в сеансе человека, их
// HKCU — его ветка). Окно может быть запущено и не из Program Files (пробная
// копия из «Загрузок»), запись всё равно ведём на Program Files: этот файл
// держит служба, и уборка «Загрузок» его не тронет. Беда, ради которой это
// написано: у Вовы запись смотрела на удалённый 18.10 Downloads\Kelevra.exe,
// и значок при входе не поднимался три дня (avtozapusk.Pochinka).
//
// Переменная — точка подмены для тестов: настоящий вызов правит реестр.
var pochinitAvtozapusk = func() {
	putPF := vinsluzhba.PutUstanovlennoy()
	if putPF == "" {
		return
	}
	bylo, stalo, izmeneno, err := avtozapusk.PochinitNaProgramFiles(putPF)
	if err != nil {
		log.Printf("автозапуск: не сверить запись с Program Files: %v", err)
		return
	}
	if izmeneno {
		log.Printf("автозапуск: был %s, стал %s", bylo, stalo)
	}
}
