package sluzhba

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/pult"
)

// Пульт в службе: пробный ключ мозга, пульт на диске во временной папке.
type stendPulta struct {
	priv ed25519.PrivateKey
	p    *pult.Pult
}

func novyyStendPulta(t *testing.T) *stendPulta {
	t.Helper()
	t.Setenv("KELEVRA_DIR", t.TempDir())
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p := pult.Zagruzit(hranenie.PutPulta(), reestrDesktop(), umeyuDesktop)
	p.Klyuchi = []ed25519.PublicKey{priv.Public().(ed25519.PublicKey)}
	return &stendPulta{priv: priv, p: p}
}

func (st *stendPulta) blok(t *testing.T, d map[string]any) json.RawMessage {
	t.Helper()
	dannye, _ := json.Marshal(d)
	sig := ed25519.Sign(st.priv, append([]byte("kelevra-pult-v1\n"), dannye...))
	b, _ := json.Marshal(pult.Blok{Dannye: base64.StdEncoding.EncodeToString(dannye), Podpis: base64.StdEncoding.EncodeToString(sig)})
	return b
}

func dannyePulta(versiya int64, komu string, nastroyki map[string]any, komandy ...map[string]any) map[string]any {
	if komandy == nil {
		komandy = []map[string]any{}
	}
	return map[string]any{"v": 1, "versiya": versiya, "komu": komu, "nastroyki": nastroyki, "komandy": komandy}
}

func komandaPulta(id, chto string) map[string]any {
	return map[string]any{"id": id, "chto": chto, "do": time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "param": map[string]any{}}
}

// Без пульта (служба собрана мимо Novaya, ключей нет) всё берётся из кода —
// ровно те значения, что были до пульта.
func TestBezPultaZnacheniyaIzKoda(t *testing.T) {
	for _, s := range []*Sluzhba{
		{Nastroyki: &hranenie.Nastroyki{ObnovlyatMin: 60}},
		{Nastroyki: &hranenie.Nastroyki{ObnovlyatMin: 60}, pult: pult.Zagruzit("", reestrDesktop(), umeyuDesktop)},
	} {
		if got := s.periodProfilya(); got != time.Hour {
			t.Errorf("период профиля %s, был час", got)
		}
		if got := s.periodZhurnalov(time.Now()); got != PeriodOtpravkiZhurnalov {
			t.Errorf("период журнала %s", got)
		}
		if !s.avtomatRazreshen() || s.vykl(vyklZapasnoyPut) || s.vykl(vyklSverkaPravil) {
			t.Error("без пульта что-то выключено")
		}
		if !s.pult.FlagIli("obnovleniya.tiho", true) {
			t.Error("тихая установка без пульта выключена")
		}
	}
	s := &Sluzhba{Nastroyki: &hranenie.Nastroyki{ObnovlyatMin: 5}}
	if got := s.periodProfilya(); got != 15*time.Minute {
		t.Errorf("пол в 15 минут потерян: %s", got)
	}
}

// Каждый ключ реестра уникален, а таблица docs/PULT-KLYUCHI-desktop.md
// совпадает с реестром (KELEVRA_OBNOVIT_KLYUCHI=1 — переписать таблицу).
func TestTablicaKlyuchey(t *testing.T) {
	byli := map[string]bool{}
	for _, k := range reestrDesktop() {
		if byli[k.Imya] {
			t.Fatalf("ключ %s дважды", k.Imya)
		}
		byli[k.Imya] = true
	}
	for _, nado := range []string{"opros.info_min", "zhurnal.otpravka_min", "zhurnal.chasto_do", "obnovleniya.tiho", "funkcii.avtorezhim"} {
		if !byli[nado] {
			t.Errorf("ключа договора v1 %s нет в реестре Desktop", nado)
		}
	}
	tablica := tablicaKlyuchey()
	put := filepath.Join("..", "..", "docs", "PULT-KLYUCHI-desktop.md")
	if os.Getenv("KELEVRA_OBNOVIT_KLYUCHI") == "1" {
		if err := os.WriteFile(put, []byte(tablica), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(put)
	if err != nil {
		t.Fatalf("нет таблицы ключей: %v", err)
	}
	if strings.ReplaceAll(string(b), "\r\n", "\n") != tablica {
		t.Fatal("docs/PULT-KLYUCHI-desktop.md разошлась с реестром: KELEVRA_OBNOVIT_KLYUCHI=1 go test ./internal/sluzhba -run TestTablicaKlyuchey")
	}
}

// Сервер своей сети: /info с пультом, запоминает X-Pult каждого запроса.
type serverPulta struct {
	srv   *httptest.Server
	mu    sync.Mutex
	blok  json.RawMessage
	xPult []string
}

func (sp *serverPulta) zagolovki() []string {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return append([]string(nil), sp.xPult...)
}

func novyyServerPulta(t *testing.T) *serverPulta {
	sp := &serverPulta{}
	sp.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sp.mu.Lock()
		sp.xPult = append(sp.xPult, r.Header.Get("X-Pult"))
		blok := sp.blok
		sp.mu.Unlock()
		if !strings.HasSuffix(r.URL.Path, "/info") {
			http.NotFound(w, r)
			return
		}
		telo := map[string]any{"name": "owner", "active": true}
		if blok != nil {
			telo["pult"] = blok
		}
		_ = json.NewEncoder(w).Encode(telo)
	}))
	t.Cleanup(sp.srv.Close)
	return sp
}

