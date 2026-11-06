package sluzhba

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/zaslon"
)

// zaslonStenda — система запрета на подмене: настоящих фильтров WFP в
// проверках нет и быть не должно (сеть машины проверяющего не меняется).
// stoit — стоит ли полный запрет («запрет остального исходящего»), zakrytiy —
// сколько раз он ставился.
type zaslonStenda struct {
	mu       sync.Mutex
	filtry   []zaslon.Filtr
	zakrytiy int
	oshibka  error
}

func polnyyVFiltrah(ff []zaslon.Filtr) bool {
	for _, f := range ff {
		if f.Zapret && f.Ves == zaslon.VesZapreta && len(f.Usloviya) == 0 {
			return true
		}
	}
	return false
}

func (z *zaslonStenda) Primenit(ff []zaslon.Filtr) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.oshibka != nil {
		return z.oshibka
	}
	z.filtry = append([]zaslon.Filtr(nil), ff...)
	if polnyyVFiltrah(ff) {
		z.zakrytiy++
	}
	return nil
}

func (z *zaslonStenda) Snyat() error {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.filtry = nil
	return nil
}

func (z *zaslonStenda) sostoyanie() (stoit bool, zakrytiy int) {
	z.mu.Lock()
	defer z.mu.Unlock()
	return polnyyVFiltrah(z.filtry), z.zakrytiy
}

// anthropic — стоит ли запрет Anthropic мимо туннеля (уровень 1).
func (z *zaslonStenda) anthropic() bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	for _, f := range z.filtry {
		if f.Zapret && f.Ves == zaslon.VesAnthropicZapret {
			return true
		}
	}
	return false
}

func (z *zaslonStenda) pusto() bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	return len(z.filtry) == 0
}

// stendZaslona — служба с поднятой связью, подменённым запуском ядра и
// запретом на подмене. zapusk — что делает «запуск ядра»; видел — стоял ли
// запрет в момент каждого запуска.
func stendZaslona(t *testing.T, zapusk func() error) (*Sluzhba, *zaslonStenda, *syncBuffer, *[]bool) {
	t.Helper()
	s := gotovStendLestnicy(t)
	if s.zaslon == nil || s.zaslon.Sys != nil {
		t.Fatal("на стенде (KELEVRA_DIR) служба взяла настоящий WFP")
	}
	sys := &zaslonStenda{}
	s.zaslon = &zaslon.Zaslon{Sys: sys, Vhod: s.vhodZaslona, Dannye: s.dannyeZaslona}
	s.zashchitaPodnyataDlyaStenda = func() bool { return true }
	var mu sync.Mutex
	videl := &[]bool{}
	s.zapustitYadro = func(ctx context.Context) error {
		stoit, _ := sys.sostoyanie()
		mu.Lock()
		*videl = append(*videl, stoit)
		mu.Unlock()
		if zapusk != nil {
			return zapusk()
		}
		return nil
	}
	zhurnal := &syncBuffer{}
	log.SetOutput(zhurnal)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return s, sys, zhurnal, videl
}

// Самоизлечение «Дома»: от «погасить» до «туннель поднят» запрет стоит,
// после подъёма снят.
func TestPerezapuskZakryvaetDoPodyoma(t *testing.T) {
	s, sys, zhurnal, videl := stendZaslona(t, nil)
	s.perezapustit("проверка", s.pokolenieAvtorezhima(), true)
	if len(*videl) != 1 || !(*videl)[0] {
		t.Fatalf("при запуске ядра запрет не стоял: %v", *videl)
	}
	if stoit, _ := sys.sostoyanie(); stoit || s.zaslon.Zakryto() {
		t.Fatal("туннель поднят, а запрет остался")
	}
	for _, nuzhno := range []string{
		"перезапуск ядра: трафик мимо туннеля закрыт до подъёма",
		"перезапуск ядра: трафик мимо туннеля открыт: туннель поднят за ",
	} {
		if !strings.Contains(zhurnal.String(), nuzhno) {
			t.Errorf("в журнале нет «%s»:\n%s", nuzhno, zhurnal.String())
		}
	}
	// Закрытие — раньше гашения ядра.
	tekst := zhurnal.String()
	if i, j := strings.Index(tekst, "закрыт до подъёма"), strings.Index(tekst, "дом: проверка"); i < 0 || j < 0 || i < j {
		t.Errorf("порядок строк: запрет должен встать после решения о перезапуске и до подъёма:\n%s", tekst)
	}
}

