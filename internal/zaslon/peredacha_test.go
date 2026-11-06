package zaslon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Передача держателю: правила встают в его сессии, метка готовности есть,
// новая копия удаляет файл — держатель снимает своё и уходит.
func TestDerzhatelDoOtpuska(t *testing.T) {
	perehvatitZhurnal(t)
	papka := t.TempDir()
	p := Politika{Anthropic: true, Seti: setiA, Tunnel: tunnelA}
	if err := ZapisatPeredachu(papka, Peredacha{Politika: p, Namerenie: true, Kogda: time.Now(), Do: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	sys := &lozhnaya{}
	gotovo := make(chan error, 1)
	go func() { gotovo <- Derzhat(papka, sys, 5*time.Millisecond, time.Now) }()
	if !ZhdatGotovnosti(papka, 5*time.Second, 5*time.Millisecond) {
		t.Fatal("держатель не отметил готовность")
	}
	if !sys.anthropic() {
		t.Fatal("у держателя правила не встали")
	}
	// Новая копия прочла передачу — намерение то же.
	if pr, est := ProchestPeredachu(papka, time.Now()); !est || !pr.Namerenie || !pr.Politika.Anthropic {
		t.Fatalf("передача не прочитана: %+v %v", pr, est)
	}
	OtpustitDerzhatelya(papka)
	select {
	case err := <-gotovo:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("держатель не ушёл после отпуска")
	}
	if !sys.pusto() || sys.estSessiya {
		t.Fatal("держатель ушёл, не сняв правила")
	}
	if _, err := os.Stat(filepath.Join(papka, ImyaGotovnosti)); !os.IsNotExist(err) {
		t.Error("метка готовности осталась")
	}
}

// Новая копия не поднялась: держатель уходит по сроку и убирает передачу.
func TestDerzhatelPoSroku(t *testing.T) {
	perehvatitZhurnal(t)
	papka := t.TempDir()
	nachalo := time.Now()
	_ = ZapisatPeredachu(papka, Peredacha{Politika: Politika{Polnyy: true}, Do: nachalo.Add(50 * time.Millisecond)})
	sys := &lozhnaya{}
	if err := Derzhat(papka, sys, 5*time.Millisecond, time.Now); err != nil {
		t.Fatal(err)
	}
	if !sys.pusto() {
		t.Fatal("по сроку правила не сняты")
	}
	if _, est := ProchestPeredachu(papka, time.Now().Add(-time.Hour)); est {
		t.Error("передача осталась лежать")
	}
}

// Просроченная передача не читается (стёртая уборкой не нужна: упал процесс,
// а файл остался — новая копия его не примет за свежий).
func TestPeredachaProsrochena(t *testing.T) {
	papka := t.TempDir()
	_ = ZapisatPeredachu(papka, Peredacha{Namerenie: true, Do: time.Now().Add(-time.Second)})
	if _, est := ProchestPeredachu(papka, time.Now()); est {
		t.Fatal("просроченная передача прочитана")
	}
	if err := Derzhat(papka, &lozhnaya{}, time.Millisecond, time.Now); err == nil {
		t.Fatal("держатель взялся за просроченную передачу")
	}
}

// Отказ WFP у держателя — ошибка без метки готовности: старая копия не ждёт.
func TestDerzhatelOshibka(t *testing.T) {
	perehvatitZhurnal(t)
	papka := t.TempDir()
	_ = ZapisatPeredachu(papka, Peredacha{Politika: Politika{Anthropic: true, Seti: setiA}, Do: time.Now().Add(time.Minute)})
	sys := &lozhnaya{oshibka: os.ErrPermission}
	if err := Derzhat(papka, sys, time.Millisecond, time.Now); err == nil {
		t.Fatal("ошибка WFP не дошла")
	}
	if ZhdatGotovnosti(papka, 10*time.Millisecond, time.Millisecond) {
		t.Fatal("метка готовности без правил")
	}
}
