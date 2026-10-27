package konfig

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Живой щуп входа домой: два настоящих ядра на петле, без туннеля и без
// правки сети машины.
//
//   - «хаб дома» — второе ядро с эндпоинтом WireGuard на 127.0.0.1:<порт>,
//     адрес 100.77.0.1, свой DNS (kelevra.dom → 100.77.1.160) и правило,
//     уводящее 100.77.1.160 на эхо-сервер петли;
//   - «ноутбук» — конфиг из Prigotovit с блоком дома, у которого мост — порт
//     хаба, а «Соединение» — селектор поверх direct (туннель щупу не нужен);
//     вход tun заменён входом щупа (vstavitVhodShchupa).
//
// Проверяется вся цепочка: эндпоинт «Дом» с ключом устройства, detour на
// «Соединение», правило сетей дома и DNS зоны через «Дом». Контроль порчей —
// тот же конфиг без правила дома: до 100.77.1.160 не доходит.
func TestDomZhivoeYadro(t *testing.T) {
	bin := putYadra(t)
	if bin == "" {
		t.Skipf("настоящего ядра рядом нет — щуп пропущен. Искал: %s", mestaPoiskaYadra())
	}
	klyuch := func() (string, string) {
		k, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(k.Bytes()), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes())
	}
	hubPriv, hubPub := klyuch()
	ustrPriv, ustrPub := klyuch()

	eho := echoTCP(t, "127.0.0.1", svobodnyPort(t))
	defer eho.Close()
	ehoPort := eho.Addr().(*net.TCPAddr).Port

	// Порт WireGuard хаба: свободный UDP.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	wgPort := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()

	papka := t.TempDir()
	hubMixed := svobodnyPort(t)
	hub := map[string]any{
		"log":      map[string]any{"level": "info"},
		"inbounds": []any{map[string]any{"type": "mixed", "tag": "gotov", "listen": "127.0.0.1", "listen_port": hubMixed}},
		"endpoints": []any{map[string]any{
			"type": "wireguard", "tag": "hub", "mtu": 1280,
			"address": []any{"100.77.0.1/32"}, "private_key": hubPriv, "listen_port": wgPort,
			"peers": []any{map[string]any{"public_key": ustrPub, "allowed_ips": []any{"100.77.0.5/32"}}},
		}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"dns": map[string]any{
			"servers": []any{map[string]any{"type": "hosts", "tag": "imena",
				"predefined": map[string]any{"kelevra.dom": []any{"100.77.1.160"}}}},
		},
		"route": map[string]any{
			"final": "direct",
			"rules": []any{
				map[string]any{"action": "sniff"},
				map[string]any{"port": 53, "action": "hijack-dns"},
				map[string]any{"ip_cidr": []any{"100.77.1.160/32"}, "action": "route-options", "override_address": "127.0.0.1"},
			},
		},
	}
	putHub := filepath.Join(papka, "hub", "config.json")
	if err := writeFileVPapku(t, putHub, hub); err != nil {
		t.Fatal(err)
	}
	cmdHub, logHub := zapustitYadro(t, bin, putHub, hubMixed)
	defer ostanovitYadro(cmdHub)

	mixedPort := svobodnyPort(t)
	syroy := profilDlyaZhivogoSDns(t)
	dom := &Dom{
		PrivatnyyKlyuch: ustrPriv, Adres: "100.77.0.5/32", UzelPub: hubPub,
		MostHost: "127.0.0.1", MostPort: wgPort, Seti: []string{"100.77.0.0/16"},
		DNS: "100.77.0.1", Zona: "dom", MTU: 1280,
	}
	gotovyy, k, err := Prigotovit(syroy, Vybor{Dom: dom})
	if err != nil || !k.Dom {
		t.Fatalf("Prigotovit: %v, %+v", err, k)
	}

	zapustit := func(t *testing.T, imya string, konf []byte) (string, func(), func() string) {
		konf = vstavitVhodShchupa(t, konf, mixedPort)
		put := filepath.Join(papka, imya, "config.json")
		var d map[string]any
		_ = json.Unmarshal(konf, &d)
		// DNS-правила по входу tun — и для входа щупа (vstavitVhodShchupa
		// правит только route.rules).
		if dn, ok := d["dns"].(map[string]any); ok {
			for _, p := range dn["rules"].([]any) {
				if pr := p.(map[string]any); pr["inbound"] != nil {
					pr["inbound"] = append(pr["inbound"].([]any), "mixed-in")
				}
			}
		}
		if err := writeFileVPapku(t, put, d); err != nil {
			t.Fatal(err)
		}
		cmd, log := zapustitYadro(t, bin, put, mixedPort)
		return fmt.Sprintf("127.0.0.1:%d", mixedPort), func() { ostanovitYadro(cmd) }, log.String
	}

	t.Run("имя зоны и адрес дома доходят через Дом", func(t *testing.T) {
		proksi, stop, log := zapustit(t, "noutbuk", gotovyy)
		defer stop()
		// Первое рукопожатие WireGuard — до пары секунд: пробуем с запасом.
		var adres string
		for i := 0; i < 10 && adres == ""; i++ {
			adres = dnsCherezProksi(t, proksi, "kelevra.dom")
		}
		if adres != "100.77.1.160" {
			t.Fatalf("kelevra.dom через Дом: %q, ждали 100.77.1.160\nноутбук:\n%s\nхаб:\n%s", adres, log(), logHub.String())
		}
		// Проба службы (sluzhba/dom.go) — DNS прямо на 100.77.0.1:53: правило
		// дома стоит раньше hijack-dns, и запрос уходит в дом, а не в DNS ядра.
		ctrl, relay, err := socks5UdpAssociate(proksi)
		if err != nil {
			t.Fatal(err)
		}
		otvet, _ := udpCherezReley(t, relay, "100.77.0.1", 53, dnsZapros("kelevra.dom"), 3*time.Second)
		ctrl.Close()
		if pervyyA(otvet) != "100.77.1.160" {
			t.Fatalf("DNS дома напрямую: %q\n%s", pervyyA(otvet), log())
		}
		c, kod, err := socks5Connect(t, proksi, "100.77.1.160", ehoPort)
		if err != nil || kod != 0 {
			t.Fatalf("CONNECT 100.77.1.160: %v код %d\n%s", err, kod, log())
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(5 * time.Second))
		c.Write([]byte("proba-doma"))
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		if string(buf[:n]) != "proba-doma" {
			t.Fatalf("эхо через Дом: %q\nноутбук:\n%s\nхаб:\n%s", buf[:n], log(), logHub.String())
		}
		if !strings.Contains(log(), "Дом") {
			t.Fatalf("в журнале ноутбука нет выхода «Дом»:\n%s", log())
		}
	})

	// «Закрыто по умолчанию»: блока нет, вход домой бывал — до дома не
	// доходит ни адрес, ни имя (и не утекает в обычный выход).
	t.Run("закрыто: без блока ни адрес, ни имя дома", func(t *testing.T) {
		zakrytyy, kz, err := Prigotovit(syroy, Vybor{DomZakryt: true})
		if err != nil || !kz.DomZakryt {
			t.Fatalf("закрытый конфиг: %v %+v", err, kz)
		}
		proksi, stop, log := zapustit(t, "zakryto", zakrytyy)
		defer stop()
		if a := dnsCherezProksi(t, proksi, "kelevra.dom"); a != "" {
			t.Fatalf("имя дома разрешилось при закрытом: %q", a)
		}
		c, kod, err := socks5Connect(t, proksi, "100.77.1.160", ehoPort)
		if err == nil && kod == 0 {
			defer c.Close()
			c.SetDeadline(time.Now().Add(3 * time.Second))
			c.Write([]byte("proba-doma"))
			buf := make([]byte, 64)
			if n, _ := c.Read(buf); string(buf[:n]) == "proba-doma" {
				t.Fatalf("при закрытом трафик дошёл:\n%s", log())
			}
		}
	})

	t.Run("контроль: без правила дома не доходит", func(t *testing.T) {
		var d map[string]any
		_ = json.Unmarshal(gotovyy, &d)
		r := d["route"].(map[string]any)
		ostalos := []any{}
		for _, p := range r["rules"].([]any) {
			if p.(map[string]any)["outbound"] == TegDoma {
				continue
			}
			ostalos = append(ostalos, p)
		}
		r["rules"] = ostalos
		isporchen, _ := json.Marshal(d)
		proksi, stop, _ := zapustit(t, "porcha", isporchen)
		defer stop()
		c, kod, err := socks5Connect(t, proksi, "100.77.1.160", ehoPort)
		if err != nil || kod != 0 {
			return // не соединилось вовсе — тоже «не дошло»
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		c.Write([]byte("proba-doma"))
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		if string(buf[:n]) == "proba-doma" {
			t.Fatal("без правила дома трафик всё равно дошёл — щуп ничего не проверяет")
		}
	})
}

// profilDlyaZhivogoSDns — профиль щупа с DNS: свой резолвер (молчащий порт
// петли — в сеть щуп не ходит), снифф, hijack-dns и правило частных сетей,
// как в боевом профиле.
func profilDlyaZhivogoSDns(t *testing.T) []byte {
	t.Helper()
	mertvyy := svobodnyPort(t)
	d := map[string]any{
		"log": map[string]any{"level": "info", "timestamp": true},
		"inbounds": []any{map[string]any{
			"type": "tun", "tag": "tun-in", "interface_name": "tun125", "address": []any{"172.19.0.1/30"},
		}},
		"outbounds": []any{
			map[string]any{"type": "direct", "tag": "direct"},
			map[string]any{"type": "selector", "tag": "Соединение", "outbounds": []any{"direct"}, "default": "direct"},
		},
		"dns": map[string]any{
			"final":   "local",
			"servers": []any{map[string]any{"type": "udp", "tag": "local", "server": "127.0.0.1", "server_port": mertvyy}},
		},
		"route": map[string]any{
			"final":                   "direct",
			"default_domain_resolver": map[string]any{"server": "local"},
			"rules": []any{
				map[string]any{"action": "sniff"},
				map[string]any{"type": "logical", "mode": "or", "action": "hijack-dns",
					"rules": []any{map[string]any{"protocol": "dns"}, map[string]any{"port": 53}}},
				map[string]any{"outbound": "direct", "ip_is_private": true},
			},
		},
	}
	b, _ := json.Marshal(d)
	return b
}

func writeFileVPapku(t *testing.T, put string, d map[string]any) error {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
		return err
	}
	return writeFile(t, put, b)
}

