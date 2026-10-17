package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/kopiya"
)

func TestNuzhenPerezapuskOkna(t *testing.T) {
	sluchai := []struct {
		okno, sluzhba string
		nado          bool
	}{
		{"0.6.73", "0.6.74", true},
		{"0.6.74", "0.6.74", false},
		{"0.6.75", "0.6.74", false}, // окно новее службы — перезапуск ничего не даст
		{"0.6.74", "", false},       // служба не сказала версию
		{"0.6.74-stend", "0.6.74", false},
	}
	for _, s := range sluchai {
		if got := nuzhenPerezapuskOkna(s.okno, s.sluzhba); got != s.nado {
			t.Errorf("окно %q, служба %q: перезапуск=%v, ждали %v", s.okno, s.sluzhba, got, s.nado)
		}
	}
}

func TestVersiyaSluzhby(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sostoyanie" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"versiya":"0.6.75","sost":"rabotaet"}`))
	}))
	defer server.Close()
	if v := versiyaSluzhby(server.URL + "/"); v != "0.6.75" {
		t.Fatalf("версия службы %q, ждали 0.6.75", v)
	}
	if v := versiyaSluzhby("http://127.0.0.1:1/"); v != "" {
		t.Fatalf("мёртвая служба назвала версию %q", v)
	}
}

func TestPutDlyaPerezapuska(t *testing.T) {
	papka := t.TempDir()
	novyy := filepath.Join(papka, "Kelevra.exe")
	if err := os.WriteFile(novyy, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, svoy := range []string{novyy + ".old", novyy + ".old2", filepath.Join(papka, "Kelevra.old")} {
		if p := putDlyaPerezapuska(svoy); p != novyy {
			t.Errorf("из %q запускаем %q, ждали %q", svoy, p, novyy)
		}
	}
	if p := putDlyaPerezapuska(novyy); p != novyy {
		t.Errorf("обычный путь подменён: %q", p)
	}
	// Нового файла нет — запускать нечего лучше своего пути.
	chuzhoy := filepath.Join(papka, "Drugoe.exe.old")
	if p := putDlyaPerezapuska(chuzhoy); p != chuzhoy {
		t.Errorf("без нового файла путь подменён: %q", p)
	}
}

// Служба переехала и она НОВЕЕ окна: окно не переводится на новый адрес, а
// уходит на перезапуск (жалоба 17.10: окно старой версии с новой страницей).
func TestStorozhPriNovoySluzhbePerezapuskaetOkno(t *testing.T) {
	papka := t.TempDir()
	novaya := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer novaya.Close()
	if err := kopiya.Zanyat(papka, novaya.URL, time.Now()); err != nil {
		t.Fatalf("не записал метку службы: %v", err)
	}
	pereshli := make(chan string, 4)
	sprosili := make(chan string, 4)
	gotovo := make(chan struct{})
	go func() {
		storozhitSluzhbu("http://127.0.0.1:1/", papka, 20*time.Millisecond, 3, time.Second,
			func() { t.Error("окно закрыто как при смерти службы") },
			func(a string) { pereshli <- a },
			func(a string) bool { sprosili <- a; return true })
		close(gotovo)
	}()
	select {
	case <-gotovo:
	case <-time.After(3 * time.Second):
		t.Fatal("сторож не ушёл после перезапуска окна")
	}
	if a := <-sprosili; a != novaya.URL {
		t.Fatalf("версию спросили у %q, а служба на %q", a, novaya.URL)
	}
	if len(pereshli) != 0 {
		t.Fatal("окно старой версии переведено на новый адрес вместо перезапуска")
	}
}

// Служба вернулась той же версии — всё как раньше: переход на новый адрес.
func TestStorozhPriTojZheVersiiPerevoditOkno(t *testing.T) {
	papka := t.TempDir()
	novaya := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer novaya.Close()
	if err := kopiya.Zanyat(papka, novaya.URL, time.Now()); err != nil {
		t.Fatalf("не записал метку службы: %v", err)
	}
	pereshli := make(chan string, 4)
	go storozhitSluzhbu("http://127.0.0.1:1/", papka, 20*time.Millisecond, 3, time.Second,
		func() {}, func(a string) { pereshli <- a }, func(string) bool { return false })
	select {
	case a := <-pereshli:
		if a != novaya.URL {
			t.Fatalf("переведено на %q, ждали %q", a, novaya.URL)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("сторож не перевёл окно на новый адрес")
	}
}
