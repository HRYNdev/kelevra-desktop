package main

import (
	"errors"
	"testing"
)

// Решение «ставить ли службу при обновлении» держится тестом, а не глазами:
// живого Windows у нас нет, и сам вызов проверить нечем. Значит проверяемым
// обязано быть хотя бы решение.
func TestNuzhnoStavitSluzhbu(t *testing.T) {
	sluchai := []struct {
		imya    string
		os      string
		est     bool
		oshibka error
		hochet  bool
		pochemu string
	}{
		{
			imya: "Windows, службы нет", os: "windows", hochet: true,
			pochemu: "ровно тот случай, ради которого правка: 0.6.50 стоит, а службу никто не поставил",
		},
		{
			imya: "Windows, служба уже стоит", os: "windows", est: true, hochet: false,
			pochemu: "второй запрос прав человеку, у которого всё и так работает, — беспокойство без причины",
		},
		{
			imya: "Windows, диспетчер не ответил", os: "windows", oshibka: errors.New("отказ"), hochet: false,
			pochemu: "не зная, стоит ли служба, ставить её вслепую опаснее, чем не делать ничего",
		},
		{
			imya: "не Windows", os: "linux", hochet: false,
			pochemu: "служб Windows тут нет: запрос прав кончится отказом в журнале на каждом обновлении",
		},
		{
			imya: "не Windows, да ещё и с отказом", os: "darwin", oshibka: errors.New("отказ"), hochet: false,
			pochemu: "система решает раньше отказа: до диспетчера тут дело не доходит вовсе",
		},
	}
	for _, s := range sluchai {
		t.Run(s.imya, func(t *testing.T) {
			if got := nuzhnoStavitSluzhbu(s.os, s.est, s.oshibka); got != s.hochet {
				t.Errorf("nuzhnoStavitSluzhbu(%q, est=%v, oshibka=%v) = %v, ждали %v: %s",
					s.os, s.est, s.oshibka, got, s.hochet, s.pochemu)
			}
		})
	}
}
