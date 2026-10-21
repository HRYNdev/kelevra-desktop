package main

import (
	"log"
	"os"
	"runtime"

	"github.com/HRYNdev/kelevra-desktop/internal/avtozapusk"
)

// Тумблер «Автозапуск» — дело окна, а не службы (21.10).
//
// Запись автозапуска живёт в HKCU человека. Служба Windows работает от
// системы, и её HKCU — ветка самой системы: тумблер, который ходил ручкой
// /api/avtozapusk, при службе писал запись туда, где её никто не читает, а
// состояние показывал по той же чужой ветке — тумблер врал. Окно — процесс в
// сеансе человека, его HKCU — та самая ветка. Поэтому окно читает и
// переключает автозапуск своей привязкой (как «Дать права»), а служба при
// работе от системы ручку не исполняет (internal/sluzhba: AvtozapuskVOkne).

// sostoyanieAvtozapuska — те же поля, что у /api/sostoyanie: страница
// накладывает их поверх ответа службы.
type sostoyanieAvtozapuska struct {
	Podderzhivaetsya bool   `json:"avtozapusk_podderzhivaetsya"`
	Vklyuchen        bool   `json:"avtozapusk_vklyuchen"`
	Ustarela         bool   `json:"avtozapusk_ustarela"`
	Beda             string `json:"avtozapusk_beda"`
}

// avtozapuskOkna — чем окно читает и пишет автозапуск; подменяется в тестах.
type avtozapuskOkna struct {
	podderzhivaetsya bool
	// put — exe, на который ведёт запись: копия в Program Files, если она
	// есть (её держит служба), иначе свой файл.
	put        func() string
	vklyuchen  func() (bool, error)
	ustarela   func(put string) (bool, error)
	vklyuchit  func(put string) error
	vyklyuchit func() error
}

var avtozapuskOknaNastoyashchiy = avtozapuskOkna{
	podderzhivaetsya: runtime.GOOS == "windows",
	put: func() string {
		sebya, err := os.Executable()
		if err != nil {
			return ""
		}
		return putDlyaNovoyKopii(sebya)
	},
	vklyuchen:  avtozapusk.Vklyuchen,
	ustarela:   avtozapusk.UstarelaDlya,
	vklyuchit:  avtozapusk.VklyuchitNa,
	vyklyuchit: avtozapusk.Vyklyuchit,
}

// sostoyanie — что показать на тумблере.
func (a avtozapuskOkna) sostoyanie() sostoyanieAvtozapuska {
	o := sostoyanieAvtozapuska{Podderzhivaetsya: a.podderzhivaetsya}
	if !o.Podderzhivaetsya {
		return o
	}
	vkl, err := a.vklyuchen()
	if err != nil {
		o.Beda = err.Error()
		return o
	}
	o.Vklyuchen = vkl
	if vkl {
		if put := a.put(); put != "" {
			if ust, err := a.ustarela(put); err == nil {
				o.Ustarela = ust
			}
		}
	}
	return o
}

// pereklyuchit включает (на копию в Program Files, повторное включение
// чинит устаревший путь) или выключает автозапуск и отдаёт новое состояние.
func (a avtozapuskOkna) pereklyuchit(vklyuchit bool) (sostoyanieAvtozapuska, error) {
	var err error
	if vklyuchit {
		put := a.put()
		if put == "" {
			err = errNeZnayuPut
		} else {
			err = a.vklyuchit(put)
		}
	} else {
		err = a.vyklyuchit()
	}
	if err != nil {
		log.Printf("автозапуск из окна: %v", err)
	} else {
		log.Printf("автозапуск из окна: %s", map[bool]string{true: "включён", false: "выключен"}[vklyuchit])
	}
	return a.sostoyanie(), err
}

type oshibkaAvtozapuska string

func (o oshibkaAvtozapuska) Error() string { return string(o) }

const errNeZnayuPut = oshibkaAvtozapuska("не понял, где лежит Kelevra")
