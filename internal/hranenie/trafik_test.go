package hranenie

import "testing"

// Самое хрупкое место счётчика: ядро — отдельный процесс, и его счётчик
// обнуляется с каждым его перезапуском. Разность «новое минус прошлое» тогда
// отрицательна, и наивный код съел бы уже посчитанное. Итог обязан только
// расти.
func TestPerezapuskYadraNeUmenshaetItog(t *testing.T) {
	n := &Nastroyki{}

	if got := n.UchestPokazanieYadra(1000); got != 1000 {
		t.Fatalf("первый замер дал %d, ждали 1000", got)
	}
	if got := n.UchestPokazanieYadra(2500); got != 2500 {
		t.Fatalf("прирост 1500 дал %d, ждали 2500", got)
	}
	// Ядро перезапустилось: счётчик пошёл с нуля и показал 300.
	if got := n.UchestPokazanieYadra(300); got != 2800 {
		t.Fatalf("после перезапуска ядра итог %d, ждали 2800 (2500 + 300)", got)
	}
	// И дальше считает от нового показания, а не от старого.
	if got := n.UchestPokazanieYadra(500); got != 3000 {
		t.Fatalf("итог %d, ждали 3000 (2800 + 200)", got)
	}
	if n.TrafikYadraBayt != 500 {
		t.Fatalf("отметка показания ядра %d, ждали 500", n.TrafikYadraBayt)
	}
}

// Мы перезапустились, а ядро — нет: сохранённая отметка обязана уберечь от
// двойного счёта уже посчитанного ядром.
func TestNashPerezapuskPriZhivomYadreNeSchitaetDvazhdy(t *testing.T) {
	n := &Nastroyki{TrafikBayt: 900, TrafikYadraBayt: 900}
	if got := n.UchestPokazanieYadra(1100); got != 1100 {
		t.Fatalf("итог %d, ждали 1100 (900 + прирост 200), а не 2000", got)
	}
}

// Ядро отдало бессмыслицу — не повод ни менять итог, ни сбивать отметку:
// следующий честный замер должен посчитаться от прежней отметки.
func TestOtritsatelnoePokazanieNichegoNeMenyaet(t *testing.T) {
	n := &Nastroyki{TrafikBayt: 700, TrafikYadraBayt: 700}
	if got := n.UchestPokazanieYadra(-5); got != 700 {
		t.Fatalf("итог %d, ждали 700", got)
	}
	if n.TrafikYadraBayt != 700 {
		t.Fatalf("отметка сбита в %d, ждали 700", n.TrafikYadraBayt)
	}
}

// Итог живёт между запусками — иначе «расход за всё время» обнулялся бы
// каждым обновлением приложения.
func TestItogPerezhivaetSohranenie(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())
	n, err := Zagruzit()
	if err != nil {
		t.Fatalf("не загрузились: %v", err)
	}
	n.UchestPokazanieYadra(4242)
	if err := Sohranit(n); err != nil {
		t.Fatalf("не сохранились: %v", err)
	}
	snova, err := Zagruzit()
	if err != nil {
		t.Fatalf("не перечитались: %v", err)
	}
	if snova.TrafikUstroystva() != 4242 {
		t.Fatalf("после перечтения расход %d, ждали 4242", snova.TrafikUstroystva())
	}
	if snova.TrafikYadraBayt != 4242 {
		t.Fatalf("отметка показания ядра не пережила диск: %d", snova.TrafikYadraBayt)
	}
}
