package sluzhba

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/dom"
	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/nrpt"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// blokDoma — блок dom, подписанный пробным ключом как мозг (конверт пульта
// с меткой kelevra-dom-v1).
func blokDoma(t *testing.T, s *Sluzhba, priv ed25519.PrivateKey, versiya int64, most string) json.RawMessage {
	t.Helper()
	dannye, _ := json.Marshal(map[string]any{
		"adres": "100.77.0.7/32", "uzel_pub": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)),
		"most": most, "seti": []string{"100.77.0.0/16"}, "dns": "100.77.0.1", "zona": "dom", "mtu": 1280, "versiya": versiya, "komu": "01-7", "pub": svoyPub(t, s),
	})
	sig := ed25519.Sign(priv, append([]byte(dom.Metka), dannye...))
	b, _ := json.Marshal(map[string]string{"dannye": base64.StdEncoding.EncodeToString(dannye), "podpis": base64.StdEncoding.EncodeToString(sig)})
	return b
}

func stendDoma(t *testing.T) (*Sluzhba, ed25519.PrivateKey) {
	t.Helper()
	s := stend(t)
	profil, err := os.ReadFile("../konfig/testdata/profil_telefona.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SohranitProfil(profil); err != nil {
		t.Fatal(err)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s.dom.kesh.Klyuchi = []ed25519.PublicKey{priv.Public().(ed25519.PublicKey)}
	s.Nastroyki.Slot = "01-7"
	return s, priv
}

func endpointyKonfiga(t *testing.T, s *Sluzhba) []any {
	t.Helper()
	b, err := os.ReadFile(s.Yadro.PutKonfiga())
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	e, _ := d["endpoints"].([]any)
	return e
}

// Блок с верной подписью встаёт в конфиг ядра; с битой — не применяется, и
// остаётся прежний; сводка без dom — вход домой снимается. Закрытый ключ не
// попадает в журнал.
func TestDomBlokIzSvodkiVKonfig(t *testing.T) {
	s, priv := stendDoma(t)
	var zhurnal bytes.Buffer
	log.SetOutput(&zhurnal)
	defer log.SetOutput(os.Stderr)

	if len(endpointyKonfiga(t, s)) != 0 {
		t.Fatal("без блока в конфиге эндпоинт")
	}
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 1, "100.76.0.1:51820")})
	e := endpointyKonfiga(t, s)
	if len(e) != 1 {
		t.Fatalf("блок не встал в конфиг: %v", e)
	}
	k, err := s.dom.klyuch.Klyuch()
	if err != nil {
		t.Fatal(err)
	}
	ep := e[0].(map[string]any)
	if ep["tag"] != "Дом" || ep["private_key"] != k.Privatnyy() {
		t.Fatalf("эндпоинт не тот: tag=%v", ep["tag"])
	}
	s.zamok.Lock()
	vKonfige := s.kartina.Dom
	s.zamok.Unlock()
	if !vKonfige {
		t.Fatal("картина не знает про дом")
	}

	// Битая подпись (чужой ключ, мост подменён) — прежний блок.
	_, chuzhoy, _ := ed25519.GenerateKey(rand.Reader)
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, chuzhoy, 2, "203.0.113.9:51820")})
	peer := endpointyKonfiga(t, s)[0].(map[string]any)["peers"].([]any)[0].(map[string]any)
	if peer["address"] != "100.76.0.1" {
		t.Fatalf("блок с битой подписью применён: %v", peer)
	}
	if !strings.Contains(zhurnal.String(), "podpis") {
		t.Fatalf("отказ по подписи не в журнале:\n%s", zhurnal.String())
	}

	// После перезапуска службы блок с диска (без мозга).
	s.dom.kesh = &dom.Kesh{Put: hranenie.PutBlokaDoma(), PutPamyati: hranenie.PutPamyatiDoma(), Klyuchi: s.dom.kesh.Klyuchi, Komu: "01-7", SvoyPub: s.dom.kesh.SvoyPub}
	if b := s.dom.kesh.Blok(); b == nil || b.Versiya != 1 {
		t.Fatalf("блок с диска: %v", b)
	}

	// Сводка без dom — мозг снял.
	s.prinyatDom(&podpiska.Svedeniya{})
	if len(endpointyKonfiga(t, s)) != 0 {
		t.Fatal("снятый блок остался в конфиге")
	}
	if strings.Contains(zhurnal.String(), k.Privatnyy()) {
		t.Fatal("закрытый ключ попал в журнал")
	}
}

