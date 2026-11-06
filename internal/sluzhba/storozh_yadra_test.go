package sluzhba

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/lzheyadro"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Сторож ядра (storozh_yadra.go): работавшее ядро упало или зависло — служба
// поднимает его снова сама, независимо от авторежима, под запретом трафика
// мимо туннеля и с отсечкой частоты.

// korotkayaLestnica — лестница сторожа в миллисекундах на время проверки.
func korotkayaLestnica(t *testing.T, stupeni ...time.Duration) {
	t.Helper()
	bylo, byloStab := lestnicaStorozha, stabilnoPosle
	lestnicaStorozha, stabilnoPosle = stupeni, time.Hour
	t.Cleanup(func() { lestnicaStorozha, stabilnoPosle = bylo, byloStab })
}

// stendStorozha — служба на подменах (запуск ядра, запрет WFP); podnyato —
// «связь поднята» глазами службы, её ставит удачный подменный запуск.
type stendStorozha struct {
	s        *Sluzhba
	sys      *zaslonStenda
	zhurnal  *syncBuffer
	podnyato atomic.Bool
	mu       sync.Mutex
	zapuski  []time.Time
	videl    []bool // стоял ли запрет при каждом запуске
	otkazy   []error
}

func novyyStendStorozha(t *testing.T, otkazy ...error) *stendStorozha {
	t.Helper()
	st := &stendStorozha{otkazy: otkazy}
	st.s, st.sys, st.zhurnal, _ = stendZaslona(t, nil)
	st.s.zashchitaPodnyataDlyaStenda = st.podnyato.Load
	st.s.zapustitYadro = func(ctx context.Context) error {
		stoit, _ := st.sys.sostoyanie()
		st.mu.Lock()
		n := len(st.zapuski)
		st.zapuski = append(st.zapuski, time.Now())
		st.videl = append(st.videl, stoit)
		st.mu.Unlock()
		if n < len(st.otkazy) {
			return st.otkazy[n]
		}
		st.podnyato.Store(true)
		return nil
	}
	return st
}

func (st *stendStorozha) chisloZapuskov() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.zapuski)
}

func padenieDlyaProverki() yadro.Padenie {
	return yadro.Padenie{PID: 4242, Svoyo: true, Kod: 3, Hvost: "FATAL[0000] проверка: ядро упало", Kogda: time.Now()}
}

func TestStorozhPodnimaetUpavsheeYadroPodZapretom(t *testing.T) {
	korotkayaLestnica(t, 20*time.Millisecond)
	st := novyyStendStorozha(t)
	st.s.podnyatPoslePadeniya(context.Background(), padenieDlyaProverki())

	if n := st.chisloZapuskov(); n != 1 {
		t.Fatalf("запусков ядра %d, ждали 1", n)
	}
	if !st.videl[0] {
		t.Fatal("ядро поднималось без запрета трафика мимо туннеля")
	}
	if stoit, _ := st.sys.sostoyanie(); stoit {
		t.Fatal("ядро поднято, а запрет остался")
	}
	tekst := st.zhurnal.String()
	for _, nuzhno := range []string{
		"сторож: ядро упало само (pid 4242, своё, код выхода 3); причина из журнала ядра: FATAL[0000] проверка: ядро упало — поднимаю заново через 20ms (попытка 1 подряд)",
		"сторож: ядро снова работает",
	} {
		if !strings.Contains(tekst, nuzhno) {
			t.Errorf("в журнале нет «%s»:\n%s", nuzhno, tekst)
		}
	}
}

