package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/kopiya"
	"github.com/HRYNdev/kelevra-desktop/internal/zaslon"
)

// Аварийный выход --snyat-zapret: держатель отпущен; живая служба —
// «Отключить»; не ответила — остановить. Ни службы, ни WFP в проверке нет.
func TestSnyatZapretPoryadok(t *testing.T) {
	papka := t.TempDir()
	_ = zaslon.ZapisatPeredachu(papka, zaslon.Peredacha{Do: time.Now().Add(time.Minute)})

	ostanovleno := false
	ostanovit := func() error { ostanovleno = true; return nil }
	rabotaet := func() (bool, error) { return true, nil }

	// Копии нет — служба останавливается.
	if itog := snyatZapret(papka, func(string) error { t.Fatal("отключать некого"); return nil }, rabotaet, ostanovit); !ostanovleno || itog != "Запрет снят: служба остановлена" {
		t.Fatalf("итог %q, остановлена %v", itog, ostanovleno)
	}
	if _, err := os.Stat(filepath.Join(papka, zaslon.ImyaPeredachi)); !os.IsNotExist(err) {
		t.Fatal("держатель не отпущен")
	}

	// Копия отвечает — «Отключить», служба не трогается.
	zhivaya := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer zhivaya.Close()
	url := zhivaya.URL + "/klyuch/"
	if err := kopiya.Zanyat(papka, url, time.Now()); err != nil {
		t.Fatal(err)
	}
	ostanovleno = false
	var adres string
	itog := snyatZapret(papka, func(a string) error { adres = a; return nil }, rabotaet, ostanovit)
	if itog != "Запрет снят: Отключено" || ostanovleno || adres != url {
		t.Fatalf("итог %q, адрес %q, остановлена %v", itog, adres, ostanovleno)
	}

	// Копия молчит — остановить; не вышло — так и сказать.
	itog = snyatZapret(papka, func(string) error { return errors.New("молчит") }, rabotaet, func() error { return errors.New("нет прав") })
	if itog != "Запрет не снят: нет прав" {
		t.Fatalf("итог %q", itog)
	}
}
