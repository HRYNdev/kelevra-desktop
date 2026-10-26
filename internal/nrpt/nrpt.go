// Пакет nrpt: имена зоны дома (.dom) на Windows — правилом NRPT (Name
// Resolution Policy Table, договор «вход домой», «Безопасность» п.6).
//
// Зачем. У tun-входа strict_route выключен (konfig/lokalnaya_set.go: иначе
// пропадает локальная сеть), и Windows вправе спросить про имя DNS-сервер
// физического адаптера — то есть сервер чужой сети (кафе, гостиница). Для
// .dom это утечка имени и подмена ответа: чужой сервер ответит что угодно.
// Правило NRPT велит Windows спрашивать про .dom только 100.77.0.1, а этот
// адрес идёт в tun и по правилу дома — в «Дом» (konfig/dom.go).
//
// Правило стоит, только пока блок дома действует и туннель поднят; снимается
// при остановке связи; на старте службы снимаются остатки от падения. Трогаем
// только свои правила: ключ реестра с именем Imya и комментарием Kommentariy.
package nrpt

import (
	"log"
	"sync"
)

// Imya — метка «все свои правила» для Ubrat (и имя прежних ключей реестра
// KelevraDom*, снимаемых как остатки). У правила Windows имя {GUID}; своё оно
// по комментарию Kommentariy.
const Imya = "KelevraDom"

// Kommentariy — комментарий правила: человек узнаёт его в Get-DnsClientNrptRule.
const Kommentariy = "Kelevra: вход домой"

// Pravilo — одно правило: имена с суффиксом Zona спрашивать у Server.
type Pravilo struct {
	Zona   string // «.dom»
	Server string // «100.77.0.1»
}

// Sistema — где живут правила. Настоящая — реестр Windows (nrpt_windows.go);
// проверки подставляют свою: на машине, где идут проверки, NRPT не трогается.
type Sistema interface {
	// Svoi — имена только своих правил (по комментарию); чужих не возвращает.
	Svoi() ([]string, error)
	// Postavit — поставить или заменить своё правило.
	Postavit(p Pravilo) error
	// Ubrat — убрать своё правило по имени; Imya — все свои.
	Ubrat(imya string) error
}

// Upravlenie — держит правило в нужном положении.
type Upravlenie struct {
	Sys Sistema // nil — ничего не делать (не Windows, стенд)

	mu    sync.Mutex
	stoit *Pravilo
}

// Soglasovat — правило p нужно (nuzhno) или нет. Повтор с тем же — без
// обращения к системе.
func (u *Upravlenie) Soglasovat(nuzhno bool, p Pravilo) error {
	if u == nil || u.Sys == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if nuzhno {
		if u.stoit != nil && *u.stoit == p {
			return nil
		}
		if err := u.Sys.Postavit(p); err != nil {
			return err
		}
		u.stoit = &p
		log.Printf("имена %s спрашиваются у %s (правило NRPT %s)", p.Zona, p.Server, Imya)
		return nil
	}
	if u.stoit == nil {
		return nil
	}
	if err := u.Sys.Ubrat(Imya); err != nil {
		return err
	}
	u.stoit = nil
	log.Printf("правило NRPT %s снято", Imya)
	return nil
}

// UbratOstatki — снять свои правила, оставшиеся от прошлой жизни службы
// (падение, выключение питания). Зовётся на старте службы.
func (u *Upravlenie) UbratOstatki() {
	if u == nil || u.Sys == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	imena, err := u.Sys.Svoi()
	if err != nil {
		log.Printf("правила NRPT не прочитаны: %v", err)
		return
	}
	for _, imya := range imena {
		if err := u.Sys.Ubrat(imya); err != nil {
			log.Printf("оставшееся правило NRPT %s не снято: %v", imya, err)
			continue
		}
		log.Printf("снято оставшееся правило NRPT %s", imya)
	}
	u.stoit = nil
}
