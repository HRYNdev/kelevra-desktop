//go:build !windows

package main

// zapustitTrey вне Windows не делает ничего: трей — часть облика Windows-
// приложения (см. trey_windows.go), а на этой платформе продукт не живёт —
// тот же принцип, что у okno_other.go, skazat_other.go и zapusk_other.go.
// Пустая реализация здесь нужна только затем, чтобы cmd/kelevra собирался и
// проверялся на сервере без Windows. Канал vyhod специально не используется:
// на этой платформе некому просигналить «Выход» через значок, служебный
// режим по-прежнему останавливается только сигналом ОС (см. zhdatSignal).
func zapustitTrey(vyhod chan<- struct{}) {}
