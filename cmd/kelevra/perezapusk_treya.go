package main

import (
	"log"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
)

// Значок (--trey) после обновления службы — перезапускается сам, как окно (17.10).
//
// Замер с ПК 17.10. Значок запущен в 10:44, обновление прошло в 19:31 и ещё
// одно в 22:06 — а значок всё это время жил из файла 0.6.73, который после
// первого обновления лежит рядом с приложением скрытым Kelevra.exe.old.
// Запущенный файл Windows стереть не даёт, поэтому уборка хвоста после
// каждого обновления писала «убрать не вышло», и .old не уходил до
// перезагрузки. Окно эту беду уже не делает (perezapusk_okna.go), значок —
// делал: за службой он не следил вовсе, только один раз ждал её метку на
// старте.
//
// Теперь значок раз в shagSlezhkiTreya спрашивает версию службы и, если
// служба СТРОГО новее, снимает свой значок, запускает тот же .exe с --trey
// (на диске он уже новый) и уходит. Новый значок дожидается смерти старого
// (--smena <pid>) и убирает хвост с повторами.

// shagSlezhkiTreya — как часто значок спрашивает службу. Не вопрос жизни и
// смерти, как у сторожа окна: значок живёт днями, а обновление случается
// раз в несколько дней.
const shagSlezhkiTreya = 15 * time.Second

// nuzhenPerezapuskTreya — служба строго новее значка и строго новее porog.
//
// porog — версия службы, которую уже не помог догнать прошлый перезапуск:
// значок, рождённый перезапуском, видит службу всё ещё новее себя, — значит
// его файл на диске не обновлён (служба живёт из другой папки), и второй
// перезапуск дал бы тот же старый файл. Без порога это петля: перезапуск
// раз в 15 секунд навсегда. Пусто — порога нет.
func nuzhenPerezapuskTreya(versiyaTreya, porog, versiyaSluzhby string) bool {
	if !nuzhenPerezapuskOkna(versiyaTreya, versiyaSluzhby) {
		return false
	}
	return porog == "" || obnovlenie.Sravnit(versiyaSluzhby, porog) > 0
}

// zhdatNovuyuSluzhbu — слежка значка за версией службы. Возвращает версию
// службы и true, когда значку пора перезапуститься; "" и false — если
// закрыли stop.
//
//	versiyaTreya  — своя версия (podpiska.Versiya).
//	posleSmeny    — этот значок сам рожден перезапуском (--trey --smena):
//	                первая же служба новее него становится порогом
//	                (nuzhenPerezapuskTreya), а не поводом перезапуститься снова.
//	adresSluzhby  — где служба сейчас (kopiya.Nayti: после обновления порт
//	                новый, адрес надо спрашивать каждый раз заново).
//	versiya       — версия службы по адресу (versiyaSluzhby); "" — не сказала.
func zhdatNovuyuSluzhbu(versiyaTreya string, posleSmeny bool, shag time.Duration,
	adresSluzhby func() (string, bool), versiya func(string) string, stop <-chan struct{}) (string, bool) {
	porog := ""
	pervyyOtvet := true
	for {
		if adres, est := adresSluzhby(); est {
			if v := versiya(adres); v != "" {
				if pervyyOtvet && posleSmeny && nuzhenPerezapuskOkna(versiyaTreya, v) {
					log.Printf("значок: перезапуск не догнал службу (служба %s, значок %s) — "+
						"файл значка не обновлён, второй раз ради этой версии не перезапускаюсь", v, versiyaTreya)
					porog = v
				}
				pervyyOtvet = false
				if nuzhenPerezapuskTreya(versiyaTreya, porog, v) {
					return v, true
				}
			}
		}
		select {
		case <-stop:
			return "", false
		case <-time.After(shag):
		}
	}
}
