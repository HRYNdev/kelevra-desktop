package main

import (
	"log"
	"time"
)

// Окно пропадало после обновления службы (стенд VM 220, 21.10).
//
// Как это шло. Служба обновилась и вернулась новее окна — окно закрывается и
// запускает себя заново (perezapusk_okna.go, okno_windows.go:
// zapustitOknoZanovo). Новая копия находит живую службу, считает её чужой
// копией и сперва ищет уже открытое окно (podnyatChuzheeOkno), чтобы не
// плодить второе. И находит — СТАРОЕ: go-webview2 в Destroy только кладёт
// WM_CLOSE в очередь окна, а очередь к этому моменту уже никто не читает
// (w.Run вернулся), так что окно висит на экране до самого выхода процесса.
// Новая копия поднимала этот труп и уходила, следом старый процесс
// завершался, и окно исчезало вместе с ним. Человек оставался без окна.
//
// Чинится с двух сторон, потому что старое окно — это уже выпущенная версия:
//   - старое окно теперь рушит себя само (DestroyWindow в своём потоке) ДО
//     запуска замены — FindWindowW его больше не находит;
//   - новая копия не верит найденному окну на слово: окно, которое не
//     отвечает на пустое сообщение, — уходящее (поток окна больше не крутит
//     очередь). Его не поднимают, а ждут, пока исчезнет, и открывают своё.
//   - и даже живое на вид окно новая копия, подняв, не бросает: следит за
//     ним srokSlezhki и, если оно исчезло, открывает своё. Стенд 22.10
//     (0.6.85 → сборная, 1 прогон из 3): старое окно 0.6.85 уже после w.Run
//     ещё отвечало на WM_NULL (поток при выходе процесса крутит присланные
//     сообщения), новая копия поднимала его и уходила, окно умирало следом.
//     Отличить «уходит» от «живёт» по ответу нельзя — надёжен только факт:
//     окно осталось или нет. Это и лечит замену окон уже выпущенных версий.

// srokUhodaUmirayushchegoOkna — сколько ждём, что найденное, но немое окно
// исчезнет. Старый процесс после запуска замены только выходит; срок щедрый
// с запасом на разбор WebView2 при выходе.
const srokUhodaUmirayushchegoOkna = 8 * time.Second

// shagUhodaUmirayushchegoOkna — как часто переспрашиваем, ушло ли окно.
const shagUhodaUmirayushchegoOkna = 100 * time.Millisecond

// srokSlezhkiZaPodnyatym — сколько новая копия следит за поднятым чужим окном,
// прежде чем уйти. Старый процесс после запуска замены только выходит (на
// стенде — секунды); 15 с — с запасом на медленную машину и разбор WebView2.
// Обычный щелчок по значку при открытом окне ничего не теряет: копия без
// окна просто живёт эти секунды и уходит.
const srokSlezhkiZaPodnyatym = 15 * time.Second

// oknoSistemy — то, что podnyatOknoEsliZhivo делает с окнами системы;
// подменяется в тестах.
type oknoSistemy struct {
	// nayti — окно Kelevra на экране (0, false — нет).
	nayti func() (uintptr, bool)
	// otvechaet — поток окна крутит очередь: пустое сообщение дошло и
	// вернулось. Уходящее окно (w.Run уже вернулся) не отвечает.
	otvechaet func(uintptr) bool
	// podnyat — развернуть и вывести на передний план.
	podnyat func(uintptr)
	// est — это окно (тот же hwnd) ещё существует (IsWindow).
	est func(uintptr) bool
}

// podnyatOknoEsliZhivo поднимает уже открытое окно Kelevra, только если оно
// живо. true — поднято, своё открывать не надо; false — живого окна нет,
// вызывающий открывает своё.
//
// Немое окно ждём до srok: ушло — false (открыть своё); заговорило (разовая
// заминка живого окна) — поднимаем его; не ушло и молчит — false: пустой
// экран хуже второго, мёртвого окна, и об этом остаётся строка в журнале.
//
// Поднятое окно караулим srokSlezhki (karaulit): исчезло — false, своё
// открывает вызывающий. true — окно поднято и пережило срок.
func podnyatOknoEsliZhivo(o oknoSistemy, srok, srokSlezhki, shag time.Duration) bool {
	hwnd, est := o.nayti()
	if !est {
		soobshchitOPromahePoiska("поднятие чужого окна")
		return false
	}
	if o.otvechaet(hwnd) {
		o.podnyat(hwnd)
		log.Printf("поднятие чужого окна: нашёл hwnd=%#x, развернул и вывел на передний план", hwnd)
		return karaulit(o, hwnd, srokSlezhki, shag)
	}
	log.Printf("поднятие чужого окна: hwnd=%#x не отвечает — похоже, уходит (перезапуск после обновления); жду до %s", hwnd, srok)
	predel := time.Now().Add(srok)
	for time.Now().Before(predel) {
		time.Sleep(shag)
		h, est := o.nayti()
		if !est {
			log.Printf("поднятие чужого окна: hwnd=%#x ушло с экрана, открываю своё", hwnd)
			return false
		}
		if o.otvechaet(h) {
			o.podnyat(h)
			log.Printf("поднятие чужого окна: hwnd=%#x ожило, развернул и вывел на передний план", h)
			return karaulit(o, h, srokSlezhki, shag)
		}
	}
	log.Printf("ВНИМАНИЕ: окно hwnd=%#x не отвечает и не уходит %s — открываю своё поверх него", hwnd, srok)
	return false
}

// karaulit следит за поднятым окном srok: исчезло — false (открыть своё),
// осталось — true.
func karaulit(o oknoSistemy, hwnd uintptr, srok, shag time.Duration) bool {
	predel := time.Now().Add(srok)
	for time.Now().Before(predel) {
		time.Sleep(shag)
		if !o.est(hwnd) {
			log.Printf("поднятие чужого окна: поднятое hwnd=%#x исчезло (прошлая копия ушла) — открываю своё", hwnd)
			return false
		}
	}
	log.Printf("поднятие чужого окна: hwnd=%#x на месте %s, ухожу", hwnd, srok)
	return true
}
