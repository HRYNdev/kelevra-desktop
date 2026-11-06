package sluzhba

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Один замок на все переходы ядра (perehody.go).

// stendDolgogoPerezapuska — служба, у которой «запуск ядра» висит, пока тест
// его не отпустит: перезапуск «Дома» в разгаре. voshli — запуск начался.
type stendDolgogoPerezapuska struct {
	*stendStorozha
	voshli  chan struct{}
	otpusk  chan struct{}
	vyshlo  atomic.Bool // запуск закончился
	snyatiy atomic.Int32
}

func novyyStendDolgogoPerezapuska(t *testing.T) *stendDolgogoPerezapuska {
	t.Helper()
	st := &stendDolgogoPerezapuska{stendStorozha: novyyStendStorozha(t),
		voshli: make(chan struct{}, 4), otpusk: make(chan struct{})}
	st.podnyato.Store(true)
	st.s.snyatProksiDlyaStenda = func() { st.snyatiy.Add(1) }
	st.s.zapustitYadro = func(ctx context.Context) error {
		st.podnyato.Store(false)
		st.voshli <- struct{}{}
		<-st.otpusk
		st.podnyato.Store(true)
		st.vyshlo.Store(true)
		return nil
	}
	return st
}

// perezapustitVFone — перезапуск «Дома» в своей горутине; ждёт, пока он
// дойдёт до запуска ядра.
func (st *stendDolgogoPerezapuska) perezapustitVFone(t *testing.T) chan struct{} {
	t.Helper()
	gotovo := make(chan struct{})
	go func() {
		st.s.perezapustit("проверка замка", st.s.pokolenieAvtorezhima(), true)
		close(gotovo)
	}()
	select {
	case <-st.voshli:
	case <-time.After(10 * time.Second):
		t.Fatal("перезапуск не дошёл до запуска ядра")
	}
	return gotovo
}

// Установка обновления посреди перезапуска: копия уходит (хук перезапуска,
// выход) только после того, как перезапуск закончился, а не через 300 мс.
func TestObnovlenieZhdetPerezapuska(t *testing.T) {
	st := novyyStendDolgogoPerezapuska(t)
	s := st.s
	put := vosstanovitSebya(t)
	soderzhimoe := sborkaPohozhayaNaSebya(t, put, "PODMENA-SBORKI-ZAMOK")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, soderzhimoe)
	}))
	t.Cleanup(srv.Close)
	s.zamok.Lock()
	s.naydennoeObnovlenie = &obnovlenie.Novaya{Versiya: "9.9.8", Ssylka: srv.URL, Razmer: int64(len(soderzhimoe))}
	s.zamok.Unlock()

	var hukPriPerezapuske atomic.Bool
	huk := make(chan struct{}, 1)
	s.PerezapuskPosleObnovleniya = func(string, int) error {
		hukPriPerezapuske.Store(!st.vyshlo.Load())
		huk <- struct{}{}
		return nil
	}
	vyshel := make(chan struct{}, 1)
	s.ZadatVyhod(func() { vyshel <- struct{}{} })

	gotovo := st.perezapustitVFone(t)
	if _, err := s.PostavitNaydennoe(); err != nil {
		t.Fatalf("установка: %v", err)
	}
	select {
	case <-huk:
		t.Fatal("копия ушла на перезапуск посреди перезапуска ядра")
	case <-time.After(time.Second):
	}
	close(st.otpusk)
	<-gotovo
	select {
	case <-huk:
	case <-time.After(5 * time.Second):
		t.Fatal("после конца перезапуска копия так и не ушла")
	}
	select {
	case <-vyshel:
	case <-time.After(5 * time.Second):
		t.Fatal("выход не позван")
	}
	if hukPriPerezapuske.Load() {
		t.Fatal("хук перезапуска позван до конца перезапуска ядра")
	}
	// Замок у уходящей копии: новый переход не начинается.
	if s.zamokPodyoma.TryLock() {
		t.Fatal("после ухода замок переходов свободен — новый перезапуск мог бы начаться")
	}
	s.zamokPodyoma.Unlock() // не то, что бой делает, но тест должен закончиться
	if !strings.Contains(st.zhurnal.String(), "дождался конца перехода ядра") {
		t.Errorf("нет строки ожидания:\n%s", st.zhurnal.String())
	}
}

