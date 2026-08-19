package yadro

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func sobratArhiv(t *testing.T, imena []string) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "yadro.zip")
	f, err := os.Create(put)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z := zip.NewWriter(f)
	for _, imya := range imena {
		w, err := z.Create(imya)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("ядро")); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return put
}

func imyaYadra() string {
	if runtime.GOOS == "windows" {
		return "sing-box.exe"
	}
	return "sing-box"
}

// Ядро лежит во вложенной папке — достать его всё равно надо.
func TestRaspakovatIzPodpapki(t *testing.T) {
	arhiv := sobratArhiv(t, []string{"sing-box-1.14/" + imyaYadra(), "sing-box-1.14/LICENSE"})
	kuda := filepath.Join(t.TempDir(), imyaYadra())
	if err := raspakovat(arhiv, kuda); err != nil {
		t.Fatalf("не распаковалось: %v", err)
	}
	st, err := os.Stat(kuda)
	if err != nil {
		t.Fatalf("ядра нет на месте: %v", err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0 {
		t.Fatalf("ядро распаковано без права на запуск: %v", st.Mode())
	}
}

// Имена внутри архива приходят снаружи: путь с выходом наверх не должен
// положить файл мимо папки приложения.
func TestRaspakovatNePishetMimo(t *testing.T) {
	papka := t.TempDir()
	arhiv := sobratArhiv(t, []string{"../../" + imyaYadra()})
	kuda := filepath.Join(papka, imyaYadra())
	if err := raspakovat(arhiv, kuda); err != nil {
		t.Fatalf("не распаковалось: %v", err)
	}
	if _, err := os.Stat(kuda); err != nil {
		t.Fatalf("ядро ушло не туда: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(papka)), imyaYadra())); err == nil {
		t.Fatal("файл записан выше папки приложения")
	}
}

// Архив без ядра — понятная ошибка, а не тихий успех с пустым файлом.
func TestRaspakovatBezYadra(t *testing.T) {
	arhiv := sobratArhiv(t, []string{"README.md"})
	kuda := filepath.Join(t.TempDir(), imyaYadra())
	if err := raspakovat(arhiv, kuda); err == nil {
		t.Fatal("архив без ядра прошёл как годный")
	}
	if _, err := os.Stat(kuda); err == nil {
		t.Fatal("на месте ядра остался мусор")
	}
}
