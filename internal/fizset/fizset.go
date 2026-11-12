// Пакет fizset: проба «физическая сеть есть» — не зависящая от ядра.
//
// Зачем. Живой случай 07.11.2026 (ноут, проба 0.6.103): Wi-Fi выключили и
// включили, сеть вернулась, а ядро ещё полторы минуты не могло разрешить имя
// своего узла — туннель молчал, пока человек не нажал «Отключить →
// Подключить» (свежее ядро ответило за секунду). Служба в это время писала
// «напрямую node2kv… не отвечает (EOF / нет ответа за 10s), через туннель
// тоже»: её «прямая» проба падала ВМЕСТЕ с туннелем. Прямой путь службы —
// обычный сокет и системный DNS, а при поднятом TUN и то и другое идёт в
// ядро (маршрут по умолчанию на tun125, DNS перехватывает hijack-dns, выход
// direct ядро набирает само своим резолвером). То есть «напрямую» на деле
// значило «через то же самое ядро, только мимо узла».
//
// Здесь проба, которой ядро не нужно:
//   - сокет привязан к физическому адаптеру: IP_UNICAST_IF (индекс
//     интерфейса) и свой адрес адаптера — пакеты уходят в Wi-Fi/кабель, а не
//     в TUN, тем же способом, каким ядро само ходит к узлам
//     (auto_detect_interface);
//   - соединение по IP, без DNS: адреса узлов из конфига ядра (поле server,
//     если там адрес, иначе — адреса, которые сервер зашил в профиль, dns.servers
//     type:"hosts");
//   - только TCP-рукопожатие: встало — путь до узла есть.
//
// Итог — три ответа: до узлов есть путь (SetEst), до узлов нет, а до общих
// адресов есть (UzlyNedostupny: узел лежит или оператор режет его адрес),
// нет ни до чего (NetSeti). Запрет мимо туннеля (WFP) пробе не мешает:
// служба разрешена в нём по своему exe.
package fizset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/zapasnoy"
)

// Itog — что показала проба.
type Itog int

const (
	// NetSeti — физического адаптера с выходом нет или ни один адрес не
	// ответил: сети нет, ядро тут ни при чём.
	NetSeti Itog = iota
	// UzlyNedostupny — общие адреса отвечают, а узлы напрямую нет: узел лежит
	// или оператор режет его адрес. Перезапуск ядра этого не лечит.
	UzlyNedostupny
	// SetEst — до узла есть путь по физической сети (или, когда адресов узлов
	// не знаем, до общего адреса).
	SetEst
)

func (i Itog) String() string {
	switch i {
	case SetEst:
		return "сеть есть"
	case UzlyNedostupny:
		return "узлы напрямую не отвечают"
	default:
		return "нет сети"
	}
}

// ObshchieCeli — общие адреса для ответа «сеть вообще есть», когда узлы
// напрямую молчат: крупные анонимные DNS-службы на 443, без имён.
var ObshchieCeli = []string{"1.1.1.1:443", "8.8.8.8:443", "77.88.8.8:443"}

// SrokPoUmolchaniyu — сколько ждать рукопожатия на один круг целей.
const SrokPoUmolchaniyu = 3 * time.Second

// predelCeley — больше стольких узлов за раз не трогаем.
const predelCeley = 8

// Rezultat — итог пробы и одной строкой — чем доказан.
type Rezultat struct {
	Itog     Itog
	Opisanie string
}

// Proba — одна проба. Пустые поля — настоящие (FizicheskieAdaptery,
// ObshchieCeli, SrokPoUmolchaniyu, привязанный к адаптеру net.Dialer).
type Proba struct {
	// Uzly — host:port узлов (адрес IPv4, без имён); см. CeliIzKonfiga.
	Uzly []string
	// Obshchie — host:port общих адресов; nil — ObshchieCeli.
	Obshchie []string
	// Adaptery — физические адаптеры; nil — avtorezhim.FizicheskieAdaptery.
	Adaptery func() []avtorezhim.FizAdapter
	// Srok — на один круг целей.
	Srok time.Duration
	// Nabrat — подмена набора (проверки): TCP-соединение через адаптер a.
	Nabrat func(ctx context.Context, a avtorezhim.FizAdapter, adres string) (net.Conn, error)
}

// Proverit — проба: сперва узлы, не ответил ни один — общие адреса.
func (p Proba) Proverit(ctx context.Context) Rezultat {
	vzyat := p.Adaptery
	if vzyat == nil {
		vzyat = avtorezhim.FizicheskieAdaptery
	}
	adaptery := vzyat()
	if len(adaptery) == 0 {
		return Rezultat{NetSeti, "нет физического адаптера с адресом и шлюзом"}
	}
	obshchie := p.Obshchie
	if obshchie == nil {
		obshchie = ObshchieCeli
	}
	var bedaUzlov string
	if len(p.Uzly) > 0 {
		kto, beda := p.krug(ctx, adaptery, p.Uzly)
		if kto != "" {
			return Rezultat{SetEst, "узел " + kto}
		}
		bedaUzlov = beda
	}
	if ctx.Err() != nil {
		return Rezultat{NetSeti, "проба прервана"}
	}
	kto, beda := p.krug(ctx, adaptery, obshchie)
	switch {
	case kto != "" && len(p.Uzly) == 0:
		return Rezultat{SetEst, "общий адрес " + kto + " (адресов узлов в конфиге нет)"}
	case kto != "":
		return Rezultat{UzlyNedostupny, "узлы: " + bedaUzlov + "; общий адрес " + kto + " отвечает"}
	case bedaUzlov != "":
		return Rezultat{NetSeti, "узлы: " + bedaUzlov + "; общие адреса: " + beda}
	default:
		return Rezultat{NetSeti, "общие адреса: " + beda}
	}
}

