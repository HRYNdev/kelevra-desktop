package hranenie

import (
	"path/filepath"
	"runtime"
	"testing"
)

// KELEVRA_DIR обязан перекрывать %LOCALAPPDATA% именно на Windows — там, где
// живёт продукт. До 22.08 порядок был обратный, и любой windows-тест,
// поставивший себе t.TempDir(), молча писал в живую папку приложения: настройки
// одного теста доставались другому (стенд краснел на выборе узла), а на машине
// человека прогон тестов затёр бы ему его собственный профиль.
//
// Тест сторожит ПОРЯДОК, поэтому обе переменные ставим одновременно: проверка
// «KELEVRA_DIR работает» без заданного LOCALAPPDATA зеленела бы и на старом
// коде, то есть не проверяла бы ничего.
func TestKelevraDirSilneeLocalAppData(t *testing.T) {
	svoya := t.TempDir()
	chuzhaya := t.TempDir()
	t.Setenv("KELEVRA_DIR", svoya)
	t.Setenv("LOCALAPPDATA", chuzhaya)

	if p := Papka(); p != svoya {
		t.Fatalf("папка %q вместо заданной KELEVRA_DIR %q (LOCALAPPDATA=%q)", p, svoya, chuzhaya)
	}
	// Всё остальное считается от Papka(): если корень уехал, уедет и профиль,
	// и журнал, и конфиг ядра. Щупаем один производный путь, чтобы поймать
	// случай, когда Papka() починили, а кто-то рядом склеивает путь сам.
	if got, want := PutProfilya(), filepath.Join(svoya, "profil.json"); got != want {
		t.Fatalf("профиль лёг в %q, ждали %q", got, want)
	}
}

// Без KELEVRA_DIR на Windows остаётся %LOCALAPPDATA%\Kelevra — это боевой путь,
// и переставленное условие не должно было его сломать.
func TestBezPeremennoyWindowsBeretLocalAppData(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("проверяет ветку Windows")
	}
	chuzhaya := t.TempDir()
	t.Setenv("KELEVRA_DIR", "")
	t.Setenv("LOCALAPPDATA", chuzhaya)

	if got, want := Papka(), filepath.Join(chuzhaya, "Kelevra"); got != want {
		t.Fatalf("папка %q вместо %q", got, want)
	}
}
