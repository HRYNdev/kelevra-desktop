package main

import "testing"

// TestTihiyZapusk держит различие, которое я дважды подряд стирал (см.
// комментарий у tihiyZapusk): «перезапуск ради свежего файла» молчит, а
// «перезапуск ради прав, которые человек только что выдал в UAC» обязан
// показать окно — иначе снаружи это выглядит как «нажал, и ничего».
//
// Наборы аргументов взяты не с потолка, а те, что приложение шлёт себе само:
//
//	cmd/kelevra/obnovlenie.go   zapustitSmenuPosleObnovleniya: --tiho --smena <pid>
//	internal/prava/prava_windows.go  Poprosit:                        --smena <pid>
//	internal/zapusk (автозапуск с Windows):                           --tiho
//	человек щёлкнул по значку:                                        (без аргументов)
func TestTihiyZapusk(t *testing.T) {
	sluchai := []struct {
		imya    string
		args    []string
		tiho    bool
		pochemu string
	}{
		{
			imya:    "смена прав после UAC",
			args:    []string{"Kelevra.exe", "--smena", "1234"},
			tiho:    false,
			pochemu: "человек нажал «Включить для всех программ» и согласился в UAC — он ждёт окно, а не тишину",
		},
		{
			imya:    "смена после автообновления",
			args:    []string{"Kelevra.exe", "--tiho", "--smena", "1234"},
			tiho:    true,
			pochemu: "приложение обновило само себя фоном; окна тут никто не просил",
		},
		{
			imya:    "автозапуск с Windows",
			args:    []string{"Kelevra.exe", "--tiho"},
			tiho:    true,
			pochemu: "вход в систему: служба поднимается молча, значок в трее",
		},
		{
			imya:    "человек щёлкнул по значку",
			args:    []string{"Kelevra.exe"},
			tiho:    false,
			pochemu: "обычный запуск руками — окно обязано открыться",
		},
	}
	for _, s := range sluchai {
		t.Run(s.imya, func(t *testing.T) {
			if got := tihiyZapusk(s.args); got != s.tiho {
				t.Errorf("tihiyZapusk(%q) = %v, ждали %v: %s", s.args, got, s.tiho, s.pochemu)
			}
		})
	}
}