// Ядро не поднимается — попытки идут по лестнице, паузы растут; удачный
// подъём лестницу останавливает, а следующее падение продолжает её (ядро,
// падающее раз за разом, не крутится в петле). Проработало дольше
// stabilnoPosle — лестница сначала.
func TestStorozhLestnicaOtsechki(t *testing.T) {
	korotkayaLestnica(t, 30*time.Millisecond, 90*time.Millisecond, 200*time.Millisecond)
	beda := errors.New("ядро упало при старте: проверка")
	st := novyyStendStorozha(t, beda, beda, beda)
	nachalo := time.Now()
	st.s.podnyatPoslePadeniya(context.Background(), padenieDlyaProverki())

	if n := st.chisloZapuskov(); n != 4 {
		t.Fatalf("попыток %d, ждали 4 (три отказа и удача)", n)
	}
	// Паузы перед попытками: 30, 90, 200, 200 мс.
	zhdali := []time.Duration{30, 90, 200, 200}
	prev := nachalo
	for i, z := range st.zapuski {
		if d := z.Sub(prev); d < zhdali[i]*time.Millisecond {
			t.Errorf("попытка %d через %s, а лестница требует не меньше %dms", i+1, d, zhdali[i])
		}
		prev = z
	}
	tekst := st.zhurnal.String()
	if !strings.Contains(tekst, "следующая попытка через 200ms (попытка 4 подряд)") {
		t.Errorf("нет строки четвёртой попытки:\n%s", tekst)
	}
	// Следующее падение сразу после подъёма — лестница не с начала.
	st.podnyato.Store(false)
	st.s.podnyatPoslePadeniya(context.Background(), padenieDlyaProverki())
	if !strings.Contains(st.zhurnal.String(), "через 200ms (попытка 5 подряд)") {
		t.Errorf("падение сразу после подъёма начало лестницу сначала:\n%s", st.zhurnal.String())
	}
	// Проработало дольше stabilnoPosle — сначала.
	stabilnoPosle = 0
	st.podnyato.Store(false)
	st.s.podnyatPoslePadeniya(context.Background(), padenieDlyaProverki())
	if n := strings.Count(st.zhurnal.String(), "через 30ms (попытка 1 подряд)"); n != 2 {
		t.Errorf("после стабильной работы лестница не началась сначала:\n%s", st.zhurnal.String())
	}
}

// «Отключить» во время паузы — ядро не поднимается.
func TestStorozhUvazhaetOtklyuchit(t *testing.T) {
	korotkayaLestnica(t, 600*time.Millisecond)
	st := novyyStendStorozha(t)
	gotovo := make(chan struct{})
	go func() {
		st.s.podnyatPoslePadeniya(context.Background(), padenieDlyaProverki())
		close(gotovo)
	}()
	time.Sleep(100 * time.Millisecond)
	postOtklyuchit(t, st.s)
	select {
	case <-gotovo:
	case <-time.After(5 * time.Second):
		t.Fatal("сторож не закончил после «Отключить»")
	}
	if n := st.chisloZapuskov(); n != 0 {
		t.Fatalf("после «Отключить» сторож поднял ядро (%d запусков)", n)
	}
	if stoit, _ := st.sys.sostoyanie(); stoit {
		t.Fatal("после «Отключить» запрет остался")
	}
}

// Отказ, который повтором не лечится (нет конфига/файла ядра), лестницу
// обрывает: сторож не молотит впустую.
func TestStorozhBezNadezhdyNePovtoryaet(t *testing.T) {
	korotkayaLestnica(t, 10*time.Millisecond)
	st := novyyStendStorozha(t, fmt.Errorf("%w: нет конфига", yadro.ErrNeZapuskalos), nil, nil)
	st.s.podnyatPoslePadeniya(context.Background(), padenieDlyaProverki())
	if n := st.chisloZapuskov(); n != 1 {
		t.Fatalf("попыток %d, ждали 1", n)
	}
	if stoit, _ := st.sys.sostoyanie(); stoit {
		t.Fatal("подъём не удался, а запрет остался")
	}
}

// Связь уже поднял кто-то другой (авторежим, кнопка) — сторож второе ядро не
// поднимает и снимает свой запрет.
func TestStorozhNePodnimaetVtoroeYadro(t *testing.T) {
	korotkayaLestnica(t, 50*time.Millisecond)
	st := novyyStendStorozha(t)
	st.podnyato.Store(true)
	st.s.podnyatPoslePadeniya(context.Background(), padenieDlyaProverki())
	if n := st.chisloZapuskov(); n != 0 {
		t.Fatalf("при поднятой связи сторож запустил ядро (%d)", n)
	}
	if stoit, _ := st.sys.sostoyanie(); stoit {
		t.Fatal("запрет сторожа остался при поднятой связи")
	}
}

