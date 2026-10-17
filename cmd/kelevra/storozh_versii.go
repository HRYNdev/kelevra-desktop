package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// Беда 17.10, словами владельца (handoff его Claude): «это что за уебанство
// на компе, открывается БРАУЗЕР НАХУЙ». Причина — не браузер сам по себе
// (см. index.html, кнопка «Панель сети»), а то, что ОКНО осталось от СТАРОЙ
// сборки (0.6.73 и раньше), а служба за ним на Windows обновилась тихо (сеанс
// 0, без переспроса) и перезапустилась уже новой. storozh_okna.go переводит
// такое окно на адрес новой службы, оно грузит НОВЫЙ HTML — но своей
// привязки kelevraPanel (w.Bind, появилась в okno_windows.go в 0.6.74) у
// СТАРОГО бинаря нет никогда, сколько HTML ему ни показывай. Лечить нужно не
// кнопку (она лишь обнажает дыру), а сам разрыв: окно обязано заметить, что
// бинарь под ним устарел, и переоткрыть себя свежим — вот этот файл.

// shagProverkiVersii — как часто окно спрашивает службу, какой она версии.
//
// Реже, чем storozhitSluzhbu (shagStorozha = 2с): та проверка про жизнь и
// смерть, секунды там решают, человек видит «зависло» или нет. Здесь же не
// секунды: тихое обновление службы — событие на фоне, не ежеминутное, и
// лишний опрос раз в две секунды не даёт ничего, кроме нагрузки на локальный
// HTTP.
const shagProverkiVersii = 10 * time.Second

// peremennayaPopytki — через неё защёлка poslednyayaPopytka переживает сам
// перезапуск. Без неё новая копия начинает с пустой защёлки, и если свежий
// .exe на диске НЕ новее (служба стоит из другой папки, а окно запущено из
// старой копии — например, из «Загрузок»), окно перезапускало бы само себя
// каждые shagProverkiVersii, вечно.
const peremennayaPopytki = "KELEVRA_SMENA_PO_VERSII"

// putUstanovki — канонический путь установки: папка, где лежит .exe, плюс
// его настоящее имя (obnovlenie.ImyaFayla), а не os.Executable() в лоб и не
// голый obnovlenie.PutSebya(). Этот код зовётся из ОКНА, которое в момент
// звонка само вполне может оказаться тем самым переименованным хвостом
// (Kelevra.exe.old) — ровно та беда, что уже разобрана в cmd/kelevra/
// obnovlenie.go у zapustitSmenuPosleObnovleniya («на Linux os.Executable() у
// такого процесса возвращает путь к переименованному .old-файлу»): новая
// копия стартовала бы от старого файла и перекачивала бы себя заново, а то и
// просто не нашла бы свежего .exe, если .old к этому моменту уже убран.
// filepath.Dir не трогает ИМЯ файла, только путь к папке — она не меняется
// от переименования файла внутри неё, поэтому ответ верный независимо от
// того, как сейчас называется файл, из которого мы сами запущены.
func putUstanovki() (string, error) {
	svoy, err := obnovlenie.PutSebya()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(svoy), obnovlenie.ImyaFayla), nil
}

// nuzhnoPerezapuskatOknoPoVersii — чистое решение без единого похода в сеть
// или на диск, покрыто тестом storozh_versii_test.go.
//
// Направление НАМЕРЕННО одностороннее: перезапуск только когда служба СТРОГО
// новее окна (obnovlenie.Sravnit(versiyaSluzhby, versiyaOkna) > 0), а не
// просто когда версии отличаются. Служба старше окна — значит либо кто-то
// руками откатил службу, либо (на стенде) окно собрано свежее самой службы;
// в обоих случаях перезапуск окна ничего не чинит и не его дело подталкивать
// откат назад — окно умеет разговаривать по HTTP с чужой версией службы и
// без этого.
//
// poslednyayaPopytka — версия службы, на которую уже пробовали переехать.
// Без этой защёлки расхождение, которое перезапуском НЕ лечится (новый .exe
// не поднимается — файл побит, антивирус держит каждую свежую копию по
// кругу), гоняло бы окно вечно: увидел новую версию — перезапустился — снова
// увидел ту же новую версию (новый процесс ещё не успел замениться) —
// перезапустился опять. Один перезапуск на одну версию службы: не вышло —
// молчим до СЛЕДУЮЩЕЙ версии, а не долбим диск бесконечной петлёй.
func nuzhnoPerezapuskatOknoPoVersii(versiyaOkna, versiyaSluzhby, poslednyayaPopytka string) bool {
	if versiyaSluzhby == "" || versiyaSluzhby == versiyaOkna {
		return false
	}
	if obnovlenie.Sravnit(versiyaSluzhby, versiyaOkna) <= 0 {
		return false
	}
	return versiyaSluzhby != poslednyayaPopytka
}