// Открытый ключ уходит POST /k/<код>/dom/klyuch {"slot","pub"} с заголовками
// устройства; сбой — повтор; ответ «zhdet» — строка окна «ждёт подтверждения».
func TestDomOtpravkaKlyucha(t *testing.T) {
	s, _ := stendDoma(t)
	var zaprosy []map[string]string
	var zagolovki []http.Header
	otkazat := true
	dver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/k/K7QM2XRT9PLA/dom/klyuch" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var m map[string]string
		_ = json.Unmarshal(b, &m)
		zaprosy = append(zaprosy, m)
		zagolovki = append(zagolovki, r.Header.Clone())
		if otkazat {
			otkazat = false
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"sostoyanie":"zhdet","dom":true}`))
	}))
	defer dver.Close()
	host := strings.TrimPrefix(dver.URL, "http://")
	t.Setenv("KELEVRA_PODPISKA", host)
	s.Podpiska.Shema = "http"
	s.Nastroyki.Kod = "K7QM2XRT9PLA"
	s.Nastroyki.Podpiska = dver.URL + "/u/KLYUCH_USTROYSTVA_7"
	s.Nastroyki.Slot = "01-7"

	ctx := context.Background()
	s.otpravitKlyuchDomaEsliPora(ctx)
	if len(zaprosy) != 1 || !s.dom.klyuch.NuzhnoOtpravit() {
		t.Fatalf("первая отправка: %d запросов", len(zaprosy))
	}
	// Повтор не раньше паузы.
	s.otpravitKlyuchDomaEsliPora(ctx)
	if len(zaprosy) != 1 {
		t.Fatal("повтор раньше паузы")
	}
	s.dom.mu.Lock()
	s.dom.kogdaOtpravit = time.Time{}
	s.dom.mu.Unlock()
	s.otpravitKlyuchDomaEsliPora(ctx)
	if len(zaprosy) != 2 || s.dom.klyuch.NuzhnoOtpravit() {
		t.Fatalf("повтор после сбоя: %d запросов", len(zaprosy))
	}
	k, _ := s.dom.klyuch.Klyuch()
	if zaprosy[1]["pub"] != k.Pub || zaprosy[1]["slot"] != "01-7" || len(zaprosy[1]) != 2 {
		t.Fatalf("тело: %v", zaprosy[1])
	}
	for _, h := range zagolovki {
		if h.Get("X-Device-Id") == "" || h.Get("X-Device-Key") != "KLYUCH_USTROYSTVA_7" {
			t.Fatalf("заголовки устройства: %v", h)
		}
	}
	// Принятый ключ больше не шлётся.
	s.dom.mu.Lock()
	s.dom.kogdaOtpravit = time.Time{}
	s.dom.mu.Unlock()
	s.otpravitKlyuchDomaEsliPora(ctx)
	if len(zaprosy) != 2 {
		t.Fatal("принятый ключ отправлен снова")
	}
	if got := s.domStroka(); got != DomZhdetPodtverzh {
		t.Fatalf("строка окна %q, ждали %q", got, DomZhdetPodtverzh)
	}
	w := httptest.NewRecorder()
	s.Obsluzhit().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/"+s.klyuch+"/api/sostoyanie", nil))
	if !strings.Contains(w.Body.String(), `"dom_klyuch":"`+dom.Otpechatok(k.Pub)+`"`) {
		t.Fatalf("окно не получило отпечаток ключа: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"dom_stroka":"`+DomZhdetPodtverzh+`"`) {
		t.Fatalf("окно не получило строку дома: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), k.Privatnyy()) {
		t.Fatal("закрытый ключ в ответе окну")
	}
}

// Вошли по ссылке /u/<ключ> — кода нет, и ключ дома не шлётся вовсе.
func TestDomBezKodaNeShlyot(t *testing.T) {
	s, _ := stendDoma(t)
	byl := false
	dver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { byl = true }))
	defer dver.Close()
	t.Setenv("KELEVRA_PODPISKA", strings.TrimPrefix(dver.URL, "http://"))
	s.Podpiska.Shema = "http"
	s.Nastroyki.Podpiska = dver.URL + "/u/KLYUCH_USTROYSTVA_8"
	s.Nastroyki.Kod = "KLYUCH_USTROYSTVA_8"
	if err := s.otpravitKlyuchDoma(context.Background()); !errors.Is(err, errNetKodaDlyaDoma) || byl {
		t.Fatalf("без кода: %v, запрос был: %v", err, byl)
	}
}

func TestDomStrokaPoProbe(t *testing.T) {
	s, priv := stendDoma(t)
	ctx := context.Background()
	rabotaet, otvet := true, error(nil)
	s.dom.rabotaetDlyaStenda = func() bool { return rabotaet }
	s.dom.proba = func(ctx context.Context, server, zona string) error {
		if server != "100.77.0.1" || zona != "dom" {
			t.Errorf("проба не туда: %s %s", server, zona)
		}
		return otvet
	}
	s.proveritSvyazDoma(ctx)
	if s.domStroka() != "" {
		t.Fatal("без блока строка есть")
	}
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 1, "100.76.0.1:51820")})
	s.proveritSvyazDoma(ctx)
	if got := s.domStroka(); got != DomNaSvyazi {
		t.Fatalf("%q, ждали %q", got, DomNaSvyazi)
	}
	otvet = errors.New("молчит")
	s.proveritSvyazDoma(ctx)
	if got := s.domStroka(); got != DomNetSvyazi {
		t.Fatalf("%q, ждали %q", got, DomNetSvyazi)
	}
	// Ядро стоит (например, дома) — строки нет.
	rabotaet = false
	s.proveritSvyazDoma(ctx)
	if got := s.domStroka(); got != "" {
		t.Fatalf("при стоящем ядре строка %q", got)
	}
}

// probaDns: ответ на свой запрос — связь есть; чужой id или молчание — нет.
func TestProbaDns(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	var chuzhoyID atomic.Bool
	go func() {
		buf := make([]byte, 512)
		for {
			n, a, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			otvet := append([]byte(nil), buf[:n]...)
			otvet[2] |= 0x80
			otvet[3] = 0x83 // NXDOMAIN — тоже ответ дома
			if chuzhoyID.Load() {
				otvet[0] ^= 0xff
			}
			pc.WriteTo(otvet, a)
		}
	}()
	ctx, otmena := context.WithTimeout(context.Background(), time.Second)
	defer otmena()
	if err := probaDns(ctx, pc.LocalAddr().String(), "dom"); err != nil {
		t.Fatalf("ответ дома не засчитан: %v", err)
	}
	chuzhoyID.Store(true)
	ctx2, otmena2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer otmena2()
	if err := probaDns(ctx2, pc.LocalAddr().String(), "dom"); err == nil {
		t.Fatal("ответ на чужой запрос засчитан")
	}
}

