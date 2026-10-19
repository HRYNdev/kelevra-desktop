package sluzhba

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// Проверки повтора фоновых дел после сна (povtor_seti.go). Ни одна не ждёт
// настоящих пауз: тики, паузы, часы и случайность подменены, проверка сама
// решает, когда «прошло» 15 секунд.

// oshibkaDNS — ровно то, что Windows отдаёт в первую секунду после сна, в
// обёртке http-клиента и с кодом доступа в адресе (как у подписки).
func oshibkaDNS() error {
	return &url.Error{Op: "Get", URL: "https://kelevra.example/k/SEKRETNYYKOD42", Err: &net.DNSError{Err: "no such host", Name: "kelevra.example"}}
}

// syncBuffer — журнал под замком: в него пишет горутина дела, читает проверка.
type syncBuffer struct {
	zamok sync.Mutex
	b     bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.zamok.Lock()
	defer s.zamok.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.zamok.Lock()
	defer s.zamok.Unlock()
	return s.b.String()
}

// stendDela — подменённое окружение одного фонового дела.
type stendDela struct {
	t     *testing.T
	tiki  chan time.Time
	pauzy chan time.Duration // какие паузы дело попросило, по порядку

	zamok   sync.Mutex
	kanaly  []chan time.Time // по каналу на каждую паузу: проверка «отпускает» её сама
	sejchas time.Time
}

func novyyStendDela(t *testing.T) *stendDela {
	return &stendDela{
		t:       t,
		tiki:    make(chan time.Time, 1),
		pauzy:   make(chan time.Duration, 100),
		sejchas: time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC),
	}
}

func (st *stendDela) posle(d time.Duration) <-chan time.Time {
	k := make(chan time.Time, 1)
	st.zamok.Lock()
	st.kanaly = append(st.kanaly, k)
	st.zamok.Unlock()
	st.pauzy <- d
	return k
}

func (st *stendDela) seychas() time.Time {
	st.zamok.Lock()
	defer st.zamok.Unlock()
	return st.sejchas
}

func (st *stendDela) postavitChasy(t time.Time) {
	st.zamok.Lock()
	st.sejchas = t
	st.zamok.Unlock()
}

// otpustit — «пауза номер i истекла».
func (st *stendDela) otpustit(i int) {
	st.zamok.Lock()
	k := st.kanaly[i]
	st.zamok.Unlock()
	k <- time.Time{}
}

func (st *stendDela) zhdatPauzu(zhdu time.Duration) {
	st.t.Helper()
	select {
	case d := <-st.pauzy:
		if d != zhdu {
			st.t.Fatalf("повтор назначен через %s, ждал %s", d, zhdu)
		}
	case <-time.After(5 * time.Second):
		st.t.Fatalf("повтор через %s так и не назначен", zhdu)
	}
}

func (st *stendDela) netPauzy() {
	st.t.Helper()
	select {
	case d := <-st.pauzy:
		st.t.Fatalf("назначен повтор через %s, а повторять было нечего", d)
	case <-time.After(100 * time.Millisecond):
	}
}

func (st *stendDela) delo(sdelat func(context.Context) error) fonovoeDelo {
	return fonovoeDelo{
		imya:    "обновление профиля",
		period:  time.Hour,
		sdelat:  sdelat,
		tiki:    st.tiki,
		posle:   st.posle,
		seychas: st.seychas,
		sluchay: func() float64 { return 0.5 }, // ровно ступень, без разброса
	}
}

