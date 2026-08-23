//go:build !windows

package proksi

// Snyat на не-Windows ничего не делает: системный прокси там приложение
// не трогает вовсе.
func Snyat() bool { return false }

// Stoit на не-Windows всегда отвечает «нет»: системного прокси в смысле
// Windows-реестра тут не существует.
func Stoit(adres string) bool { return false }

// Postavit на не-Windows ничего не делает — ставить в реестр нечего.
func Postavit(adres string) bool { return false }
