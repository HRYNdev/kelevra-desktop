//go:build !windows

package proksi

// Snyat на не-Windows реестр не трогает — трогать там нечего. Метку всё же
// убираем: она платформо-независимая (Otmetit пишет её и на Linux, где
// стенды и тесты гоняют этот же путь sluzhba.go без wine), и Snyat() —
// единственное общее место, откуда её положено снимать.
func Snyat() bool {
	UbratMetku()
	return false
}

// Stoit на не-Windows всегда отвечает «нет»: системного прокси в смысле
// Windows-реестра тут не существует.
func Stoit(adres string) bool { return false }

// Postavit на не-Windows ничего не делает — ставить в реестр нечего.
func Postavit(adres string) bool { return false }