func zhdatPopytku(t *testing.T, popytki <-chan int, zhdu int) {
	t.Helper()
	select {
	case n := <-popytki:
		if n != zhdu {
			t.Fatalf("попытка №%d, ждал №%d", n, zhdu)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("попытка №%d так и не случилась", zhdu)
	}
}

func zapustit(t *testing.T, d fonovoeDelo) {
	t.Helper()
	ctx, otmena := context.WithCancel(context.Background())
	gotovo := make(chan struct{})
	go func() { d.krutit(ctx); close(gotovo) }()
	t.Cleanup(func() { otmena(); <-gotovo })
}

// Сеть после сна не поднялась: дело повторяет само, паузы растут по лестнице,
// удача сбрасывает лестницу — следующий отказ снова начинается с 15 секунд.
func TestPovtorSNarastayushcheyPauzoy(t *testing.T) {
	st := novyyStendDela(t)
	var schyot atomic.Int32
	popytki := make(chan int, 10)
	d := st.delo(func(context.Context) error {
		n := int(schyot.Add(1))
		popytki <- n
		if n == 4 {
			return nil
		}
		return oshibkaDNS()
	})
	d.srazu = true

	var zhurnal syncBuffer
	bylo := log.Writer()
	log.SetOutput(&zhurnal)
	t.Cleanup(func() { log.SetOutput(bylo) })
	zapustit(t, d)

	zhdatPopytku(t, popytki, 1)
	st.zhdatPauzu(15 * time.Second)
	st.otpustit(0)
	zhdatPopytku(t, popytki, 2)
	st.zhdatPauzu(30 * time.Second)
	st.otpustit(1)
	zhdatPopytku(t, popytki, 3)
	st.zhdatPauzu(time.Minute)
	st.otpustit(2)
	zhdatPopytku(t, popytki, 4) // удача
	st.netPauzy()

	st.postavitChasy(st.seychas().Add(time.Hour))
	st.tiki <- st.seychas()
	zhdatPopytku(t, popytki, 5)
	st.zhdatPauzu(15 * time.Second) // лестница после удачи — с начала

	// Строка журнала: одна на повтор, с делом и паузой, без кода подписки.
	tekst := zhurnal.String()
	const zhdu = "обновление профиля: сеть не ответила (имя kelevra.example не разрешилось), повтор через 15s"
	if !strings.Contains(tekst, zhdu) {
		t.Fatalf("в журнале нет строки повтора %q:\n%s", zhdu, tekst)
	}
	if strings.Contains(tekst, "SEKRETNYYKOD42") {
		t.Fatalf("код подписки утёк в журнал:\n%s", tekst)
	}
}

// Повтор и штатный тик не накладываются: попытка всегда одна, тик сбрасывает
// лестницу и снимает назначенный повтор — старая пауза, истёкшая после тика,
// второй попытки не запускает.
func TestPovtorNeNakladyvaetsyaNaTik(t *testing.T) {
	st := novyyStendDela(t)
	var idut, bolsheOdnoy, schyot atomic.Int32
	popytki := make(chan int, 10)
	pusti := make(chan struct{})
	d := st.delo(func(context.Context) error {
		if idut.Add(1) > 1 {
			bolsheOdnoy.Store(1)
		}
		defer idut.Add(-1)
		popytki <- int(schyot.Add(1))
		<-pusti
		return oshibkaDNS()
	})
	d.srazu = true
	zapustit(t, d)

	zhdatPopytku(t, popytki, 1)
	st.postavitChasy(st.seychas().Add(time.Hour))
	st.tiki <- st.seychas() // тик пришёл, пока первая попытка ещё идёт
	pusti <- struct{}{}
	st.zhdatPauzu(15 * time.Second)

	zhdatPopytku(t, popytki, 2) // это тик, а не повтор: пауза ещё не отпущена
	st.otpustit(0)              // старая пауза истекла, пока идёт попытка тика
	pusti <- struct{}{}
	st.zhdatPauzu(15 * time.Second) // тик сбросил лестницу: снова 15 с, а не 30

	select {
	case n := <-popytki:
		t.Fatalf("попытка №%d от снятого повтора — повтор наложился на тик", n)
	case <-time.After(100 * time.Millisecond):
	}

	st.otpustit(1)
	zhdatPopytku(t, popytki, 3)
	pusti <- struct{}{}
	st.zhdatPauzu(30 * time.Second)

	if bolsheOdnoy.Load() != 0 {
		t.Fatalf("две попытки одного дела шли одновременно")
	}
}

// Ответ сервера (4xx/5xx), незнакомый код и отвергнутая подпись не
// повторяются: повтор через 15 секунд получил бы то же самое.
func TestOtvetServeraNePovtoryaetsya(t *testing.T) {
	for _, err := range []error{
		&podpiska.OshibkaKoda{Kod: 404},
		fmt.Errorf("сервер ответил %d", 503),
		&obnovlenie.OshibkaStatusa{Kod: 403},
		&obnovlenie.OshibkaRazbora{Prichina: errors.New("invalid character '<'")},
		fmt.Errorf("%w: в релизе нет подписи", obnovlenie.ErrPodpis),
		errors.Join(obnovlenie.ErrPodpis, oshibkaDNS()),
		context.Canceled,
	} {
		if setevoyOtkaz(err) {
			t.Errorf("%v сочтено сетевым отказом — повторялось бы зря", err)
		}
	}

	st := novyyStendDela(t)
	popytki := make(chan int, 10)
	var schyot atomic.Int32
	d := st.delo(func(context.Context) error {
		popytki <- int(schyot.Add(1))
		return &podpiska.OshibkaKoda{Kod: 404}
	})
	d.srazu = true
	zapustit(t, d)
	zhdatPopytku(t, popytki, 1)
	st.netPauzy()
	st.postavitChasy(st.seychas().Add(time.Hour))
	st.tiki <- st.seychas()
	zhdatPopytku(t, popytki, 2) // ждёт штатного тика, как раньше
	st.netPauzy()
}

func TestSetevoyOtkazPoTipam(t *testing.T) {
	for _, err := range []error{
		oshibkaDNS(),
		&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connectex: refused")},
		&net.OpError{Op: "read", Net: "tcp", Err: errors.New("wsarecv: reset")},
		fmt.Errorf("список релизов: %w", context.DeadlineExceeded),
		&url.Error{Op: "Get", URL: "https://api.github.com/x", Err: &net.DNSError{Err: "i/o timeout", IsTimeout: true}},
		fmt.Errorf("набор geosite: %w", &url.Error{Op: "Get", URL: "https://x/y", Err: io.EOF}),
	} {
		if !setevoyOtkaz(err) {
			t.Errorf("%v не сочтено сетевым отказом — не повторилось бы", err)
		}
	}
	if k := korotkoOSeti(oshibkaDNS()); strings.Contains(k, "SEKRETNYYKOD42") || strings.Contains(k, "/k/") {
		t.Errorf("короткая причина несёт адрес подписки: %q", k)
	}
}

// Лестница: 15 с, 30 с, 1 мин, 2 мин, 5 мин, 10 мин, 15 мин и дальше 15 мин;
// разброс ±20%, но потолок 15 мин не пробивается.
func TestPauzaPovtora(t *testing.T) {
	lestnica := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	for i, zhdu := range lestnica {
		if d := pauzaPovtora(i, 0.5); d != zhdu {
			t.Errorf("ступень %d: %s, ждал %s", i, d, zhdu)
		}
		niz, verh := pauzaPovtora(i, 0), pauzaPovtora(i, 0.999999)
		if niz < zhdu*8/10-time.Millisecond || niz >= zhdu {
			t.Errorf("ступень %d: нижний край %s вне −20%% от %s", i, niz, zhdu)
		}
		if verh > potolokPovtora || (zhdu < potolokPovtora && verh <= zhdu) {
			t.Errorf("ступень %d: верхний край %s (ступень %s, потолок %s)", i, verh, zhdu, potolokPovtora)
		}
	}
	if d := pauzaPovtora(0, 0); d != 12*time.Second {
		t.Errorf("15 с −20%% = %s, ждал 12s", d)
	}
}

// Опоздавший тик (машина спала) сперва ждёт сеть и только потом идёт в неё;
// обычный тик — сразу. Не дождались — обычный повтор.
func TestOpozdavshiyTikZhdyotSet(t *testing.T) {
	st := novyyStendDela(t)
	var zamok sync.Mutex
	var poryadok []string
	zapisat := func(s string) {
		zamok.Lock()
		poryadok = append(poryadok, s)
		zamok.Unlock()
	}
	popytki := make(chan int, 10)
	var schyot atomic.Int32
	d := st.delo(func(context.Context) error {
		zapisat("сеть")
		popytki <- int(schyot.Add(1))
		return oshibkaDNS()
	})
	d.zhdatSet = func(context.Context) (bool, time.Duration) {
		zapisat("ждать")
		return false, srokOzhidaniyaSeti // не дождались
	}
	start := st.seychas()
	zapustit(t, d)

	// Обычный тик, вовремя: сразу в сеть.
	st.postavitChasy(start.Add(time.Hour))
	st.tiki <- start.Add(time.Hour)
	zhdatPopytku(t, popytki, 1)
	st.zhdatPauzu(15 * time.Second)

	// Машина проспала три часа: тикер отдал накопленный тик в первую секунду
	// после пробуждения.
	st.postavitChasy(start.Add(4 * time.Hour))
	st.tiki <- start.Add(2 * time.Hour)
	zhdatPopytku(t, popytki, 2)
	st.zhdatPauzu(15 * time.Second) // сети не дождались — обычный повтор

	zamok.Lock()
	defer zamok.Unlock()
	if got := strings.Join(poryadok, ","); got != "сеть,ждать,сеть" {
		t.Fatalf("порядок %q, ждал «сеть,ждать,сеть»: обычный тик идёт сразу, тик после сна сперва ждёт сеть", got)
	}
}

func TestProsnulsya(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)
	chas := time.Hour
	for _, s := range []struct {
		imya                   string
		proshlyy, tik, seychas time.Time
		period                 time.Duration
		zhdu                   bool
	}{
		{"первый тик", time.Time{}, t0, t0, chas, false},
		{"вовремя", t0, t0.Add(chas), t0.Add(chas), chas, false},
		{"вовремя с дрожью", t0, t0.Add(chas), t0.Add(chas + 2*time.Second), chas, false},
		{"стенные ушли на три периода", t0, t0.Add(3 * chas), t0.Add(3 * chas), chas, true},
		{"тик отдан через 10 мин после срока", t0, t0.Add(chas), t0.Add(chas + 10*time.Minute), chas, true},
		{"разогнанный стендом тикер и пауза сборщика", t0, t0.Add(50 * time.Millisecond), t0.Add(250 * time.Millisecond), 50 * time.Millisecond, false},
	} {
		if got := prosnulsya(s.proshlyy, s.tik, s.seychas, s.period); got != s.zhdu {
			t.Errorf("%s: prosnulsya = %v, ждал %v", s.imya, got, s.zhdu)
		}
	}
}

