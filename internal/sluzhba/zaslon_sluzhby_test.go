package sluzhba

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/pult"
	"github.com/HRYNdev/kelevra-desktop/internal/ustroystvo"
	"github.com/HRYNdev/kelevra-desktop/internal/zaslon"
)

// Постоянный запрет мимо туннеля в службе (zaslon_perezapuska.go): кто и
// когда ставит уровень 1 и полный запрет. Настоящего WFP нет — подмена
// zaslonStenda (zaslon_perezapuska_test.go).

// pultStenda — пульт с пробным ключом в папке стенда (KELEVRA_DIR уже задан
// stend). nastroyki — что прислал «мозг».
func pultStenda(t *testing.T, s *Sluzhba, versiya int64, nastroyki map[string]any) {
	t.Helper()
	if s.pult == nil || len(s.pult.Klyuchi) == 0 {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		p := pult.Zagruzit(hranenie.PutPulta(), reestrDesktop(), umeyuDesktop)
		p.Klyuchi = []ed25519.PublicKey{priv.Public().(ed25519.PublicKey)}
		s.pult = p
		pultKlyuchStenda = priv
	}
	st := &stendPulta{priv: pultKlyuchStenda, p: s.pult}
	komu := ustroystvo.IdVZagolovke(s.Nastroyki.DeviceID)
	if _, err := s.pult.Prinyat(st.blok(t, dannyePulta(versiya, komu, nastroyki)), komu); err != nil {
		t.Fatal(err)
	}
	s.primenitPult()
}

var pultKlyuchStenda ed25519.PrivateKey