func svoyPub(t *testing.T, s *Sluzhba) string {
	t.Helper()
	k, err := s.dom.klyuch.Klyuch()
	if err != nil {
		t.Fatal(err)
	}
	return k.Pub
}

// Голый ответ двери «otozvan» ключ не перерождает (его может подделать
// захваченный VPS); перерождает только подписанный отзыв в блоке dom, после
// чего новый ключ сразу уходит мозгу.
func TestDomOtzyvKlyucha(t *testing.T) {
	s, priv := stendDoma(t)
	var pubs []string
	dver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]string
		_ = json.NewDecoder(r.Body).Decode(&m)
		pubs = append(pubs, m["pub"])
		if len(pubs) == 1 {
			w.Write([]byte(`{"sostoyanie":"otozvan","dom":false}`))
			return
		}
		w.Write([]byte(`{"sostoyanie":"zhdet","dom":true}`))
	}))
	defer dver.Close()
	t.Setenv("KELEVRA_PODPISKA", strings.TrimPrefix(dver.URL, "http://"))
	s.Podpiska.Shema = "http"
	s.Nastroyki.Kod = "K7QM2XRT9PLA"
	s.Nastroyki.Podpiska = dver.URL + "/u/KLYUCH_USTROYSTVA_7"

	byl := svoyPub(t, s)
	s.otpravitKlyuchDomaEsliPora(context.Background())
	if len(pubs) != 1 || svoyPub(t, s) != byl {
		t.Fatalf("ответ двери «otozvan» переродил ключ (%d запросов)", len(pubs))
	}

	// Подписанный отзыв своего ключа — новый ключ и сразу ему же отправка.
	dannye, _ := json.Marshal(map[string]any{"komu": "01-7", "pub": byl, "otozvan": true, "versiya": 4})
	sig := ed25519.Sign(priv, append([]byte(dom.Metka), dannye...))
	otzyv, _ := json.Marshal(map[string]string{"dannye": base64.StdEncoding.EncodeToString(dannye), "podpis": base64.StdEncoding.EncodeToString(sig)})
	s.prinyatDom(&podpiska.Svedeniya{Dom: otzyv})
	s.dom.perezapuski.Wait()
	stal := svoyPub(t, s)
	if stal == byl {
		t.Fatal("подписанный отзыв не переродил ключ")
	}
	s.otpravitKlyuchDomaEsliPora(context.Background())
	if len(pubs) != 2 || pubs[1] != stal || s.domStroka() != DomZhdetPodtverzh {
		t.Fatalf("новый ключ не ушёл: %d запросов, строка %q", len(pubs), s.domStroka())
	}
}

// Подписанный блок на чужой ключ не применяется; окно — «Домашняя сеть: ключ не совпадает».
func TestDomChuzhoyKlyuch(t *testing.T) {
	s, priv := stendDoma(t)
	dannye, _ := json.Marshal(map[string]any{
		"adres": "100.77.0.7/32", "uzel_pub": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)),
		"most": "100.76.0.1:51820", "seti": []string{"100.77.0.0/16"}, "dns": "100.77.0.1", "zona": "dom",
		"mtu": 1280, "versiya": 1, "komu": "01-7", "pub": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)),
	})
	sig := ed25519.Sign(priv, append([]byte(dom.Metka), dannye...))
	b, _ := json.Marshal(map[string]string{"dannye": base64.StdEncoding.EncodeToString(dannye), "podpis": base64.StdEncoding.EncodeToString(sig)})
	s.prinyatDom(&podpiska.Svedeniya{Dom: b})
	if s.dom.kesh.Blok() != nil || len(endpointyKonfiga(t, s)) != 0 {
		t.Fatal("блок на чужой ключ применён")
	}
	if got := s.domStroka(); got != DomChuzhoyKlyuch {
		t.Fatalf("строка %q, ждали %q", got, DomChuzhoyKlyuch)
	}
}

// «Закрыто по умолчанию»: вход домой бывал, блок снят — в конфиге reject для
// своей подсети и зоны dom; до того, как вход домой вообще бывал, — нет.
func TestDomZakrytoPoUmolchaniyu(t *testing.T) {
	s, priv := stendDoma(t)
	zakryto := func() bool {
		b, _ := os.ReadFile(s.Yadro.PutKonfiga())
		return strings.Contains(string(b), `"100.77.0.0/16"`) && strings.Contains(string(b), `"reject"`) && !strings.Contains(string(b), `"wireguard"`)
	}
	if zakryto() {
		t.Fatal("закрыто, хотя вход домой не бывал")
	}
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 1, "100.76.0.1:51820")})
	s.prinyatDom(&podpiska.Svedeniya{})
	s.dom.perezapuski.Wait()
	if !zakryto() {
		b, _ := os.ReadFile(s.Yadro.PutKonfiga())
		t.Fatalf("блок снят, а сети дома не закрыты:\n%.2000s", b)
	}
	s.zamok.Lock()
	k := s.kartina
	s.zamok.Unlock()
	if !k.DomZakryt || k.Dom {
		t.Fatalf("картина: %+v", k)
	}
}

