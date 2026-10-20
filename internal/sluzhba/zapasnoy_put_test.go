package sluzhba

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
	"github.com/HRYNdev/kelevra-desktop/internal/zapasnoy"
)

// Запасной путь службы через туннель (zapasnoy_put.go). Все адреса — 127.0.0.1.

func TestKonfigSluzhbyNesyotVhodSluzhby(t *testing.T) {
	s := gotovStendLestnicy(t)
	if err := s.PerestroitKonfig(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.Yadro.PutKonfiga())
	if err != nil {
		t.Fatal(err)
	}
	adres := zapasnoy.AdresVhoda(b)
	if adres == "" {
		t.Fatal("в конфиге ядра нет входа службы")
	}
	_, portS, _ := net.SplitHostPort(adres)
	port, _ := strconv.Atoi(portS)
	if port < portyVhodaSluzhby[0] || port >= portyVhodaSluzhby[1] {
		t.Fatalf("порт входа %d вне своего диапазона %v", port, portyVhodaSluzhby)
	}
	if !strings.Contains(string(b), hranenie.SekretClash()) {
		t.Fatal("вход службы не закрыт паролем устройства")
	}
}

func TestBezYadraNetZapasnogoPuti(t *testing.T) {
	if (&Sluzhba{}).adresVhodaSluzhby() != nil {
		t.Fatal("без ядра туннеля нет")
	}
	if (&Sluzhba{Yadro: &yadro.Yadro{}}).adresVhodaSluzhby() != nil {
		t.Fatal("ядро не работает — туннеля нет")
	}
}

func TestPohodyNaruzhuIdutZapasnymPutyom(t *testing.T) {
	s := stend(t)
	put := s.zapasnoyPut()
	if s.Podpiska.HTTP == nil || s.Podpiska.HTTP.Transport != put {
		t.Fatal("подписка ходит мимо запасного пути")
	}
	if o := s.otpravshchikZhurnalov(); o.HTTP == nil || o.HTTP.Transport != put {
		t.Fatal("журналы ходят мимо запасного пути")
	}
	if s.klientSeti(time.Second).Transport != put {
		t.Fatal("klientSeti — не тот транспорт")
	}
}

func TestSvobodnyyPortObhoditZanyatyy(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	zanyat := l.Addr().(*net.TCPAddr).Port
	if p := svobodnyyPort(zanyat, zanyat+1); p != 0 {
		t.Fatalf("занятый порт %d выдан как свободный", p)
	}
	if p := svobodnyyPort(zanyat, zanyat+3); p == zanyat || p == 0 {
		t.Fatalf("ждал соседний свободный, получил %d", p)
	}
}

// vyhodUzla — «узел за туннелем»: socks5 без пароля, любой CONNECT ведёт на cel.
func vyhodUzla(t *testing.T, cel string) (string, *atomic.Int32) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	var skolko atomic.Int32
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				b := make([]byte, 2)
				if _, err := io.ReadFull(r, b); err != nil {
					return
				}
				io.ReadFull(r, make([]byte, b[1]))
				c.Write([]byte{5, 0})
				h := make([]byte, 4)
				if _, err := io.ReadFull(r, h); err != nil {
					return
				}
				switch h[3] {
				case 1:
					io.ReadFull(r, make([]byte, 4))
				case 3:
					n := make([]byte, 1)
					io.ReadFull(r, n)
					io.ReadFull(r, make([]byte, n[0]))
				case 4:
					io.ReadFull(r, make([]byte, 16))
				}
				io.ReadFull(r, make([]byte, 2))
				d, err := net.Dial("tcp", cel)
				if err != nil {
					c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				defer d.Close()
				skolko.Add(1)
				c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				go io.Copy(d, r)
				io.Copy(c, d)
			}(c)
		}
	}()
	return l.Addr().String(), &skolko
}

