package main

import (
	"strings"
	"sync"
	"testing"
)

// Проверяется ровно то, ради чего метка заведена: она ПЕРЕЖИВАЕТ пузырь.
// Пузырь про версию показывается один раз и гаснет за секунды, поэтому
// после него значок обязан сам говорить, что ставить и как, — иначе
// заказанное «тыкаешь обновление и все» упирается в то, что тыкать нечего.

func TestBezObnovleniyaZnachokMolchit(t *testing.T) {
	zabytObnovlenie()
	if v := zhdushcheeObnovlenie(); v != "" {
		t.Fatalf("ждать нечего, а метка держит версию %q", v)
	}
	if got := podskazkaTreya(); got != podskazkaBezObnovleniya {
		t.Errorf("подсказка без обновления = %q, ждали %q", got, podskazkaBezObnovleniya)
	}
	if tekst, est := punktMenyuObnovleniya(); est {
		t.Errorf("ставить нечего, а в меню предлагается пункт %q", tekst)
	}
}

func TestNaydennoeObnovlenieVidnoIPosleTogoKakPuzyrPogas(t *testing.T) {
	zabytObnovlenie()
	zapomnitObnovlenie("0.6.27")

	// Пузырь уже уехал в «Центр уведомлений» — на экране от него ничего
	// не осталось. Значок обязан продолжать говорить сам.
	podskazka := podskazkaTreya()
	if !strings.Contains(podskazka, "0.6.27") {
		t.Errorf("подсказка значка = %q, в ней нет найденной версии", podskazka)
	}
	if podskazka == podskazkaBezObnovleniya {
		t.Errorf("подсказка не изменилась после находки: %q", podskazka)
	}
	// Подсказка обязана сказать, КУДА тыкать: пузыря с кнопкой уже нет.
	if !strings.Contains(podskazka, "Обновить") {
		t.Errorf("подсказка %q не называет пункт меню, которым ставят", podskazka)
	}

	tekst, est := punktMenyuObnovleniya()
	if !est {
		t.Fatal("обновление найдено, а пункта меню нет — тыкать нечего")
	}
	if !strings.Contains(tekst, "0.6.27") {
		t.Errorf("пункт меню = %q, версии в нём нет", tekst)
	}
}

func TestPosleUstanovkiMetkaSnimaetsya(t *testing.T) {
	zabytObnovlenie()
	zapomnitObnovlenie("0.6.27")
	zabytObnovlenie()
	if _, est := punktMenyuObnovleniya(); est {
		t.Error("обновление уже поставлено, а меню всё ещё зовёт его ставить")
	}
	if got := podskazkaTreya(); got != podskazkaBezObnovleniya {
		t.Errorf("подсказка после установки = %q, ждали %q", got, podskazkaBezObnovleniya)
	}
}

// Метку ставит фоновая горутина проверки обновления, а читают её поток
// сообщений трея (подсказка, меню) и обработчик тычка. Гонка тут — не
// теория: pokazatOblachkoObnovleniya зовётся из internal/sluzhba, а меню
// рисуется в цикле сообщений окна трея. Тест имеет смысл только под -race.
func TestMetkaPerezhivaetOdnovremennoeChtenieIZapis(t *testing.T) {
	zabytObnovlenie()
	var gruppa sync.WaitGroup
	for i := 0; i < 8; i++ {
		gruppa.Add(2)
		go func() { defer gruppa.Done(); zapomnitObnovlenie("0.6.27") }()
		go func() { defer gruppa.Done(); _ = podskazkaTreya(); _, _ = punktMenyuObnovleniya() }()
	}
	gruppa.Wait()
	zabytObnovlenie()
}
