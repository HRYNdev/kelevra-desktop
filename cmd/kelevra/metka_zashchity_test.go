package main

import (
	"strings"
	"sync"
	"testing"
)

// Подсказка значка обязана говорить правду о состоянии связи: до 31.08 она
// была константой «Kelevra: VPN включён» и висела и на опущенной защите.
// Неполного режима с 0.6.77 нет — значит и половинной подсказки нет.

// chistayaMetka возвращает обе метки (защиты и обновления) в исходное
// состояние и делает это ПОСЛЕ теста тоже: обе живут в пакетных переменных, и
// протёкшее состояние ломало бы соседние тесты в зависимости от порядка.
func chistayaMetka(t *testing.T) {
	t.Helper()
	zabytZashchitu()
	zabytObnovlenie()
	t.Cleanup(func() {
		zabytZashchitu()
		zabytObnovlenie()
	})
}

func TestPodskazkaGovoritPravduOSvyazi(t *testing.T) {
	chistayaMetka(t)

	// Служба только поднялась: про связь значку ещё ничего не сказали.
	if got := podskazkaTreya(); got != podskazkaBezObnovleniya {
		t.Fatalf("до подъёма связи подсказка = %q, ждали %q", got, podskazkaBezObnovleniya)
	}

	pometitZashchituBezZnachka(true)
	if got := podskazkaTreya(); got != podskazkaPolnayaZashchita {
		t.Fatalf("связь поднята: подсказка = %q, ждали %q", got, podskazkaPolnayaZashchita)
	}
	// Ни «защиты», ни неполноты в подсказках быть не должно ни в одном
	// состоянии.
	for _, p := range []string{podskazkaBezObnovleniya, podskazkaPolnayaZashchita} {
		nizhnie := strings.ToLower(p)
		for _, slovo := range []string{"защит", "не полностью", "частично", "только браузеры"} {
			if strings.Contains(nizhnie, slovo) {
				t.Errorf("подсказка значка %q содержит «%s»", p, slovo)
			}
		}
	}

	// Связь опустили — подсказка обязана вернуться к «отключено».
	pometitZashchituBezZnachka(false)
	if got := podskazkaTreya(); got != podskazkaBezObnovleniya {
		t.Fatalf("после опускания связи подсказка = %q, ждали %q", got, podskazkaBezObnovleniya)
	}
}

// Обновление ждёт тычка днями: находка версии не имеет права вытеснить из
// подсказки состояние связи — обе вещи обязаны уместиться в одну строку.
func TestObnovlenieNeVytesnyaetSostoyanieSvyazi(t *testing.T) {
	chistayaMetka(t)
	pometitZashchituBezZnachka(true)
	zapomnitObnovlenie("0.6.27")

	podskazka := podskazkaTreya()
	if !strings.Contains(podskazka, "подключено") {
		t.Fatalf("подсказка = %q — обновление вытеснило состояние связи", podskazka)
	}
	if !strings.Contains(podskazka, "0.6.27") || !strings.Contains(podskazka, "Обновить") {
		t.Fatalf("подсказка = %q — из неё пропало обновление или указание, чем его ставить", podskazka)
	}
	// szTip у NOTIFYICONDATAW — 128 UTF-16 слов вместе с нулём, то есть 127
	// знаков: длинное режется по хвосту, а в хвосте как раз обновление.
	if n := len([]rune(podskazka)); n > 127 {
		t.Fatalf("подсказка длиной %d знаков не влезет в szTip (127): %q", n, podskazka)
	}
}

// Метку защиты пишет горутина службы (PodnyatZashchitu/OpustitZashchitu), а
// читает поток сообщений окна трея. Гонка тут не теория; тест имеет смысл
// только под -race.
func TestMetkaZashchityPerezhivaetOdnovremennoeChtenieIZapis(t *testing.T) {
	chistayaMetka(t)
	var gruppa sync.WaitGroup
	for i := 0; i < 8; i++ {
		gruppa.Add(2)
		go func() { defer gruppa.Done(); pometitZashchituBezZnachka(true) }()
		go func() { defer gruppa.Done(); _ = podskazkaTreya() }()
	}
	gruppa.Wait()
}
