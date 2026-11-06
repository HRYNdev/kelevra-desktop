package sluzhba

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

func zhurnalDlyaProverki(t *testing.T) *syncBuffer {
	t.Helper()
	z := &syncBuffer{}
	log.SetOutput(z)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return z
}

// bezPauzyPoSmene — проверка по смене сети без настоящей паузы перед пробой.
func bezPauzyPoSmene(t *testing.T) {
	t.Helper()
	bylo := pauzaPeredProboyPoSmene
	pauzaPeredProboyPoSmene = 0
	t.Cleanup(func() { pauzaPeredProboyPoSmene = bylo })
}

// stendSmenySeti — слежка за сменой сети на подставных событиях,
// отпечатке сети и ожидании: shag — одна смена сети до конца проверки.
type stendSmenySeti struct {
	*stendLecheniya
	mu       sync.Mutex
	otpechat string
	sobytiya chan struct{}
	tiki     chan time.Time
	otmena   context.CancelFunc
	gotovo   chan struct{}
}

func novyyStendSmenySeti(t *testing.T, nachalo string) *stendSmenySeti {
	t.Helper()
	bezPauzyPoSmene(t)
	st := &stendSmenySeti{stendLecheniya: novyyStendLecheniya(t), otpechat: nachalo,
		sobytiya: make(chan struct{}), tiki: make(chan time.Time), gotovo: make(chan struct{})}
	st.s.dom.otpechatokSeti = func() string {
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.otpechat
	}
	ctx, otmena := context.WithCancel(context.Background())
	st.otmena = otmena
	go func() {
		defer close(st.gotovo)
		st.s.sleditZaSobytiyamiSeti(ctx, st.sobytiya, func(time.Duration) <-chan time.Time { return st.tiki })
	}()
	t.Cleanup(func() { otmena(); <-st.gotovo })
	// Событие доходит, только когда слежка уже запомнила начальную сеть.
	st.sobytiya <- struct{}{}
	return st
}

// shag — сеть стала otpechat: событие, тишина вышла, проверка; второе
// событие доходит, только когда проверка закончена, а перезапуск (если
// решён) — дождан.
func (st *stendSmenySeti) shag(otpechat string) {
	st.mu.Lock()
	st.otpechat = otpechat
	st.mu.Unlock()
	st.sobytiya <- struct{}{}
	st.tiki <- time.Time{}
	st.sobytiya <- struct{}{}
	st.s.dom.perezapuski.Wait()
}

