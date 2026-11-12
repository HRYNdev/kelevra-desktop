package sluzhba

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/zaslon"
)

// Стыки трёх частей канала без обрывов: постоянный запрет мимо туннеля
// (zaslon_perezapuska.go), сторож и общий замок переходов (storozh_yadra.go,
// perehody.go), отложенный перезапуск (otlozhennyy_perezapusk.go,
// perezapusk_yadra.go). Настоящего WFP нет — подмена zaslonStenda.

// otlozhennoeZhdet — ждёт ли отложенный перезапуск чего-нибудь.
func otlozhennoeZhdet(s *Sluzhba) bool {
	_, _, nuzhen := s.otlozh.nuzhenS()
	return nuzhen
}

// (а) Сторож поднимает упавшее ядро: на время подъёма — полный временный
// запрет поверх уровня 1, после подъёма — снова ровно уровень политики
// (Anthropic только через туннель), а отложенный перезапуск снят: ядро
// поднято на свежем конфиге.
func TestSvodStorozhZapretKUrovnyuPolitiki(t *testing.T) {
	korotkayaLestnica(t, 20*time.Millisecond)
	st := novyyStendStorozha(t)
	s := st.s
	s.ustanovitNamerenie(true, "проверка")
	if !st.sys.anthropic() {
		t.Fatal("«подключено»: уровня 1 нет до падения")
	}
	if stoit, _ := st.sys.sostoyanie(); stoit {
		t.Fatal("«подключено» без строгого: полный запрет до падения")
	}
	s.otlozhitPerezapusk(prichinaKonfig, "проверка: новый профиль", "")
	if !otlozhennoeZhdet(s) {
		t.Fatal("отложенный перезапуск не запомнился")
	}

	s.podnyatPoslePadeniya(context.Background(), padenieDlyaProverki())

	if n := st.chisloZapuskov(); n != 1 {
		t.Fatalf("запусков ядра %d, ждали 1", n)
	}
	if !st.videl[0] {
		t.Fatal("сторож поднимал ядро без полного временного запрета")
	}
	if stoit, _ := st.sys.sostoyanie(); stoit || s.zaslon.Perezapusk() {
		t.Fatal("ядро поднято, а временный полный запрет остался")
	}
	if !st.sys.anthropic() || s.zapretDlyaOkna() != "anthropic" {
		t.Fatalf("после подъёма ждали уровень 1, стоит %q", s.zapretDlyaOkna())
	}
	if otlozhennoeZhdet(s) {
		t.Fatal("подъём сторожем не снял отложенный перезапуск")
	}
	tekst := st.zhurnal.String()
	for _, nuzhno := range []string{
		"перезапуск ядра: трафик мимо туннеля закрыт до подъёма",
		"открыт: туннель поднят за ",
		"сторож: ядро снова работает",
		"отложенный перезапуск ядра выполнен попутно",
	} {
		if !strings.Contains(tekst, nuzhno) {
			t.Errorf("в журнале нет «%s»:\n%s", nuzhno, tekst)
		}
	}

	// Строгий из пульта: после подъёма сторожем полный запрет остаётся —
	// своим правилом, а не временным.
	pultStenda(t, s, 1, map[string]any{klyuchStrogiy: true})
	st.podnyato.Store(false)
	s.podnyatPoslePadeniya(context.Background(), padenieDlyaProverki())
	if n := st.chisloZapuskov(); n != 2 {
		t.Fatalf("строгий: запусков ядра %d, ждали 2", n)
	}
	if stoit, _ := st.sys.sostoyanie(); !stoit || s.zaslon.Perezapusk() {
		t.Fatal("строгий: после подъёма сторожем ждали полный запрет политики без временного")
	}
}

