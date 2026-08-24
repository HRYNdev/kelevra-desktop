package yadro

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Два окна опрашивают состояние независимо (см. internal/sluzhba/oblik) и оба
// могут решить «надо подключиться» одновременно. Повторный Zapustit на уже
// запущенном ядре должен вести себя как Ostanovit на остановленном — цель уже
// достигнута, это не ошибка. Жалоба хозяина 23.08: «ошибка при отключении, когда
// открыты 2 приложения».
//
// Эта половина теста бьёт ровно в развилку `y.process != nil` и потому не
// зависит от платформы: живого ядра тут нет вовсе, есть лишь занятое поле.
func TestZapustitNaZanyatomYadreNeOshibka(t *testing.T) {
	y := &Yadro{}
	y.process = &exec.Cmd{} // «ядро уже работает» — дальше развилки не идём
	defer func() { y.process = nil }()

	if err := y.Zapustit(context.Background()); err != nil {
		t.Fatalf("повторный запуск на уже работающем ядре должен быть идемпотентен, получили ошибку: %v", err)
	}
}

// А эта половина проходит весь настоящий путь запуска: поднимает процесс,
// дожидается ответа API и только потом зовёт Zapustit второй раз.
// Не под Windows: подставное ядро здесь — скрипт /bin/sh, под wine его нечем
// исполнить (замер 24.08: приёмка windows.sh краснела именно на этом).
// Свойство от платформы не зависит, его держит тест выше.
func TestZapustitDvazhdyIdempotenten(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("подставное ядро — скрипт /bin/sh; развилку проверяет TestZapustitNaZanyatomYadreNeOshibka")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	adres := strings.TrimPrefix(srv.URL, "http://")

	papka := t.TempDir()
	bin := filepath.Join(papka, imyaYadra())
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(papka, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	y := &Yadro{Bin: bin, Papka: papka, Api: adres}
	defer y.Ostanovit()

	if err := y.Zapustit(context.Background()); err != nil {
		t.Fatalf("первый запуск не удался: %v", err)
	}
	if err := y.Zapustit(context.Background()); err != nil {
		t.Fatalf("повторный запуск на уже работающем ядре должен быть идемпотентен, получили ошибку: %v", err)
	}
}
