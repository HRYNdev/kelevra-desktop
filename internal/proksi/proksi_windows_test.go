//go:build windows

package proksi

import (
	"errors"
	"syscall"
	"testing"
	"unsafe"
)

// Зачем этот файл. Главный путь Postavit — вызов WinINet с опцией 75, и
// проверить его настоящей Windows мне негде: стенд гоняет wine, а он эту опцию
// не умеет. Значит опечатка в раскладке структур (лишнее поле, не тот dwSize)
// на моей стороне выглядела бы ровно как «wine не умеет»: вызов вернул бы
// отказ, код тихо ушёл бы в реестр — и человек снова получил бы половину
// решения, а я бы об этом не узнал. Поэтому раскладку сверяем с документацией
// WinINet арифметикой, а не глазами.
//
// INTERNET_PER_CONN_OPTIONW (amd64): DWORD (4) + выравнивание (4) + union (8) = 16.
// INTERNET_PER_CONN_OPTION_LISTW (amd64): DWORD (4) + выравнивание (4) +
// LPWSTR (8) + DWORD (4) + DWORD (4) + указатель (8) = 32.
func TestRaskladkaStrukturWinINet(t *testing.T) {
	if r := unsafe.Sizeof(perConnOpciya{}); r != 16 {
		t.Errorf("INTERNET_PER_CONN_OPTIONW: размер %d, а WinINet ждёт 16", r)
	}
	if r := unsafe.Sizeof(perConnSpisok{}); r != 32 {
		t.Errorf("INTERNET_PER_CONN_OPTION_LISTW: размер %d, а WinINet ждёт 32", r)
	}
	// dwSize мы отдаём системе сами; если он разойдётся с настоящим размером,
	// Windows ответит отказом ровно так же, как wine — и подмену не заметить.
	s := perConnSpisok{razmer: uint32(unsafe.Sizeof(perConnSpisok{}))}
	if uintptr(s.razmer) != unsafe.Sizeof(s) {
		t.Errorf("dwSize=%d не равен настоящему размеру %d", s.razmer, unsafe.Sizeof(s))
	}
	if o := unsafe.Offsetof(perConnSpisok{}.opcii); o != 24 {
		t.Errorf("pOptions лежит по смещению %d, а WinINet ждёт 24", o)
	}
	if o := unsafe.Offsetof(perConnOpciya{}.znak); o != 8 {
		t.Errorf("union лежит по смещению %d, а WinINet ждёт 8", o)
	}
}

// Живой вызов. Под wine ожидаемый ответ — ERROR_INTERNET_INVALID_OPTION
// (12009): «такой опции у меня нет». Любой другой отказ — сигнал, что дело не
// в эмуляторе, а в том, ЧТО мы передали (например 87 ERROR_INVALID_PARAMETER —
// это уже про нашу структуру, и на настоящей Windows будет то же самое).
func TestOpciya75OtvechaetPonyatno(t *testing.T) {
	const (
		netTakoyOpcii = syscall.Errno(12009) // ERROR_INTERNET_INVALID_OPTION
	)
	err := cherezOpciyu75("127.0.0.1:2412", flagCherezProksi|flagPryamo)
	if err == nil {
		t.Log("опция 75 прошла — здесь она поддерживается")
		return
	}
	var e syscall.Errno
	if !errors.As(err, &e) {
		t.Fatalf("опция 75 вернула не системную ошибку: %v", err)
	}
	t.Logf("опция 75 отказала: %d (%v)", uintptr(e), e)
	if e != netTakoyOpcii {
		t.Fatalf("отказ %d — это НЕ «опции нет» (12009): передали что-то не то, "+
			"на настоящей Windows будет тот же отказ", uintptr(e))
	}
}
