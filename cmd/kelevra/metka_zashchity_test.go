package main

import (
	"strings"
	"sync"
	"testing"
)

// Подсказка значка обязана различать полную защиту и половинную.
//
// До 31.08 она была константой «Kelevra: VPN включён» и звучала одинаково: и
// когда через Kelevra шёл весь трафик машины, и когда шли только браузеры и
// только TCP (весь UDP, а с ним QUIC и YouTube, — мимо), и когда защиты не
// было вовсе. Копия висит в трее неделями, окно человек открывает редко —
// эта строка и есть то, чем приложение отчитывается о себе.

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

func TestPodskazkaRazlichaetObyomZashchity(t *testing.T) {
	chistayaMetka(t)

	// Служба только поднялась: про защиту значку ещё ничего не сказали.
	if got := podskazkaTreya(); got != podskazkaBezObnovleniya {
		t.Fatalf("до подъёма защиты подсказка = %q, ждали %q", got, podskazkaBezObnovleniya)
	}

	pometitZashchituBezZnachka(true, false, "")
	polnaya := podskazkaTreya()
	if polnaya != podskazkaPolnayaZashchita {
		t.Fatalf("полная защита: подсказка = %q, ждали %q", polnaya, podskazkaPolnayaZashchita)
	}

	pometitZashchituBezZnachka(true, true, "Windows не дал Kelevra прав администратора")
	polovinnaya := podskazkaTreya()
	if polovinnaya == polnaya {
		t.Fatalf("половинная защита говорит значку то же самое, что полная: %q — "+
			"ровно эта неразличимость и есть беда 31.08", polovinnaya)
	}
	// Корень «частичн», а не целое слово: важно, что подсказка НАЗЫВАЕТ
	// половинчатость, а не в какой она падеже. Форма слова менялась вместе с
	// уходом слова «защита» из подсказок (31.08), и тест не должен ломаться
	// от такой правки — он про смысл, а не про букву.
	if !strings.Contains(polovinnaya, "частичн") {
		t.Fatalf("подсказка половинной защиты = %q — в ней не сказано, что защита частичная", polovinnaya)
	}
	// Слова «защита» в подсказке быть не должно ни в одном состоянии — ровно
	// то, про что хозяин спрашивал 22.08 и 23.08.
	for _, p := range []string{podskazkaBezObnovleniya, podskazkaPolnayaZashchita, podskazkaChastichnayaZashchita} {
		if strings.Contains(strings.ToLower(p), "защит") {
			t.Errorf("подсказка значка снова говорит про «защиту»: %q", p)
		}
	}

	// Защиту опустили — подсказка обязана вернуться к «защиты нет», а не
	// пересказывать вчерашний режим.
	pometitZashchituBezZnachka(false, true, "неважно")
	if got := podskazkaTreya(); got != podskazkaBezObnovleniya {
		t.Fatalf("после опускания защиты подсказка = %q, ждали %q", got, podskazkaBezObnovleniya)
	}
}

// Обновление ждёт тычка днями. Раньше находка версии подменяла подсказку
// ЦЕЛИКОМ — и всё это время значок молчал о том, половинная у человека защита
// или полная. Ставить обновление важно, но не важнее, чем знать, что YouTube
// идёт мимо VPN: обе вещи обязаны уместиться в одну строку.
func TestObnovlenieNeVytesnyaetObyomZashchity(t *testing.T) {
	chistayaMetka(t)
	pometitZashchituBezZnachka(true, true, "")
	zapomnitObnovlenie("0.6.27")

	podskazka := podskazkaTreya()
	// Корень «частичн» — см. тот же довод в TestPodskazkaRazlichaetObyomZashchity.
	if !strings.Contains(podskazka, "частичн") {
		t.Fatalf("подсказка = %q — обновление вытеснило объём защиты", podskazka)
	}
	if !strings.Contains(podskazka, "0.6.27") || !strings.Contains(podskazka, "Обновить") {
		t.Fatalf("подсказка = %q — из неё пропало обновление или указание, чем его ставить", podskazka)
	}
	// szTip у NOTIFYICONDATAW — 128 UTF-16 слов вместе с нулём, то есть 127
	// знаков. Длинное kopirovatStrokuUTF16 режет само, но резать оно будет
	// ХВОСТ — а в хвосте у нас как раз обновление. Сторож на глаза: если
	// строка перестанет влезать, это надо увидеть тут, а не на машине человека.
	if n := len([]rune(podskazka)); n > 127 {
		t.Fatalf("подсказка длиной %d знаков не влезет в szTip (127): %q", n, podskazka)
	}
}

// Причина половинчатости в szTip не влезает и туда не идёт, но потеряться
// не должна: она уходит в журнал, а журнал — единственный способ узнать
// правду с машины человека.
func TestPrichinaPolovinnoyZashchityDostupnaOtdelno(t *testing.T) {
	chistayaMetka(t)
	const prichina = "Windows не дал Kelevra прав администратора: без них через неё проходят только браузеры."

	pometitZashchituBezZnachka(true, true, prichina)
	if got := prichinaChastichnoyZashchity(); got != prichina {
		t.Fatalf("причина половинчатости = %q, ждали %q", got, prichina)
	}
	// Полная защита причины не имеет — выдумывать её нечего.
	pometitZashchituBezZnachka(true, false, prichina)
	if got := prichinaChastichnoyZashchity(); got != "" {
		t.Fatalf("защита полная, а причина половинчатости = %q", got)
	}
	// Опущенная защита — тем более.
	pometitZashchituBezZnachka(false, true, prichina)
	if got := prichinaChastichnoyZashchity(); got != "" {
		t.Fatalf("защита опущена, а причина половинчатости = %q", got)
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
		go func() { defer gruppa.Done(); pometitZashchituBezZnachka(true, true, "причина") }()
		go func() { defer gruppa.Done(); _ = podskazkaTreya(); _ = prichinaChastichnoyZashchity() }()
	}
	gruppa.Wait()
}
