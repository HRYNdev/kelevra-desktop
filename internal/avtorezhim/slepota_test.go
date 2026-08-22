package avtorezhim

import (
	"context"
	"testing"
)

// Здесь проверяется одна беда, найденная чтением боевого профиля, а не
// догадкой: пока НАШ туннель поднят, зонды авторежима мерят его, а не
// физическую сеть. Диапазон подменных адресов у ядра в профиле —
// 198.18.0.0/15, ровно тот, по которому зонд опознаёт домашний роутер
// (fakeIPPervyy..fakeIPPosledniy), а youtube.com и discord.com — два из трёх
// контрольных доменов при Nuzhno=2 — ядро в этот fakeip и заворачивает.
// Значит вне дома зонды дают «дома», и авторежим сам опускает защиту.

// TestReshitZondSlepSilneeLyubogoPriznaka: слепой заход — Neizvestno, даже
// когда оба зонда в один голос кричат «дома». Без флага тот же набор фактов
// даёт Doma, то есть команду ОПУСТИТЬ защиту.
func TestReshitZondSlepSilneeLyubogoPriznaka(t *testing.T) {
	da := true
	n := Nablyudeniye{EstSet: true, ZondSlep: true, DnsPriznakDoma: true, TrafikPryamoy: &da}
	if got := Reshit(n); got != Neizvestno {
		t.Fatalf("слепой заход дал %v, а обязан дать Neizvestno: зонды мерили наш же туннель", got)
	}
	// Контроль: те же факты без слепоты — по-прежнему Doma. Иначе флаг
	// «чинил» бы всё подряд, а не одну ветку.
	n.ZondSlep = false
	if got := Reshit(n); got != Doma {
		t.Fatalf("зрячий заход с обоими признаками дал %v, ожидался Doma", got)
	}
}

// TestZahodVTunneleNeSnimaetZashchitu — боевой сценарий целиком: человек ВНЕ
// дома, туннель поднят, зонды из-за этого врут «дома». Защита обязана
// остаться поднятой, сколько заходов ни делай.
func TestZahodVTunneleNeSnimaetZashchitu(t *testing.T) {
	trafik := &fakeTrafik{izmereno: true, proshel: true}
	zvali := false
	a := &Avtorezhim{
		Dns:           dnsSchitalka{zvali: &zvali},
		Trafik:        trafik,
		Zadvizhka:     NovayaZadvizhka(VneDoma),
		TunnelPodnyat: func() bool { return true },
	}
	var posledneye Nablyudeniye
	for i := 0; i < 5; i++ {
		n, izmenilos, tek := a.Zahod(context.Background(), true)
		// Сперва беда, потом метка: красный обязан говорить «защита снята
		// вне дома», а не «поле не проставлено».
		if izmenilos {
			t.Fatalf("заход %d: обстановка «сменилась» на показаниях туннеля — это и есть команда опустить защиту", i)
		}
		if tek != VneDoma {
			t.Fatalf("заход %d: состояние уехало в %v, защита снята вне дома", i, tek)
		}
		posledneye = n
	}
	if !posledneye.ZondSlep {
		t.Fatal("наблюдение не помечено слепым")
	}
	// И платить за заведомо негодный ответ двумя сетевыми запросами незачем.
	if zvali {
		t.Fatal("DNS-зонд позван при поднятом туннеле — его ответ всё равно негоден")
	}
	if trafik.zvonkov != 0 {
		t.Fatalf("зонд трафика позван %d раз при поднятом туннеле", trafik.zvonkov)
	}
}

// TestZahodBezTunnelyaZondyRabotayut — обратная сторона: когда туннеля нет
// (в том числе прокси-режим, где зонды системный прокси не читают), заход
// работает как прежде. Иначе правка тихо убила бы весь авторежим.
func TestZahodBezTunnelyaZondyRabotayut(t *testing.T) {
	trafik := &fakeTrafik{izmereno: true, proshel: true}
	zvali := false
	a := &Avtorezhim{
		Dns:           dnsSchitalka{zvali: &zvali},
		Trafik:        trafik,
		Zadvizhka:     NovayaZadvizhka(Neizvestno),
		TunnelPodnyat: func() bool { return false },
	}
	var tek Sostoyanie
	for i := 0; i < 3; i++ {
		var n Nablyudeniye
		n, _, tek = a.Zahod(context.Background(), true)
		if n.ZondSlep {
			t.Fatalf("заход %d помечен слепым без туннеля", i)
		}
	}
	if !zvali || trafik.zvonkov == 0 {
		t.Fatal("без туннеля зонды обязаны спрашиваться")
	}
	if tek != Doma {
		t.Fatalf("без туннеля три подтверждения «дома» дали %v", tek)
	}
}
