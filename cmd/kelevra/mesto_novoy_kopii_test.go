package main

import (
	"errors"
	"testing"
)

// TestRuchnoyZapuskNeTyanetSvoyuSluzhbuRyadomSoSluzhboyWindows — замер с ПК
// 17.10: после уступки места служба Windows поднимается диспетчером сама, а
// окно подняло своё служебное, и человек увидел окно ошибки.
func TestRuchnoyZapuskNeTyanetSvoyuSluzhbuRyadomSoSluzhboyWindows(t *testing.T) {
	p := vybratPodyomYadra(true, nil, true)
	if !p.zhdatSluzhbuWindows {
		t.Fatal("после уступки при установленной службе Windows окно собирается поднимать своё служебное")
	}
	if p.zapuskat {
		t.Fatal("после уступки окно само дёргает запуск службы, хотя её поднимает диспетчер")
	}
	if p.srok != srokOzhidaniyaZameny {
		t.Fatalf("после уступки ждём службу %s, а сторож окна ждёт замену %s", p.srok, srokOzhidaniyaZameny)
	}
}

func TestObychnyyZapuskSoSluzhboyWindows(t *testing.T) {
	p := vybratPodyomYadra(true, nil, false)
	if !p.zhdatSluzhbuWindows || !p.zapuskat || p.srok != srokPodnyatiyaSluzhby {
		t.Fatalf("обычный запуск при службе Windows: %+v", p)
	}
}

func TestBezSluzhbyWindowsPodnimaemSvoyProcess(t *testing.T) {
	for _, posle := range []bool{false, true} {
		if p := vybratPodyomYadra(false, nil, posle); p.zhdatSluzhbuWindows {
			t.Fatalf("службы Windows нет (после уступки=%v), а окно ждёт её: %+v", posle, p)
		}
	}
	// Диспетчер служб не ответил — прежний путь, своим процессом.
	if p := vybratPodyomYadra(false, errors.New("нет ответа"), true); p.zhdatSluzhbuWindows {
		t.Fatalf("при ошибке диспетчера окно ждёт службу Windows: %+v", p)
	}
}