// Уровень 1: «подключено» — Anthropic только из туннеля (адреса туннеля из
// профиля), дома — ничего, «Отключить» — ничего и сразу.
func TestUroven1PoNamereniyu(t *testing.T) {
	s, sys, _, _ := stendZaslona(t, nil)
	s.ustanovitNamerenie(true, "проверка")
	if !sys.anthropic() {
		t.Fatal("«подключено»: запрета Anthropic нет")
	}
	if stoit, _ := sys.sostoyanie(); stoit {
		t.Fatal("«подключено» без строгого: полный запрет")
	}
	// Разрешение из туннеля — по адресам профиля (профиль телефона: 172.19.0.1/30).
	p := s.zaslon.Primeneno()
	estTunnel := false
	for _, x := range p.Tunnel {
		estTunnel = estTunnel || x.String() == "172.19.0.0/30"
	}
	if !estTunnel {
		t.Fatalf("адресов туннеля из профиля нет в правилах: %v", p.Tunnel)
	}
	if len(p.Tunnel) < 2 {
		t.Fatalf("нет запаса на сдвиг адреса (tun126…): %v", p.Tunnel)
	}
	if s.zapretDlyaOkna() != "anthropic" {
		t.Fatalf("поле состояния %q", s.zapretDlyaOkna())
	}

	// Дома (автомат включён и решил «дома») — ничего.
	s.avtorezhimZamok.Lock()
	s.Nastroyki.Avtorezhim = true
	s.avtorezhimZamok.Unlock()
	s.zashchitaPodnyataDlyaStenda = func() bool { return false }
	s.avtorezhimKolbek(context.Background(), avtorezhim.Doma, false)
	if !sys.pusto() {
		t.Fatal("дома запрет остался")
	}
	// Вне дома — уровень 1 ещё до подъёма туннеля.
	var videlAnthropic bool
	s.zapustitYadro = func(ctx context.Context) error {
		videlAnthropic = sys.anthropic()
		return nil
	}
	s.avtorezhimKolbek(context.Background(), avtorezhim.VneDoma, false)
	if !videlAnthropic {
		t.Fatal("уход из дома: при подъёме ядра запрета Anthropic не было")
	}

	// «Отключить» — сразу ничего.
	s.otklyuchit(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/otklyuchit", nil))
	if !sys.pusto() {
		t.Fatal("после «Отключить» запрет остался")
	}
	if s.zapretDlyaOkna() != "" {
		t.Fatalf("поле состояния после «Отключить» %q", s.zapretDlyaOkna())
	}
}

// Перезапуск при «подключено»: полный запрет на время перезапуска, после —
// уровень 1, а не пусто.
func TestPerezapuskOstavlyaetUroven1(t *testing.T) {
	s, sys, zhurnal, videl := stendZaslona(t, nil)
	s.ustanovitNamerenie(true, "проверка")
	s.perezapustit("проверка", s.pokolenieAvtorezhima(), true)
	if len(*videl) != 1 || !(*videl)[0] {
		t.Fatalf("при запуске ядра полного запрета не было: %v", *videl)
	}
	if stoit, _ := sys.sostoyanie(); stoit || !sys.anthropic() {
		t.Fatal("после перезапуска ждали уровень 1 без полного запрета")
	}
	if !strings.Contains(zhurnal.String(), "открыт: туннель поднят за") {
		t.Errorf("нет строки итога:\n%s", zhurnal.String())
	}
}

// Строгий режим из пульта: полный запрет с «подключено», переживает
// перезапуск, снимается дома и на «Отключить». Адреса Anthropic — из пульта.
func TestStrogiyIzPulta(t *testing.T) {
	s, sys, _, videl := stendZaslona(t, nil)
	s.ustanovitNamerenie(true, "проверка")
	pultStenda(t, s, 1, map[string]any{klyuchStrogiy: true, klyuchAnthropicSeti: []string{"1.2.3.0/24"}})
	if stoit, _ := sys.sostoyanie(); !stoit {
		t.Fatal("строгий из пульта: полного запрета нет")
	}
	p := s.zaslon.Primeneno()
	if len(p.Seti) != 1 || p.Seti[0].String() != "1.2.3.0/24" {
		t.Fatalf("адреса Anthropic не из пульта: %v", p.Seti)
	}
	if len(p.Programmy) == 0 || !strings.HasSuffix(p.Programmy[0], "sing-box.exe") && !strings.Contains(p.Programmy[0], "sing-box") {
		t.Fatalf("ядру не разрешено наружу: %v", p.Programmy)
	}
	s.perezapustit("проверка", s.pokolenieAvtorezhima(), true)
	if len(*videl) != 1 || !(*videl)[0] {
		t.Fatalf("перезапуск строгого: %v", *videl)
	}
	if stoit, _ := sys.sostoyanie(); !stoit {
		t.Fatal("строгий: после перезапуска полный запрет снят")
	}
	// Дома — ничего.
	s.avtorezhimZamok.Lock()
	s.Nastroyki.Avtorezhim = true
	s.avtorezhimZamok.Unlock()
	s.ustanovitDoma(true, "проверка")
	if !sys.pusto() {
		t.Fatal("строгий дома: запрет стоит")
	}
	// Автомат выключен (ручной режим) — «дома» не считается.
	s.avtorezhimZamok.Lock()
	s.Nastroyki.Avtorezhim = false
	s.avtorezhimZamok.Unlock()
	s.obnovitZaslon("проверка")
	if stoit, _ := sys.sostoyanie(); !stoit {
		t.Fatal("ручной режим: «дома» автомата сняло строгий запрет")
	}
	// «Отключить» — всё снято.
	s.otklyuchit(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/otklyuchit", nil))
	if !sys.pusto() {
		t.Fatal("строгий: после «Отключить» запрет остался")
	}
}

// Выключатель пульта vykl.zapret_anthropic снимает уровень 1.
func TestVyklyuchatelUrovnya1(t *testing.T) {
	s, sys, _, _ := stendZaslona(t, nil)
	s.ustanovitNamerenie(true, "проверка")
	if !sys.anthropic() {
		t.Fatal("уровня 1 нет")
	}
	pultStenda(t, s, 1, map[string]any{vyklZapretAnthropic: true})
	if !sys.pusto() {
		t.Fatal("выключатель пульта не снял уровень 1")
	}
}

// Обновление exe: правила уходят держателю, уходящая копия ждёт его метку;
// новая копия берёт намерение из передачи и отпускает держателя.
func TestPeredachaZapretaPriObnovlenii(t *testing.T) {
	s, sys, zhurnal, _ := stendZaslona(t, nil)
	s.ustanovitNamerenie(true, "проверка")
	if !sys.anthropic() {
		t.Fatal("уровня 1 нет")
	}
	derzh := &zaslonStenda{}
	ushyol := make(chan error, 1)
	s.zapustitDerzhatelya = func(put string) (func(), error) {
		go func() { ushyol <- zaslon.Derzhat(hranenie.Papka(), derzh, 5*time.Millisecond, time.Now) }()
		return func() {}, nil
	}
	s.peredatZapretNovoyKopii("Kelevra.exe", "")
	if !derzh.anthropic() {
		t.Fatal("копия ушла раньше, чем держатель поставил правила")
	}
	if !strings.Contains(zhurnal.String(), "держит держатель до подъёма новой копии") {
		t.Errorf("нет строки о передаче:\n%s", zhurnal.String())
	}

	// Новая копия: автомат выключен, но намерение — из передачи.
	s2, err := Novaya()
	if err != nil {
		t.Fatal(err)
	}
	s2.avtorezhimZamok.Lock()
	namerenie := s2.namerenie
	s2.avtorezhimZamok.Unlock()
	if !namerenie {
		t.Fatal("новая копия не взяла намерение из передачи")
	}
	select {
	case err := <-ushyol:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("держатель не ушёл после старта новой копии")
	}
	if !derzh.pusto() {
		t.Fatal("держатель ушёл, не сняв правила")
	}
}

// Держатель не встал (старая сборка без флага, отказ WFP) — уходящая копия не
// ждёт дольше срока и снимает его.
func TestDerzhatelNeVstal(t *testing.T) {
	s, _, zhurnal, _ := stendZaslona(t, nil)
	s.ustanovitNamerenie(true, "проверка")
	staryy := srokGotovnostiDerzh
	srokGotovnostiDerzh = 50 * time.Millisecond
	t.Cleanup(func() { srokGotovnostiDerzh = staryy })
	snyali := false
	s.zapustitDerzhatelya = func(string) (func(), error) { return func() { snyali = true }, nil }
	s.peredatZapretNovoyKopii("Kelevra.exe", "")
	if !snyali {
		t.Fatal("невставший держатель не снят")
	}
	if !strings.Contains(zhurnal.String(), "держатель за") {
		t.Errorf("нет строки о невставшем держателе:\n%s", zhurnal.String())
	}
}

// Нечего держать (отключено) — держатель не запускается, намерение уходит.
func TestPeredachaBezPravil(t *testing.T) {
	s, sys, _, _ := stendZaslona(t, nil)
	if !sys.pusto() {
		t.Fatal("без намерения что-то стоит")
	}
	zapuskali := false
	s.zapustitDerzhatelya = func(string) (func(), error) { zapuskali = true; return func() {}, nil }
	s.peredatZapretNovoyKopii("Kelevra.exe", "")
	if zapuskali {
		t.Fatal("держатель запущен без правил")
	}
	if p, est := zaslon.ProchestPeredachu(hranenie.Papka(), time.Now()); !est || p.Namerenie {
		t.Fatalf("передача намерения: %+v %v", p, est)
	}
}

// Ручной режим (полоса «Вручную»): «подключено» — как стоит туннель.
func TestRuchnoyRezhimNamerenie(t *testing.T) {
	s, sys, _, _ := stendZaslona(t, nil)
	s.ustanovitNamerenie(true, "проверка")
	s.zashchitaPodnyataDlyaStenda = func() bool { return false }
	zapros := httptest.NewRequest("POST", "/api/avtorezhim", strings.NewReader(`{"vklyuchit":false}`))
	s.avtorezhimRuchka(httptest.NewRecorder(), zapros)
	if !sys.pusto() {
		t.Fatal("ручной режим без туннеля: запрет стоит")
	}
}

// Встаёт сборка старее этой (откат пультом) — держатель не запускается: она
// не знает флага и поднялась бы обычным окном.
func TestDerzhatelNeDlyaStaroyVersii(t *testing.T) {
	s, _, _, _ := stendZaslona(t, nil)
	s.ustanovitNamerenie(true, "проверка")
	zapuskali := false
	s.zapustitDerzhatelya = func(string) (func(), error) { zapuskali = true; return func() {}, nil }
	s.peredatZapretNovoyKopii("Kelevra.exe", "0.0.1")
	if zapuskali {
		t.Fatal("держатель запущен для старой сборки")
	}
	if _, est := zaslon.ProchestPeredachu(hranenie.Papka(), time.Now()); !est {
		t.Fatal("намерение не передано")
	}
}
