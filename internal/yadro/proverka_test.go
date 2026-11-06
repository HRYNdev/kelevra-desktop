package yadro

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// lzheCheck — поведение лже-ядра на `check` (зовётся из TestMain в процессе-
// потомке): prinyat — код 0; otvergnut — строка FATAL и код 1; zavisnut —
// молчать минуту; inache — проверить, что конфиг передан и лежит в -D.
func lzheCheck(rezhim string, args []string) int {
	put, papka := "", ""
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "-c":
			put = args[i+1]
		case "-D":
			papka = args[i+1]
		}
	}
	b, err := os.ReadFile(put)
	if err != nil || filepath.Dir(put) != filepath.Clean(papka) {
		fmt.Fprintln(os.Stderr, "FATAL[0000] read config:", err, put, papka)
		return 1
	}
	switch rezhim {
	case "prinyat":
		return 0
	case "otvergnut":
		fmt.Fprintf(os.Stderr, "INFO лишнее\nFATAL[0000] decode config at %s: outbounds[3]: unknown field \"novoe_pole\"\n", put)
		return 1
	case "zavisnut":
		time.Sleep(time.Minute)
		return 0
	}
	if !strings.Contains(string(b), "{") {
		return 1
	}
	return 0
}

func yadroDlyaProverki(t *testing.T) *Yadro {
	t.Helper()
	svoy, err := os.Executable()
	if err != nil {
		t.Skipf("не нашёл свой exe: %v", err)
	}
	return &Yadro{Bin: svoy, Papka: t.TempDir()}
}

// Ядро приняло конфиг — nil; проверочный файл после проверки убран.
func TestProveritPrinyal(t *testing.T) {
	t.Setenv("KELEVRA_LZHE_CHECK", "prinyat")
	y := yadroDlyaProverki(t)
	if err := y.Proverit(context.Background(), []byte(`{"log":{}}`)); err != nil {
		t.Fatalf("принятый конфиг: %v", err)
	}
	if _, err := os.Stat(filepath.Join(y.Papka, ImyaProverochnogo)); !os.IsNotExist(err) {
		t.Fatalf("проверочный конфиг не убран: %v", err)
	}
}

// Ядро отвергло — OshibkaProverki с причиной ядра (строка FATAL), это
// приговор конфигу (KonfigOtvergnut).
func TestProveritOtverg(t *testing.T) {
	t.Setenv("KELEVRA_LZHE_CHECK", "otvergnut")
	y := yadroDlyaProverki(t)
	err := y.Proverit(context.Background(), []byte(`{}`))
	if !KonfigOtvergnut(err) {
		t.Fatalf("ждали «ядро не приняло», получили %v", err)
	}
	if !strings.Contains(err.Error(), "novoe_pole") || strings.Contains(err.Error(), "лишнее") {
		t.Fatalf("причина ядра: %v", err)
	}
	if errors.Is(err, ErrProverkaNeSostoyalas) {
		t.Fatal("отказ ядра выдан за несостоявшуюся проверку")
	}
}

// Проверить не вышло (нет файла ядра, срок) — не приговор конфигу.
func TestProveritNeSostoyalas(t *testing.T) {
	y := &Yadro{Bin: filepath.Join(t.TempDir(), "net-takogo.exe"), Papka: t.TempDir()}
	err := y.Proverit(context.Background(), []byte(`{}`))
	if !errors.Is(err, ErrProverkaNeSostoyalas) || KonfigOtvergnut(err) {
		t.Fatalf("нет файла ядра: %v", err)
	}

	t.Setenv("KELEVRA_LZHE_CHECK", "zavisnut")
	staryy := srokProverki
	srokProverki = 300 * time.Millisecond
	t.Cleanup(func() { srokProverki = staryy })
	y = yadroDlyaProverki(t)
	err = y.Proverit(context.Background(), []byte(`{}`))
	if !errors.Is(err, ErrProverkaNeSostoyalas) || KonfigOtvergnut(err) {
		t.Fatalf("ядро зависло на проверке: %v", err)
	}
}
