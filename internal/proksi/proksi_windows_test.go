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
	// На НАСТОЯЩЕЙ Windows этот вызов не «отвечает понятно», а РАБОТАЕТ:
	// системный прокси машины уезжает на 127.0.0.1:2412, за которым в этот
	// момент никого нет, и у того, кто гоняет проверки, пропадают сайты — та
	// самая авария, из-за которой этот пакет и появился. Проверка писалась под
	// wine (там опция 75 не реализована и честно отвечает 12009) и только там
	// безопасна, поэтому на живой Windows она себя пропускает: замер, который
	// ломает машину замеряющего, — не замер.
	if !podWine() {
		t.Skip("настоящая Windows: живой вызов опции 75 переставил бы системный " +
			"прокси машины — проверка идёт только под wine (stend/testy_pod_wine.sh)")
	}
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

// podWine — исполняемся мы под wine (стенд) или на настоящей Windows (машина
// человека). Признак не косвенный: ntdll под wine несёт экспорт
// wine_get_version, которого на настоящей Windows нет вовсе, и именно им сам
// проект wine предлагает себя опознавать.
func podWine() bool {
	ntdll, err := syscall.LoadLibrary("ntdll.dll")
	if err != nil {
		return false
	}
	defer syscall.FreeLibrary(ntdll)
	_, err = syscall.GetProcAddress(ntdll, "wine_get_version")
	return err == nil
}

// Право стереть строку ProxyServer даёт РОВНО одно: она наша. Проверка чисто
// арифметическая — реестра не касается вовсе, поэтому безопасна и на живой
// машине человека (в отличие от Snyat/Postavit, которых гейт KELEVRA_PROKSI=net
// не пускает дальше первой строки).
//
// 31.08: до этой правки Snyat() гасил ProxyEnable и оставлял
// «ProxyServer=http://127.0.0.1:2412» лежать. Функционально безвредно, но при
// разборе аварии по этой строке дважды искали причину, которой там не было.
func TestNashSled(t *testing.T) {
	sluchai := []struct {
		imya, vReestre, nash string
		zhdem                bool
	}{
		{"наш адрес со схемой, метка без неё", "http://127.0.0.1:2412", "127.0.0.1:2412", true},
		{"наш адрес один в один", "127.0.0.1:2412", "127.0.0.1:2412", true},
		{"наш адрес с пробелами по краям", "  http://127.0.0.1:2412 ", "127.0.0.1:2412", true},
		{"чужой прокси конторы", "proxy.corp.local:8080", "127.0.0.1:2412", false},
		{"чужой локальный на другом порту", "127.0.0.1:8888", "127.0.0.1:2412", false},
		{"в реестре пусто", "", "127.0.0.1:2412", false},
		{"метки нет — доказательства нет", "http://127.0.0.1:2412", "", false},
		{"обоих нет", "", "", false},
	}
	for _, s := range sluchai {
		t.Run(s.imya, func(t *testing.T) {
			if got := nashSled(s.vReestre, s.nash); got != s.zhdem {
				t.Errorf("nashSled(%q, %q) = %v, а ждали %v", s.vReestre, s.nash, got, s.zhdem)
			}
		})
	}
}