// perezapustitOknoPoVersii поднимает свежую копию ТЕМ ЖЕ способом смены
// режима, что уже работает у запроса прав по UAC (main.go: argSmena,
// zhdatSmenu, zakrytStaroeOkno) — разбор, почему этот путь закрывает старое
// окно раньше, чем новая копия решает, поднимать ли своё (гонка с
// podnyatChuzheeOkno), целиком лежит в комментариях adresKopii и
// zakrytStaroeOkno, менять там нечего.
//
// argPriStarte здесь — не вранье про UAC, а ровно та же защёлка, что уже
// держит sprositPravaNaStarte: новая копия НЕ обязана сама дёргать
// /api/podklyuchit (vosstanovitPolnuyuZashchitu) и НЕ обязана показывать
// карточку «что изменилось» (rasskazatChtoObnovilis) — человек ничего не
// нажимал и ничего не обновлял, это окно тихо чинит само себя. Без --tiho
// (pokazatLiOkno: !tiho → true): новое окно обязано появиться, а не остаться
// скрытым — человек должен увидеть ровно то же окно, что и до перезапуска.
func perezapustitOknoPoVersii(versiyaSluzhby string) error {
	put, err := putUstanovki()
	if err != nil {
		return err
	}
	cmd := exec.Command(put, argSmena, strconv.Itoa(os.Getpid()), argPriStarte)
	cmd.Env = append(os.Environ(), peremennayaPopytki+"="+versiyaSluzhby)
	return cmd.Start()
}

// storozhitVersiyu живёт рядом со storozhitSluzhbu — одно окно, два сторожа:
// тот следит за жизнью службы (секунды), этот — за версией (минуты).
// adresTekushchiy, а не застывший url: storozhitSluzhbu умеет переводить
// окно на новый адрес после переезда службы (perejtiNaAdres в pokazatOkno),
// и спрашивать версию по УЖЕ мёртвому старому адресу — значит никогда не
// увидеть ответ и никогда не починиться.
//
// zakryt — тот же `func() { w.Dispatch(w.Terminate) }`, что получает и
// storozhitSluzhbu: закрыть своё окно из чужого потока можно только через
// Dispatch (см. комментарий в pokazatOkno про Terminate и PostQuitMessage).
// Перезапуск удался — зовём zakryt и уходим совсем: новая копия уже в пути, и
// продолжать сторожить старым процессом больше незачем. Не удался — не
// закрываемся (человеку нужно хоть какое-то окно) и пробуем на следующем
// шаге — nuzhnoPerezapuskatOknoPoVersii не даст долбить один и тот же сбой
// силой, но заметит, если к тому моменту вышла ЕЩЁ более новая версия.
func storozhitVersiyu(adresTekushchiy func() string, zakryt func()) {
	poslednyayaPopytka := os.Getenv(peremennayaPopytki)
	for {
		time.Sleep(shagProverkiVersii)
		versiyaSluzhby := versiyaChuzhoyKopii(adresTekushchiy())
		if !nuzhnoPerezapuskatOknoPoVersii(podpiska.Versiya, versiyaSluzhby, poslednyayaPopytka) {
			continue
		}
		log.Printf("сторож версии: служба версии %s новее окна %s — перезапускаю окно свежим .exe", versiyaSluzhby, podpiska.Versiya)
		poslednyayaPopytka = versiyaSluzhby
		if err := perezapustitOknoPoVersii(versiyaSluzhby); err != nil {
			log.Printf("сторож версии: не поднял свежую копию (%v) — работаю старой версией дальше", err)
			continue
		}
		zakryt()
		return
	}
}
