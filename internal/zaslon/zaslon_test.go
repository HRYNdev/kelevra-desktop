package zaslon

import (
	"bytes"
	"errors"
	"log"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

// lozhnaya — система-подмена: настоящего WFP в проверках нет и быть не должно.
type lozhnaya struct {
	mu         sync.Mutex
	filtry     []Filtr
	estSessiya bool
	primeneniy int
	snyatiy    int
	oshibka    error
}

func (l *lozhnaya) Primenit(f []Filtr) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.primeneniy++
	if l.oshibka != nil {
		return l.oshibka
	}
	l.filtry = append([]Filtr(nil), f...)
	l.estSessiya = true
	return nil
}

func (l *lozhnaya) Snyat() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.snyatiy++
	l.filtry, l.estSessiya = nil, false
	return nil
}

// polnyy — стоит ли «запрет остального исходящего».
func (l *lozhnaya) polnyy() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return estZapretVsego(l.filtry)
}

func (l *lozhnaya) anthropic() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, f := range l.filtry {
		if f.Ves == VesAnthropicZapret && f.Zapret {
			return true
		}
	}
	return false
}

func (l *lozhnaya) pusto() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.filtry) == 0
}

func (l *lozhnaya) schet() (primeneniy, snyatiy int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.primeneniy, l.snyatiy
}

func estZapretVsego(f []Filtr) bool {
	for _, x := range f {
		if x.Zapret && x.Ves == VesZapreta && len(x.Usloviya) == 0 {
			return true
		}
	}
	return false
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

// stend — заслон на подмене с управляемым входом.
type stend struct {
	mu sync.Mutex
	v  Vhod
}

func (s *stend) vhod() Vhod {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v
}

func (s *stend) zadat(v Vhod) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.v = v
}

func novyyZaslon(sys Sistema, st *stend) *Zaslon {
	return &Zaslon{
		Sys:  sys,
		Vhod: st.vhod,
		Dannye: func() Dannye {
			return Dannye{
				Tunnel:    []netip.Prefix{netip.MustParsePrefix("172.19.0.0/30"), netip.MustParsePrefix("fdfe:dcba:9876::/126")},
				Programmy: []string{`C:\k\sing-box.exe`, `C:\k\Kelevra.exe`},
			}
		},
	}
}