// Ядро после перезапуска не поднялось — запрет снят сразу, с причиной.
func TestPerezapuskBezPodyomaOtkryvaet(t *testing.T) {
	s, sys, zhurnal, videl := stendZaslona(t, func() error { return errors.New("ядро упало при старте: проверка") })
	s.perezapustit("проверка", s.pokolenieAvtorezhima(), true)
	if len(*videl) == 0 || !(*videl)[0] {
		t.Fatalf("при запуске ядра запрет не стоял: %v", *videl)
	}
	if stoit, _ := sys.sostoyanie(); stoit {
		t.Fatal("подъём не удался, а запрет остался")
	}
	if !strings.Contains(zhurnal.String(), "трафик мимо туннеля открыт через ") || !strings.Contains(zhurnal.String(), "связь не поднялась") {
		t.Errorf("нет строки о снятии с причиной:\n%s", zhurnal.String())
	}
}

// Предохранитель: ядро поднимается дольше срока — запрет снимается сам, а
// поздний подъём ничего уже не трогает.
func TestPerezapuskPredohranitel(t *testing.T) {
	var sys *zaslonStenda
	s, sys, zhurnal, _ := stendZaslona(t, func() error {
		srok := time.Now().Add(5 * time.Second)
		for time.Now().Before(srok) {
			if stoit, _ := sys.sostoyanie(); !stoit {
				return nil
			}
			time.Sleep(5 * time.Millisecond)
		}
		return errors.New("запрет не снялся по сроку")
	})
	s.zaslon.Srok = 30 * time.Millisecond
	s.perezapustit("проверка", s.pokolenieAvtorezhima(), true)
	if stoit, _ := sys.sostoyanie(); stoit {
		t.Fatal("запрет остался после срока")
	}
	tekst := zhurnal.String()
	if !strings.Contains(tekst, "открыт по сроку") || !strings.Contains(tekst, "туннель не поднялся") {
		t.Errorf("нет строки о снятии по сроку:\n%s", tekst)
	}
	if strings.Contains(tekst, "туннель поднят за") {
		t.Errorf("после срока запрет «открыт» второй раз:\n%s", tekst)
	}
}

// WFP недоступен — перезапуск идёт как раньше: ядро поднимается, строка в журнал.
func TestOshibkaZaslonaNeLomaetPerezapusk(t *testing.T) {
	s, sys, zhurnal, videl := stendZaslona(t, nil)
	sys.oshibka = errors.New("WFP недоступен")
	s.perezapustit("проверка", s.pokolenieAvtorezhima(), true)
	if len(*videl) != 1 || (*videl)[0] {
		t.Fatalf("запуски ядра %v, ждали один без запрета", *videl)
	}
	if !strings.Contains(zhurnal.String(), "закрыть трафик мимо туннеля не вышло (WFP недоступен) — перезапускаю как раньше") {
		t.Errorf("нет строки об ошибке:\n%s", zhurnal.String())
	}
	if !strings.Contains(zhurnal.String(), "поднят туннель") {
		t.Errorf("ядро не поднято:\n%s", zhurnal.String())
	}
}

