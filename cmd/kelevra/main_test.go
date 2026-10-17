package main

import (
	"errors"
	"testing"
)

// TestTihiyZapusk держит различие, которое я дважды подряд стирал (см.
// комментарий у tihiyZapusk): «перезапуск ради свежего файла» молчит, а
// «перезапуск ради прав, которые человек только что выдал в UAC» обязан
// показать окно — иначе снаружи это выглядит как «нажал, и ничего».
//
// Наборы аргументов взяты не с потолка, а те, что приложение шлёт себе само:
//
//	cmd/kelevra/obnovlenie.go   zapustitSmenuPosleObnovleniya: --tiho --smena <pid>
//	(до 0.6.77) prava.Poprosit, кнопка прав:                          --smena <pid>
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

// TestPriStarteZapusk держит разбор --pri-starte (sprositPravaNaStarte,
// internal/prava.PoprositPriStarte): смена после запроса прав ПРИ СТАРТЕ
// обязана прийти БЕЗ --tiho (человек ждёт окно) и С --pri-starte (main.go по
// нему не должен звать vosstanovitPolnuyuZashchitu — см. её комментарий),
// а смена после автообновления (--tiho --smena) --pri-starte не несёт вовсе.
func TestPriStarteZapusk(t *testing.T) {
	sluchai := []struct {
		imya      string
		args      []string
		tiho      bool
		priStarte bool
	}{
		{
			imya:      "смена после запроса прав при старте",
			args:      []string{"Kelevra.exe", "--smena", "1234", "--pri-starte"},
			tiho:      false,
			priStarte: true,
		},
		{
			imya:      "смена после автообновления",
			args:      []string{"Kelevra.exe", "--tiho", "--smena", "1234"},
			tiho:      true,
			priStarte: false,
		},
	}
	for _, s := range sluchai {
		t.Run(s.imya, func(t *testing.T) {
			if got := tihiyZapusk(s.args); got != s.tiho {
				t.Errorf("tihiyZapusk(%q) = %v, ждали %v", s.args, got, s.tiho)
			}
			if got := priStarteZapusk(s.args); got != s.priStarte {
				t.Errorf("priStarteZapusk(%q) = %v, ждали %v", s.args, got, s.priStarte)
			}
		})
	}
}

// TestNuzhnoStavitSluzhbuNaStarte — решение 17.10: без службы Windows Kelevra
// не работает, и запуск руками сразу просит права на её установку. Не просит
// там, где человек приложение не открывал (автозапуск, смена после
// обновления), где служба уже стоит или где рядом живая копия (клик по трею).
func TestNuzhnoStavitSluzhbuNaStarte(t *testing.T) {
	sbo := errors.New("диспетчер служб не ответил")
	sluchai := []struct {
		imya              string
		os                string
		sluzhbaEst        bool
		oshibka           error
		tiho              bool
		smenaPID          int
		estChuzhayaKopiya bool
		hochet            bool
	}{
		{"первый запуск без службы — просить", "windows", false, nil, false, 0, false, true},
		{"служба уже стоит — не просить", "windows", true, nil, false, 0, false, false},
		{"диспетчер служб не ответил — не лезть", "windows", false, sbo, false, 0, false, false},
		{"тихий автозапуск — не просить", "windows", false, nil, true, 0, false, false},
		{"смена после обновления — своим путём", "windows", false, nil, true, 4321, false, false},
		{"смена без --tiho — не просить второй раз", "windows", false, nil, false, 4321, false, false},
		{"рядом живая копия — её окно покажет «Дать права»", "windows", false, nil, false, 0, true, false},
		{"не Windows — служб нет", "linux", false, nil, false, 0, false, false},
	}
	for _, s := range sluchai {
		t.Run(s.imya, func(t *testing.T) {
			got := nuzhnoStavitSluzhbuNaStarte(s.os, s.sluzhbaEst, s.oshibka, s.tiho, s.smenaPID, s.estChuzhayaKopiya)
			if got != s.hochet {
				t.Errorf("nuzhnoStavitSluzhbuNaStarte = %v, ждали %v", got, s.hochet)
			}
		})
	}
}

// Отказ в окне Windows не валит запуск: окно откроется на «Нужны права».
// Согласие — служба поставлена, запуск продолжается к ней.
func TestPostavitSluzhbuNaStarteOtkazISoglasie(t *testing.T) {
	staryy := poprositUstanovkuSluzhby
	defer func() { poprositUstanovkuSluzhby = staryy }()

	poprositUstanovkuSluzhby = func(okno uintptr) error { return errors.New("права не выданы") }
	if postavitSluzhbuNaStarte() {
		t.Fatal("при отказе в правах сказано «служба поставлена»")
	}
	poprositUstanovkuSluzhby = func(okno uintptr) error { return nil }
	if !postavitSluzhbuNaStarte() {
		t.Fatal("при согласии сказано «служба не поставлена»")
	}
}
