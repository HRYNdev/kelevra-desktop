package main

import (
	"testing"
	"time"
)

// poddelnoeOkno — окно на «экране» теста: есть ли оно на каждом опросе и
// отвечает ли.
type poddelnoeOkno struct {
	ostalosNaEkrane int  // сколько ещё опросов nayti найдёт окно (<0 — всегда)
	otvechaet       bool // крутит ли поток окна очередь
	ozhivaetCherez  int  // через сколько опросов otvechaet станет true (0 — никогда)
	opros           int
	podnyato        bool
}

func (p *poddelnoeOkno) sistema() oknoSistemy {
	return oknoSistemy{
		nayti: func() (uintptr, bool) {
			p.opros++
			if p.ostalosNaEkrane == 0 {
				return 0, false
			}
			if p.ostalosNaEkrane > 0 {
				p.ostalosNaEkrane--
			}
			return 0x1234, true
		},
		otvechaet: func(uintptr) bool {
			if p.ozhivaetCherez > 0 && p.opros > p.ozhivaetCherez {
				return true
			}
			return p.otvechaet
		},
		podnyat: func(uintptr) { p.podnyato = true },
	}
}

// Ровно гонка стенда 21.10: старое окно перезапускается после обновления
// службы — на экране ещё висит, но его поток очередь уже не крутит, и через
// несколько мгновений оно исчезнет вместе с процессом. Прежний код поднимал
// его и уходил — человек оставался без окна. Теперь новая копия обязана
// дождаться ухода и открыть своё.
func TestUhodyashcheeOknoNePodnimaetsya(t *testing.T) {
	p := &poddelnoeOkno{ostalosNaEkrane: 3}
	if podnyatOknoEsliZhivo(p.sistema(), time.Second, time.Millisecond) {
		t.Fatal("подняли уходящее окно и не открыли своё — после обновления окно пропадёт")
	}
	if p.podnyato {
		t.Fatal("уходящее окно поднято")
	}
}

// Обычный щелчок по значку при открытом живом окне: поднимаем его, второе не
// открываем (беда 23.08 — два окна).
func TestZhivoeOknoPodnimaetsya(t *testing.T) {
	p := &poddelnoeOkno{ostalosNaEkrane: -1, otvechaet: true}
	if !podnyatOknoEsliZhivo(p.sistema(), time.Second, time.Millisecond) || !p.podnyato {
		t.Fatal("живое окно не поднято — откроется второе")
	}
}

func TestNetOknaOtkryvaemSvoe(t *testing.T) {
	p := &poddelnoeOkno{}
	if podnyatOknoEsliZhivo(p.sistema(), time.Second, time.Millisecond) || p.podnyato {
		t.Fatal("окна нет, а своё не открываем")
	}
}

// Разовая заминка живого окна: замолчало и заговорило, не уходя, — поднимаем
// его, второе не нужно.
func TestZaminkaZhivogoOkna(t *testing.T) {
	p := &poddelnoeOkno{ostalosNaEkrane: -1, ozhivaetCherez: 3}
	if !podnyatOknoEsliZhivo(p.sistema(), time.Second, time.Millisecond) || !p.podnyato {
		t.Fatal("ожившее окно не поднято")
	}
}

// Немое окно, которое не уходит: по сроку открываем своё — пустой экран хуже.
func TestNemoeOknoNeUhoditOtkryvaemSvoe(t *testing.T) {
	p := &poddelnoeOkno{ostalosNaEkrane: -1}
	nachalo := time.Now()
	if podnyatOknoEsliZhivo(p.sistema(), 30*time.Millisecond, time.Millisecond) || p.podnyato {
		t.Fatal("немое окно поднято вместо своего")
	}
	if time.Since(nachalo) < 30*time.Millisecond {
		t.Fatal("не дождались срока ухода")
	}
}
