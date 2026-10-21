package avtozapusk

import "testing"

// Чистые решения починки автозапуска — без реестра, на любой ОС.

const pf = `C:\Program Files\Kelevra\Kelevra.exe`

func TestPochinka(t *testing.T) {
	sluchai := []struct {
		imya     string
		zapis    string
		est      bool
		putPF    string
		argument string
		nado     string
		menyat   bool
	}{
		// Ровно беда ПК Вовы 18–21.10: файл в «Загрузках» удалён.
		{"запись на Загрузки", `"C:\Users\vladi\Downloads\Kelevra.exe" --trey`, true, pf, ArgumentSoSluzhboy,
			`"C:\Program Files\Kelevra\Kelevra.exe" --trey`, true},
		{"уже верная", `"C:\Program Files\Kelevra\Kelevra.exe" --trey`, true, pf, ArgumentSoSluzhboy, "", false},
		{"верная, другой регистр и пробел", ` "c:\program files\kelevra\kelevra.exe" --trey `, true, pf, ArgumentSoSluzhboy, "", false},
		{"путь верный, режим старый (--tiho при службе)", `"C:\Program Files\Kelevra\Kelevra.exe" --tiho`, true, pf, ArgumentSoSluzhboy,
			`"C:\Program Files\Kelevra\Kelevra.exe" --trey`, true},
		{"старая запись без кавычек", `C:\Users\mama\AppData\Local\Kelevra.exe --tiho`, true, pf, ArgumentSoSluzhboy,
			`"C:\Program Files\Kelevra\Kelevra.exe" --trey`, true},
		{"записи нет — человек выключил", "", false, pf, ArgumentSoSluzhboy, "", false},
		{"запись пустая", "  ", true, pf, ArgumentSoSluzhboy, "", false},
		{"копии в Program Files нет", `"C:\Users\vladi\Downloads\Kelevra.exe" --trey`, true, "", ArgumentSoSluzhboy, "", false},
	}
	for _, s := range sluchai {
		nado, menyat := Pochinka(s.zapis, s.est, s.putPF, s.argument)
		if nado != s.nado || menyat != s.menyat {
			t.Errorf("%s: получили (%q, %v), ждали (%q, %v)", s.imya, nado, menyat, s.nado, s.menyat)
		}
	}
}

func TestPutIzZapisi(t *testing.T) {
	sluchai := map[string]string{
		`"C:\Program Files\Kelevra\Kelevra.exe" --trey`: `C:\Program Files\Kelevra\Kelevra.exe`,
		`C:\Users\vladi\Downloads\Kelevra.exe --tiho`:   `C:\Users\vladi\Downloads\Kelevra.exe`,
		`C:\Kelevra\Kelevra.EXE`:                        `C:\Kelevra\Kelevra.EXE`,
		`"C:\Bez kavychki\Kelevra.exe`:                  `C:\Bez kavychki\Kelevra.exe`,
		`  `:                                            ``,
	}
	for zapis, zhdali := range sluchai {
		if got := PutIzZapisi(zapis); got != zhdali {
			t.Errorf("PutIzZapisi(%q) = %q, ждали %q", zapis, got, zhdali)
		}
	}
}

func TestOpisanieDlyaDiagnostiki(t *testing.T) {
	if got := OpisanieDlyaDiagnostiki(nil); got != "net_zapisi" {
		t.Fatalf("без записей: %q", got)
	}
	got := OpisanieDlyaDiagnostiki([]Zapis{
		{Stroka: `"C:\Users\vladi\Downloads\Kelevra.exe" --trey`, Put: `C:\Users\vladi\Downloads\Kelevra.exe`},
		{Stroka: `"C:\Program Files\Kelevra\Kelevra.exe" --trey`, Put: pf, FaylEst: true},
	})
	zhdali := `%USERPROFILE%\Downloads\Kelevra.exe [net], C:\Program Files\Kelevra\Kelevra.exe [est]`
	if got != zhdali {
		t.Fatalf("описание %q, ждали %q", got, zhdali)
	}
}
