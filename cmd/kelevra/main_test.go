package main

import (
	"errors"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

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

// TestNuzhnoSprositPravaNaStarte — заказ хозяина 29.08: «чтоб это подтверждение
// тупо вылазило при старте программы и больше не мешало». Функция решает,
// не исполняет — тест покрывает все четыре сторожа разом, включая тот самый,
// из-за которого автозапрос по коннекту у существующих установок молчит
// (см. её комментарий и hranenie.Zagruzit).
func TestNuzhnoSprositPravaNaStarte(t *testing.T) {
	sluchai := []struct {
		imya              string
		estPrava          bool
		tiho              bool
		smenaPID          int
		estChuzhayaKopiya bool
		hochet            bool
	}{
		{"чистый старт без прав — спросить", false, false, 0, false, true},
		{"права уже есть — не спрашивать", true, false, 0, false, false},
		{"тихий автозапуск/автообновление — не спрашивать", false, true, 0, false, false},
		{"это уже смена режима — не спрашивать снова", false, false, 4321, false, false},
		{"рядом висит живая копия — не мешать кликом по трею", false, false, 0, true, false},
	}
	for _, s := range sluchai {
		t.Run(s.imya, func(t *testing.T) {
			got := nuzhnoSprositPravaNaStarte(s.estPrava, s.tiho, s.smenaPID, s.estChuzhayaKopiya)
			if got != s.hochet {
				t.Errorf("nuzhnoSprositPravaNaStarte(%v,%v,%v,%v) = %v, ждали %v",
					s.estPrava, s.tiho, s.smenaPID, s.estChuzhayaKopiya, got, s.hochet)
			}
		})
	}
}

// TestSprositPravaNaStarteOtkazProdolzhaet — человек нажал «Нет» в UAC:
// приложение обязано продолжить обычный запуск (прокси-режим), а не уйти,
// как при согласии. Отметку «уже спрашивали» при этом всё равно обязано
// сохранить — иначе тот же человек увидит второй попап при первом подключении
// (см. sluzhba.zaprositPravaAvtomaticheskiEsliNado).
func TestSprositPravaNaStarteOtkazProdolzhaet(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())

	staryyHook := poprositPravaPriStarte
	defer func() { poprositPravaPriStarte = staryyHook }()
	poprositPravaPriStarte = func(smenaPID int) error {
		return errors.New("права не выданы")
	}

	uyti := sprositPravaNaStarte(hranenie.Papka())
	if uyti {
		t.Fatal("при отказе sprositPravaNaStarte велела уйти — приложение осталось бы без прав и без окна")
	}

	n, err := hranenie.Zagruzit()
	if err != nil {
		t.Fatalf("не прочитал настройки: %v", err)
	}
	if !n.UzheSprosiliPrava() {
		t.Fatal("отказ не сохранил отметку «уже спрашивали» — автозапрос по коннекту спросит ещё раз")
	}
}

// TestSprositPravaNaStarteSoglasieVelitUyti — согласие в UAC обязано вернуть
// «уйти»: новая, уже повышенная копия поднимется сама, а эта — лишняя.
func TestSprositPravaNaStarteSoglasieVelitUyti(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())

	staryyHook := poprositPravaPriStarte
	defer func() { poprositPravaPriStarte = staryyHook }()
	poprositPravaPriStarte = func(smenaPID int) error { return nil }

	uyti := sprositPravaNaStarte(hranenie.Papka())
	if !uyti {
		t.Fatal("при согласии sprositPravaNaStarte не велела уйти — на машине останутся две копии")
	}

	n, err := hranenie.Zagruzit()
	if err != nil {
		t.Fatalf("не прочитал настройки: %v", err)
	}
	if !n.UzheSprosiliPrava() {
		t.Fatal("согласие не сохранило отметку «уже спрашивали»")
	}
}