// dnsCherezProksi — запрос A через UDP вход щупа на любой адрес :53 (ядро
// перехватывает его hijack-dns); "" — ответа нет или в нём нет адреса.
func dnsCherezProksi(t *testing.T, proksi, imya string) string {
	t.Helper()
	ctrl, relay, err := socks5UdpAssociate(proksi)
	if err != nil {
		t.Fatalf("UDP ASSOCIATE: %v", err)
	}
	defer ctrl.Close()
	otvet, err := udpCherezReley(t, relay, "198.51.100.53", 53, dnsZapros(imya), 1500*time.Millisecond)
	if err != nil || otvet == nil {
		return ""
	}
	return pervyyA(otvet)
}

func dnsZapros(imya string) []byte {
	q := []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, chast := range strings.Split(strings.TrimSuffix(imya, "."), ".") {
		q = append(q, byte(len(chast)))
		q = append(q, chast...)
	}
	return append(q, 0, 0, 1, 0, 1) // A, IN
}

// pervyyA — первый A из ответа (имена в ответе — указателями, как у всех серверов).
func pervyyA(b []byte) string {
	if len(b) < 12 {
		return ""
	}
	an := int(binary.BigEndian.Uint16(b[6:8]))
	i := 12
	propustitImya := func() bool {
		for i < len(b) {
			l := int(b[i])
			switch {
			case l == 0:
				i++
				return true
			case l&0xc0 == 0xc0:
				i += 2
				return true
			default:
				i += 1 + l
			}
		}
		return false
	}
	if !propustitImya() {
		return ""
	}
	i += 4
	for ; an > 0; an-- {
		if !propustitImya() || i+10 > len(b) {
			return ""
		}
		tip := binary.BigEndian.Uint16(b[i:])
		dlina := int(binary.BigEndian.Uint16(b[i+8:]))
		i += 10
		if i+dlina > len(b) {
			return ""
		}
		if tip == 1 && dlina == 4 {
			return net.IP(b[i : i+4]).String()
		}
		i += dlina
	}
	return ""
}
