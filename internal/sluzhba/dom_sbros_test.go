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
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// pultStend — подставной Clash API для сброса сети ядра: сколько раз
// сбрасывали; oshibka — сброс отвергнут (ядро ответило ошибкой или молчит).
type pultStend struct {
	mu      sync.Mutex
	sbrosy  int
	oshibka error
	// naSbros — зовётся на каждый принятый сброс.
	naSbros func()
}

func novyyPultStend() *pultStend { return &pultStend{} }

func (p *pultStend) SbrositSet(time.Duration) error {
	p.mu.Lock()
	if p.oshibka != nil {
		err := p.oshibka
		p.mu.Unlock()
		return err
	}
	p.sbrosy++
	hook := p.naSbros
	p.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (p *pultStend) skolko() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sbrosy
}

func zhurnalDlyaProverki(t *testing.T) *syncBuffer {
	t.Helper()
	z := &syncBuffer{}
	log.SetOutput(z)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return z
}

// Сброс помог: три неудачи — сброс сети ядра, ядро не тронуто; следующая
// проба удачна — в журнале, за сколько секунд ожил дом, лестница сброшена.
func TestDomSbrosOzhil(t *testing.T) {
	st := novyyStendLecheniya(t)
	z := zhurnalDlyaProverki(t)
	for i := 0; i < 3; i++ {
		st.proba()
	}
	st.zhdat(t, 1, 0, "три неудачи")
	if !strings.Contains(z.String(), "дом: нет связи (неудачных проб подряд: 3) — сбрасываю сеть ядра, туннель не трогаю") {
		t.Fatalf("сброса нет в журнале:\n%s", z.String())
	}
	st.sejchas = st.sejchas.Add(ShagSlezhkiZaDomom)
	st.otvet = nil
	st.proba()
	st.zhdat(t, 1, 0, "дом ожил")
	if !strings.Contains(z.String(), "дом: на связи после сброса сети за 30 с") {
		t.Fatalf("оживления после сброса нет в журнале:\n%s", z.String())
	}
	if st.s.dom.pauzaLecheniya != 0 || !st.s.dom.sbrosiliV.IsZero() {
		t.Fatalf("лестница не сброшена: пауза %v, сброс %v", st.s.dom.pauzaLecheniya, st.s.dom.sbrosiliV)
	}
	// Снова молчит — снова сброс сразу по порогу, без паузы.
	st.otvet = errors.New("молчит")
	for i := 0; i < 3; i++ {
		st.proba()
	}
	st.zhdat(t, 2, 0, "новый сбой после оживления")
	if strings.Contains(z.String(), "защит") {
		t.Fatalf("в строках лечения слово «защита»:\n%s", z.String())
	}
}

// Ядро не приняло сброс (ошибка HTTP или срок вышел) — сброса не было,
// перезапуск ядра сразу, в той же пробе, без ожидания двух проб.
func TestDomSbrosNeVyshel(t *testing.T) {
	st := novyyStendLecheniya(t)
	st.pult.mu.Lock()
	st.pult.oshibka = errors.New("ядро ответило 500")
	st.pult.mu.Unlock()
	for i := 0; i < 3; i++ {
		st.proba()
	}
	st.zhdat(t, 0, 1, "сброс отвергнут")
	if p := st.perezapuski[0]; !strings.Contains(p, "сеть ядра не сбросилась (ядро ответило 500) — перезапускаю ядро") {
		t.Fatalf("причина: %q", p)
	}
	if st.s.dom.pauzaLecheniya != 10*time.Minute {
		t.Fatalf("ступень лестницы %v, ждали 10m: перезапуск после неудачного сброса — то же лечение", st.s.dom.pauzaLecheniya)
	}
}