// Wi-Fi выключили и включили (замер 06.11, 21:58:56–21:59:28): пока сети
// нет — ничего; сеть вернулась, дом не отвечает — перезапуск ядра сразу, без
// трёх проб, неудачи до смены сети не в счёт. События от своего TUN и повтор
// той же сети проверки не дают. Другая сеть, дом отвечает — ядро не трогаем.
// Сеть снова сменилась раньше 30 с с прошлой проверки — не трогаем; позже —
// снова проверка и перезапуск.
func TestDomPoSmeneSeti(t *testing.T) {
	st := novyyStendSmenySeti(t, "13 192.168.1.5 192.168.1.1")
	z := zhurnalDlyaProverki(t)
	// До выключения дом был на связи.
	st.otvet = nil
	st.proba()
	st.otvet = errors.New("молчит")

	st.shag("") // Wi-Fi выключен
	st.proba()
	st.proba()
	st.zhdat(t, 0, "сети нет")
	st.sejchas = st.sejchas.Add(45 * time.Second)
	st.shag("13 192.168.1.5 192.168.1.1") // включён, та же сеть и тот же адрес
	st.zhdat(t, 1, "сеть вернулась, дом молчит")
	if p := st.perezapuski[0]; p != "сеть сменилась (сеть вернулась), дом не отвечает — перезапускаю ядро" {
		t.Fatalf("причина: %q", p)
	}
	if st.s.dom.neudachPodryad != 0 || st.s.dom.pauzaLecheniya != 0 {
		t.Fatalf("неудачи до смены сети в счёте: %d, ступень %v", st.s.dom.neudachPodryad, st.s.dom.pauzaLecheniya)
	}
	// Новое ядро: дом ожил, строки о «сбросе» нет.
	st.sejchas = st.sejchas.Add(10 * time.Second)
	st.otvet = nil
	st.proba()
	if strings.Contains(z.String(), "после сброса") {
		t.Fatalf("в журнале строка о сбросе сети, которого не было:\n%s", z.String())
	}

	// Свой TUN поднялся или сменился адрес IPv6: физическая сеть та же.
	st.sejchas = st.sejchas.Add(time.Minute)
	st.shag("13 192.168.1.5 192.168.1.1")
	if n := strings.Count(z.String(), "дом: сеть сменилась"); n != 0 {
		t.Fatalf("физическая сеть та же, а проверок в журнале %d:\n%s", n, z.String())
	}
	// Другая сеть через минуту, дом отвечает — ядро не трогаем.
	st.shag("13 172.20.10.3 172.20.10.1")
	st.zhdat(t, 1, "другая сеть, дом на связи")
	if !strings.Contains(z.String(), "дом: сеть сменилась (другая сеть), дом на связи — ядро не трогаю") {
		t.Fatalf("проверки по смене сети нет в журнале:\n%s", z.String())
	}
	// Ещё одна смена через 10 с — отсечка.
	st.otvet = errors.New("молчит")
	st.sejchas = st.sejchas.Add(10 * time.Second)
	st.shag("13 10.0.0.7 10.0.0.1")
	st.zhdat(t, 1, "смена раньше 30 с с прошлой проверки")
	if !strings.Contains(z.String(), "дом: сеть сменилась (другая сеть), но дом проверяли 10 с назад — не трогаю") {
		t.Fatalf("отсечки нет в журнале:\n%s", z.String())
	}
	st.sejchas = st.sejchas.Add(30 * time.Second)
	st.shag("13 192.168.1.5 192.168.1.1")
	st.zhdat(t, 2, "смена через 40 с, дом молчит")
	if strings.Contains(z.String()+strings.Join(st.perezapuski, "\n"), "защит") {
		t.Fatalf("в новых строках слово «защита»:\n%s", z.String())
	}
}

// Сеть вернулась, но туннель не поднят (ядро стоит), «Дома» нет в конфиге
// или очередь подъёма занята — ни пробы, ни перезапуска; первая смена при
// поднятом туннеле — проба и перезапуск.
func TestDomPoSmeneSetiNeVovremya(t *testing.T) {
	st := novyyStendSmenySeti(t, "13 192.168.1.5 192.168.1.1")
	var prob atomic.Int32
	st.s.dom.proba = func(context.Context, string, string) error { prob.Add(1); return st.otvet }
	rabotaet := false
	st.s.dom.rabotaetDlyaStenda = func() bool { return rabotaet }
	st.shag("")
	st.shag("13 192.168.1.5 192.168.1.1")
	st.s.zamok.Lock()
	st.s.kartina.Dom = false
	st.s.zamok.Unlock()
	rabotaet = true
	st.shag("13 172.20.10.3 172.20.10.1")
	st.s.zamok.Lock()
	st.s.kartina.Dom = true
	st.s.zamok.Unlock()
	st.s.zamokPodyoma.Lock()
	st.shag("13 10.0.0.7 10.0.0.1")
	st.s.zamokPodyoma.Unlock()
	st.zhdat(t, 0, "при стоящем ядре, без «Дома» и при занятой очереди")
	if n := prob.Load(); n != 0 {
		t.Fatalf("проб «Дома» %d, ждали 0", n)
	}
	if st.s.dom.lechim {
		t.Fatal("лечение осталось отмеченным")
	}
	st.shag("13 192.168.1.5 192.168.1.1")
	st.zhdat(t, 1, "туннель поднят, сеть сменилась, дом молчит")
	if n := prob.Load(); n != 2 {
		t.Fatalf("попыток пробы %d, ждали 2", n)
	}
}