func TestDozhdatsyaSeti(t *testing.T) {
	srazu := func(time.Duration) <-chan time.Time {
		k := make(chan time.Time, 1)
		k <- time.Time{}
		return k
	}
	vzglyadov := 0
	est := func() bool { vzglyadov++; return vzglyadov >= 3 }
	if !dozhdatsyaSeti(context.Background(), est, time.Minute, 2*time.Second, srazu) || vzglyadov != 3 {
		t.Fatalf("сеть появилась на третьем взгляде, а ожидание вернуло иначе (взглядов %d)", vzglyadov)
	}

	vzglyadov = 0
	nikogda := func() bool { vzglyadov++; return false }
	if dozhdatsyaSeti(context.Background(), nikogda, 10*time.Second, 2*time.Second, srazu) {
		t.Fatalf("сети нет, а ожидание сказало «есть»")
	}
	if vzglyadov != 6 { // 0, 2, 4, 6, 8, 10 с
		t.Fatalf("за 10 с шагом 2 с взглядов %d, ждал 6", vzglyadov)
	}

	ctx, otmena := context.WithCancel(context.Background())
	otmena()
	nikogdaKanal := func(time.Duration) <-chan time.Time { return nil }
	if dozhdatsyaSeti(ctx, func() bool { return false }, time.Hour, time.Second, nikogdaKanal) {
		t.Fatalf("служба гасится, а ожидание сказало «сеть есть»")
	}
}

