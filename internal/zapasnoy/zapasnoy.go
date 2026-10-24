// Пакет zapasnoy: запасной путь для запросов службы к своим серверам и к
// GitHub — через туннель, когда прямой путь режется.
//
// Зачем. Служба — отдельный процесс, и её запросы (профиль /k и /s, сводка
// /info, наборы правил, /logs, проверка и загрузка обновления) идут не через
// туннель, а мимо него: наш домен не входит ни в один набор правил профиля,
// значит уходит route.final = direct, то есть к провайдеру как есть. Журнал
// ноутбука 19–20.10.2026 (К-Телеком, «Мотив»): «lookup subkv…: no such host»,
// «EOF», «TLS handshake timeout», у GitHub — «no such host» и «context
// deadline exceeded»; до сервера эти запросы не доходили. Туннель при этом был
// поднят и работал, а телефон в той же сети брал подписку через свой туннель
// без единой ошибки. Ноутбук сутки сидел на старом профиле.
//
// Что делает пакет:
//
//   - Put — http.RoundTripper службы. Сперва прямой путь (как было всегда);
//     сетевой отказ при поднятом туннеле — тот же запрос ещё раз через вход
//     службы в ядре (socks на 127.0.0.1 с паролем устройства, см. konfig и
//     TegVhoda). Туннель не поднят — ровно прежнее поведение, повтора нет.
//   - Прямой путь при «no such host» пробует адреса своих имён, которые сервер
//     зашил в профиль (dns.servers type:"hosts", predefined), — без DNS. TLS
//     проверяется по имени, как и раньше: адрес из профиля меняет только то,
//     куда стучаться, а не то, кому верить.
//
// Петли нет по построению: запрос через вход службы уходит в выход-туннель
// ядра, а свои соединения до узла ядро ведёт мимо собственного tun (так оно
// устроено всегда, иначе не работал бы и обычный трафик). Служба же в ядро
// заходит только как клиент локального входа.
package zapasnoy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// TegVhoda — тег входа службы в конфиге ядра. По нему konfig вписывает вход,
// yadro находит его адрес в запущенном конфиге и вычёркивает из отпечатка.
const TegVhoda = "kelevra-sluzhba"

// Polzovatel — имя на входе службы. Пароль — пароль устройства (тот же, что у
// Clash API): без него вход службы не вписывается вовсе.
const Polzovatel = "kelevra"

