package yadro

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpis"
)

func zipYadra(t *testing.T, soderzhimoe string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create(imyaYadra())
	w.Write([]byte(soderzhimoe))
	z.Close()
	return b.Bytes()
}

// stendYadra поднимает «GitHub» с одним релизом ядра. arhiv — что реально лежит
// в релизе; podpisan — что подписано (если nil — подписи в релизе нет вовсе).
func stendYadra(t *testing.T, arhiv []byte, podpisan []byte) *Yadro {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	staroe, staryKlyuch := obnovlenie.TrebovatPodpis, podpis.KlyuchHex
	obnovlenie.TrebovatPodpis, podpis.KlyuchHex = true, hex.EncodeToString(pub)
	t.Cleanup(func() { obnovlenie.TrebovatPodpis, podpis.KlyuchHex = staroe, staryKlyuch })

	sig := ""
	if podpisan != nil {
		tmp := filepath.Join(t.TempDir(), "p.zip")
		os.WriteFile(tmp, podpisan, 0o644)
		var err error
		sig, err = podpis.Podpisat(base64.StdEncoding.EncodeToString(priv.Seed()), ImyaArhiva(), tmp)
		if err != nil {
			t.Fatal(err)
		}
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/spisok":
			assets := fmt.Sprintf(`{"name":%q,"browser_download_url":"%s/a.zip"}`, ImyaArhiva(), srv.URL)
			if podpisan != nil {
				assets += fmt.Sprintf(`,{"name":%q,"browser_download_url":"%s/a.sig"}`, ImyaArhiva()+".sig", srv.URL)
			}
			fmt.Fprintf(w, `[{"tag_name":"core-v1-1","assets":[%s]}]`, assets)
		case strings.HasSuffix(r.URL.Path, ".sig"):
			fmt.Fprint(w, sig)
		default:
			w.Write(arhiv)
		}
	}))
	t.Cleanup(srv.Close)
	papka := t.TempDir()
	return &Yadro{Papka: papka, Bin: filepath.Join(papka, imyaYadra()), Spisok: srv.URL + "/spisok"}
}

func TestZagruzitYadroSVernoyPodpisyu(t *testing.T) {
	a := zipYadra(t, "nastoyashchee")
	y := stendYadra(t, a, a)
	if err := y.Zagruzit(context.Background()); err != nil {
		t.Fatalf("подписанное ядро не встало: %v", err)
	}
	if b, _ := os.ReadFile(y.Bin); string(b) != "nastoyashchee" {
		t.Fatalf("в ядре %q", b)
	}
}

func TestZagruzitYadroPodmenennoeOtvergaetsya(t *testing.T) {
	y := stendYadra(t, zipYadra(t, "vredonosnoe"), zipYadra(t, "nastoyashchee"))
	if err := y.Zagruzit(context.Background()); err == nil {
		t.Fatal("подменённое ядро встало")
	}
	if _, err := os.Stat(y.Bin); err == nil {
		t.Fatal("подменённое ядро осталось на диске")
	}
}

func TestZagruzitYadroBezPodpisiOtvergaetsya(t *testing.T) {
	y := stendYadra(t, zipYadra(t, "nastoyashchee"), nil)
	if err := y.Zagruzit(context.Background()); err == nil {
		t.Fatal("ядро без подписи встало")
	}
	if _, err := os.Stat(y.Bin); err == nil {
		t.Fatal("ядро без подписи осталось на диске")
	}
}
