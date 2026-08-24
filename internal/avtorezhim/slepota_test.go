package avtorezhim

import (
	"context"
	"errors"
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
// дома, туннель поднят, адрес физического адаптера узнать НЕ вышло (нет
// SetevoyAdres — та же слепота, что была ДО SetevoyAdapter/DnsZond.AdresResolvera:
// системный резолвер был бы виден только нашему же туннелю). Защита обязана
// остаться поднятой, сколько заходов ни делай.
//
// Это НЕ единственный сценарий поднятого туннеля — см.
// TestZahodVTunneleSPrivatnymAdapteromNeSlep ниже: когда адрес адаптера
// известен и приватен, слепота снимается, и это тест на обратное — что
// именно неизвестность адреса, а не сам факт поднятого туннеля, держит
// ZondSlep.
func TestZahodVTunneleNeSnimaetZashchitu(t *testing.T) {
	trafik := &fakeTrafik{izmereno: true, proshel: true}
	zvali := false
	a := &Avtorezhim{
		Dns:           dnsSchitalka{zvali: &zvali},
		Trafik:        trafik,
		Zadvizhka:     NovayaZadvizhka(VneDoma),
		TunnelPodnyat: func() bool { return true },
		// Адрес адаптера неизвестен — ровно сценарий, который проверяет
		// этот тест. Явно, а не по умолчанию nil-поля: чтобы намерение не
		// потерялось при следующей правке структуры.
		SetevoyAdres: func() (string, string, error) {
			return "", "", errors.New("нет ни одного адаптера")
		},
	}
	var posledneye Nablyudeniye
	for i := 0; i < 5; i++ {
		n, izmenilos, tek := a.Zahod(context.Background(), true, false)
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
		n, _, tek = a.Zahod(context.Background(), true, false)
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

// TestZahodVTunneleSPrivatnymAdapteromNeSlep — лечение слепоты: туннель
// поднят, но адрес физического адаптера УДАЛОСЬ узнать и он приватный
// (192.168.1.192) — заход обязан быть зрячим: ZondSlep=false, DnsPryamoy
// позван с этим самым адресом (не системный резолвер), и наблюдение решает
// обстановку как обычно, вплоть до Doma после подтверждений.
func TestZahodVTunneleSPrivatnymAdapteromNeSlep(t *testing.T) {
	trafik := &fakeTrafik{izmereno: true, proshel: true}
	var polucheno struct{ adresResolvera, lokalnyAdres string }
	zvali := 0
	a := &Avtorezhim{
		Trafik:        trafik,
		Zadvizhka:     NovayaZadvizhka(VneDoma),
		TunnelPodnyat: func() bool { return true },
		SetevoyAdres: func() (string, string, error) {
			return "192.168.1.192:53", "192.168.1.77", nil
		},
		DnsPryamoy: func(adresResolvera, lokalnyAdres string) DnsProver {
			zvali++
			polucheno.adresResolvera, polucheno.lokalnyAdres = adresResolvera, lokalnyAdres
			return fakeDns{doma: true}
		},
	}

	var posledneye Nablyudeniye
	var tek Sostoyanie
	for i := 0; i < Podtverzhdeniy; i++ {
		posledneye, _, tek = a.Zahod(context.Background(), true, false)
		if posledneye.ZondSlep {
			t.Fatalf("заход %d: наблюдение помечено слепым, хотя адрес адаптера известен и приватен", i)
		}
	}
	if zvali == 0 {
		t.Fatal("DnsPryamoy ни разу не позван — заход не спрашивал прямой резолвер")
	}
	if polucheno.adresResolvera != "192.168.1.192:53" || polucheno.lokalnyAdres != "192.168.1.77" {
		t.Fatalf("DnsPryamoy получил (%q, %q), хочу (192.168.1.192:53, 192.168.1.77)", polucheno.adresResolvera, polucheno.lokalnyAdres)
	}
	if !posledneye.DnsPriznakDoma {
		t.Fatal("DNS-признак дома не выставлен, хотя fakeDns вернул doma=true")
	}
	if tek != Doma {
		t.Fatalf("после %d подтверждений «дома» через прямой резолвер обстановка = %v, хочу Doma", Podtverzhdeniy, tek)
	}
}

// TestZahodVTunneleSPublichnymAdresomOstayotsyaSlep — контроль на
// подмену: адрес адаптера узнать УДАЛОСЬ, но он не приватный (публичный
// DNS) — это подозрительно похоже на ту же беду, от которой лечим, поэтому
// заход остаётся слепым, как и при полностью неизвестном адресе.
func TestZahodVTunneleSPublichnymAdresomOstayotsyaSlep(t *testing.T) {
	dnsZvali := 0
	a := &Avtorezhim{
		Trafik:        &fakeTrafik{},
		Zadvizhka:     NovayaZadvizhka(VneDoma),
		TunnelPodnyat: func() bool { return true },
		SetevoyAdres: func() (string, string, error) {
			return "1.1.1.1:53", "203.0.113.5", nil
		},
		DnsPryamoy: func(adresResolvera, lokalnyAdres string) DnsProver {
			dnsZvali++
			return fakeDns{doma: true}
		},
	}
	n, _, _ := a.Zahod(context.Background(), true, false)
	if !n.ZondSlep {
		t.Fatal("публичный адрес резолвера принят как приватный — заход обязан остаться слепым")
	}
	if dnsZvali != 0 {
		t.Fatal("DnsPryamoy позван с публичным адресом резолвера — не должен был")
	}
}
