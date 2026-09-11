package sluzhba

import (
	"context"
	"os"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Тот же инвариант, что и в svyaz_posle_smeny_versii_test.go («после смены
// версии, если прошлая копия держала туннель, связь обязана подняться сама»),
// но на СОСЕДНЕЙ развилке PrinyatZhivoeYadro — где записка НАЙДЕНА, но
// Yadro.Prinyat её не опознал (default-ветка, priyom_yadra.go:70-82).
//
// Записка остаётся нечитаемой ровно тогда, когда сама смена версии и рвёт
// связь: старое ядро умерло не штатно или его служебный порт ещё/уже не
// отвечает, а файл записки на диске остался (Ostanovit его снял бы, но этот
// путь как раз про нештатную смерть). Тот же класс беды, что чинили в #119,
// только вместо «записки нет вовсе» тут «записка есть, но не опознана».
//
// #119 научил связь подниматься заново ТОЛЬКО в ветке !est (priyom_yadra.go,
// строка 49: `if smenaVersii { s.podnyatSvyazPosleSmenyVersii(...) }`). Ветка
// default ничего подобного не зовёт — она молча UbratPeredachu и, если бы
// ядро ответило, ещё и гасит его (PogasitChuzhoe), но связь не восстанавливает.
//
// Тест использует непринятую записку самым дешёвым способом: служебный порт,
// на который она ссылается, никто не слушает (заведомо недоступный адрес) —
// Yadro.Prinyat отвечает Pochemu="служебный порт ядра молчит" и не опознаёт
// её — ровно default-ветка. Настоящий процесс не трогаем и не губим: PID —
// os.Getpid() тестового процесса, а раз порт не отвечает, PogasitChuzhoe в
// default-ветке и не позовётся (см. её код: `if _, zhivo := ZhivoPoZapiske`).
func TestNeOpoznannayaZapiskaPosleSmenyVersiiSvyazNePodnimaetsya(t *testing.T) {
	s := stendSPravami(t)
	podyomov := 0
	s.zapustitYadro = func(ctx context.Context) error {
		podyomov++
		return nil
	}

	// Смена версии: прошлый раз стояла другая версия.
	s.Nastroyki.RabotalaVersiya = "0.6.69"

	// Записка есть, но служебный порт молчит (заведомо недоступный адрес) —
	// Yadro.Prinyat её не опознает и уйдёт в default-ветку.
	yadro.ZapisatPeredachu(hranenie.PapkaYadra(), yadro.Peredacha{
		PID: os.Getpid(),
		Bin: s.Yadro.Bin,
		Api: "127.0.0.1:1",
	})

	// След туннеля есть: прошлая копия держала туннель на tun125.
	s.PrinyatZhivoeYadro(context.Background(), "tun125")

	if podyomov == 0 {
		t.Error("связь не поднята: записка не опознана (default-ветка) после смены версии, " +
			"человек остался без обхода — тот же класс беды, что #119 чинил только для случая «записки нет вовсе»")
	}
}
