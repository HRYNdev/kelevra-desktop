package zapasnoy

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Всё в этих проверках живёт на 127.0.0.1: «прямой путь режется» — свой
// сервер, рвущий соединение; «туннель» — свой socks5 с паролем, который
// ведёт к живому серверу. Ни одного внешнего адреса и ни одного DNS-запроса
// в сеть (резолвер подменён).

// rvushchiy — сервер, который принимает TCP и сразу закрывает: клиент видит
// EOF до отправки запроса (так режет провайдер ноутбука из разбора 20.10).
// chitat — сперва дочитать запрос целиком (запрос «ушёл»), потом закрыть.
type rvushchiy struct {
	l       net.Listener
	prinyal atomic.Int32
}

func novyyRvushchiy(t *testing.T, chitat bool) *rvushchiy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &rvushchiy{l: l}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			r.prinyal.Add(1)
			if chitat {
				_, _ = http.ReadRequest(bufio.NewReader(c))
			}
			c.Close()
		}
	}()
	t.Cleanup(func() { l.Close() })
	return r
}

func (r *rvushchiy) adres() string { return r.l.Addr().String() }

// molchun — сервер, который принимает соединение и молчит (TLS handshake timeout / deadline).
func novyyMolchun(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		l.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
	})
	return l.Addr().String()
}

// socks5 — минимальный socks5 с паролем: любой CONNECT ведёт на cel
// (так туннель «доводит» запрос до сервера, до которого прямой путь не доходит).
type socks5 struct {
	l          net.Listener
	cel        string
	parol      string
	podklyuch  atomic.Int32
	poslKuda   atomic.Value // string — куда просили CONNECT
	plohoyVhod atomic.Int32
}

func novyySocks5(t *testing.T, cel, parol string) *socks5 {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &socks5{l: l, cel: cel, parol: parol}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go s.obsluzhit(c)
		}
	}()
	t.Cleanup(func() { l.Close() })
	return s
}

func (s *socks5) url() *url.URL {
	return &url.URL{Scheme: "socks5", User: url.UserPassword(Polzovatel, s.parol), Host: s.l.Addr().String()}
}

func (s *socks5) obsluzhit(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(r, hdr); err != nil || hdr[0] != 5 {
		return
	}
	metody := make([]byte, hdr[1])
	if _, err := io.ReadFull(r, metody); err != nil {
		return
	}
	_, _ = c.Write([]byte{5, 2}) // только логин/пароль
	ver := make([]byte, 2)
	if _, err := io.ReadFull(r, ver); err != nil {
		return
	}
	imya := make([]byte, ver[1])
	_, _ = io.ReadFull(r, imya)
	dl := make([]byte, 1)
	_, _ = io.ReadFull(r, dl)
	parol := make([]byte, dl[0])
	_, _ = io.ReadFull(r, parol)
	if string(imya) != Polzovatel || string(parol) != s.parol {
		s.plohoyVhod.Add(1)
		_, _ = c.Write([]byte{1, 1})
		return
	}
	_, _ = c.Write([]byte{1, 0})
	zapros := make([]byte, 4)
	if _, err := io.ReadFull(r, zapros); err != nil {
		return
	}
	var host string
	switch zapros[3] {
	case 1:
		b := make([]byte, 4)
		_, _ = io.ReadFull(r, b)
		host = net.IP(b).String()
	case 3:
		n := make([]byte, 1)
		_, _ = io.ReadFull(r, n)
		b := make([]byte, n[0])
		_, _ = io.ReadFull(r, b)
		host = string(b)
	case 4:
		b := make([]byte, 16)
		_, _ = io.ReadFull(r, b)
		host = net.IP(b).String()
	}
	pb := make([]byte, 2)
	_, _ = io.ReadFull(r, pb)
	s.poslKuda.Store(net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb)))))
	s.podklyuch.Add(1)
	dalshe, err := net.Dial("tcp", s.cel)
	if err != nil {
		_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer dalshe.Close()
	_, _ = c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
	go func() { _, _ = io.Copy(dalshe, r) }()
	_, _ = io.Copy(c, dalshe)
}

