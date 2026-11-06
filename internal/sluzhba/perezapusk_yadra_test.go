package sluzhba

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// profilS — тестовый профиль телефона с правкой.
func profilS(t *testing.T, pravka func(d map[string]any)) []byte {
	t.Helper()
	b, err := os.ReadFile("../konfig/testdata/profil_telefona.json")
	if err != nil {
		t.Fatal(err)
	}
	if pravka == nil {
		return b
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	pravka(d)
	b, err = json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func vyhodProfilya(d map[string]any, teg string) map[string]any {
	for _, o := range d["outbounds"].([]any) {
		if o.(map[string]any)["tag"] == teg {
			return o.(map[string]any)
		}
	}
	return nil
}

func bezVyhoda(d map[string]any, tegi ...string) {
	var ost []any
	for _, o := range d["outbounds"].([]any) {
		ubrat := false
		for _, t := range tegi {
			ubrat = ubrat || o.(map[string]any)["tag"] == t
		}
		if !ubrat {
			ost = append(ost, o)
		}
	}
	d["outbounds"] = ost
}

// Ядро работает на tun126 (tun125 был занят — подобрано соседнее имя, адреса
// сдвинуты). Новый профиль приходит каждый час — одно правило:
//   - тот же профиль — перезапуск не нужен (сдвиг имени и адреса не в счёт);
//   - те же узлы, другие параметры — отложен, ядро не трогается;
//   - профиль вернулся — отложенный снят;
//   - узел, на котором сидим, пропал — перезапуск сразу.
func TestPravilo_NovyyProfil(t *testing.T) {
	s := gotovStendLestnicy(t)
	var v konfig.Vybor
	s.dobavitPravilaIzKesha(&v, false)
	v.TunImya, v.TunSdvigAdresa = "tun126", 1
	rabota, _, err := s.sobratKonfig(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rabota), "tun126") {
		t.Fatal("живой конфиг не на tun126")
	}
	s.otlozh.konfigVRabote = func() (yadro.KonfigRaboty, bool) { return yadro.KonfigRaboty{Telo: rabota}, true }
	s.vybranoDlyaStenda = func([]byte) map[string]string {
		return map[string]string{"Соединение": "Нидерланды", "Нидерланды": "Нидерланды · прямой"}
	}
	var srazu []string
	s.otlozh.perezapustit = func(prichina string) bool { srazu = append(srazu, prichina); return true }
	zhdet := func() bool { _, _, n := s.otlozh.nuzhenS(); return n }

	if err := s.SohranitProfil(profilS(t, nil)); err != nil {
		t.Fatal(err)
	}
	if zhdet() || len(srazu) > 0 {
		t.Fatalf("тот же профиль (ядро на tun126): отложено %v, сразу %v", zhdet(), srazu)
	}

	if err := s.SohranitProfil(profilS(t, func(d map[string]any) { vyhodProfilya(d, "Нидерланды · прямой")["server_port"] = 8443 })); err != nil {
		t.Fatal(err)
	}
	if !zhdet() || len(srazu) > 0 {
		t.Fatalf("другие параметры узла: отложено %v, сразу %v — ждали только отложенный", zhdet(), srazu)
	}
	if _, slova, _ := s.otlozh.nuzhenS(); !strings.Contains(slova, "Нидерланды · прямой") {
		t.Fatalf("причина отложенного: %q", slova)
	}

	if err := s.SohranitProfil(profilS(t, nil)); err != nil {
		t.Fatal(err)
	}
	if zhdet() {
		t.Fatal("профиль вернулся, а отложенный перезапуск остался")
	}

	if err := s.SohranitProfil(profilS(t, func(d map[string]any) {
		bezVyhoda(d, "Нидерланды", "Нидерланды · прямой")
		vyhodProfilya(d, "Соединение")["outbounds"] = []any{"Комната"}
		vyhodProfilya(d, "Соединение")["default"] = "Комната"
	})); err != nil {
		t.Fatal(err)
	}
	if len(srazu) != 1 || !strings.Contains(srazu[0], "пропал") {
		t.Fatalf("узел, на котором сидим, пропал: сразу %v", srazu)
	}
}

// Приём живого ядра при обновлении — то же правило, что и у профиля:
// ядро на tun126 принимается как есть (раньше — «конфиг устарел» и смена
// при каждом обновлении), другие параметры — принимается с отложенным
// перезапуском, пропавший узел — смена сразу, а если новый конфиг ядро не
// принимает — остаёмся на живом.
func TestPravilo_PriyomYadra(t *testing.T) {
	s := gotovStendLestnicy(t)
	sobrat := func(imya string, sdvig int) []byte {
		var v konfig.Vybor
		s.dobavitPravilaIzKesha(&v, false)
		v.TunImya, v.TunSdvigAdresa = imya, sdvig
		b, _, err := s.sobratKonfig(v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	s.vybranoDlyaStenda = func([]byte) map[string]string { return map[string]string{"Соединение": "Нидерланды"} }
	// Ядро прежней версии без снимка: конфиг его берётся из config.json, если
	// отпечаток сходится с запиской.
	pod := func(rabota []byte) yadro.Peredacha {
		if err := os.WriteFile(s.Yadro.PutKonfiga(), rabota, 0o600); err != nil {
			t.Fatal(err)
		}
		return yadro.Peredacha{PID: 4242, Otpechatok: yadro.OtpechatokKonfigaBayt(rabota)}
	}

	smenit, r, _ := s.reshenieDlyaPriyoma(pod(sobrat("tun126", 1)))
	if smenit || r.Nuzhda != yadro.NeNuzhen {
		t.Fatalf("ядро на tun126: сменить %v, %s (%s)", smenit, r.Nuzhda, r.Slovami())
	}

	// Живое ядро — на прежних параметрах узла: профиль на диске новее.
	staryyProfil := profilS(t, func(d map[string]any) { vyhodProfilya(d, "Нидерланды · прямой")["server_port"] = 8443 })
	if err := hranenie.ZapisatZakryto(hranenie.PutProfilya(), staryyProfil); err != nil {
		t.Fatal(err)
	}
	rabota := sobrat("", 0)
	if err := hranenie.ZapisatZakryto(hranenie.PutProfilya(), profilS(t, nil)); err != nil {
		t.Fatal(err)
	}
	smenit, r, _ = s.reshenieDlyaPriyoma(pod(rabota))
	if smenit || r.Nuzhda != yadro.Otlozhit {
		t.Fatalf("другие параметры узла: сменить %v, %s", smenit, r.Nuzhda)
	}
	s.planirovatPoslePriyoma(r)
	if _, _, n := s.otlozh.nuzhenS(); !n {
		t.Fatal("принято с устаревшим конфигом, а перезапуск не отложен")
	}

	// Живое ядро сидит на «Комнате» — в новом профиле её нет.
	staryyProfil = profilS(t, nil)
	novyyProfil := profilS(t, func(d map[string]any) {
		bezVyhoda(d, "Комната")
		vyhodProfilya(d, "Соединение")["outbounds"] = []any{"Нидерланды"}
	})
	if err := hranenie.ZapisatZakryto(hranenie.PutProfilya(), staryyProfil); err != nil {
		t.Fatal(err)
	}
	rabota = sobrat("", 0)
	if err := hranenie.ZapisatZakryto(hranenie.PutProfilya(), novyyProfil); err != nil {
		t.Fatal(err)
	}
	s.vybranoDlyaStenda = func([]byte) map[string]string { return map[string]string{"Соединение": "Комната"} }
	proverok := 0
	s.proverkaKonfiga = func(context.Context, []byte) error { proverok++; return nil }
	smenit, r, _ = s.reshenieDlyaPriyoma(pod(rabota))
	if !smenit || r.Nuzhda != yadro.Srazu || proverok != 1 {
		t.Fatalf("пропал узел, на котором сидим: сменить %v, %s, проверок %d", smenit, r.Nuzhda, proverok)
	}

	z := zhurnalDlyaProverki(t)
	s.proverkaKonfiga = func(context.Context, []byte) error { return yadro.OshibkaProverki{Vyvod: "FATAL неизвестное поле"} }
	smenit, _, _ = s.reshenieDlyaPriyoma(pod(rabota))
	if smenit {
		t.Fatal("ядро отвергло новый конфиг, а живое всё равно меняем")
	}
	if !strings.Contains(z.String(), "рабочее ядро не трогаю, остаюсь на прежнем") {
		t.Fatalf("нет строки об отказе проверки:\n%s", z.String())
	}
}

// Перезапуск ради нового конфига: ядро конфиг отвергло — рабочее ядро не
// погашено и не поднято заново; приняло — перезапуск идёт.
func TestPerezapuskNaNovomProverkaDoOstanovki(t *testing.T) {
	s := gotovStendLestnicy(t)
	s.zashchitaPodnyataDlyaStenda = func() bool { return true }
	zapuskov := 0
	s.zapustitYadro = func(context.Context) error { zapuskov++; return nil }
	s.otlozhitPerezapusk(prichinaKonfig, "новый профиль: выходы: ~узел", "")
	z := zhurnalDlyaProverki(t)

	s.proverkaKonfiga = func(context.Context, []byte) error { return yadro.OshibkaProverki{Vyvod: "FATAL decode config"} }
	if !s.perezapustitNaNovom(context.Background(), "проверка", s.pokolenieAvtorezhima()) {
		t.Fatal("отказ проверки не закончил отложенное")
	}
	if zapuskov != 0 || strings.Contains(z.String(), "перезапускаю ядро") {
		t.Fatalf("ядро отвергло конфиг, а перезапуск пошёл: запусков %d\n%s", zapuskov, z.String())
	}
	if _, _, n := s.otlozh.nuzhenS(); n {
		t.Fatal("отвергнутый конфиг остался в отложенных")
	}
	// Тот же конфиг ежечасно — снова не планируется.
	telo, _ := s.zhelaemyyKonfig()
	s.otlozhitPerezapusk(prichinaKonfig, "снова", yadro.OtpechatokKonfigaBayt(telo))
	if _, _, n := s.otlozh.nuzhenS(); n {
		t.Fatal("отвергнутый ядром конфиг запланирован снова")
	}

	s.proverkaKonfiga = func(context.Context, []byte) error { return nil }
	if !s.perezapustitNaNovom(context.Background(), "проверка", s.pokolenieAvtorezhima()) || zapuskov != 1 {
		t.Fatalf("принятый конфиг: запусков %d", zapuskov)
	}

	// Проверить не вышло — перезапуск как раньше.
	s.proverkaKonfiga = func(context.Context, []byte) error { return yadro.ErrProverkaNeSostoyalas }
	if !s.perezapustitNaNovom(context.Background(), "проверка", s.pokolenieAvtorezhima()) || zapuskov != 2 {
		t.Fatalf("проверка не состоялась: запусков %d", zapuskov)
	}
}

// Смена блока «Дома» при поднятом туннеле: новый конфиг ядро отвергло —
// ядро не перезапускается (раньше гасилось и падало на подъёме).
func TestDomPerezapuskProverkaKonfiga(t *testing.T) {
	s := gotovStendLestnicy(t)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s.dom.kesh.Klyuchi = []ed25519.PublicKey{priv.Public().(ed25519.PublicKey)}
	s.Nastroyki.Slot = "01-7"
	s.zashchitaPodnyataDlyaStenda = func() bool { return true }
	zapuskov := 0
	s.zapustitYadro = func(context.Context) error { zapuskov++; return nil }
	s.proverkaKonfiga = func(context.Context, []byte) error { return yadro.OshibkaProverki{Vyvod: "FATAL endpoints"} }
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 1, "100.76.0.1:51820")})
	s.dom.perezapuski.Wait()
	if zapuskov != 0 {
		t.Fatalf("ядро отвергло конфиг с «Домом», а перезапуск пошёл: %d", zapuskov)
	}
	s.proverkaKonfiga = nil // как в бою без ядра: проверка не состоялась — перезапуск как раньше
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 2, "100.76.0.1:51821")})
	s.dom.perezapuski.Wait()
	if zapuskov != 1 {
		t.Fatalf("проверка не состоялась: запусков %d, ждали 1", zapuskov)
	}
}

