package main

import "sync"

// Метка «что именно сейчас защищено» на значке трея.
//
// Беда, которую эта метка закрывает (31.08, дословно: «впн не выполняет свою
// основную функцию»). Приложение умеет два разных режима, и разница между
// ними — пропасть:
//
//   - туннель: через Kelevra идёт ВЕСЬ трафик машины, любой программы,
//     любым протоколом;
//   - системный прокси: только те программы, которые системный прокси
//     уважают, и только TCP. Весь UDP — а значит и QUIC, а значит и YouTube,
//     который по нему ходит, — уходит к провайдеру мимо Kelevra.
//
// Подсказка значка при этом была КОНСТАНТОЙ «Kelevra: VPN включён» и звучала
// одинаково в обоих. Копия висит в трее неделями; наведённая мышь — то
// единственное, что человек видит, не открывая окна. Значит она обязана
// различать полную защиту и половинную, иначе половинная выглядит полной.
//
// Файл нарочно без build-тега (тем же принципом, что metka_obnovleniya.go
// рядом): состояние и тексты собираются и проверяются на сервере без
// Windows, там же живёт их тест. Трей на Windows берёт отсюда готовую
// строку, а сам занят только WinAPI.

var (
	zashchitaZamok sync.Mutex
	// zashchitaPodnyata — защита прямо сейчас поднята. Ложь — и ядро стоит,
	// и говорить про объём защиты нечего: объёма нет.
	zashchitaPodnyata bool
	// zashchitaChastichnaya — поднятая защита ПОЛОВИННАЯ
	// (konfig.Kartina.Chastichnaya). Отдельным полем от «поднята», а не
	// третьим значением одного: «нет защиты» и «половина защиты» — разные
	// ответы человеку, и склеивать их нельзя.
	zashchitaChastichnaya bool
	// zashchitaPochemu — почему половинная, словами человека. В сам szTip не
	// попадает (там 127 знаков вместе с нулём, а причина длиннее любого
	// разумного остатка) — идёт в журнал и в окно; здесь лежит, чтобы
	// значок и окно брали причину из одного источника.
	zashchitaPochemu string
)

// podskazkaBezObnovleniya — то, что человек видит, наведя мышь на значок,
// когда ставить нечего И про защиту значку ещё ничего не сказали (служба
// только поднялась, ядро не запускалось) либо защита опущена.
//
// Раньше здесь стояло «Kelevra: VPN включён» — строка, которая была неправдой
// в трёх состояниях из четырёх: до подъёма защиты, после её опускания и в
// прокси-режиме. Имя константы историческое (по ней бьётся
// metka_obnovleniya_test.go): она отвечает на «подсказка, когда обновления
// нет», а объём защиты добавляет к ней podskazkaZashchity ниже.
const podskazkaBezObnovleniya = "Kelevra: отключено"

// Тексты объёма защиты — их читает не программист. Поэтому ни «туннеля», ни
// «прокси-режима», ни «QUIC»: человеку важно, что игры и видео идут мимо, а
// не как называется протокол, которым они ходят. Те же слова, что в окне
// (internal/konfig: ZametkaVes, ZametkaBezPrav) — значок и окно обязаны
// говорить одно и то же, иначе человек решит, что видит разные приложения.
//
// САМОГО СЛОВА «защита» в этих строках больше нет (31.08). Про него
// спрашивали трижды и каждый раз одно и то же: непонятно, почему это вообще
// называется защитой (22.08, повторено 23.08).
// На телефоне, который держат за эталон, этого слова нет НИГДЕ на экране
// (проверено по kelevra-box: «защита» встречается только в комментариях кода):
// там говорят «Подключено»/«Отключено» и «Сеть». Значок теперь говорит так же
// и теми же словами, что круг в окне (index.html, podpisi: «отключено»,
// «подключено») и заметка режима (konfig.ZametkaVes: «Любая программа идёт
// через Kelevra»). Имена констант остались прежними — по ним бьются тесты, и
// переименование ничего человеку не добавит.
const (
	// podskazkaPolnayaZashchita — режим туннеля.
	podskazkaPolnayaZashchita = "Kelevra: работает — любая программа идёт через Kelevra"
	// podskazkaChastichnayaZashchita — режим системного прокси. Слово
	// «частично» стоит до тире: подсказка обрезается по ширине всплывающего
	// окошка, и главное не должно уехать в обрезанный хвост.
	podskazkaChastichnayaZashchita = "Kelevra: работает частично — только браузеры, игры и видео мимо"
)

// pometitZashchitu — хук sluzhba.MetkaZashchity: защиту подняли или опустили.
// Зовётся на КАЖДОМ подъёме и опускании, а не только при смене режима: значок
// не имеет права пережить опущенную защиту со словами про поднятую (ровно так
// «Kelevra: VPN включён» и висел после «Отключить»).
//
// obnovitPodskazkuTreya живёт в trey_windows.go и trey_other.go — тут про
// платформу знать нечего, тем же принципом, что и у pometitObnovlenie рядом.
func pometitZashchitu(podnyata, chastichnaya bool, pochemu string) {
	zashchitaZamok.Lock()
	zashchitaPodnyata = podnyata
	zashchitaChastichnaya = chastichnaya
	zashchitaPochemu = pochemu
	zashchitaZamok.Unlock()
	obnovitPodskazkuTreya()
}

// zabytZashchitu возвращает метку в исходное «про защиту ничего не известно».
// Нужна тестам, чтобы состояние не протекало между ними; в бою её роль играет
// pometitZashchitu(false, ...) на опускании защиты.
func zabytZashchitu() { pometitZashchituBezZnachka(false, false, "") }

// pometitZashchituBezZnachka — то же самое, но не трогая значок: перерисовка
// значка в тесте не нужна и на не-Windows всё равно только пишет в журнал.
func pometitZashchituBezZnachka(podnyata, chastichnaya bool, pochemu string) {
	zashchitaZamok.Lock()
	defer zashchitaZamok.Unlock()
	zashchitaPodnyata = podnyata
	zashchitaChastichnaya = chastichnaya
	zashchitaPochemu = pochemu
}

// podskazkaZashchity — объём защиты одной строкой для szTip.
func podskazkaZashchity() string {
	zashchitaZamok.Lock()
	defer zashchitaZamok.Unlock()
	if !zashchitaPodnyata {
		return podskazkaBezObnovleniya
	}
	if zashchitaChastichnaya {
		return podskazkaChastichnayaZashchita
	}
	return podskazkaPolnayaZashchita
}

// PrichinaChastichnoyZashchity — почему защита половинная, словами человека.
// Отдельно от подсказки: в szTip она не влезает, а в журнал (и оттуда в
// разбор беды) обязана попасть.
func prichinaChastichnoyZashchity() string {
	zashchitaZamok.Lock()
	defer zashchitaZamok.Unlock()
	if !zashchitaPodnyata || !zashchitaChastichnaya {
		return ""
	}
	return zashchitaPochemu
}