func TestZakrytIOtkrytPriPodyome(t *testing.T) {
	zh := perehvatitZhurnal(t)
	sys := &lozhnaya{}
	z := novyyZaslon(sys, &stend{})
	if !z.Zakryt() {
		t.Fatal("запрет не встал")
	}
	if !sys.polnyy() || !z.Zakryto() || !z.Perezapusk() {
		t.Fatal("после Zakryt нет полного запрета")
	}
	// Повтор при идущем перезапуске — без второго обращения к системе.
	if pr, _ := sys.schet(); !z.Zakryt() || pr != 1 {
		t.Fatalf("повторное закрытие дёрнуло систему (%d раз)", pr)
	}
	z.OtkrytPodnyat()
	if !sys.pusto() || z.Zakryto() || z.Perezapusk() {
		t.Fatal("туннель поднят, а запрет стоит (намерения нет — должно быть пусто)")
	}
	z.OtkrytPodnyat() // повтор — ничего
	if _, sn := sys.schet(); sn != 1 {
		t.Fatalf("снятий %d, ждали 1", sn)
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

// Подключено: после перезапуска остаётся уровень 1, а не пусто.
func TestPoslePerezapuskaOstayotsyaUroven1(t *testing.T) {
	zh := perehvatitZhurnal(t)
	sys := &lozhnaya{}
	st := &stend{v: Vhod{Namerenie: true}}
	z := novyyZaslon(sys, st)
	if err := z.Obnovit("старт"); err != nil {
		t.Fatal(err)
	}
	if !sys.anthropic() || sys.polnyy() {
		t.Fatal("подключено: ждали только уровень 1")
	}
	z.Zakryt()
	if !sys.polnyy() || !sys.anthropic() {
		t.Fatal("перезапуск: ждали полный запрет вместе с Anthropic")
	}
	z.OtkrytPodnyat()
	if sys.polnyy() || !sys.anthropic() {
		t.Fatal("после подъёма: ждали уровень 1")
	}
	if _, sn := sys.schet(); sn != 0 {
		t.Fatalf("при смене правил сессию закрывали (%d) — щель", sn)
	}
	if !strings.Contains(zh.String(), "открыт: туннель поднят за ") || !strings.Contains(zh.String(), "(Anthropic только через туннель)") {
		t.Errorf("нет строки итога с уровнем 1:\n%s", zh.String())
	}
}

// Строгий: временный запрет кончился по сроку — полный остаётся.
func TestStrogiyPoSrokuOstayotsyaZakrytym(t *testing.T) {
	zh := perehvatitZhurnal(t)
	sys := &lozhnaya{}
	st := &stend{v: Vhod{Namerenie: true, Strogiy: true}}
	z := novyyZaslon(sys, st)
	z.Srok = 20 * time.Millisecond
	_ = z.Obnovit("старт")
	z.Zakryt()
	srok := time.Now().Add(5 * time.Second)
	for z.Perezapusk() && time.Now().Before(srok) {
		time.Sleep(5 * time.Millisecond)
	}
	if z.Perezapusk() {
		t.Fatal("временный запрет не кончился по сроку")
	}
	if !sys.polnyy() {
		t.Fatal("строгий: после срока полного запрета нет")
	}
	if !strings.Contains(zh.String(), "полный запрет остаётся") {
		t.Errorf("нет строки «полный запрет остаётся»:\n%s", zh.String())
	}
}

// Предохранитель: туннель не поднялся — временный запрет снимается сам.
func TestPredohranitelPoSroku(t *testing.T) {
	zh := perehvatitZhurnal(t)
	sys := &lozhnaya{}
	z := novyyZaslon(sys, &stend{})
	z.Srok = 30 * time.Millisecond
	if !z.Zakryt() {
		t.Fatal("запрет не встал")
	}
	srok := time.Now().Add(5 * time.Second)
	for sys.polnyy() && time.Now().Before(srok) {
		time.Sleep(5 * time.Millisecond)
	}
	if sys.polnyy() || z.Zakryto() {
		t.Fatal("запрет не снялся по сроку")
	}
	if !strings.Contains(zh.String(), "открыт по сроку") || !strings.Contains(zh.String(), "туннель не поднялся") {
		t.Errorf("нет строки о снятии по сроку:\n%s", zh.String())
	}
	// Поздний подъём после срока — ничего не трогает.
	z.OtkrytPodnyat()
	if _, sn := sys.schet(); sn != 1 {
		t.Fatalf("снятий %d, ждали 1", sn)
	}
}

// Срок прошлого перезапуска не снимает новый.
func TestStaryySrokNeTrogaetNovyyZapret(t *testing.T) {
	perehvatitZhurnal(t)
	sys := &lozhnaya{}
	z := novyyZaslon(sys, &stend{})
	z.Srok = time.Hour
	z.Zakryt()
	z.mu.Lock()
	staryy := z.nomer
	z.mu.Unlock()
	z.Otkryt("проверка")
	z.Zakryt()
	z.poSroku(staryy, time.Hour) // срабатывание таймера прошлого закрытия
	if !z.Zakryto() || !sys.polnyy() {
		t.Fatal("таймер прошлого закрытия снял новый запрет")
	}
	z.Otkryt("конец")
}

// Ошибка системы — перезапуск идёт как раньше: запрета нет, строка в журнал.
func TestOshibkaSistemyNeLomaet(t *testing.T) {
	zh := perehvatitZhurnal(t)
	sys := &lozhnaya{oshibka: errors.New("WFP недоступен")}
	z := novyyZaslon(sys, &stend{})
	if z.Zakryt() {
		t.Fatal("при ошибке системы Zakryt сказал «закрыто»")
	}
	if z.Zakryto() || z.Perezapusk() {
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

// Отказ WFP на уровне 1 — повтор сам, без нового события.
func TestOshibkaPovtoryaetsya(t *testing.T) {
	perehvatitZhurnal(t)
	sys := &lozhnaya{oshibka: errors.New("BFE занят")}
	z := novyyZaslon(sys, &stend{v: Vhod{Namerenie: true}})
	if err := z.Obnovit("старт"); err == nil {
		t.Fatal("ошибка системы не дошла")
	}
	sys.mu.Lock()
	sys.oshibka = nil
	sys.mu.Unlock()
	srok := time.Now().Add(PovtorPosleOshibki + 5*time.Second)
	for !sys.anthropic() && time.Now().Before(srok) {
		time.Sleep(50 * time.Millisecond)
	}
	if !sys.anthropic() {
		t.Fatal("уровень 1 не встал повтором")
	}
}

// «Отключить» снимает всё сразу — и уровень 1, и строгий.
func TestOtklyuchitSnimaetVsyo(t *testing.T) {
	perehvatitZhurnal(t)
	sys := &lozhnaya{}
	st := &stend{v: Vhod{Namerenie: true, Strogiy: true}}
	z := novyyZaslon(sys, st)
	_ = z.Obnovit("старт")
	if !sys.polnyy() {
		t.Fatal("строгий не встал")
	}
	st.zadat(Vhod{Namerenie: false, Strogiy: true})
	_ = z.Obnovit("«Отключить»")
	if !sys.pusto() || sys.estSessiya {
		t.Fatal("после «Отключить» что-то осталось")
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
	_ = nichto.Obnovit("x")
	if !nichto.Primeneno().Pusto() {
		t.Fatal("nil-заслон что-то держит")
	}
	pustoy := &Zaslon{}
	if pustoy.Zakryt() {
		t.Fatal("заслон без системы закрылся")
	}
	pustoy.Otkryt("x")
}

func TestOtkrytSPrichinoy(t *testing.T) {
	zh := perehvatitZhurnal(t)
	sys := &lozhnaya{}
	z := novyyZaslon(sys, &stend{})
	z.Zakryt()
	z.Otkryt("решение о связи сменилось")
	if sys.polnyy() {
		t.Fatal("запрет не снят")
	}
	if !strings.Contains(zh.String(), ": решение о связи сменилось") {
		t.Errorf("нет причины в журнале:\n%s", zh.String())
	}
}

// Тот же вход — система не дёргается (идемпотентность приведения).
func TestTotZheVhodNeDyorgaet(t *testing.T) {
	perehvatitZhurnal(t)
	sys := &lozhnaya{}
	z := novyyZaslon(sys, &stend{v: Vhod{Namerenie: true}})
	for i := 0; i < 5; i++ {
		_ = z.Obnovit("заход")
	}
	if pr, _ := sys.schet(); pr != 1 {
		t.Fatalf("одни и те же правила ставились %d раз", pr)
	}
}

// Неверные адреса пульта: берутся понятые, а ни одного — умолчание.
func TestAdresaPulta(t *testing.T) {
	perehvatitZhurnal(t)
	sys := &lozhnaya{}
	seti := []string{"мусор", "1.2.3.0/24"}
	z := &Zaslon{Sys: sys, Vhod: func() Vhod { return Vhod{Namerenie: true} },
		Dannye: func() Dannye { return Dannye{Seti: seti} }}
	_ = z.Obnovit("x")
	p := z.Primeneno()
	if len(p.Seti) != 1 || p.Seti[0].String() != "1.2.3.0/24" {
		t.Fatalf("адреса %v, ждали только 1.2.3.0/24", p.Seti)
	}
	seti = []string{"мусор"}
	_ = z.Obnovit("y")
	p = z.Primeneno()
	if len(p.Seti) != len(AnthropicPoUmolchaniyu) || p.Seti[0].String() != "160.79.104.0/23" {
		t.Fatalf("при негодном списке адреса %v, ждали умолчание", p.Seti)
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
