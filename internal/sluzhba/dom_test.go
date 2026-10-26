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
	chuzhoyID := false
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
			if chuzhoyID {
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
	chuzhoyID = true
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
