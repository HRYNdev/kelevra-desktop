package sluzhba

import (
	"os"
	"path/filepath"
	"testing"
)

// Разрыв строки посреди записи: ядро дописывает журнал не атомарно, и заход
// может застать файл ровно на границе одной строки. Смещение обязано
// остановиться на последнем переводе строки, иначе недописанный хвост будет
// одновременно выброшен из выдачи и пройден смещением — то есть потерян
// навсегда, без следа в логе.
func TestChtenieNeTeryaetStrokuRazorvannuyuPosredine(t *testing.T) {
	dir := t.TempDir()
	put := filepath.Join(dir, "yadro.log")

	polnayaStroka := `+0500 2026-09-10 21:00:00 ERROR [1 10ms] connection: open connection to [1.2.3.4]:443 using outbound/direct[direct]: dial tcp: i/o timeout`
	seredina := len(polnayaStroka) / 2
	pervayaPolovina := polnayaStroka[:seredina]
	vtorayaPolovina := polnayaStroka[seredina:]

	if err := os.WriteFile(put, []byte(pervayaPolovina), 0o644); err != nil {
		t.Fatalf("не смог написать первую половину: %v", err)
	}

	n := &nablyudatelYadra{put: put, kody: map[string]int{}, imena: map[string]int{}}

	// Заход первый: в файле только половина строки, без перевода строки.
	// Отдавать нечего, а смещение обязано остаться на месте — иначе половина
	// потеряется, не дождавшись своего продолжения.
	stroki, err := n.prochitatNovoe()
	if err != nil {
		t.Fatalf("первый заход: %v", err)
	}
	if len(stroki) != 0 {
		t.Fatalf("первый заход отдал %d строк, ждали 0 (строка ещё не дописана): %v", len(stroki), stroki)
	}
	if n.smeshenie != 0 {
		t.Fatalf("первый заход сдвинул смещение на %d — недописанный хвост потерян", n.smeshenie)
	}

	// Ядро дописывает вторую половину и перевод строки.
	f, err := os.OpenFile(put, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("не смог открыть файл на дозапись: %v", err)
	}
	if _, err := f.WriteString(vtorayaPolovina + "\n"); err != nil {
		t.Fatalf("не смог дописать вторую половину: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("не смог закрыть файл: %v", err)
	}

	// Заход второй: строка полная. Обязана прийти ОДНА и ЦЕЛАЯ, а не обрубок
	// (потерян хвост) и не две половины по отдельности.
	stroki, err = n.prochitatNovoe()
	if err != nil {
		t.Fatalf("второй заход: %v", err)
	}
	if len(stroki) != 1 {
		t.Fatalf("второй заход отдал %d строк, ждали ровно 1: %v", len(stroki), stroki)
	}
	if stroki[0] != polnayaStroka {
		t.Fatalf("строка собралась криво:\n  получили %q\n  ждали    %q", stroki[0], polnayaStroka)
	}
}

// Буфер вообще без перевода строки (одна затянувшаяся запись) не должен
// двигать смещение ни на байт — иначе следующий заход упрётся в тот же
// неполный кусок и будет вечно перечитывать его, не продвигаясь дальше.
func TestChtenieBezPerevodaStrokiNeDvigaetSmeshenie(t *testing.T) {
	dir := t.TempDir()
	put := filepath.Join(dir, "yadro.log")
	nedopisano := "+0500 2026-09-10 21:00:00 ERROR запись без конца"

	if err := os.WriteFile(put, []byte(nedopisano), 0o644); err != nil {
		t.Fatalf("не смог написать файл: %v", err)
	}

	n := &nablyudatelYadra{put: put, kody: map[string]int{}, imena: map[string]int{}}

	for zahod := 1; zahod <= 2; zahod++ {
		stroki, err := n.prochitatNovoe()
		if err != nil {
			t.Fatalf("заход %d: %v", zahod, err)
		}
		if len(stroki) != 0 {
			t.Fatalf("заход %d отдал %d строк, ждали 0", zahod, len(stroki))
		}
		if n.smeshenie != 0 {
			t.Fatalf("заход %d сдвинул смещение на %d при отсутствии перевода строки", zahod, n.smeshenie)
		}
	}
}