// zhivoy — сервер «за туннелем»: отвечает и запоминает тело POST.
func novyyZhivoy(t *testing.T) (*httptest.Server, *atomic.Value) {
	t.Helper()
	var telo atomic.Value
	telo.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		telo.Store(string(b))
		fmt.Fprintf(w, "ok %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	return srv, &telo
}

type stroki struct {
	mu sync.Mutex
	s  []string
}

func (z *stroki) pishi(f string, a ...any) {
	z.mu.Lock()
	z.s = append(z.s, fmt.Sprintf(f, a...))
	z.mu.Unlock()
}

func (z *stroki) vse() string {
	z.mu.Lock()
	defer z.mu.Unlock()
	return strings.Join(z.s, "\n")
}

func prochest(t *testing.T, otvet *http.Response) string {
	t.Helper()
	defer otvet.Body.Close()
	b, err := io.ReadAll(otvet.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPryamoyRezhetsyaTunnelVyvozit(t *testing.T) {
	rv := novyyRvushchiy(t, false)
	zhivoy, _ := novyyZhivoy(t)
	tun := novyySocks5(t, zhivoy.Listener.Addr().String(), "parol-ustroystva")
	z := &stroki{}
	p := &Put{Tunnel: func() *url.URL { return tun.url() }, Zhurnal: z.pishi}

	otvet, err := p.Klient(10 * time.Second).Get("http://" + rv.adres() + "/s/KOD/info")
	if err != nil {
		t.Fatalf("через туннель должно было выйти: %v", err)
	}
	if got := prochest(t, otvet); got != "ok GET /s/KOD/info" {
		t.Fatalf("ответ %q", got)
	}
	if rv.prinyal.Load() == 0 {
		t.Fatal("прямой путь даже не пробовали")
	}
	if tun.podklyuch.Load() != 1 {
		t.Fatalf("через туннель подключений %d, ждал 1", tun.podklyuch.Load())
	}
	if kuda, _ := tun.poslKuda.Load().(string); kuda != rv.adres() {
		t.Fatalf("туннель просили вести на %q, ждал %q (тот же адрес, что напрямую)", kuda, rv.adres())
	}
	if !strings.Contains(z.vse(), "через туннель вышло") {
		t.Fatalf("в журнале нет строки про туннель: %q", z.vse())
	}
	if strings.Contains(z.vse(), "KOD") {
		t.Fatalf("в журнал попал путь с кодом: %q", z.vse())
	}

	// Второй запрос к тому же имени — сразу туннелем, прямой не трогаем.
	bylo := rv.prinyal.Load()
	otvet, err = p.Klient(10 * time.Second).Get("http://" + rv.adres() + "/rules/youtube.srs")
	if err != nil {
		t.Fatal(err)
	}
	prochest(t, otvet)
	if rv.prinyal.Load() != bylo {
		t.Fatal("после удачного обхода прямой путь пробовали снова — 23 набора правил ждали бы его по очереди")
	}
}

func TestBezTunnelyaKakRanshe(t *testing.T) {
	rv := novyyRvushchiy(t, false)
	zhivoy, _ := novyyZhivoy(t)
	tun := novyySocks5(t, zhivoy.Listener.Addr().String(), "p")
	p := &Put{Tunnel: func() *url.URL { return nil }}
	_, err := p.Klient(5 * time.Second).Get("http://" + rv.adres() + "/k/KOD")
	if err == nil {
		t.Fatal("без туннеля прямой отказ должен остаться отказом")
	}
	if !setevoy(err) {
		t.Fatalf("ошибка должна быть прежней (обрыв), а не обёрнутой: %v", err)
	}
	var dva *OshibkaDvuhPutey
	if errors.As(err, &dva) {
		t.Fatal("без туннеля второго пути не было")
	}
	if tun.podklyuch.Load() != 0 {
		t.Fatal("туннель не поднят — к входу ядра ходить нельзя")
	}
	// Пустой Put — тоже обычный транспорт.
	otvet, err := (&Put{}).Klient(5 * time.Second).Get(zhivoy.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	prochest(t, otvet)
}

func TestPostPovtoryaetsyaTolkoNeUshedshiy(t *testing.T) {
	// Соединение рвётся на рукопожатии TLS (EOF, как у «Мотива»): POST не
	// ушёл — повторяем через туннель с тем же телом.
	var telo atomic.Value
	zhivoy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		telo.Store(string(b))
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(zhivoy.Close)
	tun := novyySocks5(t, zhivoy.Listener.Addr().String(), "p")
	p := &Put{Tunnel: func() *url.URL { return tun.url() }, Zhurnal: func(string, ...any) {},
		tlsDlyaStenda: zhivoy.Client().Transport.(*http.Transport).TLSClientConfig}

	rv := novyyRvushchiy(t, false)
	otvet, err := p.Klient(10*time.Second).Post("https://"+rv.adres()+"/logs", "application/octet-stream", strings.NewReader("zhurnal-telo"))
	if err != nil {
		t.Fatalf("неушедший POST должен был пройти туннелем: %v", err)
	}
	prochest(t, otvet)
	if got, _ := telo.Load().(string); got != "zhurnal-telo" {
		t.Fatalf("тело через туннель %q", got)
	}

	// Сервер дочитал запрос и только потом оборвал: POST ушёл — второй раз не шлём.
	zhivoy2, _ := novyyZhivoy(t)
	tun2 := novyySocks5(t, zhivoy2.Listener.Addr().String(), "p")
	p2 := &Put{Tunnel: func() *url.URL { return tun2.url() }, Zhurnal: func(string, ...any) {}}
	rv2 := novyyRvushchiy(t, true)
	_, err = p2.Klient(10*time.Second).Post("http://"+rv2.adres()+"/k/KOD/ustroystvo", "text/plain", strings.NewReader("x"))
	if err == nil {
		t.Fatal("ушедший POST повторять нельзя, ждал отказ")
	}
	if tun2.podklyuch.Load() != 0 {
		t.Fatal("ушедший POST повторён через туннель — сервер мог принять его дважды")
	}

	// Тот же обрыв после запроса для GET — повторяем (запрос без последствий).
	rv3 := novyyRvushchiy(t, true)
	otvet, err = p2.Klient(10 * time.Second).Get("http://" + rv3.adres() + "/s/KOD")
	if err != nil {
		t.Fatalf("GET после обрыва должен был пройти туннелем: %v", err)
	}
	prochest(t, otvet)
}

func TestMolchashchiyPryamoyNeSyedaetSrok(t *testing.T) {
	molchun := novyyMolchun(t)
	zhivoy, _ := novyyZhivoy(t)
	tun := novyySocks5(t, zhivoy.Listener.Addr().String(), "p")
	p := &Put{Tunnel: func() *url.URL { return tun.url() }, SrokPervogo: 300 * time.Millisecond, Zhurnal: func(string, ...any) {}}
	nachalo := time.Now()
	otvet, err := p.Klient(5 * time.Second).Get("http://" + molchun + "/k/KOD")
	if err != nil {
		t.Fatalf("молчащий прямой путь должен был уступить туннелю: %v", err)
	}
	prochest(t, otvet)
	if d := time.Since(nachalo); d > 3*time.Second {
		t.Fatalf("ждали %s — прямой путь съел срок клиента", d)
	}
}

func TestOshibkaSrokaEstTaymaut(t *testing.T) {
	molchun := novyyMolchun(t)
	molchun2 := novyyMolchun(t)
	// Туннель тоже молчит: socks5 ведёт в молчуна.
	tun := novyySocks5(t, molchun2, "p")
	p := &Put{Tunnel: func() *url.URL { return tun.url() }, SrokPervogo: 200 * time.Millisecond, Zhurnal: func(string, ...any) {}}
	_, err := p.Klient(1 * time.Second).Get("http://" + molchun + "/x")
	if err == nil {
		t.Fatal("оба пути молчат — ждал отказ")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("свой срок первого пути не должен выглядеть «службу гасят» (context.Canceled): %v", err)
	}
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("ждал таймаут для повтора службы: %v", err)
	}
}

func TestOtkazObeihStoron(t *testing.T) {
	rv := novyyRvushchiy(t, false)
	rv2 := novyyRvushchiy(t, false)
	tun := novyySocks5(t, rv2.adres(), "p")
	z := &stroki{}
	p := &Put{Tunnel: func() *url.URL { return tun.url() }, Zhurnal: z.pishi}
	_, err := p.Klient(5 * time.Second).Get("http://" + rv.adres() + "/x")
	var dva *OshibkaDvuhPutey
	if !errors.As(err, &dva) {
		t.Fatalf("ждал OshibkaDvuhPutey: %v", err)
	}
	if !setevoy(err) {
		t.Fatalf("сетевой отказ прямого пути должен остаться виден: %v", err)
	}
	if !strings.Contains(err.Error(), "через туннель") {
		t.Fatalf("текст ошибки: %v", err)
	}
	if !strings.Contains(z.vse(), "через туннель тоже") {
		t.Fatalf("журнал: %q", z.vse())
	}
}

func TestNevernyyParolNeProhodit(t *testing.T) {
	rv := novyyRvushchiy(t, false)
	zhivoy, _ := novyyZhivoy(t)
	tun := novyySocks5(t, zhivoy.Listener.Addr().String(), "pravilnyy")
	chuzhoy := tun.url()
	chuzhoy.User = url.UserPassword(Polzovatel, "chuzhoy")
	p := &Put{Tunnel: func() *url.URL { return chuzhoy }, Zhurnal: func(string, ...any) {}}
	if _, err := p.Klient(5 * time.Second).Get("http://" + rv.adres() + "/x"); err == nil {
		t.Fatal("вход службы без верного пароля пускать не должен")
	}
	if tun.plohoyVhod.Load() == 0 {
		t.Fatal("пароль не спрашивали")
	}
}

// bezDNS — резолвер, который на любое имя отвечает «нет такого», не выходя в сеть.
func bezDNS() *net.Dialer {
	return &net.Dialer{Timeout: 5 * time.Second, Resolver: &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, &net.DNSError{Err: "no such host", Name: "test", IsNotFound: true}
		},
	}}
}

func TestImyaIzProfilyaBezDNS(t *testing.T) {
	zhivoy, _ := novyyZhivoy(t)
	_, port, _ := net.SplitHostPort(zhivoy.Listener.Addr().String())
	z := &stroki{}
	p := &Put{
		Adresa: func(host string) []string {
			if host == "subkv.primer.test" {
				return []string{"127.0.0.1"}
			}
			return nil
		},
		Zhurnal: z.pishi,
		dialer:  bezDNS(),
	}
	otvet, err := p.Klient(5 * time.Second).Get("http://subkv.primer.test:" + port + "/s/KOD/info")
	if err != nil {
		t.Fatalf("адрес из профиля должен был выручить: %v", err)
	}
	prochest(t, otvet)
	if !strings.Contains(z.vse(), "по адресу из профиля 127.0.0.1") {
		t.Fatalf("журнал: %q", z.vse())
	}
	// Имени нет в профиле — прежний отказ DNS.
	_, err = p.Klient(5 * time.Second).Get("http://chuzhoe.primer.test:" + port + "/")
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) {
		t.Fatalf("ждал прежнюю ошибку DNS: %v", err)
	}
}