// Правило NRPT: стоит, пока блок вписан и туннель поднят; снимается при
// отключении и при снятии блока; на старте службы остатки снимаются.
// Настоящий реестр не трогается — подставная система.
type nrptStend struct {
	pravila map[string]nrpt.Pravilo
	vyzovov int
}

func (n *nrptStend) Svoi() ([]string, error) {
	var imena []string
	for i := range n.pravila {
		if strings.HasPrefix(i, nrpt.Imya) { // настоящая система отдаёт только своё
			imena = append(imena, i)
		}
	}
	return imena, nil
}
func (n *nrptStend) Postavit(p nrpt.Pravilo) error {
	n.vyzovov++
	n.pravila[nrpt.Imya] = p
	return nil
}
func (n *nrptStend) Ubrat(imya string) error { n.vyzovov++; delete(n.pravila, imya); return nil }

func TestDomNrpt(t *testing.T) {
	s := gotovStendLestnicy(t)
	if s.dom.nrpt.Sys != nil {
		t.Fatal("на стенде (KELEVRA_DIR) служба взяла настоящую NRPT машины")
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s.dom.kesh.Klyuchi = []ed25519.PublicKey{priv.Public().(ed25519.PublicKey)}
	s.Nastroyki.Slot = "01-7"
	sys := &nrptStend{pravila: map[string]nrpt.Pravilo{nrpt.Imya + "-ostatok": {}, "Chuzhoe": {}}}
	s.dom.nrpt = &nrpt.Upravlenie{Sys: sys}
	s.dom.nrpt.UbratOstatki()
	if _, est := sys.pravila[nrpt.Imya+"-ostatok"]; est {
		t.Fatal("остаток своего правила не снят на старте")
	}
	if _, est := sys.pravila["Chuzhoe"]; !est {
		t.Fatal("снято чужое правило")
	}
	podnyat := true
	s.zashchitaPodnyataDlyaStenda = func() bool { return podnyat }
	s.zapustitYadro = func(ctx context.Context) error { return nil }

	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 1, "100.76.0.1:51820")})
	s.dom.perezapuski.Wait()
	if p, est := sys.pravila[nrpt.Imya]; !est || p.Zona != ".dom" || p.Server != "100.77.0.1" {
		t.Fatalf("правило NRPT при поднятом туннеле с блоком: %v", sys.pravila)
	}
	n := sys.vyzovov
	s.soglasovatNrpt()
	if sys.vyzovov != n {
		t.Fatal("правило ставится заново на каждом тике")
	}
	podnyat = false
	_ = s.OpustitZashchitu()
	if _, est := sys.pravila[nrpt.Imya]; est {
		t.Fatal("правило NRPT осталось после отключения")
	}
	podnyat = true
	s.soglasovatNrpt()
	s.prinyatDom(&podpiska.Svedeniya{})
	s.dom.perezapuski.Wait()
	if _, est := sys.pravila[nrpt.Imya]; est {
		t.Fatal("правило NRPT осталось после снятия блока")
	}
}

// Слот при входе не запомнен — берётся из сводки (device.slot), и только
// тогда блок, адресованный этому слоту, применяется.
func TestDomSlotIzSvodki(t *testing.T) {
	s, priv := stendDoma(t)
	s.Nastroyki.Slot = ""
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 1, "100.76.0.1:51820")})
	if s.dom.kesh.Blok() != nil {
		t.Fatal("блок принят без своего слота")
	}
	s.prinyatDom(&podpiska.Svedeniya{
		Ustroystvo: &podpiska.Imenovannyy{Imya: "nout", Slot: "01-7"},
		Dom:        blokDoma(t, s, priv, 1, "100.76.0.1:51820"),
	})
	s.dom.perezapuski.Wait()
	if s.dom.kesh.Blok() == nil || s.Nastroyki.Slot != "01-7" {
		t.Fatalf("слот из сводки не взят: %q", s.Nastroyki.Slot)
	}
	if n, _ := hranenie.Zagruzit(); n.Slot != "01-7" {
		t.Fatalf("слот не сохранён на диск: %q", n.Slot)
	}
}

// Блок появился, сменился, снят при поднятом туннеле — ядро перезапускается
// на новом конфиге (с «Домом» и без); тот же блок ещё раз — не перезапускается;
// туннель не поднят — тоже нет.
func TestDomPerezapuskYadraPriSmeneBloka(t *testing.T) {
	s := gotovStendLestnicy(t)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s.dom.kesh.Klyuchi = []ed25519.PublicKey{priv.Public().(ed25519.PublicKey)}
	s.Nastroyki.Slot = "01-7"
	podnyat := true
	s.zashchitaPodnyataDlyaStenda = func() bool { return podnyat }
	var zapuski []int // сколько эндпоинтов «Дом» в конфиге на каждом запуске
	s.zapustitYadro = func(ctx context.Context) error {
		zapuski = append(zapuski, len(endpointyKonfiga(t, s)))
		return nil
	}
	primenit := func(raw json.RawMessage) {
		s.prinyatDom(&podpiska.Svedeniya{Dom: raw})
		s.dom.perezapuski.Wait()
	}

	primenit(blokDoma(t, s, priv, 1, "100.76.0.1:51820"))
	if len(zapuski) != 1 || zapuski[0] != 1 {
		t.Fatalf("появление блока: запуски %v, ждали [1]", zapuski)
	}
	primenit(blokDoma(t, s, priv, 1, "100.76.0.1:51820")) // тот же блок, новая подпись
	if len(zapuski) != 1 {
		t.Fatalf("тот же блок перезапустил ядро: %v", zapuski)
	}
	primenit(blokDoma(t, s, priv, 2, "100.76.0.1:51821"))
	if len(zapuski) != 2 || zapuski[1] != 1 {
		t.Fatalf("смена блока: запуски %v", zapuski)
	}
	primenit(nil)
	if len(zapuski) != 3 || zapuski[2] != 0 {
		t.Fatalf("снятие блока: запуски %v", zapuski)
	}
	podnyat = false
	primenit(blokDoma(t, s, priv, 3, "100.76.0.1:51820"))
	if len(zapuski) != 3 {
		t.Fatalf("туннель не поднят, а ядро запускали: %v", zapuski)
	}
	if len(endpointyKonfiga(t, s)) != 1 {
		t.Fatal("при опущенном туннеле конфиг на диске без блока")
	}
}

