package main

import "testing"

// podmenitZnachok — подмена ОС для podnyatZnachokEsliNet: служба есть/нет,
// значок жив/нет; возвращает счётчик запусков --trey.
func podmenitZnachok(t *testing.T, sluzhba, zhiv bool) *int {
	t.Helper()
	zapuskov := 0
	staryyZhiv, staryySluzhba, staryyZapusk := znachokZhiv, sluzhbaDlyaZnachkaEst, zapustitZnachok
	znachokZhiv = func() bool { return zhiv }
	sluzhbaDlyaZnachkaEst = func() bool { return sluzhba }
	zapustitZnachok = func() { zapuskov++ }
	t.Cleanup(func() {
		znachokZhiv, sluzhbaDlyaZnachkaEst, zapustitZnachok = staryyZhiv, staryySluzhba, staryyZapusk
	})
	return &zapuskov
}

// Ноут 01-3, 24.10: процесса --trey нет, крестик окна убирает приложение
// с глаз совсем. Окно обязано поднять значок само.
func TestOknoPodnimaetZnachokEsliEgoNet(t *testing.T) {
	n := podmenitZnachok(t, true, false)
	if !podnyatZnachokEsliNet("закрытии окна") || *n != 1 {
		t.Fatalf("значка нет, служба есть — окно не подняло значок (запусков %d)", *n)
	}
}

func TestOknoNePodnimaetVtoroyZnachok(t *testing.T) {
	n := podmenitZnachok(t, true, true)
	if podnyatZnachokEsliNet("открытии окна") || *n != 0 {
		t.Fatalf("значок жив — второй поднимать нельзя (запусков %d)", *n)
	}
}

// Без службы Windows туннеля держать некому, кроме окна: значку показывать
// нечего, и раньше его в этом случае не поднимали.
func TestBezSluzhbyZnachokNePodnimaetsya(t *testing.T) {
	n := podmenitZnachok(t, false, false)
	if podnyatZnachokEsliNet("закрытии окна") || *n != 0 {
		t.Fatalf("без службы значок не поднимаем (запусков %d)", *n)
	}
}

func TestMozhnoStavitZnachok(t *testing.T) {
	sluchai := []struct {
		imya                     string
		zamok, smena, staroeOkno bool
		zhdu                     bool
	}{
		{"замок взят, других нет", true, false, false, true},
		{"замок занят другим значком", false, false, false, false},
		{"замок занят, даже при смене", false, true, false, false},
		{"есть значок старой версии без замка", true, false, true, false},
		{"смена: окно прежнего значка ещё не ушло", true, true, true, true},
	}
	for _, c := range sluchai {
		if got := mozhnoStavitZnachok(c.zamok, c.smena, c.staroeOkno); got != c.zhdu {
			t.Errorf("%s: %v, ждал %v", c.imya, got, c.zhdu)
		}
	}
}