// TestPodpiskaCherezTunnelZhivymYadrom — вся цепочка на настоящем ядре
// (`sing-box run`, без tun): прямой путь к двери рвётся, служба повторяет
// запрос /info через вход службы своего ядра, ядро ведёт его в «туннель»
// (выход-узел), узел доводит до двери. Нужен KELEVRA_YADRO (путь к sing-box);
// без него щуп пропущен.
func TestPodpiskaCherezTunnelZhivymYadrom(t *testing.T) {
	bin := os.Getenv("KELEVRA_YADRO")
	if bin == "" {
		t.Skip("KELEVRA_YADRO не задана — настоящего ядра нет, щуп пропущен")
	}
	s := stend(t)
	var stroki []string
	var mu sync.Mutex
	s.zapasnoyPut().Zhurnal = func(f string, a ...any) {
		mu.Lock()
		stroki = append(stroki, fmt.Sprintf(f, a...))
		mu.Unlock()
	}

	// Дверь: настоящий ответ /info.
	dver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/info") {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"name":"cherez-tunnel","active":true}`)
	}))
	defer dver.Close()
	// Прямой путь к двери режется: адрес, который принимает и сразу рвёт.
	rv, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rv.Close()
	var pryamo atomic.Int32
	go func() {
		for {
			c, err := rv.Accept()
			if err != nil {
				return
			}
			pryamo.Add(1)
			c.Close()
		}
	}()
	uzel, cherezUzel := vyhodUzla(t, dver.Listener.Addr().String())
	uzelHost, uzelPort, _ := net.SplitHostPort(uzel)
	var uzelPortN int
	fmt.Sscan(uzelPort, &uzelPortN)

	clashPort := svobodnyyPort(20180, 20220)
	sekret := hranenie.SekretClash()
	profil := map[string]any{
		"log":      map[string]any{"level": "warn"},
		"inbounds": []any{map[string]any{"type": "tun", "tag": "tun-in", "interface_name": "tun125", "address": []any{"172.19.0.1/30"}}},
		"outbounds": []any{
			map[string]any{"type": "direct", "tag": "direct"},
			map[string]any{"type": "selector", "tag": "Соединение", "outbounds": []any{"uzel"}, "default": "uzel"},
			map[string]any{"type": "socks", "tag": "uzel", "server": uzelHost, "server_port": uzelPortN},
		},
		"route": map[string]any{
			"final":    "direct",
			"rules":    []any{map[string]any{"rule_set": []any{"zablok"}, "outbound": "Соединение"}},
			"rule_set": []any{map[string]any{"type": "inline", "tag": "zablok", "rules": []any{map[string]any{"domain": []any{"zablokirovano.test"}}}}},
		},
		"experimental": map[string]any{"clash_api": map[string]any{"external_controller": fmt.Sprintf("127.0.0.1:%d", clashPort)}},
	}
	syroy, _ := json.Marshal(profil)
	vhod := vhodSluzhbyDlyaSborki()
	if vhod == nil {
		t.Fatal("вход службы не собрать (нет пароля или порта)")
	}
	gotovyy, k, err := konfig.Prigotovit(syroy, konfig.Vybor{ClashSekret: sekret, VhodSluzhby: vhod})
	if err != nil {
		t.Fatal(err)
	}
	// tun проверке не поднять (прав нет и сеть машины трогать нельзя) — убираем его.
	var d map[string]any
	json.Unmarshal(gotovyy, &d)
	var vhody []any
	for _, v := range d["inbounds"].([]any) {
		if v.(map[string]any)["type"] != "tun" {
			vhody = append(vhody, v)
		}
	}
	d["inbounds"] = vhody
	gotovyy, _ = json.Marshal(d)

	papka := filepath.Join(t.TempDir(), "yadro")
	s.Yadro = &yadro.Yadro{Bin: bin, Papka: papka, Api: k.ClashAdres, Sekret: k.ClashSekret}
	if err := s.Yadro.ZapisatKonfig(gotovyy); err != nil {
		t.Fatal(err)
	}
	ctx, otmena := context.WithTimeout(context.Background(), 30*time.Second)
	defer otmena()
	if err := s.Yadro.Zapustit(ctx); err != nil {
		t.Fatalf("ядро не поднялось: %v", err)
	}
	defer s.Yadro.Ostanovit()
	if s.Yadro.VhodSluzhby() == "" {
		t.Fatal("работающее ядро не назвало вход службы")
	}

	sv, err := s.Podpiska.SvedeniyaPoAdresu(ctx, "http://"+rv.Addr().String()+"/u/KLYUCHUSTROYSTVA")
	if err != nil {
		t.Fatalf("сведения через туннель не пришли: %v", err)
	}
	if sv.Imya != "cherez-tunnel" {
		t.Fatalf("сведения: %+v", sv)
	}
	if pryamo.Load() == 0 {
		t.Fatal("прямой путь не пробовали первым")
	}
	if cherezUzel.Load() == 0 {
		t.Fatal("запрос дошёл, но не через узел туннеля")
	}
	mu.Lock()
	zhurnal := strings.Join(stroki, "\n")
	mu.Unlock()
	if !strings.Contains(zhurnal, "через туннель вышло") {
		t.Fatalf("в журнале нет строки про туннель: %q", zhurnal)
	}
	if strings.Contains(zhurnal, "KLYUCHUSTROYSTVA") {
		t.Fatalf("ключ устройства попал в журнал: %q", zhurnal)
	}

	// Ядро остановлено — туннеля нет, прямой отказ остаётся отказом, как раньше.
	s.Yadro.Ostanovit()
	if s.adresVhodaSluzhby() != nil {
		t.Fatal("ядро остановлено, а вход службы всё ещё назван")
	}
	bylo := cherezUzel.Load()
	if _, err := s.Podpiska.SvedeniyaPoAdresu(context.Background(), "http://"+rv.Addr().String()+"/u/KLYUCHUSTROYSTVA"); err == nil {
		t.Fatal("без туннеля прямой отказ должен остаться отказом")
	}
	if cherezUzel.Load() != bylo {
		t.Fatal("без туннеля запрос ушёл через узел")
	}
}
