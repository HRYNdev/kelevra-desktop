package vinsluzhba

import (
	"path/filepath"
	"testing"
)

func TestPutSluzhbyVProgramFiles(t *testing.T) {
	pf := filepath.Join("C:", string(filepath.Separator), "Program Files")
	if got, want := PutSluzhby(pf), filepath.Join(pf, "Kelevra", "Kelevra.exe"); got != want {
		t.Fatalf("путь службы %q, ждали %q", got, want)
	}
}

// Служба из «Загрузок», профиля или любой пользовательской папки переносится;
// из своей папки в Program Files — нет.
func TestNuzhnoPerenosit(t *testing.T) {
	pf := filepath.Join("C:", string(filepath.Separator), "Program Files")
	sep := string(filepath.Separator)
	tablica := []struct {
		imya string
		put  string
		pf   string
		nado bool
	}{
		{"загрузки", "C:" + sep + "Users" + sep + "user" + sep + "Downloads" + sep + "Kelevra.exe", pf, true},
		{"профиль", "C:" + sep + "Users" + sep + "user" + sep + "AppData" + sep + "Local" + sep + "Programs" + sep + "Kelevra" + sep + "Kelevra.exe", pf, true},
		{"уже на месте", PutSluzhby(pf), pf, false},
		{"другой регистр", "c:" + sep + "program files" + sep + "kelevra" + sep + "KELEVRA.EXE", pf, false},
		{"сосед по префиксу", pf + " (x86)" + sep + "Kelevra" + sep + "Kelevra.exe", pf, true},
		{"другая папка в Program Files", pf + sep + "Drugoe" + sep + "Kelevra.exe", pf, true},
		{"выход наверх через ..", pf + sep + "Kelevra" + sep + ".." + sep + ".." + sep + "Users" + sep + "x" + sep + "Kelevra.exe", pf, true},
		{"вложенная папка службы", pf + sep + "Kelevra" + sep + "sub" + sep + "Kelevra.exe", pf, false},
		{"Program Files неизвестна", "C:" + sep + "x" + sep + "Kelevra.exe", "", false},
		{"путь пуст", "", pf, false},
	}
	for _, c := range tablica {
		if got := NuzhnoPerenosit(c.put, c.pf); got != c.nado {
			t.Errorf("%s: NuzhnoPerenosit(%q, %q) = %v, ждали %v", c.imya, c.put, c.pf, got, c.nado)
		}
	}
}
