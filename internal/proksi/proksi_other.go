//go:build !windows

package proksi

// Snyat на не-Windows ничего не делает: системный прокси там приложение
// не трогает вовсе.
func Snyat() bool { return false }
