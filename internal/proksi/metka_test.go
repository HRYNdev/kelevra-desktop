package proksi

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Обновление из прокси-режима со службой Windows: данные переехали в общую
// папку, а метка старой версии осталась в папке человека (hranenie.Perenesti
// её не переносит). Уборка обязана найти её там и убрать — иначе адрес из
// метки не попадёт в список своих, а сама метка будет лежать вечно.
func TestMetkaIzPapkiChelovekaNahoditsyaIUbiraetsya(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("папка человека (LOCALAPPDATA) есть только в Windows")
	}
	// KELEVRA_DIR пуст — иначе папки человека нет вовсе (hranenie.PapkaLichnaya).
	// Обе настоящие папки подменены временными.
	programdata, lichnaya := t.TempDir(), t.TempDir()
	t.Setenv("KELEVRA_DIR", "")
	t.Setenv("PROGRAMDATA", programdata)
	t.Setenv("LOCALAPPDATA", lichnaya)
	if err := os.MkdirAll(filepath.Join(programdata, "Kelevra"), 0o755); err != nil {
		t.Fatal(err)
	}
	papka := filepath.Join(lichnaya, "Kelevra")
	if err := os.MkdirAll(papka, 0o755); err != nil {
		t.Fatal(err)
	}
	put := filepath.Join(papka, imyaMetki)
	if err := os.WriteFile(put, []byte(`{"adres":"127.0.0.1:3000"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	adres, est := ProchestMetku()
	if !est || adres != "127.0.0.1:3000" {
		t.Fatalf("метка из папки человека не прочитана: %q %v", adres, est)
	}
	nashi := nashiAdresa()
	if len(nashi) != 2 || nashi[0] != StaryyAdres || nashi[1] != "127.0.0.1:3000" {
		t.Fatalf("свои адреса %v — ждали адрес старого режима и адрес из метки", nashi)
	}
	UbratMetku()
	if _, err := os.Stat(put); !os.IsNotExist(err) {
		t.Fatal("метка в папке человека осталась после уборки")
	}
}

// Без метки свой адрес один — тот, на котором стоял прокси-режим.
func TestBezMetkiSvoyAdresOdin(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if nashi := nashiAdresa(); len(nashi) != 1 || nashi[0] != StaryyAdres {
		t.Fatalf("свои адреса без метки: %v", nashi)
	}
}
