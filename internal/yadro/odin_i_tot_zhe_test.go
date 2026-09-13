package yadro

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Два разных пути к одному файлу — один и тот же бинарь. Строками и
// EvalSymlinks жёсткую ссылку не узнать, узнаёт только ФС (os.SameFile).
func TestOdinItotZhePoFS(t *testing.T) {
	papka := t.TempDir()
	a := filepath.Join(papka, "sing-box.exe")
	if err := os.WriteFile(a, []byte("yadro"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(papka, "drugoe-imya.exe")
	if err := os.Link(a, b); err != nil {
		t.Skipf("жёсткая ссылка не создалась: %v", err)
	}
	if !odinItotZhe(a, b) {
		t.Fatal("два имени одного файла не признаны одним бинарём")
	}
	c := filepath.Join(papka, "chuzhoy.exe")
	if err := os.WriteFile(c, []byte("yadro"), 0o755); err != nil {
		t.Fatal(err)
	}
	if odinItotZhe(a, c) {
		t.Fatal("разные файлы с одинаковым содержимым признаны одним бинарём")
	}
}

// Данные приложения лежат на другом разделе через junction. ОС называет
// образ ядра путём ПОСЛЕ разворота (путь на том разделе), конфиг хранит путь
// ДО него
// (C:\ProgramData\Kelevra\yadro\sing-box.exe). С Go 1.23 EvalSymlinks
// junction не разворачивает.
func TestOdinItotZheCherezJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction есть только на Windows")
	}
	nastoyashchaya := filepath.Join(t.TempDir(), "dannye")
	if err := os.MkdirAll(nastoyashchaya, 0o755); err != nil {
		t.Fatal(err)
	}
	fayl := filepath.Join(nastoyashchaya, "sing-box.exe")
	if err := os.WriteFile(fayl, []byte("yadro"), 0o755); err != nil {
		t.Fatal(err)
	}
	perehod := filepath.Join(t.TempDir(), "perehod")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", perehod, nastoyashchaya).CombinedOutput(); err != nil {
		t.Skipf("junction не создался: %v %s", err, out)
	}
	if !odinItotZhe(filepath.Join(perehod, "sing-box.exe"), fayl) {
		t.Fatal("один файл через junction и напрямую не признан одним бинарём — своё ядро будет объявлено чужой программой")
	}
}
