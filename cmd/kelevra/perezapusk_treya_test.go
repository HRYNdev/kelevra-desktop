package main

import (
	"sync"
	"testing"
	"time"
)

func TestNuzhenPerezapuskTreya(t *testing.T) {
	sluchai := []struct {
		trey, porog, sluzhba string
		nado                 bool
	}{
		{"0.6.73", "", "0.6.75", true},
		{"0.6.75", "", "0.6.75", false},
		{"0.6.76", "", "0.6.75", false}, // значок новее службы — перезапуск ничего не даст
		{"0.6.73", "", "", false},       // служба не сказала версию
		{"0.6.73", "0.6.75", "0.6.75", false},
		{"0.6.73", "0.6.75", "0.6.76", true},
	}
	for _, s := range sluchai {
		if got := nuzhenPerezapuskTreya(s.trey, s.porog, s.sluzhba); got != s.nado {
			t.Errorf("значок %q, порог %q, служба %q: перезапуск=%v, ждали %v", s.trey, s.porog, s.sluzhba, got, s.nado)
		}
	}
}

// poddelnayaSluzhba отдаёт версии службы по очереди, последнюю — навсегда.
type poddelnayaSluzhba struct {
	mu      sync.Mutex
	versii  []string
	voprosy int
}

func (p *poddelnayaSluzhba) versiya(string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := p.voprosy
	if i >= len(p.versii) {
		i = len(p.versii) - 1
	}
	p.voprosy++
	return p.versii[i]
}

func (p *poddelnayaSluzhba) skolkoSprosili() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.voprosy
}

func adresEst() (string, bool) { return "http://127.0.0.1:1/x/", true }

func TestZnachokPerezapuskaetsyaKogdaSluzhbaStalaNovee(t *testing.T) {
	s := &poddelnayaSluzhba{versii: []string{"0.6.74", "", "0.6.74", "0.6.75"}}
	v, nado := zhdatNovuyuSluzhbu("0.6.74", false, time.Millisecond, adresEst, s.versiya, make(chan struct{}))
	if !nado || v != "0.6.75" {
		t.Fatalf("ждали перезапуск ради 0.6.75, получили %q, %v", v, nado)
	}
	if s.skolkoSprosili() != 4 {
		t.Fatalf("перезапуск не на том вопросе: спросили %d раз, ждали 4", s.skolkoSprosili())
	}
}

func TestZnachokNeKrutitsyaPosleNeudachnogoPerezapuska(t *testing.T) {
	// Значок рождён перезапуском, а служба всё ещё новее: файл значка не
	// обновлён. Ради 0.6.75 больше не перезапускаемся, ради 0.6.76 — да.
	s := &poddelnayaSluzhba{versii: []string{"0.6.75", "0.6.75", "0.6.75", "0.6.76"}}
	v, nado := zhdatNovuyuSluzhbu("0.6.73", true, time.Millisecond, adresEst, s.versiya, make(chan struct{}))
	if !nado || v != "0.6.76" {
		t.Fatalf("ждали перезапуск только ради 0.6.76, получили %q, %v", v, nado)
	}
}

func TestZnachokPosleSmenyBezPorogaKogdaSluzhbaNeNovee(t *testing.T) {
	// Перезапуск удался (служба той же версии) — порога нет, следующее
	// обновление снова перезапустит значок.
	s := &poddelnayaSluzhba{versii: []string{"0.6.75", "0.6.76"}}
	v, nado := zhdatNovuyuSluzhbu("0.6.75", true, time.Millisecond, adresEst, s.versiya, make(chan struct{}))
	if !nado || v != "0.6.76" {
		t.Fatalf("ждали перезапуск ради 0.6.76, получили %q, %v", v, nado)
	}
}

func TestZnachokNePerezapuskaetsyaRadiStaroySluzhby(t *testing.T) {
	s := &poddelnayaSluzhba{versii: []string{"0.6.74"}}
	stop := make(chan struct{})
	gotovo := make(chan bool, 1)
	go func() {
		_, nado := zhdatNovuyuSluzhbu("0.6.75", false, time.Millisecond, adresEst, s.versiya, stop)
		gotovo <- nado
	}()
	for s.skolkoSprosili() < 20 {
		time.Sleep(time.Millisecond)
	}
	close(stop)
	select {
	case nado := <-gotovo:
		if nado {
			t.Fatal("значок перезапустился ради службы старее себя")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("слежка не остановилась по stop")
	}
}

func TestZnachokBezSluzhbyNeSprashivaetVersiyu(t *testing.T) {
	s := &poddelnayaSluzhba{versii: []string{"0.6.99"}}
	stop := make(chan struct{})
	gotovo := make(chan bool, 1)
	go func() {
		_, nado := zhdatNovuyuSluzhbu("0.6.75", false, time.Millisecond,
			func() (string, bool) { return "", false }, s.versiya, stop)
		gotovo <- nado
	}()
	time.Sleep(20 * time.Millisecond)
	close(stop)
	if nado := <-gotovo; nado || s.skolkoSprosili() != 0 {
		t.Fatalf("без адреса службы: перезапуск=%v, вопросов %d", nado, s.skolkoSprosili())
	}
}