func TestImenaIzProfilya(t *testing.T) {
	profil := []byte(`{"dns":{"servers":[
		{"type":"fakeip","tag":"fakeip"},
		{"type":"hosts","tag":"svoi-imena","predefined":{
			"subkv.chickenkiller.com":["77.239.102.44"],
			"NODE2KV.chickenkiller.com.":"31.77.203.26",
			"reklama.example":["0.0.0.0"],
			"lokalnyy.example":["192.168.1.1","127.0.0.1"]}},
		{"type":"tcp","tag":"local","server":"1.1.1.1"}]}}`)
	imena := ImenaIzProfilya(profil)
	if got := imena["subkv.chickenkiller.com"]; len(got) != 1 || got[0] != "77.239.102.44" {
		t.Fatalf("subkv: %v", got)
	}
	if got := imena["node2kv.chickenkiller.com"]; len(got) != 1 || got[0] != "31.77.203.26" {
		t.Fatalf("node2kv (одиночная строка, регистр и точка в конце): %v", got)
	}
	if _, est := imena["reklama.example"]; est {
		t.Fatal("0.0.0.0 — это блокировка, а не адрес")
	}
	if _, est := imena["lokalnyy.example"]; est {
		t.Fatal("внутренние и петлевые адреса — не адреса своих серверов")
	}
	if ImenaIzProfilya([]byte(`{"dns":{"servers":[{"type":"tcp","server":"1.1.1.1"}]}}`)) != nil {
		t.Fatal("без hosts — пусто")
	}
}

