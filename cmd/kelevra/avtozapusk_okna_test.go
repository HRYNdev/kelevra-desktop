package main

import (
	"errors"
	"testing"
)

// poddelnyyAvtozapusk — реестр человека в памяти.
type poddelnyyAvtozapusk struct {
	zapis string // "" — записи нет
	beda  error
}

func (p *poddelnyyAvtozapusk) okno(put string) avtozapuskOkna {
	return avtozapuskOkna{
		podderzhivaetsya: true,
		put:              func() string { return put },
		vklyuchen:        func() (bool, error) { return p.zapis != "", p.beda },
		ustarela: func(put string) (bool, error) {
			return p.zapis != "" && p.zapis != put, nil
		},
		vklyuchit:  func(put string) error { p.zapis = put; return p.beda },
		vyklyuchit: func() error { p.zapis = ""; return p.beda },
	}
}

const putPF = `C:\Program Files\Kelevra\Kelevra.exe`

// Тумблер в окне пишет запись человеку (не службе) и на копию в Program
// Files; состояние читает оттуда же.
func TestTumblerAvtozapuskaVOkne(t *testing.T) {
	p := &poddelnyyAvtozapusk{zapis: `C:\Users\u\Downloads\Kelevra.exe`}
	a := p.okno(putPF)

	if s := a.sostoyanie(); !s.Vklyuchen || !s.Ustarela {
		t.Fatalf("запись на «Загрузки» обязана читаться включённой и устаревшей: %+v", s)
	}
	s, err := a.pereklyuchit(true)
	if err != nil {
		t.Fatal(err)
	}
	if p.zapis != putPF {
		t.Fatalf("включение записало %q, ждали копию в Program Files", p.zapis)
	}
	if !s.Vklyuchen || s.Ustarela || !s.Podderzhivaetsya {
		t.Fatalf("после включения состояние %+v", s)
	}
	if s, err = a.pereklyuchit(false); err != nil || s.Vklyuchen || p.zapis != "" {
		t.Fatalf("выключение: %+v %v, запись %q", s, err, p.zapis)
	}
}

func TestTumblerAvtozapuskaBedy(t *testing.T) {
	p := &poddelnyyAvtozapusk{}
	if _, err := p.okno("").pereklyuchit(true); err == nil {
		t.Fatal("без пути к exe включение обязано отказать")
	}
	p.beda = errors.New("нет доступа")
	if s := p.okno(putPF).sostoyanie(); s.Beda == "" {
		t.Fatalf("ошибка чтения реестра не дошла до тумблера: %+v", s)
	}
	if s := (avtozapuskOkna{}).sostoyanie(); s.Podderzhivaetsya || s.Beda != "" {
		t.Fatalf("вне Windows тумблер прячется молча: %+v", s)
	}
}
