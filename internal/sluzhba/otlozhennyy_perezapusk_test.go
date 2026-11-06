package sluzhba

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// stendOtlozh — слежка отложенного перезапуска на подменах: свои события
// сети, тики, часы, счётчик трафика и перезапуск; ожидания — без пауз.
type stendOtlozh struct {
	s        *Sluzhba
	mu       sync.Mutex
	seychas  time.Time
	set      string
	vsego    int64
	sobytiya chan struct{}
	tiki     chan time.Time
	sdelano  chan string
	tikGotov chan struct{}
	otmena   context.CancelFunc
}

func novyyStendOtlozh(t *testing.T) *stendOtlozh {
	t.Helper()
	st := &stendOtlozh{
		s:        &Sluzhba{},
		seychas:  time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC),
		set:      "wifi-dom",
		sobytiya: make(chan struct{}),
		tiki:     make(chan time.Time),
		sdelano:  make(chan string, 16),
		tikGotov: make(chan struct{}),
	}
	o := &st.s.otlozh
	o.seychas = func() time.Time { st.mu.Lock(); defer st.mu.Unlock(); return st.seychas }
	o.otpechatokSeti = func() string { st.mu.Lock(); defer st.mu.Unlock(); return st.set }
	o.trafik = func() (int64, bool) { st.mu.Lock(); defer st.mu.Unlock(); return st.vsego, true }
	o.perezapustit = func(prichina string) bool {
		st.sdelano <- prichina
		st.s.otlozhennyyVypolnen() // подъём на свежем конфиге снимает отложенное
		return true
	}
	o.posleTika = func() { st.tikGotov <- struct{}{} }
	srazu := func(time.Duration) <-chan time.Time {
		c := make(chan time.Time, 1)
		c <- time.Time{}
		return c
	}
	ctx, otmena := context.WithCancel(context.Background())
	st.otmena = otmena
	gotovo := make(chan struct{})
	go func() {
		st.s.vestiOtlozhennyy(ctx, st.sobytiya, st.tiki, srazu)
		close(gotovo)
	}()
	t.Cleanup(func() { otmena(); <-gotovo })
	return st
}

// tik — прошла минута (часы и счётчик), тик отдан слежке и обработан.
func (st *stendOtlozh) tik(bayt int64, proshlo time.Duration) {
	st.mu.Lock()
	st.seychas = st.seychas.Add(proshlo)
	st.vsego += bayt
	t := st.seychas
	st.mu.Unlock()
	st.tiki <- t
	<-st.tikGotov
}

func (st *stendOtlozh) smenitSet(set string) {
	st.mu.Lock()
	st.set = set
	st.mu.Unlock()
	st.sobytiya <- struct{}{}
}

func (st *stendOtlozh) zhdatPerezapusk(t *testing.T, chto string) string {
	t.Helper()
	select {
	case p := <-st.sdelano:
		if !strings.Contains(p, chto) {
			t.Fatalf("перезапуск «%s», ждали причину с «%s»", p, chto)
		}
		return p
	case <-time.After(2 * time.Second):
		t.Fatalf("перезапуска (%s) не было", chto)
	}
	return ""
}

func (st *stendOtlozh) netPerezapuska(t *testing.T, chto string) {
	t.Helper()
	select {
	case p := <-st.sdelano:
		t.Fatalf("%s: лишний перезапуск «%s»", chto, p)
	case <-time.After(100 * time.Millisecond):
	}
}

// Смена физической сети — естественный момент: отложенный перезапуск идёт;
// событие от своего TUN (сеть та же) и смена сети без отложенного — нет.
func TestOtlozhennyyPoSmeneSeti(t *testing.T) {
	st := novyyStendOtlozh(t)
	st.smenitSet("wifi-kafe")
	st.netPerezapuska(t, "смена сети без отложенного")

	st.s.otlozhitPerezapusk(prichinaKonfig, "новый профиль: выходы: ~Финляндия · прямой", "")
	st.smenitSet("wifi-kafe") // событие своего TUN: физическая сеть та же
	st.netPerezapuska(t, "та же сеть")
	st.smenitSet("wifi-dom")
	p := st.zhdatPerezapusk(t, "сеть сменилась")
	if !strings.Contains(p, "Финляндия") {
		t.Fatalf("в причине нет того, ради чего отложено: %q", p)
	}
	if _, _, n := st.s.otlozh.nuzhenS(); n {
		t.Fatal("после перезапуска отложенное осталось")
	}
	// Сеть пропала и вернулась (Wi-Fi выключали) — тоже момент.
	st.s.otlozhitPerezapusk(prichinaYadro, "новое ядро core-v1.14.1-7", "")
	st.smenitSet("")
	st.netPerezapuska(t, "сети нет")
	st.smenitSet("wifi-dom")
	st.zhdatPerezapusk(t, "новое ядро")
}

