package zaslon

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

// lozhnaya — система-подмена: настоящего WFP в проверках нет и быть не должно.
type lozhnaya struct {
	mu        sync.Mutex
	zakryto   bool
	zakrytiy  int
	otkrytiy  int
	programmy []string
	oshibka   error
}

func (l *lozhnaya) Zakryt(p []string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.zakrytiy++
	l.programmy = p
	if l.oshibka != nil {
		return l.oshibka
	}
	l.zakryto = true
	return nil
}

func (l *lozhnaya) Otkryt() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.otkrytiy++
	l.zakryto = false
	return nil
}

func (l *lozhnaya) schet() (zakrytiy, otkrytiy int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.zakrytiy, l.otkrytiy
}

func (l *lozhnaya) stoit() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.zakryto
}

// zhurnal — журнал пакета log на время проверки.
type zhurnal struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (z *zhurnal) Write(p []byte) (int, error) {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.buf.Write(p)
}

func (z *zhurnal) String() string {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.buf.String()
}

func perehvatitZhurnal(t *testing.T) *zhurnal {
	t.Helper()
	z := &zhurnal{}
	staryy, flagi := log.Writer(), log.Flags()
	log.SetOutput(z)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(staryy)
		log.SetFlags(flagi)
	})
	return z
}

func TestZakrytIOtkrytPriPodyome(t *testing.T) {
	zh := perehvatitZhurnal(t)
	sys := &lozhnaya{}
	z := &Zaslon{Sys: sys, Programmy: func() []string { return []string{`C:\k\sing-box.exe`, `C:\k\Kelevra.exe`} }}
	if !z.Zakryt() {
		t.Fatal("запрет не встал")
	}
	if !sys.stoit() || !z.Zakryto() {
		t.Fatal("после Zakryt система не закрыта")
	}
	if len(sys.programmy) != 2 || !strings.HasSuffix(sys.programmy[0], "sing-box.exe") {
		t.Fatalf("разрешены не те программы: %v", sys.programmy)
	}
	// Повтор при стоящем запрете — без второго обращения к системе.
	if zk, _ := sys.schet(); !z.Zakryt() || zk != 1 {
		t.Fatalf("повторное закрытие дёрнуло систему (%d раз)", zk)
	}
	z.OtkrytPodnyat()
	if sys.stoit() || z.Zakryto() {
		t.Fatal("туннель поднят, а запрет стоит")
	}
	z.OtkrytPodnyat() // повтор — ничего
	if _, ot := sys.schet(); ot != 1 {
		t.Fatalf("снятий %d, ждали 1", ot)
	}
	tekst := zh.String()
	for _, nuzhno := range []string{
		"перезапуск ядра: трафик мимо туннеля закрыт до подъёма",
		"перезапуск ядра: трафик мимо туннеля открыт: туннель поднят за ",
	} {
		if !strings.Contains(tekst, nuzhno) {
			t.Errorf("в журнале нет строки «%s»:\n%s", nuzhno, tekst)
		}
	}
	if strings.Contains(strings.ToLower(tekst), "защит") {
		t.Errorf("в журнале слово «защита»:\n%s", tekst)
	}
}

// Предохранитель: туннель не поднялся — запрет снимается сам по сроку.
func TestPredohranitelPoSroku(t *testing.T) {
	zh := perehvatitZhurnal(t)
	sys := &lozhnaya{}
	z := &Zaslon{Sys: sys, Srok: 30 * time.Millisecond}
	if !z.Zakryt() {
		t.Fatal("запрет не встал")
	}
	srok := time.Now().Add(5 * time.Second)
	for sys.stoit() && time.Now().Before(srok) {
		time.Sleep(5 * time.Millisecond)
	}
	if sys.stoit() || z.Zakryto() {
		t.Fatal("запрет не снялся по сроку")
	}
	if !strings.Contains(zh.String(), "открыт по сроку") || !strings.Contains(zh.String(), "туннель не поднялся") {
		t.Errorf("нет строки о снятии по сроку:\n%s", zh.String())
	}
	// Поздний подъём после срока — ничего не трогает.
	z.OtkrytPodnyat()
	if _, ot := sys.schet(); ot != 1 {
		t.Fatalf("снятий %d, ждали 1", ot)
	}
}

// Срок прошлого закрытия не снимает новое.
func TestStaryySrokNeTrogaetNovyyZapret(t *testing.T) {
	perehvatitZhurnal(t)
	sys := &lozhnaya{}
	z := &Zaslon{Sys: sys, Srok: time.Hour}
	z.Zakryt()
	z.mu.Lock()
	staryy := z.nomer
	z.mu.Unlock()
	z.Otkryt("проверка")
	z.Zakryt()
	z.poSroku(staryy, time.Hour) // срабатывание таймера прошлого закрытия
	if !z.Zakryto() || !sys.stoit() {
		t.Fatal("таймер прошлого закрытия снял новый запрет")
	}
	z.Otkryt("конец")
}

// Ошибка системы — перезапуск идёт как раньше: запрета нет, строка в журнал.
func TestOshibkaSistemyNeLomaet(t *testing.T) {
	zh := perehvatitZhurnal(t)
	sys := &lozhnaya{oshibka: errors.New("WFP недоступен")}
	z := &Zaslon{Sys: sys}
	if z.Zakryt() {
		t.Fatal("при ошибке системы Zakryt сказал «закрыто»")
	}
	if z.Zakryto() {
		t.Fatal("при ошибке системы запрет числится стоящим")
	}
	if !strings.Contains(zh.String(), "закрыть трафик мимо туннеля не вышло (WFP недоступен) — перезапускаю как раньше") {
		t.Errorf("нет строки об ошибке:\n%s", zh.String())
	}
	z.OtkrytPodnyat() // без запрета — без строки «открыт»
	if strings.Contains(zh.String(), "открыт") {
		t.Errorf("открыт несуществующий запрет:\n%s", zh.String())
	}
}

// Без системы (не Windows, стенд) и nil — ничего не делать и не падать.
func TestBezSistemy(t *testing.T) {
	var nichto *Zaslon
	if nichto.Zakryt() || nichto.Zakryto() {
		t.Fatal("nil-заслон закрылся")
	}
	nichto.OtkrytPodnyat()
	nichto.Otkryt("x")
	pustoy := &Zaslon{}
	if pustoy.Zakryt() {
		t.Fatal("заслон без системы закрылся")
	}
	pustoy.Otkryt("x")
}

func TestOtkrytSPrichinoy(t *testing.T) {
	zh := perehvatitZhurnal(t)
	sys := &lozhnaya{}
	z := &Zaslon{Sys: sys}
	z.Zakryt()
	z.Otkryt("решение о связи сменилось")
	if sys.stoit() {
		t.Fatal("запрет не снят")
	}
	if !strings.Contains(zh.String(), ": решение о связи сменилось") {
		t.Errorf("нет причины в журнале:\n%s", zh.String())
	}
}

func TestSekundy(t *testing.T) {
	for d, nuzhno := range map[time.Duration]string{
		1800 * time.Millisecond: "1,8 с",
		60 * time.Second:        "60 с",
		2 * time.Second:         "2 с",
		300 * time.Millisecond:  "0,3 с",
	} {
		if est := sekundy(d); est != nuzhno {
			t.Errorf("sekundy(%s) = %q, ждали %q", d, est, nuzhno)
		}
	}
}
