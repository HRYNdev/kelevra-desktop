// Пакет lzheyadro — сборка лже-ядра (cmd/lzhe_yadro) для проверок слежки за
// ядром (internal/yadro, internal/sluzhba). Только для тестов: в приложение
// не входит.
//
// Лже-ядро собирается один раз на процесс проверок тем же go, которым они
// запущены, и кладётся под именем настоящего ядра (sing-box[.exe]): опознание
// принятого ядра сверяет образ процесса с путём ядра.
package lzheyadro

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var (
	odinRaz sync.Once
	put     string
	oshibka error
	propusk string
)

// Put — путь к собранному лже-ядру. Нет go под рукой — проверка пропускается.
func Put(t testing.TB) string {
	t.Helper()
	odinRaz.Do(sobrat)
	if propusk != "" {
		t.Skip(propusk)
	}
	if oshibka != nil {
		t.Fatalf("лже-ядро не собралось: %v", oshibka)
	}
	return put
}

// V — копия лже-ядра в папке papka под именем ядра; путь к ней.
func V(t testing.TB, papka string) string {
	t.Helper()
	ishodnik := Put(t)
	kuda := filepath.Join(papka, imya())
	b, err := os.ReadFile(ishodnik)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kuda, b, 0o755); err != nil {
		t.Fatal(err)
	}
	return kuda
}

// SvobodnyyAdres — свободный адрес на петле для Clash API лже-ядра.
func SvobodnyyAdres(t testing.TB) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := l.Addr().String()
	l.Close()
	return a
}

func imya() string {
	if runtime.GOOS == "windows" {
		return "sing-box.exe"
	}
	return "sing-box"
}

func sobrat() {
	goBin, err := exec.LookPath("go")
	if err != nil {
		if kandidat := filepath.Join(runtime.GOROOT(), "bin", "go"); runtime.GOROOT() != "" {
			if runtime.GOOS == "windows" {
				kandidat += ".exe"
			}
			if _, e := os.Stat(kandidat); e == nil {
				goBin, err = kandidat, nil
			}
		}
	}
	if err != nil {
		propusk = "нет go для сборки лже-ядра"
		return
	}
	_, etot, _, ok := runtime.Caller(0)
	if !ok {
		oshibka = fmt.Errorf("не знаю, где лежит исходник")
		return
	}
	koren := filepath.Join(filepath.Dir(etot), "..", "..")
	papka, err := os.MkdirTemp("", "kelevra-lzhe-yadro")
	if err != nil {
		oshibka = err
		return
	}
	put = filepath.Join(papka, imya())
	cmd := exec.Command(goBin, "build", "-o", put, "./cmd/lzhe_yadro")
	cmd.Dir = koren
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if vyvod, err := cmd.CombinedOutput(); err != nil {
		oshibka = fmt.Errorf("%v: %s", err, vyvod)
	}
}

// Ubrat убирает собранное лже-ядро (зовётся из TestMain после проверок).
func Ubrat() {
	if put != "" {
		_ = os.RemoveAll(filepath.Dir(put))
	}
}
