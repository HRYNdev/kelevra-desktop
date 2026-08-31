package sluzhba

import (
	"testing"
	"time"
)

// Расписание суточной отправки целиком — таблицей, без ожидания настоящих
// 23:30 и без сети.
func TestRaspisanieOtpravkiZhurnalov(t *testing.T) {
	den := func(chas, minuta int) time.Time {
		return time.Date(2026, 8, 31, chas, minuta, 0, 0, time.Local)
	}
	vchera := func(chas, minuta int) time.Time {
		return time.Date(2026, 8, 30, chas, minuta, 0, 0, time.Local)
	}
	sluchai := []struct {
		imya           string
		seychas        time.Time
		uspeh, popytka time.Time
		zhdem          bool
	}{
		{"днём делать нечего: вечер ещё не наступил, за вчерашний отчитались",
			den(14, 0), vchera(23, 31), time.Time{}, false},
		{"вечер наступил, за него не отчитались — шлём",
			den(23, 31), vchera(23, 31), time.Time{}, true},
		{"за этот вечер уже отчитались — молчим",
			den(23, 45), den(23, 32), den(23, 32), false},
		{"вечер провалили, но пробовали 20 минут назад — ждём час",
			den(23, 50), vchera(23, 31), den(23, 31), false},
		{"вечер провалили, час прошёл — повторяем",
			den(0, 40).AddDate(0, 0, 1), vchera(23, 31), den(23, 31), true},
		{"машина спала в 23:30 и проснулась утром — догоняем сразу",
			den(9, 0), vchera(10, 0), time.Time{}, true},
		{"не отправляли ни разу — первый же наступивший вечер наш",
			den(23, 31), time.Time{}, time.Time{}, true},
	}
	for _, s := range sluchai {
		if got := poraOtpravlyatZhurnaly(s.seychas, s.uspeh, s.popytka); got != s.zhdem {
			t.Errorf("%s: got %v, ждали %v", s.imya, got, s.zhdem)
		}
	}
}

// Повтор — не чаще раза в час. Тикер ходит каждые пять минут, и без этого
// правила отказ сервера превратился бы в двенадцать посылок по 25 МБ в час.
func TestPovtorNeChashcheRazaVChas(t *testing.T) {
	vecher := time.Date(2026, 8, 31, 23, 30, 0, 0, time.Local)
	uspeh := vecher.Add(-24 * time.Hour)
	popytka := vecher.Add(time.Minute)
	for _, minut := range []int{5, 15, 30, 59} {
		if poraOtpravlyatZhurnaly(popytka.Add(time.Duration(minut)*time.Minute), uspeh, popytka) {
			t.Errorf("через %d минут после отказа уже полезли повторять", minut)
		}
	}
	if !poraOtpravlyatZhurnaly(popytka.Add(61*time.Minute), uspeh, popytka) {
		t.Error("через час после отказа повтор не случился")
	}
}