// «Отключить» посреди перезапуска: запрет снимается сразу (связь опущена —
// трафик идёт напрямую), подъём на нём не держится.
func TestOtklyuchitPosredinePerezapuskaSnimaetZapret(t *testing.T) {
	var s *Sluzhba
	var posleOtklyucheniya bool
	s, sys, _, _ := stendZaslona(t, func() error {
		s.avtorezhimZamok.Lock()
		s.avtorezhimPokolenie++
		s.otklyucheniy++
		s.avtorezhimZamok.Unlock()
		_ = s.OpustitZashchitu()
		posleOtklyucheniya = s.zaslon.Zakryto()
		return errors.New("запуск ядра прерван: ядро остановили посреди старта")
	})
	s.perezapustit("проверка", s.pokolenieAvtorezhima(), true)
	if posleOtklyucheniya {
		t.Fatal("после «Отключить» запрет остался стоять")
	}
	if stoit, _ := sys.sostoyanie(); stoit {
		t.Fatal("запрет остался после перезапуска")
	}
}

// Решение о связи сменилось раньше перезапуска: ядро не трогается, запрета нет.
func TestResheniyeSmenilosBezZapreta(t *testing.T) {
	s, sys, zhurnal, videl := stendZaslona(t, nil)
	pokolenie := s.pokolenieAvtorezhima()
	s.avtorezhimZamok.Lock()
	s.avtorezhimPokolenie++
	s.avtorezhimZamok.Unlock()
	// Решение сменилось ещё до начала — ядро не трогается и запрет не ставится.
	s.perezapustit("проверка", pokolenie, true)
	if _, zakrytiy := sys.sostoyanie(); zakrytiy != 0 || len(*videl) != 0 {
		t.Fatalf("решение сменилось до перезапуска, а запрет ставился (%d) или ядро поднималось (%v)", zakrytiy, *videl)
	}
	if strings.Contains(zhurnal.String(), "закрыт до подъёма") {
		t.Errorf("лишняя строка о запрете:\n%s", zhurnal.String())
	}
}

// Обычное «Подключиться» из выключенного — не перезапуск: запрета нет.
func TestPodyomIzVyklyuchennogoBezZapreta(t *testing.T) {
	s, sys, _, videl := stendZaslona(t, nil)
	s.zashchitaPodnyataDlyaStenda = func() bool { return false }
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("подъём: %v", err)
	}
	if _, zakrytiy := sys.sostoyanie(); zakrytiy != 0 || len(*videl) != 1 || (*videl)[0] {
		t.Fatalf("обычный подъём поставил запрет (закрытий %d, запуски %v)", zakrytiy, *videl)
	}
	// «Отключить» запрета не ставит.
	if err := s.OpustitZashchitu(); err != nil {
		t.Fatalf("опускание: %v", err)
	}
	if _, zakrytiy := sys.sostoyanie(); zakrytiy != 0 {
		t.Fatalf("«Отключить» поставил запрет (%d)", zakrytiy)
	}
}

// Подъём после смены версии (прошлая копия держала туннель): запрет до
// подъёма; следа туннеля нет — ничего.
func TestPosleSmenyVersiiZakryvaet(t *testing.T) {
	s, sys, zhurnal, videl := stendZaslona(t, nil)
	s.podnyatSvyazPosleSmenyVersii(context.Background(), "")
	if _, zakrytiy := sys.sostoyanie(); zakrytiy != 0 || len(*videl) != 0 {
		t.Fatalf("без следа туннеля: закрытий %d, запуски %v", zakrytiy, *videl)
	}
	s.podnyatSvyazPosleSmenyVersii(context.Background(), "tun125")
	if len(*videl) != 1 || !(*videl)[0] {
		t.Fatalf("при подъёме после смены версии запрет не стоял: %v", *videl)
	}
	if stoit, _ := sys.sostoyanie(); stoit {
		t.Fatal("запрет остался после подъёма")
	}
	for _, nuzhno := range []string{"закрыт до подъёма", "туннель поднят за", "связь после смены версии поднята"} {
		if !strings.Contains(zhurnal.String(), nuzhno) {
			t.Errorf("в журнале нет «%s»:\n%s", nuzhno, zhurnal.String())
		}
	}
}
