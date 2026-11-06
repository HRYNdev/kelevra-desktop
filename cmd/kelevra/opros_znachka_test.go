package main

import (
	"reflect"
	"testing"
	"time"
)

// progon гоняет опрос по сценарию ответов и возвращает все вызовы pometit.
// Когда сценарий кончился, закрывает stop.
func progon(t *testing.T, otvety []string, adresEst bool) []bool {
	t.Helper()
	var vyzovy []bool
	stop := make(chan struct{})
	gotovo := make(chan struct{})
	i := 0
	sost := func(string) string {
		s := otvety[i]
		i++
		if i >= len(otvety) {
			close(stop)
		}
		return s
	}
	go func() {
		defer close(gotovo)
		oprashivatSostoyanie(time.Millisecond,
			func() (string, bool) { return "http://x/", adresEst },
			sost, func(p bool) { vyzovy = append(vyzovy, p) }, stop)
	}()
	select {
	case <-gotovo:
	case <-time.After(5 * time.Second):
		t.Fatal("опрос не вышел")
	}
	return vyzovy
}

func TestOprosZnachkaSmenaZnacheniya(t *testing.T) {
	got := progon(t, []string{"stoit", "podnimaem", "rabotaet", "rabotaet", "stoit"}, true)
	if want := []bool{true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("вызовы %v, ждали %v", got, want)
	}
}

func TestOprosZnachkaOdinakovoeNeZovet(t *testing.T) {
	if got := progon(t, []string{"stoit", "slomalos", ""}, true); len(got) != 0 {
		t.Fatalf("вызовы %v, ждали ни одного", got)
	}
}

func TestOprosZnachkaOshibkaOznachaetOtklyucheno(t *testing.T) {
	got := progon(t, []string{"rabotaet", "", "rabotaet"}, true)
	if want := []bool{true, false, true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("вызовы %v, ждали %v", got, want)
	}
}

func TestOprosZnachkaBezSluzhbyOtklyucheno(t *testing.T) {
	stop := make(chan struct{})
	gotovo := make(chan struct{})
	var vyzovy []bool
	go func() {
		defer close(gotovo)
		oprashivatSostoyanie(time.Millisecond, func() (string, bool) { return "", false },
			func(string) string { t.Error("спрашивать некого"); return "" },
			func(p bool) { vyzovy = append(vyzovy, p) }, stop)
	}()
	time.Sleep(20 * time.Millisecond)
	close(stop)
	<-gotovo
	if len(vyzovy) != 0 {
		t.Fatalf("вызовы %v, ждали ни одного", vyzovy)
	}
}

func TestOprosZnachkaStopVyhodit(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	gotovo := make(chan struct{})
	go func() {
		oprashivatSostoyanie(time.Hour, func() (string, bool) { return "", false },
			func(string) string { return "" }, func(bool) {}, stop)
		close(gotovo)
	}()
	select {
	case <-gotovo:
	case <-time.After(5 * time.Second):
		t.Fatal("stop не остановил опрос")
	}
}