// (б) Установка обновления exe во время отложенного перезапуска: копия ждёт
// конца перезапуска (общий замок), держатель запрета встаёт с правилами
// политики (уровень 1, без временного полного) раньше хука перезапуска, и
// только потом выход. Отложенное снято самим перезапуском.
func TestSvodObnovlenieVoVremyaOtlozhennogo(t *testing.T) {
	st := novyyStendDolgogoPerezapuska(t)
	s := st.s
	s.proverkaKonfiga = func(context.Context, []byte) error { return nil }
	s.ustanovitNamerenie(true, "проверка")
	s.otlozhitPerezapusk(prichinaKonfig, "проверка: новый профиль", "")

	put := vosstanovitSebya(t)
	soderzhimoe := sborkaPohozhayaNaSebya(t, put, "PODMENA-SBORKI-SVOD")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, soderzhimoe)
	}))
	t.Cleanup(srv.Close)
	s.zamok.Lock()
	s.naydennoeObnovlenie = &obnovlenie.Novaya{Versiya: "9.9.8", Ssylka: srv.URL, Razmer: int64(len(soderzhimoe))}
	s.zamok.Unlock()

	// Держатель — в этом же процессе, на своей подмене WFP.
	derzh := &zaslonStenda{}
	derzhUshel := make(chan error, 1)
	var derzhZapushchen atomic.Bool
	s.zapustitDerzhatelya = func(string) (func(), error) {
		derzhZapushchen.Store(true)
		go func() { derzhUshel <- zaslon.Derzhat(hranenie.Papka(), derzh, 5*time.Millisecond, time.Now) }()
		return func() {}, nil
	}
	t.Cleanup(func() {
		zaslon.OtpustitDerzhatelya(hranenie.Papka())
		select {
		case <-derzhUshel:
		case <-time.After(5 * time.Second):
		}
	})

	type snimok struct {
		perezapuskKonchen, derzhAnthropic, derzhPolnyy bool
	}
	huk := make(chan snimok, 1)
	s.PerezapuskPosleObnovleniya = func(string, int) error {
		polnyy, _ := derzh.sostoyanie()
		huk <- snimok{st.vyshlo.Load(), derzh.anthropic(), polnyy}
		return nil
	}
	vyshel := make(chan struct{}, 1)
	s.ZadatVyhod(func() { vyshel <- struct{}{} })

	// Отложенный перезапуск в разгаре: дошёл до запуска ядра.
	gotovo := make(chan bool, 1)
	go func() {
		gotovo <- s.perezapustitNaNovom(context.Background(), "отложенный перезапуск (проверка) — тихое окно", s.pokolenieAvtorezhima())
	}()
	select {
	case <-st.voshli:
	case <-time.After(10 * time.Second):
		t.Fatal("отложенный перезапуск не дошёл до запуска ядра")
	}
	if stoit, _ := st.sys.sostoyanie(); !stoit {
		t.Fatal("посреди отложенного перезапуска полного временного запрета нет")
	}

	if _, err := s.PostavitNaydennoe(); err != nil {
		t.Fatalf("установка: %v", err)
	}
	select {
	case <-huk:
		t.Fatal("копия ушла на перезапуск посреди отложенного перезапуска ядра")
	case <-time.After(time.Second):
	}
	if derzhZapushchen.Load() {
		t.Fatal("держатель запущен посреди перезапуска — передал бы временный полный запрет")
	}
	close(st.otpusk)
	if sdelano := <-gotovo; !sdelano {
		t.Fatal("отложенный перезапуск не выполнен")
	}
	var sn snimok
	select {
	case sn = <-huk:
	case <-time.After(10 * time.Second):
		t.Fatalf("после конца перезапуска копия так и не ушла; журнал:\n%s", st.zhurnal.String())
	}
	select {
	case <-vyshel:
	case <-time.After(5 * time.Second):
		t.Fatal("выход не позван")
	}
	if !sn.perezapuskKonchen {
		t.Fatal("хук перезапуска позван до конца отложенного перезапуска")
	}
	if !sn.derzhAnthropic || sn.derzhPolnyy {
		t.Fatalf("к хуку держатель держит не уровень 1: anthropic=%v полный=%v", sn.derzhAnthropic, sn.derzhPolnyy)
	}
	if otlozhennoeZhdet(s) {
		t.Fatal("выполненный отложенный перезапуск остался ждать")
	}
	// Замок у уходящей копии: следующая попытка отложенного не начнётся.
	if s.perezapustitNaNovom(context.Background(), "проверка после ухода", s.pokolenieAvtorezhima()) {
		t.Fatal("после ухода копии перезапуск ядра начался")
	}
	s.zamokPodyoma.Unlock() // бой так не делает: процесс уходит; тесту — закончиться
	tekst := st.zhurnal.String()
	iDerzh := strings.Index(tekst, "держит держатель до подъёма новой копии")
	iPodnyat := strings.Index(tekst, "открыт: туннель поднят за ")
	if iDerzh < 0 || iPodnyat < 0 || iDerzh < iPodnyat {
		t.Errorf("порядок: держатель должен встать после конца перезапуска:\n%s", tekst)
	}
}