// Настоящий Clash API (yadro.SbrositSet): DELETE /connections с паролем;
// ядро не ответило в срок — перезапуск сразу.
func TestDomSbrosCherezClashApi(t *testing.T) {
	var mu sync.Mutex
	var zaprosy []string
	molchat := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		zaprosy = append(zaprosy, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		select {
		case <-molchat:
			<-r.Context().Done()
			return
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	st := novyyStendLecheniya(t)
	st.s.dom.pult = nil
	st.s.Yadro.Api = strings.TrimPrefix(srv.URL, "http://")
	st.s.Yadro.Sekret = "sekret"
	st.s.Yadro.Klient = &http.Client{Timeout: 200 * time.Millisecond}
	for i := 0; i < 3; i++ {
		st.proba()
	}
	st.zhdat(t, 0, 0, "сброс через Clash API")
	mu.Lock()
	got := append([]string(nil), zaprosy...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "DELETE /connections Bearer sekret" {
		t.Fatalf("запросы к Clash API: %v", got)
	}
	if st.s.dom.sbrosiliV.IsZero() {
		t.Fatal("удачный сброс не запомнен — перезапуск после двух проб не наступит")
	}

	// Новое лечение (пауза вышла), а ядро молчит дольше srokSbrosa — сразу
	// перезапуск. Срок сброса — свой (5 с), а не короткий клиент проверки:
	// ждём не дольше него.
	st.otvet = nil
	st.proba()
	st.otvet = errors.New("молчит")
	close(molchat)
	st.sejchas = st.sejchas.Add(time.Hour)
	nachalo := time.Now()
	for i := 0; i < 3; i++ {
		st.proba()
	}
	if proshlo := time.Since(nachalo); proshlo > srokSbrosa+3*time.Second {
		t.Fatalf("сброс ждал ядро %v, срок %v", proshlo, srokSbrosa)
	}
	st.zhdat(t, 0, 1, "ядро не ответило на сброс")
	if p := st.perezapuski[0]; !strings.Contains(p, "сеть ядра не сбросилась") {
		t.Fatalf("причина: %q", p)
	}
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
// событие доходит, только когда проверка (и сброс) закончены.
func (st *stendSmenySeti) shag(otpechat string) {
	st.mu.Lock()
	st.otpechat = otpechat
	st.mu.Unlock()
	st.sobytiya <- struct{}{}
	st.tiki <- time.Time{}
	st.sobytiya <- struct{}{}
}

// Wi-Fi выключили и включили (замер 06.11, 20:43:52–20:44:37): пока сети
// нет — не сбрасываем; сеть вернулась — сброс сети ядра сразу, без проб;
// следующая проба удачна — строка «на связи после сброса». События от
// своего TUN и повтор той же сети сброса не дают. Сеть снова сменилась
// раньше 30 с с прошлого сброса — не сбрасываем; позже — сбрасываем.
func TestDomSbrosPoSmeneSeti(t *testing.T) {
	st := novyyStendSmenySeti(t, "13 192.168.1.5 192.168.1.1")
	z := zhurnalDlyaProverki(t)
	// До выключения дом был на связи.
	st.otvet = nil
	st.proba()
	st.otvet = errors.New("молчит")

	st.shag("") // Wi-Fi выключен
	st.proba()
	st.proba()
	if n := st.pult.skolko(); n != 0 {
		t.Fatalf("сети нет, а сеть ядра сбрасывали %d раз", n)
	}
	st.sejchas = st.sejchas.Add(45 * time.Second)
	st.shag("13 192.168.1.5 192.168.1.1") // включён, та же сеть и тот же адрес
	if n := st.pult.skolko(); n != 1 {
		t.Fatalf("сеть вернулась: сбросов %d, ждали 1", n)
	}
	if !strings.Contains(z.String(), "дом: сеть сменилась — сбрасываю сеть ядра, туннель не трогаю (сеть вернулась)") {
		t.Fatalf("сброса по смене сети нет в журнале:\n%s", z.String())
	}
	if st.s.dom.neudachPodryad != 0 {
		t.Fatalf("неудачи до смены сети в счёте: %d", st.s.dom.neudachPodryad)
	}
	st.sejchas = st.sejchas.Add(10 * time.Second)
	st.otvet = nil
	st.proba()
	if !strings.Contains(z.String(), "дом: на связи после сброса сети за 10 с") {
		t.Fatalf("оживления после сброса нет в журнале:\n%s", z.String())
	}
	st.zhdat(t, 1, 0, "сброс по смене сети — не лечение")

	// Свой TUN поднялся или сменился адрес IPv6: физическая сеть та же.
	st.sejchas = st.sejchas.Add(time.Minute)
	st.shag("13 192.168.1.5 192.168.1.1")
	if n := st.pult.skolko(); n != 1 {
		t.Fatalf("физическая сеть та же, а сбросов %d", n)
	}
	// Другая сеть через минуту — сброс; ещё одна через 10 с — отсечка.
	st.shag("13 172.20.10.3 172.20.10.1")
	if n := st.pult.skolko(); n != 2 {
		t.Fatalf("другая сеть: сбросов %d, ждали 2", n)
	}
	st.sejchas = st.sejchas.Add(10 * time.Second)
	st.shag("13 10.0.0.7 10.0.0.1")
	if n := st.pult.skolko(); n != 2 {
		t.Fatalf("смена раньше 30 с с прошлого сброса: сбросов %d, ждали 2", n)
	}
	if !strings.Contains(z.String(), "сбрасывали 10 с назад — не сбрасываю") {
		t.Fatalf("отсечки нет в журнале:\n%s", z.String())
	}
	st.sejchas = st.sejchas.Add(30 * time.Second)
	st.shag("13 192.168.1.5 192.168.1.1")
	if n := st.pult.skolko(); n != 3 {
		t.Fatalf("смена через 40 с: сбросов %d, ждали 3", n)
	}
	if strings.Contains(z.String(), "защит") {
		t.Fatalf("в новых строках слово «защита»:\n%s", z.String())
	}
}

// Сеть вернулась, но туннель не поднят (ядро стоит), «Дома» нет в конфиге
// или идёт перезапуск — сбрасывать нечего; первая смена при поднятом
// туннеле — сброс.
func TestDomSbrosPoSmeneSetiNeVovremya(t *testing.T) {
	st := novyyStendSmenySeti(t, "13 192.168.1.5 192.168.1.1")
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
	if n := st.pult.skolko(); n != 0 {
		t.Fatalf("сбросов %d при стоящем ядре, без «Дома» и при занятой очереди", n)
	}
	st.shag("13 192.168.1.5 192.168.1.1")
	if n := st.pult.skolko(); n != 1 {
		t.Fatalf("туннель поднят, сеть сменилась: сбросов %d, ждали 1", n)
	}
}

// Адрес появился раньше шлюза: первое событие застаёт сеть пустой, нового
// события нет — слежка смотрит ещё несколько раз сама и сбрасывает, когда
// шлюз появился.
func TestDomSbrosShlyuzPozzheAdresa(t *testing.T) {
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
	st.tiki <- time.Time{}    // повтор: шлюз есть — сброс
	st.sobytiya <- struct{}{} // проверка закончена
	if n := st.pult.skolko(); n != 1 {
		t.Fatalf("сбросов %d, ждали 1", n)
	}
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
// block). Служба такое ядро не принимает как есть (конфиг устарел), а плавно
// меняет: новое ядро на конфиге без селектора, старое гасится. Дальше «Дом»
// лечится сбросом сети, без паники и без вечной сверки.
func TestDomPriyomYadra0698SoShchelchkom(t *testing.T) {
	s := stendSPravami(t)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s.dom.kesh.Klyuchi = []ed25519.PublicKey{priv.Public().(ed25519.PublicKey)}
	s.Nastroyki.Slot = "01-7"
	pult := novyyPultStend()
	s.dom.pult = pult
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
		t.Fatalf("ядро 0.6.98 не сменено плавно: запусков %d, погашено %v\n%s", zapuskov, pogasheno, z.String())
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
	s.dom.perezapustit = func(string, uint64) {
		t.Fatal("сброс сети вышел, а ядро перезапускают")
	}
	for i := 0; i < 3; i++ {
		s.proveritSvyazDoma(context.Background())
		s.dom.perezapuski.Wait()
	}
	if n := pult.skolko(); n != 1 {
		t.Fatalf("сбросов сети %d, ждали 1", n)
	}
}