// Пробуждение машины (тик опоздал на минуты) — естественный момент.
func TestOtlozhennyyPosleSna(t *testing.T) {
	st := novyyStendOtlozh(t)
	st.s.otlozhitPerezapusk(prichinaYadro, "новое ядро core-v1.14.1-7", "")
	st.tik(1<<20, time.Minute)
	st.netPerezapuska(t, "обычный тик с трафиком")
	st.tik(0, 40*time.Minute) // машина спала
	st.zhdatPerezapusk(t, "после сна")
}

// Тихое окно: tishinaPoUmolchaniyu подряд почти без трафика — перезапуск;
// трафик посреди окна счёт сбрасывает.
func TestOtlozhennyyTihoeOkno(t *testing.T) {
	st := novyyStendOtlozh(t)
	st.s.otlozhitPerezapusk(prichinaKonfig, "новый профиль: DNS", "")
	minut := int(tishinaPoUmolchaniyu / time.Minute)
	st.tik(0, time.Minute) // первый тик: счётчик запомнен
	for i := 0; i < minut-1; i++ {
		st.tik(2<<10, time.Minute) // фон: 2 КБ в минуту
	}
	st.tik(5<<20, time.Minute) // человек открыл страницу — окно сначала
	for i := 0; i < minut-1; i++ {
		st.tik(1<<10, time.Minute)
	}
	st.netPerezapuska(t, "окно ещё не набралось после трафика")
	st.tik(1<<10, time.Minute)
	st.zhdatPerezapusk(t, "тихое окно")
}

// Сеть не меняется и трафик идёт всё время — перезапуск по сроку, не позже.
func TestOtlozhennyyPoSroku(t *testing.T) {
	staryy := srokPoUmolchaniyu
	srokPoUmolchaniyu = 30 * time.Minute
	t.Cleanup(func() { srokPoUmolchaniyu = staryy })
	st := novyyStendOtlozh(t)
	st.s.otlozhitPerezapusk(prichinaKonfig, "новый профиль: вход tun", "")
	for i := 0; i < 29; i++ {
		st.tik(1<<20, time.Minute)
	}
	st.netPerezapuska(t, "срок не вышел")
	st.tik(1<<20, time.Minute)
	st.zhdatPerezapusk(t, "срок")
}

// Ничего не отложено — ни тихое окно, ни срок, ни сон перезапуска не дают.
func TestOtlozhennyyNichegoNeZhdet(t *testing.T) {
	st := novyyStendOtlozh(t)
	for i := 0; i < 20; i++ {
		st.tik(0, time.Minute)
	}
	st.tik(0, time.Hour)
	st.smenitSet("wifi-kafe")
	st.netPerezapuska(t, "ничего не отложено")
}

// Повтор той же причины срок не сдвигает; подъём ядра снимает отложенное.
func TestOtlozhennyySrokSPervoy(t *testing.T) {
	s := &Sluzhba{}
	t0 := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	seychas := t0
	s.otlozh.seychas = func() time.Time { return seychas }
	s.otlozhitPerezapusk(prichinaKonfig, "раз", "")
	seychas = t0.Add(time.Hour)
	s.otlozhitPerezapusk(prichinaKonfig, "два", "")
	nachalo, slova, nuzhen := s.otlozh.nuzhenS()
	if !nuzhen || !nachalo.Equal(t0) || slova != "два" {
		t.Fatalf("отложено с %v (%q), ждали с %v", nachalo, slova, t0)
	}
	s.otlozhennyyVypolnen()
	if _, _, n := s.otlozh.nuzhenS(); n {
		t.Fatal("подъём ядра не снял отложенное")
	}
	// Ядро стоит — новое ядро не откладывается (встанет при подъёме само).
	s.otlozhitDlyaYadra("core-v1")
	if _, _, n := s.otlozh.nuzhenS(); n {
		t.Fatal("ядра нет, а замена отложена")
	}
}
