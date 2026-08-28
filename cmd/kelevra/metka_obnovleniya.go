package main

import "sync"

// Метка «есть обновление» на значке трея — то, чего не хватало заказу
// человека от 26.08 («просто приходит обновления сами и ты тыкаешь
// обновление и все»).
//
// Беда, которую эта метка закрывает. Пузырь (pokazatOblachkoObnovleniya)
// про каждую версию говорит РОВНО ОДИН РАЗ навсегда — отметка лежит на
// диске (internal/obnovlenie.ObyavlennoeObnovlenie), а сам балун Windows
// живёт секунды и уезжает в «Центр уведомлений». Отошёл от компьютера,
// не заметил — и тыкать больше нечего: значок в трее выглядит ровно так
// же, как без обновления, а повторить пузырь нельзя (26.08 09:41 дословно:
// «надоело, почему постоянно приходит эта ерунда» — про повторяющиеся
// уведомления). Метка — не второй пузырь: она ничего не перебивает и
// никуда не всплывает, а просто остаётся видимой, пока человек сам не
// ткнёт. Ровно так это и сделано на телефоне, на который он сослался:
// уведомление показывают один раз, но значок с точкой висит, пока не
// нажмёшь.
//
// Файл нарочно без build-тега (не в trey_windows.go): состояние и тексты
// метки должны собираться и проверяться на сервере без Windows, там же
// живёт их тест (metka_obnovleniya_test.go). Трей на Windows берёт отсюда
// готовые строки, а сам занят только WinAPI.

var (
	metkaZamok   sync.Mutex
	metkaVersiya string
)

// podskazkaBezObnovleniya — то, что человек видит, наведя мышь на значок,
// когда ставить нечего. Ровно та строка, что стояла в dobavitZnachokTreya
// до появления метки.
const podskazkaBezObnovleniya = "Kelevra: VPN включён"

// zapomnitObnovlenie — фоновая проверка нашла версию новее нынешней.
// Зовётся оттуда же, откуда показывается пузырь, и переживает его: пузырь
// гаснет через секунды, метка держится до установки.
func zapomnitObnovlenie(versiya string) {
	metkaZamok.Lock()
	defer metkaZamok.Unlock()
	metkaVersiya = versiya
}

// zabytObnovlenie — обновление поставлено (или отменено): метку снять.
// Установка сама по себе уводит копию на смену, но забыть метку всё равно
// надо: сцена «поставили и живём дальше» бывает и без ухода (стенды), а
// значок с обещанием несуществующего обновления — прямая ложь человеку.
func zabytObnovlenie() {
	metkaZamok.Lock()
	defer metkaZamok.Unlock()
	metkaVersiya = ""
}

// zhdushcheeObnovlenie — версия, которая ждёт тычка, или "" если ждать
// нечего.
func zhdushcheeObnovlenie() string {
	metkaZamok.Lock()
	defer metkaZamok.Unlock()
	return metkaVersiya
}

// pometitObnovlenie — хук sluzhba.MetkaObnovleniya: фоновая проверка
// закончилась, versiya — что нашла ("" — ставить нечего). Зовётся на КАЖДОЙ
// проверке, а не только на новой находке: после перезапуска копии пузырь
// про уже названную версию молчит навсегда (povestitEsliNovaya), и без
// этого вызова значок выглядел бы так, будто обновления нет.
// obnovitZnachokTreya живёт в trey_windows.go и trey_other.go — тут про
// платформу знать нечего.
func pometitObnovlenie(versiya string) {
	if versiya == "" {
		zabytObnovlenie()
	} else {
		zapomnitObnovlenie(versiya)
	}
	obnovitZnachokTreya()
}

// podskazkaTreya — текст всплывающей подсказки значка (szTip). Пока
// обновления нет — обычная строка про включённый VPN; как только нашлось —
// подсказка сама говорит, что делать дальше, потому что пузыря к этому
// моменту уже может не быть на экране.
//
// szTip у NOTIFYICONDATAW — массив на 128 UTF-16 слов вместе с нулём, то
// есть 127 знаков; наши строки заведомо короче, а kopirovatStrokuUTF16
// режет длинное сама.
func podskazkaTreya() string {
	versiya := zhdushcheeObnovlenie()
	if versiya == "" {
		return podskazkaBezObnovleniya
	}
	return "Kelevra: вышла версия " + versiya + " — правый клик по значку, «Обновить»"
}

// punktMenyuObnovleniya — первый пункт меню правого клика, когда
// обновление ждёт: «Обновить до 0.6.27». Второе значение false — ставить
// нечего, пункта в меню быть не должно (пункт «обновить», который ничего
// не обновляет, хуже, чем его отсутствие).
func punktMenyuObnovleniya() (string, bool) {
	versiya := zhdushcheeObnovlenie()
	if versiya == "" {
		return "", false
	}
	return "Обновить до " + versiya, true
}