func zhdat(t *testing.T, chto string, uslovie func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if uslovie() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("не дождался: %s", chto)
}

// Путь целиком: /info приносит подписанный пульт — настройки встают, команда
// выполняется один раз, квитанция уезжает в X-Pult следующего запроса,
// сводка лежит на диске.
func TestPultCherezSvodku(t *testing.T) {
	st := novyyStendPulta(t)
	sp := novyyServerPulta(t)
	s := &Sluzhba{
		Nastroyki: &hranenie.Nastroyki{Kod: "TESTOVYY_KOD_1234", DeviceID: "dev-1", RuchnoyVybor: true,
			Uzly: map[string]string{"Соединение": "Финляндия"}},
		Podpiska: &podpiska.Klient{Host: strings.TrimPrefix(sp.srv.URL, "http://"), Shema: "http", DeviceID: "dev-1"},
		pult:     st.p,
	}
	s.Podpiska.Pult = s.pult.Zagolovok
	sp.blok = st.blok(t, dannyePulta(5, "dev-1",
		map[string]any{"zhurnal.otpravka_min": 30, "obnovleniya.tiho": false, "neznakomyy": 1},
		komandaPulta("k1", "sbros_avtomata"), komandaPulta("k2", "stantsuy")))

	s.ObnovitSvedeniya(context.Background())
	zhdat(t, "квитанция k1", func() bool { return strings.Contains(s.pult.Zagolovok(), "k1:ok") })

	if s.Nastroyki.RuchnoyVybor || s.Nastroyki.Uzly != nil {
		t.Fatal("sbros_avtomata не забыл ручной выбор и выбор узла")
	}
	if got := s.periodZhurnalov(time.Now()); got != 30*time.Minute {
		t.Fatalf("период журнала из пульта %s", got)
	}
	if s.pult.FlagIli("obnovleniya.tiho", true) {
		t.Fatal("obnovleniya.tiho=false не встал")
	}
	if _, err := os.Stat(hranenie.PutSvodki()); err != nil {
		t.Fatalf("сводка не легла на диск: %v", err)
	}
	if n, _ := hranenie.Zagruzit(); n.RuchnoyVybor {
		t.Fatal("сброс автомата не сохранён на диск")
	}

	s.ObnovitSvedeniya(context.Background())
	zag := sp.zagolovki()
	posledniy := zag[len(zag)-1]
	for _, nado := range []string{"v=1;umeet=1;versiya=5", "k1:ok", "k2:ne_umeyu", "oshibki=neznakomyy"} {
		if !strings.Contains(posledniy, nado) {
			t.Errorf("в X-Pult нет %q: %q", nado, posledniy)
		}
	}
	if !strings.HasPrefix(zag[0], "v=1;umeet=1;versiya=0") {
		t.Errorf("первый запрос без пульта: X-Pult %q", zag[0])
	}
}

// Чужой адресат и чужой ключ — пульт не принят, прежнее держится.
func TestPultChuzhoyNePrinyat(t *testing.T) {
	st := novyyStendPulta(t)
	sp := novyyServerPulta(t)
	s := &Sluzhba{
		Nastroyki: &hranenie.Nastroyki{Kod: "TESTOVYY_KOD_1234", DeviceID: "dev-1"},
		Podpiska:  &podpiska.Klient{Host: strings.TrimPrefix(sp.srv.URL, "http://"), Shema: "http", DeviceID: "dev-1"},
		pult:      st.p,
	}
	sp.blok = st.blok(t, dannyePulta(3, "dev-2", map[string]any{"zhurnal.otpravka_min": 30}))
	s.ObnovitSvedeniya(context.Background())
	if s.periodZhurnalov(time.Now()) != PeriodOtpravkiZhurnalov || !strings.Contains(s.pult.Zagolovok(), "pult:komu") {
		t.Fatalf("чужой адресат: %q", s.pult.Zagolovok())
	}
	if s.svedeniya == nil {
		t.Fatal("отказ пульта не должен ронять сводку")
	}
}