// Перезапуск по смене сети идёт (долгий), сеть снова сменилась позже
// отсечки — второй перезапуск рядом не начинается; самоизлечение пробами в
// это время тоже ждёт.
func TestDomPoSmeneSetiOdinPerezapusk(t *testing.T) {
	st := novyyStendSmenySeti(t, "13 192.168.1.5 192.168.1.1")
	var vyzovov atomic.Int32
	otpustit := make(chan struct{})
	st.s.dom.perezapustit = func(string, uint64) {
		vyzovov.Add(1)
		<-otpustit
	}
	st.mu.Lock()
	st.otpechat = "13 172.20.10.3 172.20.10.1"
	st.mu.Unlock()
	st.sobytiya <- struct{}{}
	st.tiki <- time.Time{}
	st.sobytiya <- struct{}{} // проверка закончена, перезапуск висит
	st.sejchas = st.sejchas.Add(time.Minute)
	st.mu.Lock()
	st.otpechat = "13 10.0.0.7 10.0.0.1"
	st.mu.Unlock()
	st.tiki <- time.Time{}
	st.sobytiya <- struct{}{}
	for i := 0; i < 4; i++ {
		st.s.proveritSvyazDoma(context.Background())
	}
	close(otpustit)
	st.s.dom.perezapuski.Wait()
	if n := vyzovov.Load(); n != 1 {
		t.Fatalf("перезапусков %d, ждали 1", n)
	}
}

// Служба останавливается во время паузы перед пробой — слежка выходит
// сразу, а не по концу паузы; ни пробы, ни перезапуска, лечение не висит.
func TestDomPoSmeneSetiOtmena(t *testing.T) {
	st := novyyStendSmenySeti(t, "13 192.168.1.5 192.168.1.1")
	pauzaPeredProboyPoSmene = time.Hour
	var prob atomic.Int32
	st.s.dom.proba = func(context.Context, string, string) error { prob.Add(1); return st.otvet }
	st.mu.Lock()
	st.otpechat = "13 172.20.10.3 172.20.10.1"
	st.mu.Unlock()
	st.sobytiya <- struct{}{}
	st.tiki <- time.Time{} // проверка началась и ждёт паузу
	st.otmena()
	select {
	case <-st.gotovo:
	case <-time.After(5 * time.Second):
		t.Fatal("слежка не вышла по отмене — ждёт конца паузы")
	}
	st.s.dom.perezapuski.Wait()
	st.zhdat(t, 0, "отмена во время паузы")
	if n := prob.Load(); n != 0 {
		t.Fatalf("проб %d после отмены", n)
	}
	st.s.dom.mu.Lock()
	lechim := st.s.dom.lechim
	st.s.dom.mu.Unlock()
	if lechim {
		t.Fatal("после отмены лечение осталось отмеченным — самоизлечение встанет")
	}
}

// Адрес появился раньше шлюза: первое событие застаёт сеть пустой, нового
// события нет — слежка смотрит ещё несколько раз сама и проверяет дом, когда
// шлюз появился.
func TestDomPoSmeneShlyuzPozzheAdresa(t *testing.T) {
	st := novyyStendSmenySeti(t, "")
	// Что увидит каждая следующая проверка, по порядку (без гонки с
	// проверкой, которая уже идёт).
	ochered := []string{"", "", "13 192.168.1.5 192.168.1.1"}
	st.s.dom.otpechatokSeti = func() string {
		st.mu.Lock()
		defer st.mu.Unlock()
		if len(ochered) == 0 {
			return "13 192.168.1.5 192.168.1.1"
		}
		o := ochered[0]
		ochered = ochered[1:]
		return o
	}
	st.sobytiya <- struct{}{}
	st.tiki <- time.Time{}    // проверка: сети ещё нет
	st.tiki <- time.Time{}    // повтор: всё ещё нет
	st.tiki <- time.Time{}    // повтор: шлюз есть — проба, дом молчит
	st.sobytiya <- struct{}{} // проверка закончена
	st.s.dom.perezapuski.Wait()
	st.zhdat(t, 1, "шлюз позже адреса")
}

