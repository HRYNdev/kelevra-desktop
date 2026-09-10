package sluzhba

import (
	"testing"
	"time"
)

// Расписание отправки журналов целиком — таблицей, без ожидания настоящего
// часа и без сети.
//
// Правило изменилось 10.09.2026: было «раз в вечер, в 23:30», стало «раз в час
// от последней удачи». Причина простая: ночью ноутбуки выключены,
// и у половины машин посылка не уходила вовсе.
func TestRaspisanieOtpravkiZhurnalov(t *testing.T) {
	tochka := func(chas, minuta int) time.Time {
		return time.Date(2026, 8, 31, chas, minuta, 0, 0, time.Local)
	}
	sluchai := []struct {
		imya           string
		seychas        time.Time
		uspeh, popytka time.Time
		zhdem          bool
	}{
		{"никогда не отправляли — шлём сразу, не дожидаясь ночи",
			tochka(14, 0), time.Time{}, time.Time{}, true},
		{"удача была 20 минут назад — рано",
			tochka(14, 0), tochka(13, 40), time.Time{}, false},
		{"удача была 59 минут назад — всё ещё рано",
			tochka(14, 0), tochka(13, 1), time.Time{}, false},
		{"удача была час назад — пора",
			tochka(14, 0), tochka(13, 0), time.Time{}, true},
		{"час прошёл, но отказ был 20 минут назад — ждём час после отказа",
			tochka(14, 0), tochka(12, 0), tochka(13, 40), false},
		{"час прошёл и после отказа тоже — шлём",
			tochka(14, 0), tochka(12, 0), tochka(12, 30), true},
		{"ночь тут ни при чём: утром на свежей удаче молчим",
			tochka(9, 0), tochka(8, 30), time.Time{}, false},
		{"ноутбук включили утром после суток простоя — шлём немедленно",
			tochka(9, 0), tochka(9, 0).AddDate(0, 0, -1), time.Time{}, true},
	}
	for _, s := range sluchai {
		if got := poraOtpravlyatZhurnaly(s.seychas, s.uspeh, s.popytka); got != s.zhdem {
			t.Errorf("%s: got %v, ждали %v", s.imya, got, s.zhdem)
		}
	}
}

// Повтор — не чаще раза в час. Тикер ходит каждые пять минут, и без этого
// правила отказ сервера превратился бы в двенадцать посылок в час.
func TestPovtorNeChashcheRazaVChas(t *testing.T) {
	nachalo := time.Date(2026, 8, 31, 12, 0, 0, 0, time.Local)
	uspeh := nachalo.Add(-24 * time.Hour)
	popytka := nachalo
	for _, minut := range []int{5, 15, 30, 59} {
		if poraOtpravlyatZhurnaly(popytka.Add(time.Duration(minut)*time.Minute), uspeh, popytka) {
			t.Errorf("через %d минут после отказа уже полезли повторять", minut)
		}
	}
	if !poraOtpravlyatZhurnaly(popytka.Add(61*time.Minute), uspeh, popytka) {
		t.Error("через час после отказа повтор не случился")
	}
}