// krug — все цели через все адаптеры разом; первый успех гасит остальных.
// kto — «адрес через адаптер» того, кто ответил; иначе beda — первая причина.
func (p Proba) krug(ctx context.Context, adaptery []avtorezhim.FizAdapter, celi []string) (kto, beda string) {
	srok := p.Srok
	if srok <= 0 {
		srok = SrokPoUmolchaniyu
	}
	nabrat := p.Nabrat
	if nabrat == nil {
		nabrat = NabratCherezAdapter
	}
	kctx, otmena := context.WithTimeout(ctx, srok)
	defer otmena()
	type otvet struct {
		kto string
		err error
	}
	otvety := make(chan otvet, len(adaptery)*len(celi))
	var wg sync.WaitGroup
	for _, a := range adaptery {
		for _, c := range celi {
			wg.Add(1)
			go func(a avtorezhim.FizAdapter, c string) {
				defer wg.Done()
				conn, err := nabrat(kctx, a, c)
				if err == nil {
					conn.Close()
				}
				otvety <- otvet{kto: c + opisatAdapter(a), err: err}
			}(a, c)
		}
	}
	go func() { wg.Wait(); close(otvety) }()
	for o := range otvety {
		if o.err == nil {
			otmena()
			if kto == "" {
				kto = o.kto
			}
			continue
		}
		if beda == "" {
			beda = o.kto + ": " + korotko(o.err)
		}
	}
	return kto, beda
}

func opisatAdapter(a avtorezhim.FizAdapter) string {
	if a.Indeks == 0 && a.IP == "" {
		return ""
	}
	return fmt.Sprintf(" через адаптер %d (%s)", a.Indeks, a.IP)
}

func korotko(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "нет ответа"
	case errors.Is(err, context.Canceled):
		return "отменено"
	}
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && i+2 < len(s) {
		s = s[i+2:]
	}
	return s
}

// NabratCherezAdapter — TCP до adres (IPv4:порт) сокетом, привязанным к
// адаптеру: свой адрес адаптера и IP_UNICAST_IF (privyazka_windows.go). Адрес
// и индекс не известны — обычный сокет.
func NabratCherezAdapter(ctx context.Context, a avtorezhim.FizAdapter, adres string) (net.Conn, error) {
	d := net.Dialer{}
	if ip := net.ParseIP(a.IP).To4(); ip != nil {
		d.LocalAddr = &net.TCPAddr{IP: ip}
	}
	if a.Indeks != 0 {
		d.Control = privyazat(a.Indeks)
	}
	return d.DialContext(ctx, "tcp4", adres)
}

// tcpVyhody — типы выходов ядра, до узла которых путь — TCP. UDP-выходы
// (hysteria, tuic, wireguard) рукопожатием TCP не проверить.
var tcpVyhody = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "shadowsocks": true,
	"anytls": true, "shadowtls": true, "http": true, "socks": true, "naive": true,
}

// CeliIzKonfiga — адреса узлов (IPv4:порт) из конфига ядра: выходы с
// TCP-путём до сервера; имя сервера — через адреса, которые сервер зашил в
// профиль (dns.servers type:"hosts"), без DNS. Петлевые и внутренние адреса
// (локальная комната) не берутся.
func CeliIzKonfiga(syroy []byte) []string {
	var d struct {
		Outbounds []struct {
			Type       string `json:"type"`
			Server     string `json:"server"`
			ServerPort int    `json:"server_port"`
		} `json:"outbounds"`
	}
	if json.Unmarshal(syroy, &d) != nil {
		return nil
	}
	imena := zapasnoy.ImenaIzProfilya(syroy)
	var celi []string
	byli := map[string]bool{}
	for _, o := range d.Outbounds {
		if !tcpVyhody[strings.ToLower(o.Type)] || o.Server == "" {
			continue
		}
		port := o.ServerPort
		if port <= 0 {
			port = 443
		}
		var adresa []string
		if _, err := netip.ParseAddr(o.Server); err == nil {
			adresa = []string{o.Server}
		} else {
			adresa = imena[strings.ToLower(strings.TrimSuffix(o.Server, "."))]
		}
		for _, a := range adresa {
			ip, err := netip.ParseAddr(a)
			if err != nil || !ip.Is4() || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
				continue
			}
			c := net.JoinHostPort(ip.String(), strconv.Itoa(port))
			if byli[c] {
				continue
			}
			byli[c] = true
			celi = append(celi, c)
			if len(celi) >= predelCeley {
				return celi
			}
		}
	}
	return celi
}
