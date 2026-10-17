package main

import "sync"

// Метка «поднята ли связь» на значке трея.
//
// Подсказка значка до 31.08 была КОНСТАНТОЙ «Kelevra: VPN включён» и висела
// и на опущенной защите. Копия висит в трее неделями; наведённая мышь — то
// единственное, что человек видит, не открывая окна. Значит она обязана
// говорить правду о состоянии.
//
// До 0.6.77 здесь же жила «половинная» подсказка (прокси-режим, «идут только
// браузеры»). Неполного режима больше нет (решение владельца 17.10) — и
// половинной подсказки тоже.
//
// Файл нарочно без build-тега (тем же принципом, что metka_obnovleniya.go
// рядом): состояние и тексты собираются и проверяются на сервере без
// Windows, там же живёт их тест. Трей на Windows берёт отсюда готовую
// строку, а сам занят только WinAPI.

var (
	zashchitaZamok sync.Mutex
	// zashchitaPodnyata — связь прямо сейчас поднята.
	zashchitaPodnyata bool
)

// podskazkaBezObnovleniya — то, что человек видит, наведя мышь на значок,
// когда ставить нечего и связь не поднята (служба только поднялась, ядро не
// запускалось, защита опущена). Имя константы историческое (по ней бьётся
// metka_obnovleniya_test.go).
const podskazkaBezObnovleniya = "Kelevra: отключено"

// podskazkaPolnayaZashchita — связь поднята. Слова те же, что у круга в окне
// (index.html, podpisi: «Подключено») и заметки (konfig.ZametkaVes). Слова
// «защита» нет нарочно (31.08: непонятно, почему это называется защитой).
const podskazkaPolnayaZashchita = "Kelevra: подключено, весь трафик идёт через Kelevra"

// pometitZashchitu — хук sluzhba.MetkaZashchity: связь подняли или опустили.
// Зовётся на КАЖДОМ подъёме и опускании: значок не имеет права пережить
// опущенную связь со словами про поднятую.
//
// obnovitPodskazkuTreya живёт в trey_windows.go и trey_other.go — тут про
// платформу знать нечего, тем же принципом, что и у pometitObnovlenie рядом.
func pometitZashchitu(podnyata bool) {
	zashchitaZamok.Lock()
	zashchitaPodnyata = podnyata
	zashchitaZamok.Unlock()
	obnovitPodskazkuTreya()
}

// zabytZashchitu возвращает метку в исходное «связь не поднята». Нужна
// тестам, чтобы состояние не протекало между ними.
func zabytZashchitu() { pometitZashchituBezZnachka(false) }

// pometitZashchituBezZnachka — то же самое, но не трогая значок: перерисовка
// значка в тесте не нужна и на не-Windows всё равно только пишет в журнал.
func pometitZashchituBezZnachka(podnyata bool) {
	zashchitaZamok.Lock()
	defer zashchitaZamok.Unlock()
	zashchitaPodnyata = podnyata
}

// podskazkaZashchity — состояние связи одной строкой для szTip.
func podskazkaZashchity() string {
	zashchitaZamok.Lock()
	defer zashchitaZamok.Unlock()
	if !zashchitaPodnyata {
		return podskazkaBezObnovleniya
	}
	return podskazkaPolnayaZashchita
}