func TestAdresVhodaIUbratVhod(t *testing.T) {
	konfig := []byte(`{"inbounds":[{"type":"tun","tag":"tun-in"},
		{"type":"socks","tag":"kelevra-sluzhba","listen":"127.0.0.1","listen_port":20125}],
		"route":{"rules":[{"inbound":["kelevra-sluzhba"],"outbound":"Соединение"},{"action":"sniff"},{"inbound":["tun-in","kelevra-sluzhba"],"outbound":"direct"}]}}`)
	if got := AdresVhoda(konfig); got != "127.0.0.1:20125" {
		t.Fatalf("адрес входа %q", got)
	}
	if AdresVhoda([]byte(`{"inbounds":[{"type":"tun","tag":"tun-in"}]}`)) != "" {
		t.Fatal("без входа службы адрес пуст")
	}
	var d map[string]any
	if err := json.Unmarshal(konfig, &d); err != nil {
		t.Fatal(err)
	}
	if !UbratVhod(d) {
		t.Fatal("вход был — должен был тронуть")
	}
	if len(d["inbounds"].([]any)) != 1 {
		t.Fatal("вход службы не вычеркнут")
	}
	pravila := d["route"].(map[string]any)["rules"].([]any)
	if len(pravila) != 2 {
		t.Fatalf("вычеркнуть надо ровно своё правило, осталось %d", len(pravila))
	}
	if UbratVhod(d) {
		t.Fatal("второй раз трогать нечего")
	}
}