// Настоящие ошибки фоновой проверки обновления, а не собранные руками:
// молчащий GitHub — сетевой отказ (повтор), ответ 403 — нет (ждём тика).
func TestFonovayaProverkaOshibkiPoPravde(t *testing.T) {
	s := stend(t)

	staryy := srokFonovoyProverkiObnovleniya
	srokFonovoyProverkiObnovleniya = 200 * time.Millisecond
	t.Cleanup(func() { srokFonovoyProverkiObnovleniya = staryy })

	otpushcheno := make(chan struct{})
	molchit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-otpushcheno:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(otpushcheno); molchit.Close() })
	t.Setenv("KELEVRA_RELIZY", molchit.URL)
	if err := s.proveritObnovlenieRaz(context.Background()); !setevoyOtkaz(err) {
		t.Fatalf("GitHub молчит дольше срока: %v — не сочтено сетевым отказом", err)
	}

	zapret := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(zapret.Close)
	t.Setenv("KELEVRA_RELIZY", zapret.URL)
	err := s.proveritObnovlenieRaz(context.Background())
	if err == nil || setevoyOtkaz(err) {
		t.Fatalf("GitHub ответил 403: %v — повторять такое нельзя", err)
	}
}

// Срок фоновой проверки длинный (через туннель GitHub отвечает ~10 с), а у
// кнопки в окне — короткий: там человек стоит и смотрит.
func TestSrokiProverkiObnovleniya(t *testing.T) {
	if srokFonovoyProverkiObnovleniya != 30*time.Second {
		t.Fatalf("срок фоновой проверки %s, ждал 30s", srokFonovoyProverkiObnovleniya)
	}
	if srokProverkiObnovleniya >= srokFonovoyProverkiObnovleniya {
		t.Fatalf("срок кнопки %s не короче фонового %s", srokProverkiObnovleniya, srokFonovoyProverkiObnovleniya)
	}
}