// (в) «Отключить» во время подъёма сторожем: запрет (и временный, и уровень 1)
// и намерение сняты сразу; конец подъёма их не возвращает, а поднятое
// подъёмом ядро доводится до опущенного.
func TestSvodOtklyuchitVoVremyaStorozha(t *testing.T) {
	korotkayaLestnica(t, 10*time.Millisecond)
	st := novyyStendDolgogoPerezapuska(t)
	s := st.s
	s.ustanovitNamerenie(true, "проверка")
	st.podnyato.Store(false) // ядро упало

	gotovo := make(chan struct{})
	go func() {
		s.podnyatPoslePadeniya(context.Background(), padenieDlyaProverki())
		close(gotovo)
	}()
	select {
	case <-st.voshli:
	case <-time.After(10 * time.Second):
		t.Fatal("сторож не дошёл до запуска ядра")
	}
	if stoit, _ := st.sys.sostoyanie(); !stoit {
		t.Fatal("сторож поднимает ядро без полного временного запрета")
	}

	postOtklyuchit(t, s)
	namerenie := func() bool {
		s.avtorezhimZamok.Lock()
		defer s.avtorezhimZamok.Unlock()
		return s.namerenie
	}
	if !st.sys.pusto() || s.zaslon.Perezapusk() {
		t.Fatalf("«Отключить» посреди подъёма сторожем: запрет остался (%q)", s.zapretDlyaOkna())
	}
	if namerenie() {
		t.Fatal("«Отключить» не сняло намерение «подключено»")
	}

	close(st.otpusk)
	select {
	case <-gotovo:
	case <-time.After(10 * time.Second):
		t.Fatal("сторож не закончил")
	}
	// Доведение «Отключить» после конца перехода опускает поднятое.
	srok := time.Now().Add(5 * time.Second)
	for st.snyatiy.Load() < 2 {
		if time.Now().After(srok) {
			t.Fatalf("ядро, поднятое сторожем после «Отключить», не опущено; журнал:\n%s", st.zhurnal.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !st.sys.pusto() || s.zaslon.Perezapusk() || namerenie() {
		t.Fatalf("после конца подъёма запрет или намерение вернулись: %q, намерение %v", s.zapretDlyaOkna(), namerenie())
	}
	s.avtorezhimZamok.Lock()
	avtopodkl := s.Nastroyki.Avtopodklyuch
	s.avtorezhimZamok.Unlock()
	if avtopodkl {
		t.Fatal("после «Отключить» автоподключение осталось включённым")
	}
}

// (в') Щель паузы сторожа: временный запрет встал уже ПОСЛЕ «Отключить»
// (между проверкой и zakrytMimoTunnelya). Пауза видит «Отключить» и снимает
// его сама — не ждёт предохранителя 60 с.
func TestSvodOtklyuchitVSchelPauzyStorozha(t *testing.T) {
	st := novyyStendStorozha(t)
	s := st.s
	s.ustanovitNamerenie(true, "проверка")
	otkl := s.chisloOtklyucheniy()
	postOtklyuchit(t, s)
	s.zakrytMimoTunnelya() // запрет паузы встал после «Отключить»
	if stoit, _ := st.sys.sostoyanie(); !stoit {
		t.Fatal("подготовка: временный запрет не встал")
	}
	if s.zhdatPauzuStorozha(context.Background(), time.Minute, otkl, true) {
		t.Fatal("пауза сторожа не заметила «Отключить»")
	}
	if !st.sys.pusto() || s.zaslon.Perezapusk() {
		t.Fatalf("после «Отключить» в щели паузы запрет остался: %q", s.zapretDlyaOkna())
	}
}

// Автоподключение на старте в ручном режиме — то же слово «подключено», что
// кнопка: уровень 1 стоит уже при подъёме ядра и остаётся после него.
func TestSvodAvtopodklyuchenieStavitNamerenie(t *testing.T) {
	st := novyyStendStorozha(t)
	s := st.s
	s.Nastroyki.Kod = "stend"
	s.Nastroyki.RuchnoyVybor = true
	s.Nastroyki.Avtopodklyuch = true
	var videlAnthropic bool
	s.zapustitYadro = func(context.Context) error {
		videlAnthropic = st.sys.anthropic()
		st.podnyato.Store(true)
		return nil
	}
	s.PodklyuchitPriStarte(context.Background())
	if !videlAnthropic {
		t.Fatal("автоподключение поднимало ядро без запрета Anthropic мимо туннеля")
	}
	if !st.sys.anthropic() || s.zapretDlyaOkna() != "anthropic" {
		t.Fatalf("после автоподключения ждали уровень 1, стоит %q", s.zapretDlyaOkna())
	}
}