// Слот неизвестен — ключ всё равно уходит (мозг узнаёт устройство по
// X-Device-Id и X-Device-Key), слот в теле пустой.
func TestDomKlyuchBezSlota(t *testing.T) {
	s, _ := stendDoma(t)
	var telo map[string]string
	dver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&telo)
		w.Write([]byte(`{"sostoyanie":"zhdet","dom":true}`))
	}))
	defer dver.Close()
	t.Setenv("KELEVRA_PODPISKA", strings.TrimPrefix(dver.URL, "http://"))
	s.Podpiska.Shema = "http"
	s.Nastroyki.Kod = "K7QM2XRT9PLA"
	s.Nastroyki.Podpiska = dver.URL + "/u/KLYUCH_USTROYSTVA_7"
	s.Nastroyki.Slot = ""
	if err := s.otpravitKlyuchDoma(context.Background()); err != nil {
		t.Fatal(err)
	}
	if k, _ := s.dom.klyuch.Klyuch(); telo["pub"] != k.Pub || telo["slot"] != "" {
		t.Fatalf("тело без слота: %v", telo)
	}
}

// Память версий потеряна (оба файла), а блок на диске был: служба рождает
// новый ключ, блок не применяется, сети дома закрыты.
func TestDomPoteryaPamyati(t *testing.T) {
	s, priv := stendDoma(t)
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 3, "100.76.0.1:51820")})
	s.dom.perezapuski.Wait()
	byl := svoyPub(t, s)
	_ = os.Remove(hranenie.PutPamyatiDoma())
	_ = os.Remove(hranenie.PutPamyatiDoma() + ".zapas")
	klyuchi := s.dom.kesh.Klyuchi
	s.nachatDom() // как старт службы
	s.dom.kesh.Klyuchi = klyuchi
	if svoyPub(t, s) == byl {
		t.Fatal("после потери памяти ключ прежний")
	}
	if s.dom.kesh.Blok() != nil || !s.domZakryt() {
		t.Fatal("после потери памяти блок действует или сети дома открыты")
	}
}

// Расписание входа домой: лестница повтора отправки ключа 1→2→5→15 мин (потолок 15),
// /info раз в минуту, пока ключ ждёт подтверждения, и раз в 5 минут, пока блок действует.
func TestDomRaspisanie(t *testing.T) {
	var lest []time.Duration
	p := time.Duration(0)
	for i := 0; i < 6; i++ {
		p = sleduyushchayaPauza(p)
		lest = append(lest, p)
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	for i := range want {
		if lest[i] != want[i] {
			t.Fatalf("лестница %v, ждали %v", lest, want)
		}
	}

	s, _ := stendDoma(t)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s.dom.kesh.Klyuchi = []ed25519.PublicKey{priv.Public().(ed25519.PublicKey)}
	s.Nastroyki.Slot = "01-7"
	vyzovov := 0
	s.dom.svodka = func(context.Context) { vyzovov++ }
	ctx := context.Background()
	// Ни ключа в ожидании, ни блока — сводку ради дома не спрашиваем.
	s.sprositSvodkuDlyaDoma(ctx)
	if vyzovov != 0 {
		t.Fatal("сводка спрошена без причины")
	}
	k, _ := s.dom.klyuch.Klyuch()
	s.dom.klyuch.Otmetit(k.Pub, dom.SostZhdet, 1)
	s.sprositSvodkuDlyaDoma(ctx)
	s.sprositSvodkuDlyaDoma(ctx)
	if vyzovov != 1 {
		t.Fatalf("при ожидании подтверждения: %d запросов подряд, ждали 1", vyzovov)
	}
	if d := time.Until(s.dom.kogdaSvodka); d > periodZhdetPodtverzh || d < periodZhdetPodtverzh-5*time.Second {
		t.Fatalf("следующий запрос через %v, ждали ~1 мин", d)
	}
	s.dom.kogdaSvodka = time.Time{}
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 1, "100.76.0.1:51820")})
	s.dom.perezapuski.Wait()
	if s.dom.kesh.Blok() == nil {
		t.Fatal("блок не принят")
	}
	s.sprositSvodkuDlyaDoma(ctx)
	if vyzovov != 2 {
		t.Fatalf("при действующем блоке: %d запросов", vyzovov)
	}
	if d := time.Until(s.dom.kogdaSvodka); d > periodSledaZaBlokom || d < periodSledaZaBlokom-5*time.Second {
		t.Fatalf("следующий запрос через %v, ждали ~5 мин", d)
	}
}

