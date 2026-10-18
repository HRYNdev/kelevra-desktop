package main

import "testing"

// Служба обновилась в Program Files, а окно запущено из Загрузок: перезапуск
// «тем же файлом» дал бы ту же старую версию и бесконечный круг. Берётся файл
// службы.
func TestPerezapuskBeretFaylSluzhby(t *testing.T) {
	staroe := putUstanovlennoy
	defer func() { putUstanovlennoy = staroe }()
	putUstanovlennoy = func() string { return `C:\Program Files\Kelevra\Kelevra.exe` }
	if got := putDlyaNovoyKopii(`C:\Users\user\Downloads\Kelevra.exe`); got != `C:\Program Files\Kelevra\Kelevra.exe` {
		t.Fatalf("перезапуск из %q, ждали файл службы", got)
	}
}

// Службы в Program Files нет (другая система, служба стоит иначе): прежнее
// правило, свой пут.
func TestPerezapuskBezFaylaSluzhbyPrezhnee(t *testing.T) {
	staroe := putUstanovlennoy
	defer func() { putUstanovlennoy = staroe }()
	putUstanovlennoy = func() string { return "" }
	if got := putDlyaNovoyKopii("/tmp/Kelevra"); got != "/tmp/Kelevra" {
		t.Fatalf("перезапуск из %q, ждали свой путь", got)
	}
}