// Живое ядро прошлой копии, срочная смена, а ядро новый конфиг отвергло:
// прежнее не гасится и принимается как есть (полный путь PrinyatZhivoeYadro).
func TestPriyomProverkaOtverglaYadroNeTronuto(t *testing.T) {
	s := stendSPravami(t)
	zapuskov := 0
	s.zapustitYadro = func(context.Context) error { zapuskov++; return nil }
	// Живое ядро работало на конфиге с узлом «Старый», которого больше нет.
	var d map[string]any
	syroy, _ := os.ReadFile(s.Yadro.PutKonfiga())
	if err := json.Unmarshal(syroy, &d); err != nil {
		t.Fatal(err)
	}
	d["outbounds"] = append(d["outbounds"].([]any), map[string]any{"type": "direct", "tag": "Старый"})
	vyhodProfilya(d, "Соединение")["outbounds"] = []any{"Нидерланды", "Комната", "Старый"}
	staryy, _ := json.Marshal(d)
	if err := os.WriteFile(s.Yadro.PutKonfiga(), staryy, 0o600); err != nil {
		t.Fatal(err)
	}
	s.vybranoDlyaStenda = func([]byte) map[string]string { return map[string]string{"Соединение": "Старый"} }

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
	pogasitChuzhoe = func(pid int) { pogasheno = append(pogasheno, pid) }
	t.Cleanup(func() { pogasitChuzhoe = staroe })
	staryyPoisk := nashiYadra
	nashiYadra = func(string) []int { return nil }
	t.Cleanup(func() { nashiYadra = staryyPoisk })
	yadro.ZapisatPeredachu(hranenie.PapkaYadra(), yadro.Peredacha{PID: os.Getpid(), Bin: svoy, Api: api, Adapter: "tun125",
		Otpechatok: yadro.OtpechatokKonfigaBayt(staryy)})
	s.proverkaKonfiga = func(context.Context, []byte) error { return yadro.OshibkaProverki{Vyvod: "FATAL decode config"} }

	z := zhurnalDlyaProverki(t)
	s.PrinyatZhivoeYadro(context.Background(), "")
	if len(pogasheno) != 0 || zapuskov != 0 {
		t.Fatalf("ядро отвергло новый конфиг, а прежнее тронуто: погашено %v, запусков %d\n%s", pogasheno, zapuskov, z.String())
	}
	if s.Yadro.Sost() != yadro.Rabotaet || !strings.Contains(z.String(), "принял живое ядро") {
		t.Fatalf("прежнее ядро не принято: %s\n%s", s.Yadro.Sost(), z.String())
	}
}