// «Отключить» посреди перезапуска: перезапуск ядро всё-таки поднял (щель
// между его проверкой и запуском) — после его конца связь опускается.
func TestOtklyuchitPosredPerezapuskaDovoditsya(t *testing.T) {
	st := novyyStendDolgogoPerezapuska(t)
	gotovo := st.perezapustitVFone(t)
	postOtklyuchit(t, st.s)
	if n := st.snyatiy.Load(); n != 1 {
		t.Fatalf("«Отключить» опустило связь %d раз, ждали 1 сразу", n)
	}
	close(st.otpusk)
	<-gotovo
	srok := time.Now().Add(5 * time.Second)
	for st.snyatiy.Load() < 2 {
		if time.Now().After(srok) {
			t.Fatalf("после перезапуска связь не опущена; журнал:\n%s", st.zhurnal.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(st.zhurnal.String(), "«Отключить»: переход ядра успел поднять связь — опускаю") {
		t.Errorf("нет строки доведения:\n%s", st.zhurnal.String())
	}
}

// «Подключиться» после «Отключить», пока перезапуск ещё шёл, — доведение
// «Отключить» не гасит связь, поднятую по новому слову человека.
func TestDovedenieNeBetNovoePodklyuchenie(t *testing.T) {
	st := novyyStendDolgogoPerezapuska(t)
	st.s.Nastroyki.RuchnoyVybor = true // ручной режим: кнопка поднимает сразу
	gotovo := st.perezapustitVFone(t)
	postOtklyuchit(t, st.s)
	podkl := make(chan error, 1)
	go func() { podkl <- st.s.Podklyuchit(context.Background()) }()
	time.Sleep(100 * time.Millisecond)
	close(st.otpusk)
	<-gotovo
	select {
	case <-podkl:
	case <-time.After(10 * time.Second):
		t.Fatal("«Подключиться» не закончилось")
	}
	time.Sleep(300 * time.Millisecond)
	if n := st.snyatiy.Load(); n != 1 {
		t.Fatalf("связь опущена %d раз — доведение «Отключить» побило новое «Подключиться»", n)
	}
}

// Остановка службы: новых ядер больше нет — ни подъём, ни сторож их не
// запускают.
func TestPosleOstanovkiNovyhYaderNet(t *testing.T) {
	st := novyyStendStorozha(t)
	st.s.OstanovitYadroPriVyhode()
	err := st.s.PodnyatZashchitu(context.Background())
	if !errors.Is(err, yadro.ErrOstanovleno) {
		t.Fatalf("подъём после остановки службы: %v", err)
	}
	korotkayaLestnica(t, 10*time.Millisecond)
	st.s.podnyatPoslePadeniya(context.Background(), padenieDlyaProverki())
	if n := st.chisloZapuskov(); n != 0 {
		t.Fatalf("после остановки службы ядро запускалось %d раз", n)
	}
}

// Авторежим «дома» посреди перезапуска «Дома» не опускает связь (Г2):
// перезапуск всё равно поднял бы её снова.
func TestAvtorezhimDomaNeMeshaetPerezapusku(t *testing.T) {
	st := novyyStendDolgogoPerezapuska(t)
	gotovo := st.perezapustitVFone(t)
	st.podnyato.Store(true) // глазами авторежима связь поднята
	st.s.avtorezhimKolbek(context.Background(), avtorezhim.Doma, true)
	if n := st.snyatiy.Load(); n != 0 {
		t.Fatal("авторежим опустил связь посреди перезапуска")
	}
	close(st.otpusk)
	<-gotovo
	st.s.avtorezhimKolbek(context.Background(), avtorezhim.Doma, true)
	if n := st.snyatiy.Load(); n != 1 {
		t.Fatalf("после перезапуска авторежим не опустил связь (%d)", n)
	}
}

// Запись config.json — под тем же замком: пока идёт переход, пересборка ждёт.
func TestPerestroitKonfigZhdetPerehoda(t *testing.T) {
	st := novyyStendDolgogoPerezapuska(t)
	gotovo := st.perezapustitVFone(t)
	sobrano := make(chan error, 1)
	go func() { sobrano <- st.s.PerestroitKonfig() }()
	select {
	case <-sobrano:
		t.Fatal("config.json пересобран посреди перехода ядра")
	case <-time.After(300 * time.Millisecond):
	}
	close(st.otpusk)
	<-gotovo
	select {
	case err := <-sobrano:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("пересборка не дождалась конца перехода")
	}
}

// Автоподключение (avtopodklyuch.go): ручной режим, человек был подключён —
// служба поднимает связь на старте; отключался сам или авторежим включён —
// нет.
func TestAvtopodklyuchPriStarte(t *testing.T) {
	sluchai := []struct {
		imya          string
		avtorezhim    bool
		avtopodklyuch bool
		zhdem         int
	}{
		{"ручной, был подключён", false, true, 1},
		{"ручной, отключался сам", false, false, 0},
		{"авторежим решает сам", true, true, 0},
	}
	for _, sl := range sluchai {
		t.Run(sl.imya, func(t *testing.T) {
			st := novyyStendStorozha(t)
			st.s.Nastroyki.Kod = "stend"
			st.s.Nastroyki.Avtorezhim = sl.avtorezhim
			st.s.Nastroyki.RuchnoyVybor = !sl.avtorezhim
			st.s.Nastroyki.Avtopodklyuch = sl.avtopodklyuch
			st.s.PodklyuchitPriStarte(context.Background())
			if n := st.chisloZapuskov(); n != sl.zhdem {
				t.Fatalf("запусков %d, ждали %d", n, sl.zhdem)
			}
		})
	}
}

// Поле пишется словами человека и переживает перезапуск (на диске).
func TestAvtopodklyuchPishetsyaSlovomCheloveka(t *testing.T) {
	st := novyyStendStorozha(t)
	st.s.Nastroyki.RuchnoyVybor = true
	if err := st.s.Podklyuchit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n, _ := hranenie.Zagruzit(); !n.Avtopodklyuch {
		t.Fatal("«Подключиться» не запомнило автоподключение на диске")
	}
	postOtklyuchit(t, st.s)
	if n, _ := hranenie.Zagruzit(); n.Avtopodklyuch {
		t.Fatal("«Отключить» не сняло автоподключение на диске")
	}
}

// Ручной режим при принятом живом ядре: поле запоминается (первый старт
// версии, где поле наконец читается).
func TestAvtopodklyuchPriPrinyatomYadre(t *testing.T) {
	st := novyyStendStorozha(t)
	st.podnyato.Store(true)
	st.s.Nastroyki.RuchnoyVybor = true
	st.s.PodklyuchitPriStarte(context.Background())
	if n := st.chisloZapuskov(); n != 0 {
		t.Fatalf("при живом ядре поднималось второе (%d)", n)
	}
	if n, _ := hranenie.Zagruzit(); !n.Avtopodklyuch {
		t.Fatal("связь держали, а автоподключение не запомнено")
	}
}
