package fizset

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
)

// Конфиг как у ноутбука 07.11: узлы по именам, адреса имён зашиты в профиль
// (svoi-imena), комната — на 127.0.0.1, Hysteria2 — UDP.
const konfigNouta = `{
 "dns": {"servers": [
  {"type": "local", "tag": "local"},
  {"type": "hosts", "tag": "svoi-imena", "predefined": {
    "node2kv.chickenkiller.com": ["203.0.113.10", "2001:db8::10"],
    "nodekv.chickenkiller.com": "198.51.100.20"
  }}
 ]},
 "outbounds": [
  {"type": "selector", "tag": "Соединение", "outbounds": ["Нидерланды", "Финляндия", "Комната"]},
  {"type": "direct", "tag": "direct"},
  {"type": "vless", "tag": "Финляндия · XHTTP", "server": "node2kv.chickenkiller.com", "server_port": 2053},
  {"type": "vless", "tag": "Финляндия · Vision", "server": "node2kv.chickenkiller.com", "server_port": 443},
  {"type": "hysteria2", "tag": "Финляндия · Hysteria2", "server": "node2kv.chickenkiller.com", "server_port": 8443},
  {"type": "vless", "tag": "Нидерланды · XHTTP", "server": "nodekv.chickenkiller.com", "server_port": 2083},
  {"type": "vless", "tag": "Без адреса", "server": "neizvestnyy.example", "server_port": 443},
  {"type": "trojan", "tag": "По адресу", "server": "192.0.2.7"},
  {"type": "socks", "tag": "Комната", "server": "127.0.0.1", "server_port": 8808}
 ]
}`

func TestCeliIzKonfiga(t *testing.T) {
	celi := CeliIzKonfiga([]byte(konfigNouta))
	zhdali := []string{"203.0.113.10:2053", "203.0.113.10:443", "198.51.100.20:2083", "192.0.2.7:443"}
	if !reflect.DeepEqual(celi, zhdali) {
		t.Fatalf("цели %v, ждали %v (UDP, комната, IPv6 и имя без адреса не берутся; порт по умолчанию 443)", celi, zhdali)
	}
	if CeliIzKonfiga([]byte("не json")) != nil {
		t.Fatal("битый конфиг — целей нет")
	}
}

// nabor — подменный набор: адрес → ответ.
func nabor(zhivye ...string) func(context.Context, avtorezhim.FizAdapter, string) (net.Conn, error) {
	return func(ctx context.Context, _ avtorezhim.FizAdapter, adres string) (net.Conn, error) {
		for _, z := range zhivye {
			if z == adres {
				a, b := net.Pipe()
				b.Close()
				return a, nil
			}
		}
		return nil, errors.New("connectex: A connection attempt failed")
	}
}

func odinAdapter() []avtorezhim.FizAdapter {
	return []avtorezhim.FizAdapter{{Indeks: 13, IP: "192.168.1.50", Shlyuz: "192.168.1.1"}}
}

func TestProverit(t *testing.T) {
	uzly := []string{"203.0.113.10:2053", "198.51.100.20:2083"}
	obshchie := []string{"1.1.1.1:443"}
	sluchai := []struct {
		imya     string
		uzly     []string
		zhivye   []string
		adaptery func() []avtorezhim.FizAdapter
		itog     Itog
		est      string
	}{
		{"узел отвечает", uzly, []string{"198.51.100.20:2083"}, odinAdapter, SetEst, "узел 198.51.100.20:2083 через адаптер 13"},
		{"узлы молчат, общий отвечает", uzly, []string{"1.1.1.1:443"}, odinAdapter, UzlyNedostupny, "общий адрес 1.1.1.1:443"},
		{"не отвечает ничто", uzly, nil, odinAdapter, NetSeti, "узлы: "},
		{"нет адаптера", uzly, uzly, func() []avtorezhim.FizAdapter { return nil }, NetSeti, "нет физического адаптера"},
		{"адресов узлов нет, общий отвечает", nil, []string{"1.1.1.1:443"}, odinAdapter, SetEst, "адресов узлов в конфиге нет"},
	}
	for _, c := range sluchai {
		t.Run(c.imya, func(t *testing.T) {
			p := Proba{Uzly: c.uzly, Obshchie: obshchie, Adaptery: c.adaptery, Srok: time.Second, Nabrat: nabor(c.zhivye...)}
			r := p.Proverit(context.Background())
			if r.Itog != c.itog || !strings.Contains(r.Opisanie, c.est) {
				t.Fatalf("итог %v (%q), ждали %v с %q", r.Itog, r.Opisanie, c.itog, c.est)
			}
		})
	}
}

// Молчащий узел (рукопожатие не встаёт) — не дольше срока круга.
func TestProveritSrok(t *testing.T) {
	molchit := func(ctx context.Context, _ avtorezhim.FizAdapter, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	p := Proba{Uzly: []string{"203.0.113.10:443"}, Obshchie: []string{"1.1.1.1:443"}, Adaptery: odinAdapter, Srok: 50 * time.Millisecond, Nabrat: molchit}
	nachalo := time.Now()
	r := p.Proverit(context.Background())
	if r.Itog != NetSeti || !strings.Contains(r.Opisanie, "нет ответа") {
		t.Fatalf("итог %v (%q)", r.Itog, r.Opisanie)
	}
	if proshlo := time.Since(nachalo); proshlo > 2*time.Second {
		t.Fatalf("проба шла %s — срок не держится", proshlo)
	}
}

// Настоящий набор: сокет со своим адресом (и на Windows — привязкой
// IP_UNICAST_IF к петлевому интерфейсу) до слушателя на 127.0.0.1.
func TestNastoyashchiyNabor(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	a := avtorezhim.FizAdapter{IP: "127.0.0.1"}
	if ifs, err := net.Interfaces(); err == nil {
		for _, i := range ifs {
			if i.Flags&net.FlagLoopback != 0 && i.Flags&net.FlagUp != 0 {
				a.Indeks = uint32(i.Index)
				break
			}
		}
	}
	ctx, otmena := context.WithTimeout(context.Background(), 3*time.Second)
	defer otmena()
	c, err := NabratCherezAdapter(ctx, a, l.Addr().String())
	if err != nil {
		t.Fatalf("набор через адаптер %+v: %v", a, err)
	}
	c.Close()
	r := Proba{Uzly: []string{l.Addr().String()}, Adaptery: func() []avtorezhim.FizAdapter { return []avtorezhim.FizAdapter{a} }}.Proverit(ctx)
	if r.Itog != SetEst {
		t.Fatalf("итог %v (%q)", r.Itog, r.Opisanie)
	}
}
