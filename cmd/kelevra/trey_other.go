//go:build !windows

package main

import "log"

// zapustitTrey вне Windows не делает ничего: трей — часть облика Windows-
// приложения (см. trey_windows.go), а на этой платформе продукт не живёт —
// тот же принцип, что у okno_other.go, skazat_other.go и zapusk_other.go.
// Пустая реализация здесь нужна только затем, чтобы cmd/kelevra собирался и
// проверялся на сервере без Windows. Канал vyhod специально не используется:
// на этой платформе некому просигналить «Выход» через значок, служебный
// режим по-прежнему останавливается только сигналом ОС (см. zhdatSignal).
func zapustitTrey(vyhod chan<- struct{}) {}

// pokazatOblachkoObnovleniya вне Windows не показывает настоящий пузырь —
// трея тут нет вовсе (см. zapustitTrey выше). Строка в журнале — не пустая
// формальность: живой linux-стенд (stend/trey_oblachko.sh) гоняет настоящий
// бинарь под --sluzhba и не видит ничего, кроме журнала, — без этого следа
// сценам «сказали один раз» / «не повторили» нечего было бы проверить.
// На Windows этот файл не участвует в сборке вовсе (go:build !windows),
// поэтому настоящему пузырю (trey_windows.go) эта строка никогда не мешает.
func pokazatOblachkoObnovleniya(versiya string) {
	log.Printf("трей (не-Windows заглушка): пузырь про версию %s", versiya)
}

// obnovitPodskazkuTreya вне Windows рисовать негде — значка нет. Строка в
// журнале не формальность: метка «обновление ждёт» (metka_obnovleniya.go)
// сама по себе от платформы не зависит, и живой linux-стенд
// (stend/trey_oblachko.sh) проверяет по этой строке главное свойство метки —
// что она ПЕРЕЖИВАЕТ пузырь и перезапуск копии. Настоящую отрисовку значка
// на Windows это не доказывает и не подменяет (там свой файл, свой вызов
// Shell_NotifyIconW и свой след в журнале).
func obnovitPodskazkuTreya() {
	log.Printf("трей (не-Windows заглушка): подсказка значка -> %q", podskazkaTreya())
}
