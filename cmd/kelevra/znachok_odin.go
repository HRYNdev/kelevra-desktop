package main

import (
	"log"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/vinsluzhba"
)

// Значок в трее (--trey) — отдельный процесс, и крестик окна в трей не
// сворачивает: окно после закрытия просто выходит. Значок поднимали только
// автозапуск при входе и установка службы. Нет процесса значка (убит,
// упал, автозапуск смотрел на удалённый файл) — крестик убирал приложение с
// глаз совсем, служба жила невидимой («тупо закрывается», ноут Вовы 01-3,
// 24.10). Теперь окно само смотрит, жив ли значок, при открытии и при
// закрытии, и поднимает его, если нет.
//
// Единственность значка держит сам значок: именованный замок сеанса
// (zanyatMestoZnachka), а окно только спрашивает, занят ли он
// (znachokZhiv). Два окна, открытые разом, поднимут два --trey — второй
// увидит занятый замок и уйдёт, не показав значка.

// Точки подмены для тестов: настоящие вызовы смотрят в ОС и порождают процесс.
var (
	znachokZhiv           = znachokZhivOS
	sluzhbaDlyaZnachkaEst = func() bool {
		est, err := vinsluzhba.Ustanovlena()
		return err == nil && est
	}
)

// podnyatZnachokEsliNet — окно (kogda — «открытие окна» / «закрытие окна»)
// проверяет значок и поднимает его, если его нет. Значок нужен, только
// когда туннель держит служба Windows: без неё окно само и есть приложение,
// и значку показывать нечего (так было и до этой правки: zapustitZnachok
// звучал только после установки службы). Возвращает, поднимал ли.
func podnyatZnachokEsliNet(kogda string) bool {
	if !sluzhbaDlyaZnachkaEst() {
		return false
	}
	if znachokZhiv() {
		return false
	}
	log.Printf("значок: при %s процесса значка нет — поднимаю (--trey)", kogda)
	zapustitZnachok()
	return true
}

// srokZamkaZnachkaPosleSmeny — сколько значок, поднятый прежним значком
// (--trey --smena), ждёт освобождения замка: прежний уже вышел (zhdatSmenu),
// а ОС освобождает замок мёртвого процесса не в то же мгновение.
const srokZamkaZnachkaPosleSmeny = 5 * time.Second

// mozhnoStavitZnachok — решение значка при старте, без ОС (для тестов):
// замок взят — ставим; не взят — значок уже есть у другого процесса, второй
// не нужен. Старый значок без замка (версии до этой правки) виден по его
// окну трея: при обычном старте уступаем и ему; при смене (--smena) прежний
// значок и есть тот, кто нас поднял и уже ушёл.
func mozhnoStavitZnachok(zamokVzyat, posleSmeny, staroeOknoTreyaEst bool) bool {
	if !zamokVzyat {
		return false
	}
	if !posleSmeny && staroeOknoTreyaEst {
		return false
	}
	return true
}