// Сквозная проверка на лже-ядре: ядро поднято настоящим подъёмом, зависает
// (процесс жив, Clash API молчит) — слежка гасит его, сторож поднимает новое,
// и в миг запуска нового старый процесс уже мёртв (не два TUN рядом, #168).
func TestStorozhZavissheeYadroGasitDoNovogo(t *testing.T) {
	korotkayaLestnica(t, 50*time.Millisecond)
	s, sys, zhurnal, _ := stendZaslona(t, nil)
	s.zashchitaPodnyataDlyaStenda = nil // настоящее состояние ядра
	lzheyadro.V(t, s.Yadro.Papka)       // лже-ядро на место ядра
	adres := lzheyadro.SvobodnyyAdres(t)
	s.Yadro.SbrasyvatKeshDns = false // кеш DNS машины проверки не трогаем
	s.Yadro.ShagSlezhki = 100 * time.Millisecond
	s.Yadro.PorogZavisaniya = 600 * time.Millisecond
	t.Setenv("LZHE_REZHIM", "zavis")
	t.Setenv("LZHE_POSLE_MS", "400")
	t.Setenv("LZHE_ADRES", adres)
	t.Cleanup(func() { _ = s.Yadro.Ostanovit() })

	var mu sync.Mutex
	var pidy []string
	stariyZhiv := false
	s.zapustitYadro = func(ctx context.Context) error {
		mu.Lock()
		if len(pidy) > 0 {
			// Второй запуск: первое ядро обязано быть мёртвым.
			pid, _ := strconv.Atoi(pidy[0])
			stariyZhiv = yadro.ObrazNash(pid, s.Yadro.Bin)
			os.Setenv("LZHE_REZHIM", "rabota")
		}
		mu.Unlock()
		s.Yadro.Api = adres
		err := s.Yadro.Zapustit(ctx)
		mu.Lock()
		pidy = append(pidy, s.Yadro.PID())
		mu.Unlock()
		return err
	}
	ctx, otmena := context.WithCancel(context.Background())
	defer otmena()
	if err := s.PodnyatZashchitu(ctx); err != nil {
		t.Fatalf("первый подъём: %v", err)
	}
	go s.StorozhYadra(ctx)

	srok := time.Now().Add(20 * time.Second)
	for {
		mu.Lock()
		n := len(pidy)
		mu.Unlock()
		if n >= 2 && s.Yadro.Sost() == yadro.Rabotaet {
			break
		}
		if time.Now().After(srok) {
			t.Fatalf("сторож не поднял ядро после зависания; журнал:\n%s", zhurnal.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if stariyZhiv {
		t.Fatal("новое ядро запущено, пока зависшее старое ещё живо")
	}
	if pidy[0] == pidy[1] {
		t.Fatalf("тот же процесс: %v", pidy)
	}
	if stoit, _ := sys.sostoyanie(); stoit {
		t.Fatal("ядро поднято, а запрет остался")
	}
	tekst := zhurnal.String()
	for _, nuzhno := range []string{"считаю зависшим: гашу процесс и жду его выхода", "сторож: ядро зависло", "сторож: ядро снова работает"} {
		if !strings.Contains(tekst, nuzhno) {
			t.Errorf("в журнале нет «%s»:\n%s", nuzhno, tekst)
		}
	}
}

// Ядро не стартует вовсе — сторож бросает после predelPopytok попыток.
func TestStorozhPredelPopytok(t *testing.T) {
	korotkayaLestnica(t, 5*time.Millisecond)
	bylo := predelPopytok
	predelPopytok = 3
	t.Cleanup(func() { predelPopytok = bylo })
	beda := errors.New("ядро упало при старте: сломанный профиль")
	st := novyyStendStorozha(t, beda, beda, beda, beda, beda)
	st.s.podnyatPoslePadeniya(context.Background(), padenieDlyaProverki())
	if n := st.chisloZapuskov(); n != 3 {
		t.Fatalf("попыток %d, ждали 3", n)
	}
	if !strings.Contains(st.zhurnal.String(), "не поднялось 3 раз подряд — бросаю") {
		t.Errorf("нет строки отказа:\n%s", st.zhurnal.String())
	}
}
