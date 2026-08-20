package yadro

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Ядро красит свой вывод. Раньше эта раскраска уезжала прямо в окно
// пользователя: вместо причины он видел «[36mINFO[0m».
func TestHvostBezCvetovIToljkoPrichina(t *testing.T) {
	put := filepath.Join(t.TempDir(), "yadro.log")
	log := "+0500 2026-08-20 05:15:18 \x1b[36mINFO\x1b[0m network: updated default interface eth0\n" +
		"+0500 2026-08-20 05:15:18 \x1b[36mINFO\x1b[0m inbound/mixed[mixed-in]: tcp server started at 127.0.0.1:2412\n" +
		"\x1b[31mFATAL\x1b[0m[0000] start service: initialize system proxy: unsupported desktop environment\n"
	if err := os.WriteFile(put, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	h := hvostLoga(put)
	if strings.Contains(h, "\x1b") {
		t.Fatalf("цвета остались в тексте для человека: %q", h)
	}
	if !strings.Contains(h, "initialize system proxy") {
		t.Fatalf("причина потерялась: %q", h)
	}
	if strings.Contains(h, "updated default interface") {
		t.Fatalf("INFO-шум попал в причину: %q", h)
	}
}

// Ядро умерло молча, без FATAL: тогда показываем хвост как есть — это всё,
// что о случившемся вообще известно.
func TestHvostBezFatalOtdayotHvost(t *testing.T) {
	put := filepath.Join(t.TempDir(), "yadro.log")
	if err := os.WriteFile(put, []byte("первая\nвторая\nтретья\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if h := hvostLoga(put); h != "первая | вторая | третья" {
		t.Fatalf("хвост не тот: %q", h)
	}
}

func TestHvostaNetFaylaNet(t *testing.T) {
	if h := hvostLoga(filepath.Join(t.TempDir(), "netu.log")); h != "" {
		t.Fatalf("на пустом месте выдумался текст: %q", h)
	}
}
