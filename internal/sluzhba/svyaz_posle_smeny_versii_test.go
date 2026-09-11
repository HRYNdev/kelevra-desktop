package sluzhba

import (
	"context"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// Связь, оборванная обновлением СО СТАРОЙ версии, возвращается сама.
//
// Зачем это отдельно от приёма живого ядра. Приём работает, когда уходящая
// копия оставила записку и не погасила ядро. Но обновление ведёт СТАРАЯ копия
// своим старым кодом, и пока у людей стоят версии до 0.6.71, она гасит ядро
// безусловно и записок не пишет. Разрыв на этом переходе сделать нечем — его
// делает код, уже стоящий на машине. А вот остаться БЕЗ связи после него
// нельзя: автоподключение у людей выключено, авторежим дома считает туннель
// ненужным, и человек молча оказывается без обхода.
//
// Замечено 11.09.2026 при разборе выпуска 0.6.70: код приёма был зелёным и
// проверенным на стенде, но на самом переходе к нему не исполнялся вовсе.

// Пустая запись — ТОЖЕ смена версии, и это главный случай, а не исключение.
//
// Поле появилось в 0.6.71, а обновляются люди со старых версий, которые его
// не знали: у них записано пусто. Замер на стенде 11.09.2026 — переход
// 0.6.69 → 0.6.71 связь не вернул именно потому, что пустота считалась
// «первым запуском» и обновление проходило мимо всей защиты.
//
// От чистой установки защищает не это условие, а отсутствие следа туннеля
// (см. TestBezSledaTunnelyaSvyazNePodnimaem).
func TestPustayaZapisEtoTozheSmenaVersii(t *testing.T) {
	s := sluzhbaDlyaProverki(t)
	s.Nastroyki.RabotalaVersiya = ""

	if !s.zapomnitSvoyuVersiyu() {
		t.Error("обновление со старой версии, не знавшей этого поля, не распознано как смена")
	}
	if s.Nastroyki.RabotalaVersiya != podpiska.Versiya {
		t.Errorf("версия не запомнилась: %q", s.Nastroyki.RabotalaVersiya)
	}
}

// Тот же запуск той же версии (перезапуск службы, UAC, ручной перезапуск) —
// не смена.
func TestTaZheVersiyaNeSmena(t *testing.T) {
	s := sluzhbaDlyaProverki(t)
	s.Nastroyki.RabotalaVersiya = podpiska.Versiya

	if s.zapomnitSvoyuVersiyu() {
		t.Error("перезапуск той же версии принят за смену")
	}
}

// А вот другая версия — смена, и отметка обновляется на диске.
func TestDrugayaVersiyaEtoSmena(t *testing.T) {
	s := sluzhbaDlyaProverki(t)
	s.Nastroyki.RabotalaVersiya = "0.6.69"

	if !s.zapomnitSvoyuVersiyu() {
		t.Fatal("смена версии не распознана")
	}
	if s.Nastroyki.RabotalaVersiya != podpiska.Versiya {
		t.Errorf("отметка не обновилась: %q", s.Nastroyki.RabotalaVersiya)
	}
	// Отметка обязана пережить перезапуск — иначе каждый старт выглядел бы
	// сменой версии и дёргал туннель.
	svezhie, err := hranenie.Zagruzit()
	if err != nil {
		t.Fatalf("настройки не перечитались: %v", err)
	}
	if svezhie.RabotalaVersiya != podpiska.Versiya {
		t.Errorf("на диске осталось %q", svezhie.RabotalaVersiya)
	}
}

// Нет следа туннеля — связь не поднимаем. Человек мог отключиться сам:
// штатное «Отключить» след снимает, и трогать его выбор нельзя.
func TestBezSledaTunnelyaSvyazNePodnimaem(t *testing.T) {
	s := stendSPravami(t)
	podyomov := 0
	s.zapustitYadro = func(ctx context.Context) error {
		podyomov++
		return nil
	}

	s.podnyatSvyazPosleSmenyVersii(context.Background(), "")

	if podyomov != 0 {
		t.Errorf("подняли связь %d раз, хотя человек отключился сам", podyomov)
	}
}

// А со следом — поднимаем: прошлая копия держала туннель и не просила его
// опускать, значит обновление оборвало связь против воли человека.
func TestSoSledomSvyazPodnimaetsya(t *testing.T) {
	s := stendSPravami(t)
	podyomov := 0
	s.zapustitYadro = func(ctx context.Context) error {
		podyomov++
		return nil
	}

	// Имя адаптера приходит параметром: сам след к этому моменту уже снят
	// уборкой в начале main.
	s.podnyatSvyazPosleSmenyVersii(context.Background(), "tun125")

	if podyomov == 0 {
		t.Error("связь не поднята, человек остался без обхода после обновления")
	}
}
