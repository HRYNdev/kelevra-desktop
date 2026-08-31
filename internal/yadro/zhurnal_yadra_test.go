package yadro

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Сторож журнала ядра.
//
// Беда 31.08, приборно: %LOCALAPPDATA%\Kelevra\yadro\yadro.log было 129 822
// байта, после перезапуска ядра стало 6 445 — os.Create обнулял его на каждом
// старте. Посмертной истории не оставалось, а именно она нужна для разбора
// «подключено, но не грузится»: человек к моменту жалобы уже нажал
// «Отключить» и «Подключить» ещё раз и тем самым стёр всё объяснение.

func TestZhurnalYadraSohranyaetProshlyy(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "yadro.log")
	proshlyy := put + ".proshlyy"

	if err := os.WriteFile(put, []byte("первый запуск: тут была причина аварии\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := sozdatZhurnalYadra(papka)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("второй запуск\n")
	f.Close()

	b, err := os.ReadFile(proshlyy)
	if err != nil {
		t.Fatalf("прошлого журнала ядра нет: %v", err)
	}
	if !strings.Contains(string(b), "тут была причина аварии") {
		t.Fatalf("в .proshlyy не то содержимое: %q", b)
	}
	novyy, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(novyy), "первый запуск") {
		t.Fatalf("новый журнал не начат с чистого листа: %q", novyy)
	}
	if !strings.Contains(string(novyy), "второй запуск") {
		t.Fatalf("в новом журнале нет своей строки: %q", novyy)
	}
}

// Третий запуск не имеет права оставить нас вовсе без истории: .proshlyy
// переписывается предыдущим, а не отказывается переписываться (на Windows
// os.Rename поверх существующего файла — отдельный разговор, поэтому это
// проверяется, а не предполагается).
func TestTretiyZapuskPerezapisyvaetProshlyy(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "yadro.log")

	for _, stroka := range []string{"запуск 1\n", "запуск 2\n", "запуск 3\n"} {
		f, err := sozdatZhurnalYadra(papka)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(stroka)
		f.Close()
	}

	tekushchiy, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tekushchiy), "запуск 3") {
		t.Fatalf("текущий журнал не от третьего запуска: %q", tekushchiy)
	}
	proshlyy, err := os.ReadFile(put + ".proshlyy")
	if err != nil {
		t.Fatalf("после третьего запуска прошлого журнала нет: %v", err)
	}
	if !strings.Contains(string(proshlyy), "запуск 2") {
		t.Fatalf(".proshlyy должен быть от ВТОРОГО запуска, а там: %q", proshlyy)
	}
}

// Пустой журнал не ротируем: сохранить ноль байт поверх настоящей истории
// прошлого запуска — это её потерять. Так бывает, когда ядро упало, не
// написав ни строки, а разбирать надо как раз предыдущий, живой запуск.
func TestPustoyZhurnalNeZatiraetProshlyy(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "yadro.log")

	if err := os.WriteFile(put, []byte("настоящая история\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := sozdatZhurnalYadra(papka) // ротация: история уехала в .proshlyy
	if err != nil {
		t.Fatal(err)
	}
	f.Close() // ядро не написало ни строки

	f2, err := sozdatZhurnalYadra(papka) // следующий запуск
	if err != nil {
		t.Fatal(err)
	}
	f2.Close()

	b, err := os.ReadFile(put + ".proshlyy")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "настоящая история") {
		t.Fatalf("пустой журнал затёр историю в .proshlyy: %q", b)
	}
}

// PutZhurnala/PutProshlogoZhurnala — то же место, куда пишет sozdatZhurnalYadra.
// Два определения одного пути разъезжаются молча.
func TestPutiZhurnalaYadraSovpadayut(t *testing.T) {
	papka := t.TempDir()
	y := &Yadro{Papka: papka}
	f, err := sozdatZhurnalYadra(papka)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("строка\n")
	f.Close()
	if _, err := os.Stat(y.PutZhurnala()); err != nil {
		t.Fatalf("PutZhurnala указывает не туда: %v", err)
	}
	f2, err := sozdatZhurnalYadra(papka)
	if err != nil {
		t.Fatal(err)
	}
	f2.Close()
	if _, err := os.Stat(y.PutProshlogoZhurnala()); err != nil {
		t.Fatalf("PutProshlogoZhurnala указывает не туда: %v", err)
	}
}
