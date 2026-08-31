package sluzhba

import (
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/tunnel"
)

// След туннеля: кто его ставит, кто снимает и что остаётся после аварии.
//
// Это тот же класс беды, что случился 31.08 с системным прокси, только в
// более тяжёлом виде: там за мёртвым процессом осталась запись в реестре и у
// человека перестали открываться сайты. Туннель трогает не реестр, а
// маршрутизацию всей машины — если после падения от него что-то останется,
// цена будет выше. Поэтому след ведётся на диске: следующий запуск читает его
// и ПРОВЕРЯЕТ приборно (cmd/kelevra: snyatOsirotevshiySledTunnelya).

// TestPodyomTunnelyaOstavlyaetSled — подъём туннеля обязан оставить след с
// именем адаптера. Без имени следующий запуск не сможет спросить систему, жив
// ли ещё адаптер, и авария пройдёт незамеченной.
func TestPodyomTunnelyaOstavlyaetSled(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())

	zakrepitTunnel("tun125")

	adapter, pid, est := tunnel.ProchestMetku()
	if !est {
		t.Fatal("туннель подняли, а следа на диске нет — после жёсткой смерти процесса проверять будет нечего")
	}
	if adapter != "tun125" {
		t.Fatalf("в следе имя адаптера %q, ждали tun125", adapter)
	}
	if pid == 0 {
		t.Fatal("в следе нет pid поднявшей копии")
	}
}

// TestOpuskanieZashchitySnimaetSled — штатное опускание защиты снимает след.
// Ровно по его отсутствию следующий запуск отличает «ушли аккуратно» от
// «умерли жёстко»: оставленный след заставил бы каждый обычный запуск искать
// давно исчезнувший адаптер и тревожить человека на пустом месте.
func TestOpuskanieZashchitySnimaetSled(t *testing.T) {
	s := stend(t)
	zakrepitTunnel("tun125")
	if _, _, est := tunnel.ProchestMetku(); !est {
		t.Fatal("след не записался — проверять нечего")
	}

	_ = s.OpustitZashchitu()

	if _, _, est := tunnel.ProchestMetku(); est {
		t.Fatal("защиту опустили штатно, а след туннеля остался — следующий запуск сочтёт выход аварийным")
	}
}

// TestZnachokUznaetProPolovinnuyuZashchitu — подсказка значка в трее до 31.08
// была константой «Kelevra: VPN включён» и звучала одинаково в обоих режимах.
// Копия висит в трее неделями, и наведённая мышь — единственное, что человек
// видит, не открывая окна. Хук обязан получать РАЗНОЕ в разных режимах и
// «защиты нет» при её опускании.
func TestZnachokUznaetProPolovinnuyuZashchitu(t *testing.T) {
	s := stend(t)
	type skazano struct {
		podnyata, chastichnaya bool
		pochemu                string
	}
	var slyshal []skazano
	s.MetkaZashchity = func(podnyata, chastichnaya bool, pochemu string) {
		slyshal = append(slyshal, skazano{podnyata, chastichnaya, pochemu})
	}

	// Половинная защита поднята.
	s.zamok.Lock()
	s.kartina = konfig.Kartina{
		Rezhim:              konfig.Proksi,
		Chastichnaya:        true,
		PochemuChastichnaya: konfig.PrichinaBezPrav,
	}
	s.zamok.Unlock()
	s.soobshchitTreyuProZashchitu(true)

	// Полная защита поднята.
	s.zamok.Lock()
	s.kartina = konfig.Kartina{Rezhim: konfig.Tunnel}
	s.zamok.Unlock()
	s.soobshchitTreyuProZashchitu(true)

	// Защиту опустили — картина на месте (её никто не чистит), но значок
	// обязан сказать «защиты нет», а не пересказать вчерашний режим.
	s.zamok.Lock()
	s.kartina = konfig.Kartina{
		Rezhim:              konfig.Proksi,
		Chastichnaya:        true,
		PochemuChastichnaya: konfig.PrichinaBezPrav,
	}
	s.zamok.Unlock()
	s.soobshchitTreyuProZashchitu(false)

	zhdali := []skazano{
		{true, true, konfig.PrichinaBezPrav},
		{true, false, ""},
		{false, false, ""},
	}
	if len(slyshal) != len(zhdali) {
		t.Fatalf("значку сказали %d раз, ждали %d: %+v", len(slyshal), len(zhdali), slyshal)
	}
	for i := range zhdali {
		if slyshal[i] != zhdali[i] {
			t.Fatalf("значку сказали %+v, ждали %+v (шаг %d)", slyshal[i], zhdali[i], i+1)
		}
	}
}

// Хук не подключён (так собран стенд и так собирается любая проверка внутри
// пакета) — служба не имеет права из-за этого падать: значок не защита.
func TestBezHukaZnachkaSluzhbaNePadaet(t *testing.T) {
	s := stend(t)
	s.MetkaZashchity = nil
	s.soobshchitTreyuProZashchitu(true)
	s.soobshchitTreyuProZashchitu(false)
}
