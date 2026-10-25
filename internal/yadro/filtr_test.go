package yadro

import (
	"bytes"
	"strings"
	"testing"
)

// TestFiltrVyrezaetShum — из 5 строк 3 шумовые (http2 response body closed):
// в выходе остаются 2 обычные строки плюс одна итоговая «пропущено 3».
func TestFiltrVyrezaetShum(t *testing.T) {
	vhod := strings.Join([]string{
		"obychnaya stroka 1",
		"http2: response body closed",
		"obychnaya stroka 2",
		"http2: response body closed",
		"http2: response body closed",
	}, "\n") + "\n"

	var vyhod bytes.Buffer
	filtrLogYadra(strings.NewReader(vhod), &vyhod, 0)

	stroki := strings.Split(strings.TrimRight(vyhod.String(), "\n"), "\n")
	if len(stroki) != 3 {
		t.Fatalf("ожидал 3 строки в выходе (2 обычные + итог), получил %d: %q", len(stroki), vyhod.String())
	}
	if stroki[0] != "obychnaya stroka 1" || stroki[1] != "obychnaya stroka 2" {
		t.Fatalf("обычные строки искажены: %q", stroki[:2])
	}
	itog := stroki[2]
	if !strings.Contains(itog, "[фильтр]") || !strings.Contains(itog, "пропущено 3 строк") {
		t.Fatalf("итоговая строка не похожа на ожидаемую: %q", itog)
	}
}

// TestFiltrBezShumaProzrachen — если шумовых строк нет, выход байт в байт
// равен входу, итоговой строки нет вовсе.
func TestFiltrBezShumaProzrachen(t *testing.T) {
	vhod := "stroka odna\nstroka dva\nstroka tri\n"

	var vyhod bytes.Buffer
	filtrLogYadra(strings.NewReader(vhod), &vyhod, 0)

	if vyhod.String() != vhod {
		t.Fatalf("выход не равен входу байт в байт:\nвход:  %q\nвыход: %q", vhod, vyhod.String())
	}
}

// TestFiltrDlinnayaStroka — строка длиннее 64 КиБ (лимит по умолчанию у
// bufio.Scanner) должна пройти целиком, не оборвавшись и не упав с ошибкой.
func TestFiltrDlinnayaStroka(t *testing.T) {
	dlinnaya := strings.Repeat("x", 70*1024) // 70 КиБ > 64 КиБ
	vhod := dlinnaya + "\n"

	var vyhod bytes.Buffer
	filtrLogYadra(strings.NewReader(vhod), &vyhod, 0)

	got := strings.TrimRight(vyhod.String(), "\n")
	if got != dlinnaya {
		t.Fatalf("длинная строка искажена: ожидал длину %d, получил %d", len(dlinnaya), len(got))
	}
}
