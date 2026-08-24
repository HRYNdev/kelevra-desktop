package yadro

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Два окна опрашивают состояние независимо (см. internal/sluzhba/oblik) и оба
// могут решить «надо подключиться» одновременно. Повторный Zapustit на уже
// запущенном ядре должен вести себя как Ostanovit на остановленном — цель уже
// достигнута, это не ошибка.
func TestZapustitDvazhdyIdempotenten(t *testing.T) {
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
