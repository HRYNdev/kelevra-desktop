package sluzhba

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

type kluchKto4 struct{}

// Ревью, круг 4. A держит пробу (кнопка, ctx Background), B ждёт на замке
// (второе окно / авторежим / вторая кнопка, тоже Background). Человек жмёт
// «Отключить»: поколение растёт, Ostanovit роняет старт A (ErrOstanovleno).
// A отпускает пробу. B получает замок — поднимет ли он связь ПОСЛЕ
// «Отключить»?
func TestRev4OtklyuchitPokaBZhdyot(t *testing.T) {
	s := stendZamenyYadra(t)
	var mu sync.Mutex
	var poryadok []string
	aVnutri := make(chan struct{})
	razreshitA := make(chan struct{})
	s.zapustitYadro = func(ctx context.Context) error {
		kto, _ := ctx.Value(kluchKto4{}).(string)
		chto := chtoVYadre(t, s)
		mu.Lock()
		poryadok = append(poryadok, kto+":"+chto)
		mu.Unlock()
		if kto == "A" {
			close(aVnutri)
			<-razreshitA
			return yadro.ErrOstanovleno
		}
		return nil
	}
	ctxA := context.WithValue(context.Background(), kluchKto4{}, "A")
	ctxB := context.WithValue(context.Background(), kluchKto4{}, "B")
	doneA := make(chan error, 1)
	go func() { doneA <- s.PodnyatZashchitu(ctxA) }()
	<-aVnutri
	doneB := make(chan error, 1)
	go func() { doneB <- s.PodnyatZashchitu(ctxB) }()
	time.Sleep(200 * time.Millisecond)
	// «Отключить»: как otklyuchit — поколение растёт, ядро гасится.
	s.avtorezhimZamok.Lock()
	s.avtorezhimPokolenie++
	s.avtorezhimZamok.Unlock()
	close(razreshitA)
	errA, errB := <-doneA, <-doneB
	mu.Lock()
	defer mu.Unlock()
	t.Logf("A=%v B=%v порядок=%v на месте=%q", errA, errB, poryadok, chtoVYadre(t, s))
	for _, z := range poryadok {
		if len(z) > 2 && z[:2] == "B:" {
			t.Fatalf("после «Отключить» B запустил ядро (%s) и поднял связь: %v", z, poryadok)
		}
	}
	if !errors.Is(errB, OshibkaReshenieSmenilos) {
		t.Fatalf("B: %v, ждал OshibkaReshenieSmenilos", errB)
	}
	if got := chtoVYadre(t, s); got != "novoe" {
		t.Fatalf("проба не сохранилась для следующего подъёма: на месте %q", got)
	}
}

// Откат не удался, а других подписанных сборок нет: что остаётся на машине.
func TestRev4NetNiOdnoyNeOtvergnutoy(t *testing.T) {
	s := stendZamenyYadra(t)
	var poryadok []string
	s.zapustitYadro = func(ctx context.Context) error {
		chto := chtoVYadre(t, s)
		poryadok = append(poryadok, chto)
		if chto == "novoe" {
			// .prezhnee пропал (антивирус) посреди пробы
			_ = os.Remove(s.Yadro.Bin + ".prezhnee")
			return errors.New("ядро упало при старте: FATAL bad core")
		}
		return nil
	}
	err := s.PodnyatZashchitu(context.Background())
	_, estBin := os.Stat(s.Yadro.Bin)
	_, estOtv := os.Stat(s.Yadro.Bin + ".otvergnutoe")
	t.Logf("первый подъём: err=%v запуски=%v ядро есть=%v otvergnutoe есть=%v", err, poryadok, estBin == nil, estOtv == nil)
	err2 := s.PodnyatZashchitu(context.Background())
	_, estBin2 := os.Stat(s.Yadro.Bin)
	t.Logf("второй подъём: err=%v запуски=%v ядро есть=%v", err2, poryadok, estBin2 == nil)
	if err == nil || err2 == nil {
		t.Fatalf("связь поднялась без ядра?")
	}
	if estBin2 == nil {
		t.Fatalf("на месте ядра что-то стоит: %q", chtoVYadre(t, s))
	}
	if estOtv != nil {
		t.Fatal("упавшая сборка не отодвинута")
	}
	if !errors.Is(err2, yadro.ErrNetNeOtvergnutoy) {
		t.Fatalf("второй подъём: %v, ждал понятную причину ErrNetNeOtvergnutoy", err2)
	}
}

// Подъём ждёт чужую пробу не дольше srokOzhidaniyaSmeny и возвращает
// понятную причину, а не висит.
func TestOzhidanieProbyOgranicheno(t *testing.T) {
	s := stendZamenyYadra(t)
	staryy := srokOzhidaniyaSmeny
	srokOzhidaniyaSmeny = 200 * time.Millisecond
	t.Cleanup(func() { srokOzhidaniyaSmeny = staryy })
	s.zamokSmenyYadra.Lock() // проба у другого подъёма
	defer s.zamokSmenyYadra.Unlock()
	nachalo := time.Now()
	err := s.PodnyatZashchitu(context.Background())
	if !errors.Is(err, OshibkaIdetObnovlenieYadra) {
		t.Fatalf("ошибка %v, ждал OshibkaIdetObnovlenieYadra", err)
	}
	if time.Since(nachalo) > 2*time.Second {
		t.Fatalf("ждал %s", time.Since(nachalo))
	}
}

// Панику держателя пробы: проба отпущена, замок отпущен, следующий подъём идёт.
func TestRev4PanikaOtpuskaet(t *testing.T) {
	s := stendZamenyYadra(t)
	n := 0
	s.zapustitYadro = func(ctx context.Context) error {
		n++
		if n == 1 {
			panic("бах")
		}
		return nil
	}
	func() {
		defer func() { _ = recover() }()
		_ = s.PodnyatZashchitu(context.Background())
	}()
	done := make(chan error, 1)
	go func() { done <- s.PodnyatZashchitu(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("второй подъём: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("замок не отпущен после паники")
	}
	if v := s.Yadro.Versiya(); v != "core-v2-1" {
		t.Fatalf("после паники и второго подъёма версия %q", v)
	}
}