// setevoy — обрыв соединения: EOF, (на Windows) сброс, *net.OpError — или
// «http: server closed idle connection». Последнюю net/http отдаёт вместо EOF,
// когда сервер закрыл свежее соединение раньше, чем транспорт успел записать
// запрос (гонка readLoop и writeLoop; на CI с Windows ловится изредка). Это тот
// же обрыв, только неэкспортируемая ошибка errServerClosedIdle — разобрать её
// можно лишь по тексту.
func setevoy(err error) bool {
	var op *net.OpError
	return errors.Is(err, io.EOF) || errors.As(err, &op) ||
		strings.Contains(err.Error(), "server closed idle connection")
}

// POST с ключом повтора (Idempotency-Key) оборвался ПОСЛЕ того, как ушёл
// (у /logs так рвётся «bad record MAC»/«EOF» посреди тела) — повторяем через
// туннель с тем же телом: сервер узнаёт повтор сам. Без ключа — как раньше.
func TestPostSKlyuchomPovtoraIdyotTunnelem(t *testing.T) {
	zhivoy, telo := novyyZhivoy(t)
	tun := novyySocks5(t, zhivoy.Listener.Addr().String(), "p")
	p := &Put{Tunnel: func() *url.URL { return tun.url() }, Zhurnal: func(string, ...any) {}}
	rv := novyyRvushchiy(t, true) // дочитал запрос и оборвал: POST «ушёл»

	req, err := http.NewRequest(http.MethodPost, "http://"+rv.adres()+"/logs", strings.NewReader("zhurnal-telo"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Idempotency-Key", "0123abcd")
	otvet, err := p.Klient(10 * time.Second).Do(req)
	if err != nil {
		t.Fatalf("POST с ключом повтора должен был пройти туннелем: %v", err)
	}
	prochest(t, otvet)
	if got, _ := telo.Load().(string); got != "zhurnal-telo" {
		t.Fatalf("тело через туннель %q", got)
	}
	if tun.podklyuch.Load() != 1 {
		t.Fatalf("через туннель подключений %d, ждал 1", tun.podklyuch.Load())
	}
}
