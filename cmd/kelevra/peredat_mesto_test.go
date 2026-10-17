package main

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/sluzhba"
)

// Служебное в сеансе человека, увидев поставленную службу Windows, обязано
// уйти: туннель держит служба, второго хозяина у связи быть не должно. Уходит
// штатным путём (канал «Выход»), а значок поднимает отдельным процессом — у
// службы своего значка нет.
func TestSluzhebnoeUstupaetMestoSluzhbeWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("служба Windows есть только в Windows")
	}
	t.Setenv("KELEVRA_DIR", t.TempDir())
	// Уборка прокси старой версии при опускании связи — только чтение реестра.
	t.Setenv("KELEVRA_PROKSI", "net")
	s, err := sluzhba.Novaya()
	if err != nil {
		t.Fatal(err)
	}

	var sprosov atomic.Int32
	staroeVstala := sluzhbaWindowsVstala
	defer func() { sluzhbaWindowsVstala = staroeVstala }()
	sluzhbaWindowsVstala = func(svoy string) (string, bool) {
		// Первые два взгляда — службы ещё нет (человек думает над окном прав).
		if sprosov.Add(1) < 3 {
			return "", false
		}
		return "http://127.0.0.1:1/sluzhba/", true
	}
	var znachkov atomic.Int32
	staryyZnachok := zapustitZnachok
	defer func() { zapustitZnachok = staryyZnachok }()
	zapustitZnachok = func() { znachkov.Add(1) }

	uyti := make(chan struct{}, 1)
	ctx, otmena := context.WithTimeout(context.Background(), 10*time.Second)
	defer otmena()
	gotovo := make(chan struct{})
	go func() {
		peredatMestoSluzhbeWindows(ctx, s, "http://127.0.0.1:2/svoy/", uyti)
		close(gotovo)
	}()
	select {
	case <-uyti:
	case <-ctx.Done():
		t.Fatal("служба Windows встала, а служебное в сеансе человека так и не ушло — у связи два хозяина")
	}
	<-gotovo
	if znachkov.Load() != 1 {
		t.Fatalf("значок поднят %d раз — ждали ровно один (у службы значка нет)", znachkov.Load())
	}
}

// Пока службы нет, служебное живёт и ждёт: без него окну не с кем говорить.
func TestSluzhebnoeBezSluzhbyNeUhodit(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("служба Windows есть только в Windows")
	}
	staroeVstala := sluzhbaWindowsVstala
	defer func() { sluzhbaWindowsVstala = staroeVstala }()
	sluzhbaWindowsVstala = func(string) (string, bool) { return "", false }

	uyti := make(chan struct{}, 1)
	ctx, otmena := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer otmena()
	peredatMestoSluzhbeWindows(ctx, nil, "http://127.0.0.1:2/svoy/", uyti)
	select {
	case <-uyti:
		t.Fatal("службы нет, а служебное ушло — окно осталось бы без службы")
	default:
	}
}
