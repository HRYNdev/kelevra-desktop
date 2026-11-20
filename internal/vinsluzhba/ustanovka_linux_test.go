//go:build linux

package vinsluzhba

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOtodvinutChuzhieYarlykiSamodelka(t *testing.T) {
	dom := t.TempDir()
	prilozheniya := filepath.Join(dom, ".local", "share", "applications")
	znachki := filepath.Join(dom, ".local", "share", "icons", "hicolor", "256x256", "apps")
	for _, p := range []string{prilozheniya, znachki} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	yarlyk := filepath.Join(prilozheniya, "kelevra.desktop")
	os.WriteFile(yarlyk, []byte("[Desktop Entry]\nName=Kelevra\nExec=python3 /home/v/.local/bin/kelevra-okno.py\n"), 0o644)
	znachok := filepath.Join(znachki, "kelevra.png")
	os.WriteFile(znachok, []byte("png"), 0o644)
	chuzhoy := filepath.Join(prilozheniya, "drugoe.desktop")
	os.WriteFile(chuzhoy, []byte("Exec=drugoe\n"), 0o644)

	vremya := time.Date(2026, 11, 20, 21, 30, 0, 0, time.UTC)
	itog := OtodvinutChuzhieYarlyki(dom, vremya)
	if len(itog) != 2 {
		t.Fatalf("ждал 2 строки, получил %v", itog)
	}
	for _, p := range []string{yarlyk, znachok} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s не отодвинут", p)
		}
		if _, err := os.Stat(p + ".bak-20261120-213000"); err != nil {
			t.Fatalf("нет копии %s: %v", p, err)
		}
	}
	if _, err := os.Stat(chuzhoy); err != nil {
		t.Fatalf("тронут посторонний ярлык: %v", err)
	}
}

func TestOtodvinutChuzhieYarlykiSvoyNeTrogaet(t *testing.T) {
	dom := t.TempDir()
	prilozheniya := filepath.Join(dom, ".local", "share", "applications")
	os.MkdirAll(prilozheniya, 0o755)
	yarlyk := filepath.Join(prilozheniya, "kelevra.desktop")
	os.WriteFile(yarlyk, []byte("[Desktop Entry]\nExec=/opt/kelevra/kelevra --trey\n"), 0o644)
	if itog := OtodvinutChuzhieYarlyki(dom, time.Now()); len(itog) != 0 {
		t.Fatalf("свой ярлык тронут: %v", itog)
	}
	if _, err := os.Stat(yarlyk); err != nil {
		t.Fatal(err)
	}
}

func TestYarlykNash(t *testing.T) {
	sluchai := map[string]bool{
		"Exec=/opt/kelevra/kelevra\n":                    true,
		"Exec=/opt/kelevra/kelevra --trey\n":             true,
		"Exec=\"/opt/kelevra/kelevra\" --trey\n":         true,
		"Exec=/opt/kelevra/kelevra-staraya\n":            false,
		"Exec=/home/v/.local/bin/kelevra\n":              false,
		"Name=Kelevra\n":                                 false,
		"Exec=/opt/kelevra/kelevra\nExec=python3 x.py\n": false,
	}
	for tekst, zhdu := range sluchai {
		if poluchil := yarlykNash(tekst); poluchil != zhdu {
			t.Errorf("%q: ждал %v, получил %v", tekst, zhdu, poluchil)
		}
	}
}