func TestPereydiNaKlyuch(t *testing.T) {
	novyyStendPulta(t)
	var status int
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/k/TESTOVYY_KOD_1234/ustroystvo":
			w.WriteHeader(status)
			if status == http.StatusOK {
				_, _ = w.Write([]byte(`{"profil":"` + srvURL + `/u/KLYUCH_MASHINY"}`))
			} else {
				_, _ = w.Write([]byte(`{"tekst":"Лимит устройств"}`))
			}
		case r.URL.Path == "/u/KLYUCH_MASHINY":
			_, _ = w.Write([]byte(`{"outbounds":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL
	staryy := srv.URL + "/k/TESTOVYY_KOD_1234"
	s := &Sluzhba{
		Nastroyki: &hranenie.Nastroyki{Kod: "TESTOVYY_KOD_1234", Podpiska: staryy, DeviceID: "dev-1"},
		Podpiska:  &podpiska.Klient{Shema: "http", DeviceID: "dev-1"},
	}

	status = http.StatusForbidden
	if itog := s.pereytiNaKlyuch(context.Background()); itog != "oshibka-otkaz" {
		t.Fatalf("отказ двери: итог %q", itog)
	}
	if s.Nastroyki.Podpiska != staryy {
		t.Fatal("при отказе адрес подписки изменился")
	}

	status = http.StatusOK
	if itog := s.pereytiNaKlyuch(context.Background()); itog != "ok" {
		t.Fatalf("удача: итог %q", itog)
	}
	if s.Nastroyki.Podpiska != srv.URL+"/u/KLYUCH_MASHINY" || s.Nastroyki.Kod != "TESTOVYY_KOD_1234" {
		t.Fatalf("адрес после перехода %q, код %q", s.Nastroyki.Podpiska, s.Nastroyki.Kod)
	}
	if n, _ := hranenie.Zagruzit(); n.Podpiska != s.Nastroyki.Podpiska {
		t.Fatal("новый адрес не сохранён на диск")
	}
	if itog := s.pereytiNaKlyuch(context.Background()); itog != "ok" {
		t.Fatalf("повтор на своём ключе: %q", itog)
	}
}

// Выкатка через пульт: сборка из пульта главнее GitHub; proba и stop GitHub не спрашивают.
func TestNaytiObnovleniePoKanalu(t *testing.T) {
	st := novyyStendPulta(t)
	var github int
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		github++
		_, _ = w.Write([]byte(`[]`))
	}))
	defer gh.Close()
	t.Setenv("KELEVRA_RELIZY", gh.URL)
	s := &Sluzhba{Nastroyki: &hranenie.Nastroyki{DeviceID: "dev-1"}, pult: st.p}
	versiya := int64(0)
	prinyat := func(nastroyki map[string]any, obnovlenie map[string]any) {
		versiya++
		d := dannyePulta(versiya, "dev-1", nastroyki)
		if obnovlenie != nil {
			d["obnovlenie"] = obnovlenie
		}
		if _, err := st.p.Prinyat(st.blok(t, d), "dev-1"); err != nil {
			t.Fatal(err)
		}
	}
	sborka := map[string]any{"versiya_imya": "99.1.0", "url": "https://primer.invalid/Kelevra.exe", "sha256": strings.Repeat("c", 64), "razmer": 123}

	n, err := s.naytiObnovlenie(context.Background(), gh.Client())
	if err != nil || n != nil || github != 1 {
		t.Fatalf("без пульта — GitHub, как раньше: %v %v, GitHub %d", n, err, github)
	}
	prinyat(map[string]any{"obnovleniya.kanal": "proba"}, nil)
	if n, _ := s.naytiObnovlenie(context.Background(), gh.Client()); n != nil || github != 1 {
		t.Fatal("канал proba без сборки спросил GitHub или что-то нашёл")
	}
	prinyat(map[string]any{"obnovleniya.kanal": "proba"}, sborka)
	n, _ = s.naytiObnovlenie(context.Background(), gh.Client())
	if n == nil || n.Versiya != "99.1.0" || n.SsylkaPodpisi != "https://primer.invalid/Kelevra.exe.sig" || n.Sha256 != strings.Repeat("c", 64) || n.Razmer != 123 || github != 1 {
		t.Fatalf("сборка из пульта: %+v, GitHub %d", n, github)
	}
	prinyat(map[string]any{}, sborka) // obychnyy: сборка из пульта всё равно главнее
	if n, _ := s.naytiObnovlenie(context.Background(), gh.Client()); n == nil || github != 1 {
		t.Fatal("в обычном канале сборка из пульта не главнее GitHub")
	}
	prinyat(map[string]any{"obnovleniya.kanal": "stop"}, sborka)
	if n, _ := s.naytiObnovlenie(context.Background(), gh.Client()); n != nil || github != 1 {
		t.Fatal("канал stop обновляет")
	}
	sborka["versiya_imya"] = "0.0.1" // не новее установленной — как будто сборки нет
	prinyat(map[string]any{}, sborka)
	if n, _ := s.naytiObnovlenie(context.Background(), gh.Client()); n != nil || github != 2 {
		t.Fatalf("старая сборка из пульта: %v, GitHub %d", n, github)
	}
}

// Выключатель funkcii.avtorezhim: служитель не встаёт, выбор человека на
// диске не трогается; разрешили снова — встаёт сам.
func TestVyklyuchatelAvtomata(t *testing.T) {
	st := novyyStendPulta(t)
	s := &Sluzhba{Nastroyki: &hranenie.Nastroyki{DeviceID: "dev-1", Avtorezhim: true}, pult: st.p}
	s.avtorezhimDlyaSluzhitelya = func() *avtorezhim.Avtorezhim {
		return &avtorezhim.Avtorezhim{
			Dns: dnsFunc(func(ctx context.Context) (bool, error) {
				<-ctx.Done()
				return false, ctx.Err()
			}),
			Zadvizhka: avtorezhim.NovayaZadvizhka(avtorezhim.Neizvestno),
		}
	}
	defer s.OstanovitAvtorezhim()
	if _, err := st.p.Prinyat(st.blok(t, dannyePulta(1, "dev-1", map[string]any{"funkcii.avtorezhim": false})), "dev-1"); err != nil {
		t.Fatal(err)
	}
	s.ZapustitAvtorezhim(context.Background())
	if s.avtorezhimOtmena != nil {
		t.Fatal("служитель встал при выключенном пультом автомате")
	}
	if !s.Nastroyki.Avtorezhim {
		t.Fatal("выключатель пульта переписал выбор человека")
	}
	if s.avtorezhimAktualen(context.Background(), s.avtorezhimPokolenie) {
		t.Fatal("решение автомата применимо при выключенной функции")
	}
	if _, err := st.p.Prinyat(st.blok(t, dannyePulta(2, "dev-1", map[string]any{"funkcii.avtorezhim": true, "avtomat.podtverzhdeniy": 5})), "dev-1"); err != nil {
		t.Fatal(err)
	}
	s.primenitPult()
	if s.avtorezhimOtmena == nil {
		t.Fatal("разрешили автомат — служитель не встал")
	}
	if s.avtorezhimOtpechatok != "avtomat.podtverzhdeniy=5;" {
		t.Fatalf("отпечаток порогов %q", s.avtorezhimOtpechatok)
	}
	if _, err := st.p.Prinyat(st.blok(t, dannyePulta(3, "dev-1", map[string]any{"funkcii.avtorezhim": false})), "dev-1"); err != nil {
		t.Fatal(err)
	}
	s.primenitPult()
	if s.avtorezhimOtmena != nil {
		t.Fatal("выключили с сервера — служитель крутится")
	}
}

// Пороги автомата из пульта ложатся на экземпляр; без ключей — константы.
func TestPorogiAvtomataIzPulta(t *testing.T) {
	st := novyyStendPulta(t)
	s := &Sluzhba{Nastroyki: &hranenie.Nastroyki{}, pult: st.p}
	a := s.avtorezhimBoevoy()
	z := a.Dns.(*avtorezhim.DnsZond)
	if a.Zadvizhka.Nuzhno != 0 || a.PodryadDoPrichinyN != 0 || z.Taimaut != avtorezhim.TaimautPoUmolchaniyu {
		t.Fatal("без ключей пороги автомата изменились")
	}
	if _, err := st.p.Prinyat(st.blok(t, dannyePulta(1, "", nil)), ""); err == nil {
		t.Fatal("пульт без адресата принят")
	}
	_, err := st.p.Prinyat(st.blok(t, dannyePulta(1, "dev", map[string]any{
		"avtomat.podtverzhdeniy": 2, "avtomat.podryad_do_prichiny": 7, "avtomat.dns_taimaut_ms": 1500,
		"avtomat.dns_domeny": []string{"a.example"}, "avtomat.dns_kontrolnyy": "k.example", "avtomat.pamyat_priznaka_s": 90,
		"avtomat.domashnie_shlyuzy": []string{"aa:bb:cc:dd:ee:ff"}, "avtomat.interval_tikera_s": 30, "avtomat.povtorov_bystrogo": 2,
	})), "dev")
	if err != nil {
		t.Fatal(err)
	}
	a = s.avtorezhimBoevoy()
	z = a.Dns.(*avtorezhim.DnsZond)
	if a.Zadvizhka.Nuzhno != 2 || a.PodryadDoPrichinyN != 7 || a.PamyatPriznakaD != 90*time.Second ||
		z.Taimaut != 1500*time.Millisecond || z.KontrolnyyDomen != "k.example" || len(z.Domeny) != 1 || len(a.DomashnieShlyuzy) != 1 {
		t.Fatalf("пороги не легли: %+v %+v", a, z)
	}
	if pz, ok := a.DnsPryamoy("1.1.1.1:53", "").(*avtorezhim.DnsZond); !ok || pz.Taimaut != 1500*time.Millisecond {
		t.Fatal("прямой зонд без порогов пульта")
	}
	sl := &avtorezhim.Sluzhitel{}
	s.nastroitSluzhitelya(sl)
	if sl.Interval != 30*time.Second || sl.PovtorovBystrogo != 2 || sl.Pauza != 0 {
		t.Fatalf("служитель: %+v", sl)
	}
}

func TestPeriodZhurnalovChasto(t *testing.T) {
	st := novyyStendPulta(t)
	s := &Sluzhba{Nastroyki: &hranenie.Nastroyki{}, pult: st.p}
	do := time.Now().Add(time.Hour)
	_, _ = st.p.Prinyat(st.blok(t, dannyePulta(1, "d", map[string]any{"zhurnal.chasto_do": do.UTC().Format(time.RFC3339), "zhurnal.otpravka_min": 120})), "d")
	if got := s.periodZhurnalov(time.Now()); got != ChastoZhurnal {
		t.Fatalf("до chasto_do период %s", got)
	}
	if got := s.periodZhurnalov(do.Add(time.Minute)); got != 2*time.Hour {
		t.Fatalf("после chasto_do период %s", got)
	}
	// Правило «пора» с периодом из пульта.
	seychas := time.Now()
	if poraOtpravlyatZhurnalyS(seychas, seychas.Add(-11*time.Minute), time.Time{}, 0, ChastoZhurnal) != true {
		t.Fatal("частый журнал не уходит через 11 минут")
	}
}

func TestPulsXSostoyanie(t *testing.T) {
	st := novyyStendPulta(t)
	s := &Sluzhba{Nastroyki: &hranenie.Nastroyki{RuchnoyVybor: true}, pult: st.p}
	s.obnovitPuls(true, false, "Финляндия")
	p := s.sostoyanieDlyaZagolovka()
	if !strings.HasPrefix(p, "avto=ruchnoy;tunnel=0;set=1;perehod=") || !strings.HasSuffix(p, ";uzel=%D0%A4%D0%B8%D0%BD%D0%BB%D1%8F%D0%BD%D0%B4%D0%B8%D1%8F") {
		t.Fatalf("пульс %q", p)
	}
	perehod := s.pulsPerehod
	time.Sleep(5 * time.Millisecond)
	s.obnovitPuls(true, false, "")
	if !s.pulsPerehod.Equal(perehod) {
		t.Fatal("переход отмечен без смены режима")
	}
	s.obnovitPuls(true, true, "")
	if s.pulsPerehod.Equal(perehod) || !strings.Contains(s.sostoyanieDlyaZagolovka(), "tunnel=1") {
		t.Fatal("подъём туннеля не отмечен переходом")
	}
}

func TestKomandaNeumeyuIProba(t *testing.T) {
	s := &Sluzhba{Nastroyki: &hranenie.Nastroyki{}}
	if itog := s.vypolnitKomandu(context.Background(), pult.Komanda{Chto: "chto_to"}); itog != "ne_umeyu" {
		t.Fatalf("итог %q", itog)
	}
	if itog := s.proba(context.Background(), ""); itog != "oshibka-net_celi" {
		t.Fatalf("проба без цели: %q", itog)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	itog := s.proba(context.Background(), srv.URL)
	if !strings.HasPrefix(itog, "ok-tun.nepr-mimo.") || strings.HasSuffix(itog, ".net") {
		t.Fatalf("проба мимо туннеля до живого адреса (формат как у Box): %q", itog)
	}
	if itog := s.proba(context.Background(), "http://127.0.0.1:1/"); itog != "ok-tun.nepr-mimo.net" {
		t.Fatalf("проба до мёртвого адреса: %q", itog)
	}
	if !strings.Contains(s.Diagnostika(), "versiya=") {
		t.Fatal("диагностика пустая")
	}
}

// X-Pult и X-Sostoyanie едут и с журналом (/logs) — это тоже запрос с
// заголовками устройства.
func TestXPultSZhurnalom(t *testing.T) {
	st := novyyStendPulta(t)
	s := &Sluzhba{Nastroyki: &hranenie.Nastroyki{DeviceID: "dev-1"}, Podpiska: &podpiska.Klient{}, pult: st.p}
	s.obnovitPuls(false, false, "")
	h := http.Header{}
	s.otpravshchikZhurnalov().Zagolovki(h, "dev-1", "0.6.90")
	if h.Get("X-Device-Id") != "dev-1" || h.Get("X-Pult") != "v=1;umeet=1;versiya=0" || !strings.HasPrefix(h.Get("X-Sostoyanie"), "avto=pauza;tunnel=0;set=0") {
		t.Fatalf("заголовки журнала: %v", h)
	}
	// Без пульта — ровно прежние заголовки.
	s2 := &Sluzhba{Nastroyki: &hranenie.Nastroyki{DeviceID: "dev-1"}, Podpiska: &podpiska.Klient{}}
	h2 := http.Header{}
	s2.otpravshchikZhurnalov().Zagolovki(h2, "dev-1", "0.6.90")
	if h2.Get("X-Pult") != "" || h2.Get("X-Sostoyanie") != "" {
		t.Fatalf("без пульта появились заголовки пульта: %v", h2)
	}
}

// Долгий опрос: коды выдачи (docs/PULT.md, «Провод») — разные паузы.
func TestPauzaDolgogoOprosa(t *testing.T) {
	sluchai := []struct {
		err    error
		zhdat  time.Duration
		rastit bool
	}{
		{&podpiska.OshibkaZhdat{Status: http.StatusForbidden}, 10 * time.Minute, false},
		{&podpiska.OshibkaZhdat{Status: http.StatusTooManyRequests, RetryAfter: 90 * time.Second}, 90 * time.Second, false},
		{&podpiska.OshibkaZhdat{Status: http.StatusTooManyRequests}, time.Minute, false},
		{&podpiska.OshibkaZhdat{Status: http.StatusNotFound}, 30 * time.Minute, false},
		{&podpiska.OshibkaZhdat{Status: http.StatusBadGateway}, 2 * time.Minute, true},
		{errors.New("сеть легла"), 2 * time.Minute, true},
	}
	for _, sl := range sluchai {
		if z, r := pauzaDolgogo(sl.err, 2*time.Minute); z != sl.zhdat || r != sl.rastit {
			t.Errorf("%v: пауза %v (растить %v), ждали %v (%v)", sl.err, z, r, sl.zhdat, sl.rastit)
		}
	}
}

// Причина сбоя выкатки словом для X-Pult — та же, что у Box.
func TestPrichinaObnovleniya(t *testing.T) {
	if p := prichinaObnovleniya(fmt.Errorf("x: %w", obnovlenie.ErrSha256)); p != "hesh" {
		t.Fatalf("sha256: %q", p)
	}
	if p := prichinaObnovleniya(fmt.Errorf("x: %w", obnovlenie.ErrPodpis)); p != "podpis" {
		t.Fatalf("подпись: %q", p)
	}
	if p := prichinaObnovleniya(errors.New("что-то")); p != "fayl" {
		t.Fatalf("прочее: %q", p)
	}
	if itogKomandy(obnovlenie.ErrSha256) != "oshibka-hesh" {
		t.Fatal("квитанция sha256 не oshibka-hesh")
	}
}
