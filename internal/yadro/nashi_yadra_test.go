package yadro

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Живой процесс из нашего файла находится по образу, а живой процесс из
// КОПИИ того же файла в другой папке — нет: сравнивается сам файл, а не имя.
func TestNashiYadraNahodyatProcessPoObrazu(t *testing.T) {
	svoy, err := os.Executable()
	if err != nil {
		t.Skipf("не нашёл свой exe: %v", err)
	}
	papka := t.TempDir()
	nashBin := filepath.Join(papka, "nashe", filepath.Base(svoy))
	chuzhoyBin := filepath.Join(papka, "chuzhoe", filepath.Base(svoy))
	for _, put := range []string{nashBin, chuzhoyBin} {
		skopirovat(t, svoy, put)
	}

	// Тестовый бинарь без подходящих тестов просто ждёт: -test.run на
	// несуществующее имя и долгий -test.timeout не годятся (выйдет сразу),
	// поэтому держим его на stdin, который не закрываем.
	zapustit := func(bin string) *exec.Cmd {
		cmd := exec.Command(bin, "-test.run=^TestNashiYadraZhdat$", "-test.v")
		cmd.Env = append(os.Environ(), "KELEVRA_TEST_ZHDAT=1")
		vhod, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Skipf("не запустил копию: %v", err)
		}
		t.Cleanup(func() { _ = vhod.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
		return cmd
	}
	nash := zapustit(nashBin)
	chuzhoy := zapustit(chuzhoyBin)

	var nashi []int
	for i := 0; i < 50; i++ {
		nashi = NashiYadra(nashBin)
		if len(nashi) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, znaem := putObrazaProcessa(nash.Process.Pid); !znaem {
		t.Skipf("ОС не отдала образ процесса на %s — проверка недоказательна", runtime.GOOS)
	}
	if len(nashi) != 1 || nashi[0] != nash.Process.Pid {
		t.Fatalf("NashiYadra(%s) = %v, ждали ровно [%d]", nashBin, nashi, nash.Process.Pid)
	}
	for _, pid := range nashi {
		if pid == chuzhoy.Process.Pid {
			t.Fatalf("процесс из копии файла в другой папке (pid %d) признан нашим ядром", pid)
		}
	}
}

// Не тест, а подопытный процесс для теста выше: живёт, пока открыт stdin.
func TestNashiYadraZhdat(t *testing.T) {
	if os.Getenv("KELEVRA_TEST_ZHDAT") != "1" {
		t.Skip("вспомогательный процесс, сам по себе не запускается")
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestNashiYadraBezFaylaPusto(t *testing.T) {
	if got := NashiYadra(filepath.Join(t.TempDir(), "net-takogo.exe")); len(got) != 0 {
		t.Fatalf("для несуществующего файла найдены процессы: %v", got)
	}
}

func skopirovat(t *testing.T, iz, v string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(v), 0o755); err != nil {
		t.Fatal(err)
	}
	vhod, err := os.Open(iz)
	if err != nil {
		t.Fatal(err)
	}
	defer vhod.Close()
	vyhod, err := os.OpenFile(v, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(vyhod, vhod); err != nil {
		t.Fatal(err)
	}
	if err := vyhod.Close(); err != nil {
		t.Fatal(err)
	}
}