// AdresVhoda — host:port входа службы в конфиге ядра; пусто — входа нет.
func AdresVhoda(konfig []byte) string {
	var d struct {
		Inbounds []struct {
			Tag        string `json:"tag"`
			Listen     string `json:"listen"`
			ListenPort int    `json:"listen_port"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(konfig, &d); err != nil {
		return ""
	}
	for _, v := range d.Inbounds {
		if v.Tag != TegVhoda || v.ListenPort <= 0 {
			continue
		}
		host := v.Listen
		if host == "" || host == "::" || host == "0.0.0.0" {
			host = "127.0.0.1"
		}
		return net.JoinHostPort(host, strconv.Itoa(v.ListenPort))
	}
	return ""
}

// UbratVhod вычёркивает из разобранного конфига вход службы и правило,
// ведущее его в туннель. Нужен отпечатку конфига (yadro.OtpechatokKonfigaBayt):
// вход — служебная деталь, как имя адаптера, и смена его порта или само его
// появление после обновления приложения не повод менять живое ядро.
// Возвращает, тронул ли что-нибудь.
func UbratVhod(d map[string]any) bool {
	tronuli := false
	if vhody, ok := d["inbounds"].([]any); ok {
		ostalis := make([]any, 0, len(vhody))
		for _, v := range vhody {
			if vh, ok := v.(map[string]any); ok {
				if t, _ := vh["tag"].(string); t == TegVhoda {
					tronuli = true
					continue
				}
			}
			ostalis = append(ostalis, v)
		}
		if tronuli {
			d["inbounds"] = ostalis
		}
	}
	if r, ok := d["route"].(map[string]any); ok {
		if pravila, ok := r["rules"].([]any); ok {
			ostalis := make([]any, 0, len(pravila))
			ubrali := false
			for _, p := range pravila {
				if pr, ok := p.(map[string]any); ok && praviloVhoda(pr) {
					ubrali = true
					continue
				}
				ostalis = append(ostalis, p)
			}
			if ubrali {
				r["rules"] = ostalis
				tronuli = true
			}
		}
	}
	return tronuli
}

// praviloVhoda — правило, которое konfig ставит для входа службы: единственный
// вход в списке, и это наш.
func praviloVhoda(pr map[string]any) bool {
	switch v := pr["inbound"].(type) {
	case string:
		return v == TegVhoda
	case []any:
		if len(v) != 1 {
			return false
		}
		s, _ := v[0].(string)
		return s == TegVhoda
	}
	return false
}

// ImenaIzProfilya — имена, которые сервер зашил в профиль (dns.servers
// type:"hosts", поле predefined): имя → адреса. Пустые, нулевые, петлевые и
// внутренние адреса отброшены — такими hosts-записями блокируют, а не ведут.
func ImenaIzProfilya(profil []byte) map[string][]string {
	var d struct {
		DNS struct {
			Servers []map[string]any `json:"servers"`
		} `json:"dns"`
	}
	if err := json.Unmarshal(profil, &d); err != nil {
		return nil
	}
	imena := map[string][]string{}
	for _, s := range d.DNS.Servers {
		if t, _ := s["type"].(string); t != "hosts" {
			continue
		}
		pred, _ := s["predefined"].(map[string]any)
		for imya, adresa := range pred {
			imya = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(imya), "."))
			if imya == "" {
				continue
			}
			var spisok []string
			switch a := adresa.(type) {
			case string:
				spisok = []string{a}
			case []any:
				for _, x := range a {
					if str, ok := x.(string); ok {
						spisok = append(spisok, str)
					}
				}
			}
			for _, a := range spisok {
				ip := net.ParseIP(strings.TrimSpace(a))
				if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
					continue
				}
				imena[imya] = append(imena[imya], ip.String())
			}
		}
	}
	if len(imena) == 0 {
		return nil
	}
	return imena
}

// Put — транспорт запросов службы с запасным путём через туннель.
// Пустой Put работоспособен и ведёт себя как обычный транспорт Go.
type Put struct {
	// Tunnel — адрес входа службы в ядре (socks5://…), если туннель поднят и
	// вход в его конфиге есть; nil — туннеля нет, повтора не будет.
	Tunnel func() *url.URL
	// Adresa — адреса имени из профиля (см. ImenaIzProfilya); nil или пусто —
	// без них.
	Adresa func(host string) []string
	// SrokPervogo — сколько первый путь ждёт ответа сервера, когда за ним есть
	// второй. Ноль — SrokPervogoPoUmolchaniyu. Без потолка прямой путь съедал
	// бы весь срок клиента, и туннелю не оставалось бы времени.
	SrokPervogo time.Duration
	// PomnitObhod — сколько помнить, что к имени напрямую не пройти: в это
	// время первым идёт туннель (иначе 23 набора правил ждали бы прямого пути
	// по очереди). Ноль — PomnitObhodPoUmolchaniyu.
	PomnitObhod time.Duration
	// Zhurnal — строка в журнал службы; nil — log.Printf.
	Zhurnal func(format string, a ...any)

	odinRaz sync.Once
	pryamoy *http.Transport
	dialer  *net.Dialer
	// tlsDlyaStenda — доверие к сертификату стенда (httptest); nil в бою.
	tlsDlyaStenda *tls.Config

	zamok     sync.Mutex
	tunnelT   *http.Transport
	tunnelURL string
	obhod     map[string]time.Time // имя → до каких пор первым идёт туннель
	molchat   map[string]time.Time // имя → до каких пор не повторять строку про адрес из профиля
}

const (
	SrokPervogoPoUmolchaniyu = 12 * time.Second
	PomnitObhodPoUmolchaniyu = 15 * time.Minute
)

// Klient — http.Client службы поверх этого транспорта.
func (p *Put) Klient(srok time.Duration) *http.Client {
	return &http.Client{Timeout: srok, Transport: p}
}

func (p *Put) zhurnal(format string, a ...any) {
	if p.Zhurnal != nil {
		p.Zhurnal(format, a...)
		return
	}
	log.Printf(format, a...)
}

func (p *Put) init() {
	p.odinRaz.Do(func() {
		if p.dialer == nil { // проверки ставят свой (резолвер без сети)
			p.dialer = &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		}
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.DialContext = p.nabrat
		if p.tlsDlyaStenda != nil {
			t.TLSClientConfig = p.tlsDlyaStenda.Clone()
		}
		p.pryamoy = t
	})
}

// nabrat — набор прямого соединения. Имя не разрешилось, а сервер зашил его
// адрес в профиль, — соединяемся по адресу из профиля без DNS.
func (p *Put) nabrat(ctx context.Context, network, addr string) (net.Conn, error) {
	c, err := p.dialer.DialContext(ctx, network, addr)
	if err == nil || p.Adresa == nil || ctx.Err() != nil {
		return c, err
	}
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) {
		return nil, err
	}
	host, port, serr := net.SplitHostPort(addr)
	if serr != nil || net.ParseIP(host) != nil {
		return nil, err
	}
	adresa := p.Adresa(strings.ToLower(host))
	for _, a := range adresa {
		c2, err2 := p.dialer.DialContext(ctx, network, net.JoinHostPort(a, port))
		if err2 == nil {
			if p.skazatProAdres(host) {
				p.zhurnal("имя %s не разрешилось (%v) — соединяюсь по адресу из профиля %s", host, korotko(err), a)
			}
			return c2, nil
		}
	}
	return nil, err
}

// skazatProAdres — писать ли строку про адрес из профиля (не чаще раза в 10 минут на имя).
func (p *Put) skazatProAdres(host string) bool {
	p.zamok.Lock()
	defer p.zamok.Unlock()
	if p.molchat == nil {
		p.molchat = map[string]time.Time{}
	}
	if time.Now().Before(p.molchat[host]) {
		return false
	}
	p.molchat[host] = time.Now().Add(10 * time.Minute)
	return true
}

func (p *Put) srokPervogo() time.Duration {
	if p.SrokPervogo > 0 {
		return p.SrokPervogo
	}
	return SrokPervogoPoUmolchaniyu
}

func (p *Put) pomnitObhod() time.Duration {
	if p.PomnitObhod > 0 {
		return p.PomnitObhod
	}
	return PomnitObhodPoUmolchaniyu
}

// transportTunnelya — транспорт через вход службы; один на адрес входа.
func (p *Put) transportTunnelya(u *url.URL) *http.Transport {
	klyuch := u.String()
	p.zamok.Lock()
	defer p.zamok.Unlock()
	if p.tunnelT != nil && p.tunnelURL == klyuch {
		return p.tunnelT
	}
	if p.tunnelT != nil {
		p.tunnelT.CloseIdleConnections()
	}
	p.tunnelT = &http.Transport{
		Proxy:                 http.ProxyURL(u),
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	if p.tlsDlyaStenda != nil {
		p.tunnelT.TLSClientConfig = p.tlsDlyaStenda.Clone()
	}
	p.tunnelURL = klyuch
	return p.tunnelT
}

func (p *Put) obhodSvezh(host string) bool {
	p.zamok.Lock()
	defer p.zamok.Unlock()
	return time.Now().Before(p.obhod[host])
}

func (p *Put) zapomnitObhod(host string, da bool) {
	p.zamok.Lock()
	defer p.zamok.Unlock()
	if !da {
		delete(p.obhod, host)
		return
	}
	if p.obhod == nil {
		p.obhod = map[string]time.Time{}
	}
	p.obhod[host] = time.Now().Add(p.pomnitObhod())
}

// RoundTrip — см. шапку пакета.
func (p *Put) RoundTrip(req *http.Request) (*http.Response, error) {
	p.init()
	var tun *url.URL
	if p.Tunnel != nil {
		tun = p.Tunnel()
	}
	if tun == nil || !mozhnoPovtorit(req) {
		// Туннеля нет — как было всегда (с одним добавлением: адрес своего
		// имени из профиля при «no such host», см. nabrat).
		return p.pryamoy.RoundTrip(req)
	}
	host := strings.ToLower(req.URL.Hostname())
	tunT := p.transportTunnelya(tun)
	snachalaTunnel := p.obhodSvezh(host)
	pervyy, vtoroy := http.RoundTripper(p.pryamoy), http.RoundTripper(tunT)
	if snachalaTunnel {
		pervyy, vtoroy = vtoroy, pervyy
	}

	otvet, napisano, err1 := popytka(req, pervyy, srokPopytki(req.Context(), p.srokPervogo()))
	if err1 == nil {
		return otvet, nil
	}
	if req.Context().Err() != nil {
		return nil, err1 // запрос отменил сам вызывающий — повторять некому
	}
	if napisano && !idempotentnyy(req.Method) && !povtorRazreshen(req) {
		// Запрос уже ушёл: сервер мог его принять, второй раз слать нельзя.
		return nil, err1
	}
	req2, err := kopiyaZaprosa(req)
	if err != nil {
		return nil, err1
	}
	otvet, _, err2 := popytka(req2, vtoroy, 0)
	switch {
	case err2 == nil && !snachalaTunnel:
		p.zapomnitObhod(host, true)
		p.zhurnal("напрямую %s не отвечает (%s) — через туннель вышло; %s буду ходить туннелем",
			host, korotko(err1), okruglit(p.pomnitObhod()))
		return otvet, nil
	case err2 == nil:
		p.zapomnitObhod(host, false)
		p.zhurnal("через туннель %s не отвечает (%s), напрямую вышло — снова хожу напрямую", host, korotko(err1))
		return otvet, nil
	case !snachalaTunnel:
		p.zhurnal("напрямую %s не отвечает (%s), через туннель тоже (%s)", host, korotko(err1), korotko(err2))
		return nil, &OshibkaDvuhPutey{Pryamo: err1, Tunnel: err2}
	default:
		p.zapomnitObhod(host, false)
		p.zhurnal("через туннель %s не отвечает (%s), напрямую тоже (%s)", host, korotko(err1), korotko(err2))
		return nil, &OshibkaDvuhPutey{Pryamo: err2, Tunnel: err1}
	}
}

// OshibkaDvuhPutey — не вышло ни напрямую, ни через туннель. Цепочка
// errors.Is/As видит обе причины (сетевой отказ остаётся сетевым отказом для
// повтора службы).
type OshibkaDvuhPutey struct {
	Pryamo error
	Tunnel error
}

func (e *OshibkaDvuhPutey) Error() string {
	return fmt.Sprintf("%v; через туннель: %v", e.Pryamo, e.Tunnel)
}

func (e *OshibkaDvuhPutey) Unwrap() []error { return []error{e.Pryamo, e.Tunnel} }

// OshibkaSroka — первый путь не дождался ответа за отведённый ему срок.
// Для повтора службы это таймаут, а не «служба гасится» (context.Canceled).
type OshibkaSroka struct {
	Srok time.Duration
}

func (e *OshibkaSroka) Error() string {
	return fmt.Sprintf("нет ответа за %s", okruglit(e.Srok))
}
func (e *OshibkaSroka) Timeout() bool   { return true }
func (e *OshibkaSroka) Temporary() bool { return true }
func (e *OshibkaSroka) Unwrap() error   { return context.DeadlineExceeded }

// srokPopytki — срок первого пути: не больше srok и не больше половины того,
// что осталось запросу (иначе второму пути не хватит времени вовсе).
func srokPopytki(ctx context.Context, srok time.Duration) time.Duration {
	if d, ok := ctx.Deadline(); ok {
		if pol := time.Until(d) / 2; pol < srok {
			srok = pol
		}
	}
	if srok < time.Second {
		srok = time.Second
	}
	return srok
}

// popytka — один проход запроса по одному пути. srok > 0 — потолок ожидания
// ответа (заголовков); тело ответа читается уже без него. napisano — успел ли
// запрос уйти серверу целиком (заголовки записаны).
func popytka(req *http.Request, rt http.RoundTripper, srok time.Duration) (*http.Response, bool, error) {
	var napisano, istek atomic.Bool
	ctx, otmena := context.WithCancel(req.Context())
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteHeaders: func() { napisano.Store(true) },
	})
	var taymer *time.Timer
	if srok > 0 {
		taymer = time.AfterFunc(srok, func() { istek.Store(true); otmena() })
	}
	otvet, err := rt.RoundTrip(req.WithContext(ctx))
	if taymer != nil && !taymer.Stop() && err == nil {
		// Срок вышел ровно в миг ответа: тело уже отменено, отдавать его нельзя.
		otvet.Body.Close()
		otvet, err = nil, context.Canceled
	}
	if err != nil {
		otmena()
		if istek.Load() {
			err = &OshibkaSroka{Srok: srok}
		}
		return nil, napisano.Load(), err
	}
	otvet.Request = req
	otvet.Body = &teloSOtmenoy{ReadCloser: otvet.Body, otmena: otmena}
	return otvet, napisano.Load(), nil
}

type teloSOtmenoy struct {
	io.ReadCloser
	otmena context.CancelFunc
}

func (t *teloSOtmenoy) Close() error {
	err := t.ReadCloser.Close()
	t.otmena()
	return err
}

// mozhnoPovtorit — тело запроса можно отправить второй раз.
func mozhnoPovtorit(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

func kopiyaZaprosa(req *http.Request) (*http.Request, error) {
	r := req.Clone(req.Context())
	if req.Body != nil && req.Body != http.NoBody {
		b, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		r.Body = b
	}
	return r, nil
}

func idempotentnyy(metod string) bool {
	switch metod {
	case "", http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// povtorRazreshen — отправитель сам объявил запрос безопасным для повтора
// заголовком Idempotency-Key (так же его понимает net/http: с ним Transport
// сам повторяет POST на свежем соединении, когда старое оказалось мёртвым).
//
// Зачем. POST /logs рвался посреди тела («remote error: tls: bad record MAC»,
// «EOF»; разбор 24.10.2026 по журналам nginx обоих VPS и устройств): заголовки
// ушли, значит по старому правилу второго пути не было, и журнал ждал час.
// А сервер /logs повтор принимает честно: отпечаток sha256 тела, второй раз
// отвечает «ok, povtor» и ничего не кладёт (KelevraServer app.py receive_logs).
func povtorRazreshen(req *http.Request) bool {
	return req.Header.Get("Idempotency-Key") != "" || req.Header.Get("X-Idempotency-Key") != ""
}

// korotko — причина отказа для журнала. Ошибки транспорта адреса запроса не
// несут (его добавляет http.Client снаружи), значит и кода доступа в них нет.
func korotko(err error) string {
	var sr *OshibkaSroka
	if errors.As(err, &sr) {
		return sr.Error()
	}
	return err.Error()
}

func okruglit(d time.Duration) time.Duration {
	if d >= time.Minute {
		return d.Round(time.Minute)
	}
	return d.Round(time.Second)
}