// Окно (и всё, что ходит через /api/vybrat) группу моста не переключает и в
// выбор человека не записывает.
func TestDomMostNeVybiraetsyaVruchnuyu(t *testing.T) {
	s, _ := stendDoma(t)
	telo, _ := json.Marshal(map[string]string{"gruppa": konfig.TegMostaDoma, "uzel": "Финляндия"})
	r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/vybrat", bytes.NewReader(telo))
	w := httptest.NewRecorder()
	s.Obsluzhit().ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatalf("«%s» переключён вручную", konfig.TegMostaDoma)
	}
	if _, est := s.Nastroyki.Uzly[konfig.TegMostaDoma]; est {
		t.Fatalf("«%s» записан в выбор человека", konfig.TegMostaDoma)
	}
}

// Обновление с 0.6.98: живое ядро прошлой копии работает по конфигу с
// селектором «Дом · щелчок» (detour эндпоинта — селектор, рядом заглушка
// block). Служба такое ядро не принимает как есть (конфиг устарел), а
// меняет: прежнее гасится, потом новое поднимается на конфиге без
// селектора, трафик мимо туннеля в это время закрыт. Дальше «Дом» лечится
// перезапуском ядра, без паники и без вечной сверки.
func TestDomPriyomYadra0698SoShchelchkom(t *testing.T) {
	s := stendSPravami(t)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s.dom.kesh.Klyuchi = []ed25519.PublicKey{priv.Public().(ed25519.PublicKey)}
	s.Nastroyki.Slot = "01-7"
	zapuskov := 0
	var poryadok []string
	s.zapustitYadro = func(ctx context.Context) error { zapuskov++; poryadok = append(poryadok, "подъём"); return nil }
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 1, "100.76.0.1:51820")})
	s.dom.perezapuski.Wait()

	// Конфиг 0.6.98 — тот же, только мост «Дом» через селектор щелчка.
	syroy, err := os.ReadFile(s.Yadro.PutKonfiga())
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(syroy, &d); err != nil {
		t.Fatal(err)
	}
	vyhod := ""
	for _, e := range d["endpoints"].([]any) {
		if e.(map[string]any)["tag"] == konfig.TegDoma {
			vyhod, _ = e.(map[string]any)["detour"].(string)
			e.(map[string]any)["detour"] = "Дом · щелчок"
		}
	}
	if vyhod == "" {
		t.Fatal("в конфиге нет эндпоинта «Дом»")
	}
	d["outbounds"] = append(d["outbounds"].([]any),
		map[string]any{"type": "block", "tag": "Дом · заглушка"},
		map[string]any{"type": "selector", "tag": "Дом · щелчок", "outbounds": []any{vyhod, "Дом · заглушка"},
			"default": vyhod, "interrupt_exist_connections": false})
	staryy, _ := json.Marshal(d)
	if err := os.WriteFile(s.Yadro.PutKonfiga(), staryy, 0o600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(srv.Close)
	api := strings.TrimPrefix(srv.URL, "http://")
	svoy, err := os.Executable()
	if err != nil {
		t.Skipf("не нашёл свой exe: %v", err)
	}
	if !yadro.ObrazNash(os.Getpid(), svoy) {
		t.Skip("ОС не подтвердила образ своего процесса на этой платформе — проверка недоказательна")
	}
	s.Yadro.Bin, s.Yadro.Api = svoy, api
	var pogasheno []int
	staroe := pogasitChuzhoe
	pogasitChuzhoe = func(pid int) { pogasheno = append(pogasheno, pid); poryadok = append(poryadok, "гашу") }
	t.Cleanup(func() { pogasitChuzhoe = staroe })
	staryyPoisk := nashiYadra
	nashiYadra = func(string) []int { return nil }
	t.Cleanup(func() { nashiYadra = staryyPoisk })
	yadro.ZapisatPeredachu(hranenie.PapkaYadra(), yadro.Peredacha{PID: os.Getpid(), Bin: svoy, Api: api, Adapter: "tun125",
		Otpechatok: yadro.OtpechatokKonfigaBayt(staryy)})

	z := zhurnalDlyaProverki(t)
	poryadok = nil
	s.PrinyatZhivoeYadro(context.Background(), "")
	// Два туннеля рядом на Windows не работают (06.11: новый tun126 молчал,
	// трафик шёл напрямую): прежнее ядро гасится ДО подъёма нового.
	if strings.Join(poryadok, ",") != "гашу,подъём" {
		t.Fatalf("порядок смены ядра %v, ждали: гашу прежнее, потом подъём нового\n%s", poryadok, z.String())
	}
	if !strings.Contains(z.String(), "трафик мимо туннеля закрыт до подъёма нового") {
		t.Fatalf("нет строки о запрете на время смены:\n%s", z.String())
	}
	if zapuskov != 1 || len(pogasheno) != 1 || pogasheno[0] != os.Getpid() {
		t.Fatalf("ядро 0.6.98 не сменено: запусков %d, погашено %v\n%s", zapuskov, pogasheno, z.String())
	}
	novyy, err := os.ReadFile(s.Yadro.PutKonfiga())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(novyy), "щелчок") || strings.Contains(string(novyy), "заглушка") {
		t.Fatalf("в новом конфиге остался селектор щелчка: %s", novyy)
	}
	if got := konfig.MostDomaIzKonfiga(novyy); got != vyhod {
		t.Fatalf("detour эндпоинта %q, ждали прежний выход %q", got, vyhod)
	}

	s.dom.rabotaetDlyaStenda = func() bool { return true }
	s.dom.proba = func(context.Context, string, string) error { return errors.New("молчит") }
	s.dom.most = func(context.Context) (bool, string) { return true, vyhod }
	var prichiny []string
	s.dom.perezapustit = func(prichina string, _ uint64) { prichiny = append(prichiny, prichina) }
	for i := 0; i < 3; i++ {
		s.proveritSvyazDoma(context.Background())
		s.dom.perezapuski.Wait()
	}
	if len(prichiny) != 1 || !strings.Contains(prichiny[0], "нет связи (неудачных проб подряд: 3) — перезапускаю ядро") {
		t.Fatalf("после приёма ядра «Дом» не лечится перезапуском: %q", prichiny)
	}
}

// Сеть вернулась, дом молчит, но и выход моста не отвечает (сеть ещё не
// ожила, узел лёг): перезапуск ядра «Дом» не вернёт, а дребезг Wi-Fi дёргал
// бы ядро каждые 30 с — ядро не трогаем.
func TestDomPoSmeneSetiMostMolchit(t *testing.T) {
	st := novyyStendSmenySeti(t, "13 192.168.1.5 192.168.1.1")
	z := zhurnalDlyaProverki(t)
	st.s.dom.most = func(context.Context) (bool, string) { return false, "Дом · мост" }
	st.otvet = errors.New("молчит")
	st.shag("")
	st.sejchas = st.sejchas.Add(45 * time.Second)
	st.shag("13 192.168.1.5 192.168.1.1")
	st.zhdat(t, 0, "мост молчит")
	if !strings.Contains(z.String(), "но и мост «Дом · мост» молчит — ядро не трогаю") {
		t.Fatalf("строки о молчащем мосте нет:\n%s", z.String())
	}
	if st.s.dom.lechim {
		t.Fatal("d.lechim не снят")
	}
}