// Гейт на баг с ноута Вовы 26.10.2026: ключ принят сервером, а «Дом» этому
// устройству выключен (Flag=-1) — ZhdetPodtverzhdeniya() лжёт «false», и
// sprositSvodkuDlyaDoma молча уходила в default-ветку (ничего не спрашивает),
// пока владелец сети не включит доступ: тогда включение доезжало только
// через periodSverkiKlyucha (час), через повторную отправку ключа. Сводку
// нужно спрашивать раз в periodSledaZaBlokom, как при действующем блоке.
func TestDomRaspisanieDomVyklyuchenSprashivaetSvodku(t *testing.T) {
	s, _ := stendDoma(t)
	vyzovov := 0
	s.dom.svodka = func(context.Context) { vyzovov++ }
	ctx := context.Background()

	k, _ := s.dom.klyuch.Klyuch()
	s.dom.klyuch.Otmetit(k.Pub, dom.SostPodtverzhden, -1) // ключ принят, «Дом» выключен

	if s.dom.klyuch.ZhdetPodtverzhdeniya() {
		t.Fatal("при выключенном «Доме» ZhdetPodtverzhdeniya обязана быть false")
	}
	if !s.dom.klyuch.DomVyklyuchen() {
		t.Fatal("DomVyklyuchen не узнал выключенный флаг")
	}

	s.sprositSvodkuDlyaDoma(ctx)
	if vyzovov != 1 {
		t.Fatalf("при выключенном «Доме»: %d запросов сводки, ждали 1 — включение доедет только через час", vyzovov)
	}
	s.sprositSvodkuDlyaDoma(ctx)
	if vyzovov != 1 {
		t.Fatalf("второй запрос подряд раньше срока: %d", vyzovov)
	}
	if d := time.Until(s.dom.kogdaSvodka); d > periodSledaZaBlokom || d < periodSledaZaBlokom-5*time.Second {
		t.Fatalf("следующий запрос через %v, ждали ~5 мин", d)
	}
}

// stendLecheniya — стенд с действующим блоком, работающим ядром и подменами
// пробы, часов и перезапуска: perezapuski — причины перезапусков по порядку.
type stendLecheniya struct {
	s           *Sluzhba
	otvet       error
	sejchas     time.Time
	mostZhiv    bool
	perezapuski []string
	kogda       []time.Time // когда решён каждый перезапуск (часы стенда)
}

func novyyStendLecheniya(t *testing.T) *stendLecheniya {
	t.Helper()
	s, priv := stendDoma(t)
	st := &stendLecheniya{s: s, otvet: errors.New("молчит"), sejchas: time.Date(2026, 10, 30, 22, 46, 0, 0, time.UTC), mostZhiv: true}
	s.dom.rabotaetDlyaStenda = func() bool { return true }
	s.dom.proba = func(ctx context.Context, server, zona string) error { return st.otvet }
	s.dom.seychas = func() time.Time { return st.sejchas }
	s.dom.most = func(context.Context) (bool, string) { return st.mostZhiv, "Финляндия" }
	s.dom.perezapustit = func(prichina string, pokolenie uint64) {
		st.perezapuski = append(st.perezapuski, prichina)
		st.kogda = append(st.kogda, st.sejchas)
	}
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 1, "100.76.0.1:51820")})
	s.dom.perezapuski.Wait()
	return st
}

// proba — один шаг слежки: проба и (если решено) перезапуск до конца.
func (st *stendLecheniya) proba() {
	st.s.proveritSvyazDoma(context.Background())
	st.s.dom.perezapuski.Wait()
}

// «Дом» молчит три пробы подряд при работающем ядре — ядро перезапускается
// один раз; следующие — не раньше паузы, пауза 10 → 20 → 40 → 60 минут и
// дальше не растёт; удачная проба сбрасывает и счёт, и паузу.
func TestDomSamoizlechenie(t *testing.T) {
	st := novyyStendLecheniya(t)
	zhdat := func(n int, chto string) {
		t.Helper()
		if len(st.perezapuski) != n {
			t.Fatalf("%s: перезапусков %d, ждали %d", chto, len(st.perezapuski), n)
		}
	}
	st.proba()
	st.proba()
	zhdat(0, "две неудачи")
	st.proba()
	zhdat(1, "три неудачи")
	if p := st.perezapuski[0]; !strings.Contains(p, "неудачных проб подряд: 3") || !strings.Contains(p, "перезапускаю ядро") {
		t.Fatalf("причина в журнале: %q", p)
	}
	st.proba()
	zhdat(1, "четвёртая неудача сразу после перезапуска")

	nachalo := st.sejchas
	for i, pauza := range []time.Duration{10 * time.Minute, 20 * time.Minute, 40 * time.Minute, time.Hour, time.Hour} {
		st.sejchas = nachalo.Add(pauza - time.Second)
		st.proba()
		zhdat(i+1, "раньше паузы "+pauza.String())
		st.sejchas = nachalo.Add(pauza)
		st.proba()
		zhdat(i+2, "пауза "+pauza.String()+" вышла")
		nachalo = st.sejchas
	}

	// Удача: счёт и пауза сброшены — снова три неудачи и сразу перезапуск.
	st.otvet = nil
	st.proba()
	if st.s.domStroka() != DomNaSvyazi {
		t.Fatalf("строка после удачи %q", st.s.domStroka())
	}
	st.otvet = errors.New("молчит")
	st.sejchas = st.sejchas.Add(time.Minute)
	st.proba()
	st.proba()
	zhdat(6, "две неудачи после удачи")
	st.proba()
	zhdat(7, "три неудачи после удачи — пауза сброшена")
	if st.s.dom.pauzaLecheniya != 10*time.Minute {
		t.Fatalf("пауза после сброса %v, ждали 10m", st.s.dom.pauzaLecheniya)
	}
}

