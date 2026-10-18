package obnovlenie

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/podpis"
)

// stendPodpisi включает требование подписи и подставляет тестовый ключ. Возвращает
// seed для подписи и сервер, раздающий файл и подпись по заданным ответам.
func stendPodpisi(t *testing.T, fayl []byte, sigTekst func(put string) string) (Novaya, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	staroe, staryKlyuch := TrebovatPodpis, podpis.KlyuchHex
	TrebovatPodpis, podpis.KlyuchHex = true, hex.EncodeToString(pub)
	t.Cleanup(func() { TrebovatPodpis, podpis.KlyuchHex = staroe, staryKlyuch })

	// подпись настоящего файла делаем тут, чтобы подмена в сервере была честной
	papka := t.TempDir()
	nastoyashchiy := filepath.Join(papka, "nastoyashchiy.exe")
	if err := os.WriteFile(nastoyashchiy, []byte("MZ nastoyashchaya versiya"), 0o755); err != nil {
		t.Fatal(err)
	}
	sig, err := podpis.Podpisat(base64.StdEncoding.EncodeToString(priv.Seed()), ImyaFayla, nastoyashchiy)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".sig"):
			if sigTekst == nil {
				http.NotFound(w, r)
				return
			}
			fmt.Fprint(w, sigTekst(sig))
		default:
			w.Write(fayl)
		}
	}))
	t.Cleanup(srv.Close)
	n := Novaya{Versiya: "9.9.9", Ssylka: srv.URL + "/Kelevra.exe", SsylkaPodpisi: srv.URL + "/Kelevra.exe.sig", Razmer: int64(len(fayl))}
	return n, sig
}

func tekushchiy(t *testing.T) (string, []byte) {
	t.Helper()
	put := filepath.Join(t.TempDir(), "Kelevra.exe")
	staroe := []byte("MZ staraya versiya")
	if err := os.WriteFile(put, staroe, 0o755); err != nil {
		t.Fatal(err)
	}
	return put, staroe
}

// Верная подпись: обновление встаёт.
func TestPostavitSVernoyPodpisyu(t *testing.T) {
	n, _ := stendPodpisi(t, []byte("MZ nastoyashchaya versiya"), func(sig string) string { return sig })
	put, _ := tekushchiy(t)
	if err := Postavit(context.Background(), nil, n, put); err != nil {
		t.Fatalf("верно подписанное обновление не встало: %v", err)
	}
	b, _ := os.ReadFile(put)
	if string(b) != "MZ nastoyashchaya versiya" {
		t.Fatalf("после установки в файле %q", b)
	}
}

// Подменённый файл при чужой (настоящей) подписи: отказ, старая версия цела.
func TestPostavitPodmenennyyFaylOtvergaetsya(t *testing.T) {
	n, _ := stendPodpisi(t, []byte("MZ podmenennyy vredonosnyy"), func(sig string) string { return sig })
	put, staroe := tekushchiy(t)
	err := Postavit(context.Background(), nil, n, put)
	if !errors.Is(err, ErrPodpis) {
		t.Fatalf("подмена прошла или отказ не тот: %v", err)
	}
	b, _ := os.ReadFile(put)
	if string(b) != string(staroe) {
		t.Fatalf("старая версия тронута: %q", b)
	}
	if HvostEst(put) {
		t.Fatal("после отказа остался хвост рядом с приложением")
	}
}

// Файла подписи нет на сервере (404): отказ.
func TestPostavitBezPodpisiOtvergaetsya(t *testing.T) {
	n, _ := stendPodpisi(t, []byte("MZ nastoyashchaya versiya"), nil)
	put, staroe := tekushchiy(t)
	if err := Postavit(context.Background(), nil, n, put); !errors.Is(err, ErrPodpis) {
		t.Fatalf("без подписи установка прошла: %v", err)
	}
	if b, _ := os.ReadFile(put); string(b) != string(staroe) {
		t.Fatal("старая версия тронута")
	}
}

// В описании релиза не нашлось ссылки на подпись вовсе.
func TestPostavitBezSsylkiNaPodpis(t *testing.T) {
	n, _ := stendPodpisi(t, []byte("MZ nastoyashchaya versiya"), func(sig string) string { return sig })
	n.SsylkaPodpisi = ""
	put, _ := tekushchiy(t)
	if err := Postavit(context.Background(), nil, n, put); !errors.Is(err, ErrPodpis) {
		t.Fatalf("без ссылки на подпись установка прошла: %v", err)
	}
}

// Proverit: релиз новее нас, но без Kelevra.exe.sig, при включённом требовании
// не предлагается к установке.
func TestProveritRelizBezPodpisiOtvergaetsya(t *testing.T) {
	stendPodpisi(t, []byte("x"), nil)
	telo := spisok(reliz("app-v9.9.9", ImyaFayla, 2000000, false, false))
	srv := server(t, telo)
	n, err := Proverit(context.Background(), nil, srv.URL, "0.6.77")
	if n != nil || !errors.Is(err, ErrPodpis) {
		t.Fatalf("релиз без подписи предложен: n=%v err=%v", n, err)
	}
}

func TestProveritRelizSPodpisyuPredlagaetSsylku(t *testing.T) {
	stendPodpisi(t, []byte("x"), nil)
	telo := `[{"tag_name":"app-v9.9.9","assets":[` +
		`{"name":"Kelevra.exe","browser_download_url":"http://primer/a","size":2000000},` +
		`{"name":"Kelevra.exe.sig","browser_download_url":"http://primer/a.sig","size":88}]}]`
	srv := server(t, telo)
	n, err := Proverit(context.Background(), nil, srv.URL, "0.6.77")
	if err != nil || n == nil {
		t.Fatalf("n=%v err=%v", n, err)
	}
	if n.SsylkaPodpisi != "http://primer/a.sig" {
		t.Fatalf("ссылка на подпись %q", n.SsylkaPodpisi)
	}
}
