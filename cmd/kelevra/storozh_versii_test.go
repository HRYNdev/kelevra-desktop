package main

import "testing"

// Красный контроль беды 17.10: служба обновилась и стала новее окна — без
// этого решения («перезапускать только когда версии РАЗНЫЕ, а не когда
// служба новее») окно так и осталось бы работать старым бинарём, и кнопка
// «Панель сети» уходила бы в window.open (см. index.html). Проверяем именно
// то решение, которое включено в прод: служба новее → перезапуск нужен.
func TestNuzhnoPerezapuskatOknoKogdaSluzhbaNovee(t *testing.T) {
	if !nuzhnoPerezapuskatOknoPoVersii("0.6.73", "0.6.74", "") {
		t.Fatal("служба новее окна, а перезапуск решили не делать — ровно беда 17.10")
	}
}

// Контроль направления: служба СТАРШЕ окна (откат руками, либо стенд, где
// окно собрано свежее). Перезапуск тут не чинит ничего — это не наше дело.
func TestNeNuzhnoPerezapuskatKogdaSluzhbaStaree(t *testing.T) {
	if nuzhnoPerezapuskatOknoPoVersii("0.6.74", "0.6.73", "") {
		t.Fatal("служба старше окна, а решение всё равно велит перезапускаться")
	}
}

// Версии совпадают — перезапускать нечего.
func TestNeNuzhnoPerezapuskatKogdaVersiiSovpadayut(t *testing.T) {
	if nuzhnoPerezapuskatOknoPoVersii("0.6.74", "0.6.74", "") {
		t.Fatal("версии совпадают, а решение велит перезапускаться")
	}
}

// Пустой ответ службы (сеть не дошла, server молчит) — не повод решать
// что-либо: versiyaChuzhoyKopii отдаёт "" именно на отказ связи.
func TestNeNuzhnoPerezapuskatKogdaVersiyaSluzhbyPusta(t *testing.T) {
	if nuzhnoPerezapuskatOknoPoVersii("0.6.73", "", "") {
		t.Fatal("версия службы не узнана (пустая строка), а решение всё равно велит перезапускаться")
	}
}

// Петля: перезапуск уже пробовали на ЭТУ ЖЕ версию службы (новый .exe не
// поднялся — например, файл побит) — второй раз долбить не должны.
func TestNeNuzhnoPerezapuskatPovtornoNaTuZheVersiyu(t *testing.T) {
	if nuzhnoPerezapuskatOknoPoVersii("0.6.73", "0.6.74", "0.6.74") {
		t.Fatal("перезапуск на эту версию уже пробовали, а решение велит повторить — это и есть петля")
	}
}

// Но если ПОСЛЕ неудачной попытки служба стала ещё новее — пробовать заново
// можно и нужно, защёлка держит только ТУ версию, на которую уже прыгали.
func TestNuzhnoPerezapuskatKogdaSluzhbaUshlaDalshePoslePopytki(t *testing.T) {
	if !nuzhnoPerezapuskatOknoPoVersii("0.6.73", "0.6.75", "0.6.74") {
		t.Fatal("служба ушла ещё дальше вперёд после неудачной попытки, а решение велит молчать")
	}
}

// putUstanovki не падает на обычном стенде (в тестах мы — не .old хвост) и
// возвращает путь, оканчивающийся именем из obnovlenie.ImyaFayla, а не
// случайным тестовым бинарём go test.
func TestPutUstanovkiVozvrashchaetKanonicheskoeImya(t *testing.T) {
	put, err := putUstanovki()
	if err != nil {
		t.Fatalf("putUstanovki упал: %v", err)
	}
	if got := put[len(put)-len(imyaFaylaDlyaTesta):]; got != imyaFaylaDlyaTesta {
		t.Fatalf("putUstanovki вернул %q, ожидал путь с именем %q на конце", put, imyaFaylaDlyaTesta)
	}
}

const imyaFaylaDlyaTesta = "Kelevra.exe"