// Самоизлечения нет без блока, без «Дома» в конфиге, при стоящем ядре
// (дома, «Отключить») и пока поднимается связь; стоящее ядро обнуляет счёт.
func TestDomSamoizlechenieNeVovremya(t *testing.T) {
	t.Run("без блока", func(t *testing.T) {
		s, _ := stendDoma(t)
		vyzovov := 0
		s.dom.rabotaetDlyaStenda = func() bool { return true }
		s.dom.proba = func(context.Context, string, string) error { return errors.New("молчит") }
		s.dom.perezapustit = func(string, uint64) { vyzovov++ }
		for i := 0; i < 5; i++ {
			s.proveritSvyazDoma(context.Background())
			s.dom.perezapuski.Wait()
		}
		if vyzovov != 0 {
			t.Fatalf("без блока перезапусков %d", vyzovov)
		}
	})
	t.Run("без дома в конфиге", func(t *testing.T) {
		st := novyyStendLecheniya(t)
		st.s.zamok.Lock()
		st.s.kartina.Dom = false
		st.s.zamok.Unlock()
		for i := 0; i < 5; i++ {
			st.proba()
		}
		if len(st.perezapuski) != 0 {
			t.Fatalf("без «Дома» в конфиге перезапусков %d", len(st.perezapuski))
		}
	})
	t.Run("ядро стоит", func(t *testing.T) {
		st := novyyStendLecheniya(t)
		rabotaet := true
		st.s.dom.rabotaetDlyaStenda = func() bool { return rabotaet }
		st.proba()
		st.proba()
		rabotaet = false // «Отключить»: ядро погашено
		st.proba()
		st.proba()
		rabotaet = true
		st.proba()
		if len(st.perezapuski) != 0 {
			t.Fatalf("счёт пережил стоящее ядро: перезапусков %d", len(st.perezapuski))
		}
		st.proba()
		st.proba()
		if len(st.perezapuski) != 1 {
			t.Fatalf("три неудачи после подъёма: перезапусков %d, ждали 1", len(st.perezapuski))
		}
	})
	t.Run("связь поднимается", func(t *testing.T) {
		st := novyyStendLecheniya(t)
		st.s.zamok.Lock()
		st.s.podyomIdet = true
		st.s.zamok.Unlock()
		for i := 0; i < 4; i++ {
			st.proba()
		}
		if len(st.perezapuski) != 0 {
			t.Fatalf("во время подъёма перезапусков %d", len(st.perezapuski))
		}
		st.s.zamok.Lock()
		st.s.podyomIdet = false
		st.s.zamok.Unlock()
		st.proba()
		if len(st.perezapuski) != 1 {
			t.Fatalf("после подъёма перезапусков %d, ждали 1", len(st.perezapuski))
		}
	})
}

// Пока перезапуск идёт, второй не начинается, даже если пауза вышла.
func TestDomSamoizlechenieOdnoZaRaz(t *testing.T) {
	st := novyyStendLecheniya(t)
	var vyzovov atomic.Int32
	otpustit := make(chan struct{})
	st.s.dom.perezapustit = func(string, uint64) {
		vyzovov.Add(1)
		<-otpustit
	}
	for i := 0; i < 3; i++ {
		st.s.proveritSvyazDoma(context.Background())
	}
	st.sejchas = st.sejchas.Add(2 * time.Hour)
	st.s.proveritSvyazDoma(context.Background())
	close(otpustit)
	st.s.dom.perezapuski.Wait()
	if n := vyzovov.Load(); n != 1 {
		t.Fatalf("перезапусков %d, ждали 1", n)
	}
}

// Машина проснулась — счёт проб заново, пауза с прошлого сбоя снята.
func TestDomSamoizlecheniePosleSna(t *testing.T) {
	st := novyyStendLecheniya(t)
	for i := 0; i < 3; i++ {
		st.proba()
	}
	if len(st.perezapuski) != 1 {
		t.Fatalf("перезапусков %d, ждали 1", len(st.perezapuski))
	}
	st.proba()
	st.proba()
	st.s.dom.posleSna()
	st.proba()
	if len(st.perezapuski) != 1 {
		t.Fatal("после сна перезапуск по неудачам до сна")
	}
	st.proba()
	st.proba()
	if len(st.perezapuski) != 2 {
		t.Fatalf("после сна три неудачи: перезапусков %d, ждали 2 (пауза 10 минут не снята)", len(st.perezapuski))
	}
}

