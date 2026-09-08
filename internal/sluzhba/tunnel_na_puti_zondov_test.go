package sluzhba

import (
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Пока ядро ПОДНИМАЕТСЯ, его туннель уже лежит на пути зондов.
//
// Беда с живой машины 08.09: вне дома авторежим поднял защиту, а через две
// секунды объявил «дома» и погасил её. Причина — окно между запуском ядра и
// его докладом «связь работает»: маршруты туннеля и подмена ответов на
// 198.18.0.x появляются на 3.7-й секунде (замер на стенде 09.09), а
// состояние «работает» ставится позже. Заход, попавший в это окно, шёл
// системным резолвером прямо в перехват собственного ядра и получал признак
// «дома» где угодно на свете.
//
// Со старым условием (только «работает») этот тест краснеет.
func TestPodnimayushchiysyaTunnelSchitaetsyaStoyashchimNaPutiZondov(t *testing.T) {
	sluchai := []struct {
		imya   string
		sost   yadro.Sostoyanie
		rezhim konfig.Rezhim
		hotim  bool
	}{
		{"поднимается в режиме туннеля", yadro.Podnimaem, konfig.Tunnel, true},
		{"работает в режиме туннеля", yadro.Rabotaet, konfig.Tunnel, true},
		{"стоит", yadro.Stoit, konfig.Tunnel, false},
		{"сломалось", yadro.Slomalos, konfig.Tunnel, false},
		// В половинном режиме ядро прописано системным прокси, а зонды ходят
		// мимо него (net.Resolver прокси не читает) — там они мерят
		// настоящую сеть и слепыми не становятся.
		{"поднимается в половинном режиме", yadro.Podnimaem, konfig.Proksi, false},
		{"работает в половинном режиме", yadro.Rabotaet, konfig.Proksi, false},
	}
	for _, s := range sluchai {
		t.Run(s.imya, func(t *testing.T) {
			if bylo := tunnelNaPutiZondov(s.sost, s.rezhim); bylo != s.hotim {
				t.Fatalf("состояние %q, режим %q: получил %v, хочу %v", s.sost, s.rezhim, bylo, s.hotim)
			}
		})
	}
}