// Настоящий путь (perezapustitDlyaDoma): ядро гасится и поднимается на
// конфиге с «Домом», причина — в журнале. Решение о связи сменилось
// («Отключить» после решения) — ядро не поднимается.
func TestDomSamoizlechenieCherezYadro(t *testing.T) {
	s := gotovStendLestnicy(t)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s.dom.kesh.Klyuchi = []ed25519.PublicKey{priv.Public().(ed25519.PublicKey)}
	s.Nastroyki.Slot = "01-7"
	s.zashchitaPodnyataDlyaStenda = func() bool { return true }
	var zapuski []int
	s.zapustitYadro = func(ctx context.Context) error {
		zapuski = append(zapuski, len(endpointyKonfiga(t, s)))
		return nil
	}
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 1, "100.76.0.1:51820")})
	s.dom.perezapuski.Wait()
	if len(zapuski) != 1 {
		t.Fatalf("появление блока: запуски %v", zapuski)
	}
	zhurnal := &syncBuffer{}
	log.SetOutput(zhurnal)
	defer log.SetOutput(os.Stderr)
	s.dom.rabotaetDlyaStenda = func() bool { return true }
	s.dom.proba = func(context.Context, string, string) error { return errors.New("молчит") }
	s.dom.most = func(context.Context) (bool, string) { return true, "Соединение" }
	for i := 0; i < 3; i++ {
		s.proveritSvyazDoma(context.Background())
		s.dom.perezapuski.Wait()
	}
	if len(zapuski) != 2 || zapuski[1] != 1 {
		t.Fatalf("самоизлечение: запуски %v, ждали [1 1]", zapuski)
	}
	if !strings.Contains(zhurnal.String(), "дом: нет связи (неудачных проб подряд: 3) — перезапускаю ядро") {
		t.Fatalf("причины нет в журнале:\n%s", zhurnal.String())
	}

	staroe := s.pokolenieAvtorezhima()
	s.avtorezhimZamok.Lock()
	s.avtorezhimPokolenie++
	s.avtorezhimZamok.Unlock()
	s.perezapustitDlyaDoma("проверка", staroe)
	if len(zapuski) != 2 {
		t.Fatalf("после смены решения о связи ядро поднято: %v", zapuski)
	}
}

// shagi — пробы раз в ShagSlezhkiZaDomom с ot до do включительно; mostZhiv(t)
// — отвечает ли мост в этот момент.
func (st *stendLecheniya) shagi(ot, do time.Time, mostZhiv func(time.Time) bool) {
	for t := ot; !t.After(do); t = t.Add(ShagSlezhkiZaDomom) {
		st.sejchas = t
		st.mostZhiv = mostZhiv(t)
		st.proba()
	}
}

// Хронология сбоя 30.10: «Дом» молчит с 22:45, сети нет вовсе (мост не
// отвечает) до 23:19:42. Пока сети нет — ни одного перезапуска и лестница не
// тратится, в журнале одна строка; первая проба после возврата сети (23:20)
// перезапускает ядро сразу, а не через час.
func TestDomSamoizlechenieKhronologiya3010(t *testing.T) {
	st := novyyStendLecheniya(t)
	zhurnal := &syncBuffer{}
	log.SetOutput(zhurnal)
	defer log.SetOutput(os.Stderr)
	den := func(ch, m, s int) time.Time { return time.Date(2026, 10, 30, ch, m, s, 0, time.UTC) }
	setVernulas := den(23, 19, 42)
	st.shagi(den(22, 45, 0), den(23, 29, 30), func(t time.Time) bool { return !t.Before(setVernulas) })
	if len(st.kogda) != 1 || !st.kogda[0].Equal(den(23, 20, 0)) {
		t.Fatalf("перезапуски в %v, ждали один в 23:20:00", st.kogda)
	}
	if st.s.dom.pauzaLecheniya != 10*time.Minute {
		t.Fatalf("пауза после перезапуска %v, ждали 10m — лестница тратилась без сети", st.s.dom.pauzaLecheniya)
	}
	if n := strings.Count(zhurnal.String(), "не отвечает — ядро не перезапускаю"); n != 1 {
		t.Fatalf("строк «мост не отвечает» в журнале %d, ждали 1:\n%s", n, zhurnal.String())
	}
	if !strings.Contains(zhurnal.String(), "мост «Финляндия» снова отвечает") {
		t.Fatalf("возврата моста нет в журнале:\n%s", zhurnal.String())
	}
}

// Перезапуск был, пауза 10 минут ещё идёт, сеть пропала и вернулась — пауза
// снята: перезапуск на первой пробе после возврата, а не по старой паузе.
func TestDomSamoizlechenieMostOzhilSnimaetPauzu(t *testing.T) {
	st := novyyStendLecheniya(t)
	den := func(ch, m, s int) time.Time { return time.Date(2026, 10, 30, ch, m, s, 0, time.UTC) }
	st.shagi(den(22, 43, 0), den(22, 44, 0), func(time.Time) bool { return true })
	if len(st.kogda) != 1 || !st.kogda[0].Equal(den(22, 44, 0)) {
		t.Fatalf("первый перезапуск %v, ждали 22:44:00", st.kogda)
	}
	st.shagi(den(22, 44, 30), den(22, 50, 0), func(t time.Time) bool { return !t.Before(den(22, 50, 0)) })
	if len(st.kogda) != 2 || !st.kogda[1].Equal(den(22, 50, 0)) {
		t.Fatalf("перезапуски %v, ждали второй в 22:50:00 (до 22:54 по старой паузе)", st.kogda)
	}
}

// Мост молчит всё время (замер мог ошибиться) — лечение не выключается
// насовсем: раз в час с начала молчания.
func TestDomSamoizlechenieMostMolchitChas(t *testing.T) {
	st := novyyStendLecheniya(t)
	t0 := time.Date(2026, 10, 30, 22, 45, 0, 0, time.UTC)
	st.shagi(t0, t0.Add(2*time.Hour+10*time.Minute), func(time.Time) bool { return false })
	molchit := t0.Add(2 * ShagSlezhkiZaDomom) // третья проба — первое решение
	ozhidali := []time.Time{molchit.Add(time.Hour), molchit.Add(2 * time.Hour)}
	if len(st.kogda) != 2 || !st.kogda[0].Equal(ozhidali[0]) || !st.kogda[1].Equal(ozhidali[1]) {
		t.Fatalf("перезапуски %v, ждали %v", st.kogda, ozhidali)
	}
}
