package konfig

// Щуп поверх nastoyashchee_yadro_test.go: тот доказывает только, что
// НАСТОЯЩЕЕ ядро согласно ПРИНЯТЬ подготовленный конфиг (`sing-box check`).
// Этот щуп идёт на шаг дальше — поднимает то же настоящее ядро КОМАНДОЙ
// `sing-box run` и пропускает через него живые пакеты SOCKS5 (CONNECT и UDP
// ASSOCIATE), чтобы увидеть, что решает МАРШРУТИЗАТОР ядра, а не то, что
// думает о своей структуре наш собственный код.
//
// Зачем именно так, а не через tun: на этой машине нет прав на tun inbound —
// поднять его нельзя. mixed-inbound (socks5) даёт то же самое решение
// маршрутизатора (network/port/rule_set у route.rules) и поддерживает UDP
// через UDP ASSOCIATE — этого достаточно, чтобы пропустить настоящий
// UDP-пакет с портом назначения 443 и настоящий TCP-пакет с портом
// назначения 443 через один и тот же route.rules, который правит
// dobavitPravilomRezhimQuic.
//
// ЧТО ИМЕННО ДОКАЗЫВАЕТСЯ (после правки 31.08, сузившей правило). Раньше щуп
// проверял «весь udp/443 отбит». Теперь правило точечное — привязано к
// rule_set заблокированного, — и щуп обязан проверять именно точность, иначе
// он зеленел бы и на грубом правиле, которое рубит QUIC всей машине:
//
//	1. TCP:443 к «заблокированному» адресу — ПРОХОДИТ (правило не про TCP);
//	2. UDP:443 к «заблокированному» адресу — ОТБИТ (иначе он ушёл бы в
//	   туннель, где UDP не работает: vless с flow xtls-rprx-vision отвергает
//	   UDP на стороне сервера, «flow does not support UDP»);
//	3. UDP:443 к адресу ВНЕ списков — ПРОХОДИТ. Это главный вердикт правки:
//	   игры, звонки и всё, что умеет только QUIC, к прямым адресам живо;
//	4. UDP на другой порт к «заблокированному» — ПРОХОДИТ (правило только
//	   про 443).
//
// Контроль порчей встроен в сам тест (не отдельный ручной прогон): второй
// подпрогон запускает ТО ЖЕ ядро на конфиге, из которого наше правило
// вырезано (praviloUdp443 отфильтровывает его же формой), и требует, чтобы
// UDP:443 в этом случае ПРОШЁЛ — если он не проходит, значит блокировку
// создаёт что-то ещё (например default route.final=block), и щуп сам не
// зелёный, а FailNow с честной причиной.
//
// «Заблокированный» адрес — 127.0.0.2, «прямой» — 127.0.0.1: оба петлевые
// (наружу щуп не ходит вообще), но список правил ссылается только на первый.

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// Адреса щупа: первый лежит в списке «заблокированного» (и потому уводится в
// туннель), второй — нет.
const (
	adresZablokirovannogo = "127.0.0.2"
	adresPryamoy          = "127.0.0.1"
)

func writeFile(t *testing.T, put string, dannye []byte) error {
	t.Helper()
	return os.WriteFile(put, dannye, 0o600)
}

// svobodnyPort — берёт свободный TCP-порт на 127.0.0.1 через bind/close;
// гонка с чужим процессом теоретически возможна, но это тот же приём, что
// используют соседние Go-тесты сети в этом репозитории.
func svobodnyPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("не нашёл свободный порт: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// echoTCP — сервер-цель: если пакет докатился физически, он вернёт его же, и
// это отличит «докатилось» от «не докатилось» надёжнее, чем просто
// отсутствие ошибки на sing-box.
func echoTCP(t *testing.T, ip string, port int) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		t.Skipf("не смог занять tcp %s:%d: %v", ip, port, err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				n, err := c.Read(buf)
				if err == nil {
					c.Write(buf[:n])
				}
			}(c)
		}
	}()
	return l
}

func echoUDP(t *testing.T, ip string, port int) net.PacketConn {
	t.Helper()
	pc, err := net.ListenPacket("udp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		t.Skipf("не смог занять udp %s:%d: %v", ip, port, err)
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			pc.WriteTo(buf[:n], addr)
		}
	}()
	return pc
}

// spisokZablokirovannogo — файл rule_set в формате source: единственная
// «заблокированная» подсеть щупа. Это тот же механизм, которым в боевом
// профиле заданы 22 списка, только локальный и крошечный — в сеть щуп не
// ходит.
func spisokZablokirovannogo(t *testing.T, dom string) string {
	t.Helper()
	put := filepath.Join(dom, "zablokirovannoe.json")
	telo := []byte(`{"version":2,"rules":[{"ip_cidr":["` + adresZablokirovannogo + `/32"]}]}`)
	if err := writeFile(t, put, telo); err != nil {
		t.Fatal(err)
	}
	return put
}

// profilDlyaZhivogo — минимальный самодостаточный профиль: mixed-inbound
// (socks5, поддерживает UDP ASSOCIATE), выход direct, выход-«туннель»
// (селектор поверх direct — тип не direct, значит для нашего кода это
// туннель, но пакеты он реально доносит, иначе вердикты было бы не отличить)
// и ОДИН локальный rule_set вместо 22 remote (сеть наружу щупу не нужна и не
// должна быть нужна — цель это решение маршрутизатора, а не успех похода в
// интернет).
func profilDlyaZhivogo(mixedPort int, putSpiska string) []byte {
	d := map[string]any{
		"log": map[string]any{"level": "info", "timestamp": true},
		"inbounds": []any{
			map[string]any{
				"type": "mixed", "tag": "mixed-in",
				"listen": "127.0.0.1", "listen_port": mixedPort,
				"set_system_proxy": false,
			},
		},
		"outbounds": []any{
			map[string]any{"type": "direct", "tag": "direct"},
			map[string]any{
				"type": "selector", "tag": "Соединение",
				"outbounds": []any{"direct"}, "default": "direct",
			},
		},
		"route": map[string]any{
			"final": "direct",
			"rules": []any{
				map[string]any{"rule_set": []any{"zablokirovannoe"}, "outbound": "Соединение"},
			},
			"rule_set": []any{
				map[string]any{
					"type": "local", "tag": "zablokirovannoe",
					"format": "source", "path": putSpiska,
				},
			},
		},
	}
	b, _ := json.Marshal(d)
	return b
}

// bezPravilaUdp443 — контроль порчей: тот же готовый конфиг, но с нашим
// правилом вырезанным (используем ЕГО ЖЕ форму praviloUdp443, а не свою
// копию условия) — так порча бьёт ровно по тому, что добавляет коммит.
func bezPravilaUdp443(t *testing.T, gotovyy []byte) []byte {
	t.Helper()
	d := razobrat(t, gotovyy)
	r := d["route"].(map[string]any)
	spisok := r["rules"].([]any)
	ostalos := []any{} // не nil: nil-слайс уходит в JSON как null, и
	// pravilaRoute() потом не смог бы прочитать route.rules обратно
	for _, p := range spisok {
		pr, ok := p.(map[string]any)
		if ok && praviloUdp443(pr) {
			continue // вырезаем — только его, остальные правила не трогаем
		}
		ostalos = append(ostalos, p)
	}
	r["rules"] = ostalos
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("не пересобрал испорченный конфиг: %v", err)
	}
	return b
}

// zapustitYadro поднимает `sing-box run -c путь` и ждёт, пока mixed-порт
// начнёт принимать TCP (это и есть готовность инбаунда — sing-box сам
// логирует старт, но polling порта надёжнее парсинга цветного лога).
func zapustitYadro(t *testing.T, bin, konfigPut string, mixedPort int) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	cmd := exec.Command(bin, "run", "-c", konfigPut)
	// Рабочая папка — та же, где лежит конфиг (внутри t.TempDir), а не
	// internal/konfig: ядро само пишет рядом cache.db, и без этого файл
	// оседал мусором в репозитории (найдено 30.08, см. cleanup ниже).
	cmd.Dir = filepath.Dir(konfigPut)
	var log bytes.Buffer
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Start(); err != nil {
		t.Fatalf("ядро не запустилось: %v", err)
	}
	adres := fmt.Sprintf("127.0.0.1:%d", mixedPort)
	ok := false
	for i := 0; i < 100; i++ {
		if c, err := net.DialTimeout("tcp", adres, 100*time.Millisecond); err == nil {
			c.Close()
			ok = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ok {
		cmd.Process.Kill()
		t.Fatalf("mixed-inbound на %s не поднялся за 5с, лог ядра:\n%s", adres, log.String())
	}
	return cmd, &log
}

func ostanovitYadro(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cmd.Process.Kill()
		<-done
	}
}

// --- минимальный SOCKS5-клиент (RFC1928): в go.mod нет golang.org/x/net,
// а нужен ровно CONNECT и UDP ASSOCIATE — тащить зависимость ради 40 строк
// протокола не стали.

func socks5Greet(c net.Conn) error {
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	buf := make([]byte, 2)
	if _, err := readPolnostyu(c, buf); err != nil {
		return err
	}
	if buf[0] != 0x05 || buf[1] != 0x00 {
		return fmt.Errorf("socks5 greet: неожиданный ответ % x", buf)
	}
	return nil
}

func readPolnostyu(c net.Conn, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := c.Read(buf[n:])
		if err != nil {
			return n, err
		}
		n += m
	}
	return n, nil
}

// adresIPv4 — четыре байта адреса щупа для заголовка SOCKS5.
func adresIPv4(t *testing.T, ip string) []byte {
	t.Helper()
	a := net.ParseIP(ip).To4()
	if a == nil {
		t.Fatalf("не IPv4-адрес: %q", ip)
	}
	return a
}

// socks5Connect — команда CONNECT, возвращает соединение к ip:port, уже
// проведённое через route.rules ядра, и код ответа сервера.
func socks5Connect(t *testing.T, proxyAddr, ip string, port int) (net.Conn, byte, error) {
	t.Helper()
	c, err := net.DialTimeout("tcp", proxyAddr, 3*time.Second)
	if err != nil {
		return nil, 0, err
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if err := socks5Greet(c); err != nil {
		c.Close()
		return nil, 0, err
	}
	req := append([]byte{0x05, 0x01, 0x00, 0x01}, adresIPv4(t, ip)...)
	req = append(req, 0, 0)
	binary.BigEndian.PutUint16(req[8:], uint16(port))
	if _, err := c.Write(req); err != nil {
		c.Close()
		return nil, 0, err
	}
	rep := make([]byte, 10)
	if _, err := readPolnostyu(c, rep); err != nil {
		c.Close()
		return nil, 0, err
	}
	c.SetDeadline(time.Time{})
	return c, rep[1], nil
}

// socks5UdpAssociate держит управляющее TCP-соединение открытым (иначе
// ассоциация закрывается) и возвращает адрес UDP-релея ядра.
func socks5UdpAssociate(proxyAddr string) (ctrl net.Conn, relay *net.UDPAddr, err error) {
	c, err := net.DialTimeout("tcp", proxyAddr, 3*time.Second)
	if err != nil {
		return nil, nil, err
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if err := socks5Greet(c); err != nil {
		c.Close()
		return nil, nil, err
	}
	req := []byte{0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	if _, err := c.Write(req); err != nil {
		c.Close()
		return nil, nil, err
	}
	rep := make([]byte, 10)
	if _, err := readPolnostyu(c, rep); err != nil {
		c.Close()
		return nil, nil, err
	}
	if rep[1] != 0x00 {
		c.Close()
		return nil, nil, fmt.Errorf("udp associate отвергнут, код %d", rep[1])
	}
	ip := net.IPv4(rep[4], rep[5], rep[6], rep[7])
	relayPort := binary.BigEndian.Uint16(rep[8:10])
	c.SetDeadline(time.Time{})
	return c, &net.UDPAddr{IP: ip, Port: int(relayPort)}, nil
}

// udpCherezReley — шлёт один датаграм на ip:port через relay и ждёт ответное
// эхо. nil, значит ответа не дождались (сеть решила отвергнуть пакет раньше,
// чем он дошёл до эхо-сервера, либо эхо-сервер и правда не увидел пакет).
func udpCherezReley(t *testing.T, relay *net.UDPAddr, ip string, port int, payload []byte, zhdat time.Duration) ([]byte, error) {
	t.Helper()
	u, err := net.DialUDP("udp", nil, relay)
	if err != nil {
		return nil, err
	}
	defer u.Close()
	hdr := append([]byte{0, 0, 0, 0x01}, adresIPv4(t, ip)...)
	hdr = append(hdr, 0, 0)
	binary.BigEndian.PutUint16(hdr[8:], uint16(port))
	if _, err := u.Write(append(hdr, payload...)); err != nil {
		return nil, err
	}
	u.SetReadDeadline(time.Now().Add(zhdat))
	buf := make([]byte, 4096)
	n, err := u.Read(buf)
	if err != nil {
		return nil, nil // таймаут — трактуем как "ответа нет", это и есть измеряемый факт
	}
	if n < 10 {
		return nil, fmt.Errorf("короткий udp-ответ: %d байт", n)
	}
	return buf[10:n], nil
}

// probaUdp — одна проба UDP через свежую ассоциацию: вернуть эхо или nil.
func probaUdp(t *testing.T, proxyAddr, ip string, port int, payload string) []byte {
	t.Helper()
	ctrl, relay, err := socks5UdpAssociate(proxyAddr)
	if err != nil {
		t.Fatalf("socks5 UDP ASSOCIATE (для %s:%d): %v", ip, port, err)
	}
	defer ctrl.Close()
	otvet, err := udpCherezReley(t, relay, ip, port, []byte(payload), 1500*time.Millisecond)
	if err != nil {
		t.Fatalf("udp %s:%d через релей: %v", ip, port, err)
	}
	return otvet
}

// TestMarshrutizatorZhivymYadromRezhetQuicTochechno — главный щуп задания:
// настоящее ядро sing-box (`sing-box run`, не `check`) на конфиге, прошедшем
// через Prigotovit, обязано отбить UDP:443 ТОЛЬКО к адресу из списка
// заблокированного и не тронуть ни TCP, ни UDP к прямому адресу, ни UDP на
// другой порт. Контроль порчей — тот же конфиг без нашего правила обязан
// ПРОПУСТИТЬ UDP:443 (иначе блокирует что-то другое, и щуп сам красный по
// честной причине).
func TestMarshrutizatorZhivymYadromRezhetQuicTochechno(t *testing.T) {
	bin := putYadra(t)
	if bin == "" {
		t.Skipf("настоящего ядра рядом нет — щуп пропущен. Искал: %s", mestaPoiskaYadra())
	}

	tcpZablok := echoTCP(t, adresZablokirovannogo, 443)
	defer tcpZablok.Close()
	udpZablok := echoUDP(t, adresZablokirovannogo, 443)
	defer udpZablok.Close()
	udpPryamoy := echoUDP(t, adresPryamoy, 443)
	defer udpPryamoy.Close()
	inoyPort := 15353
	udpIno := echoUDP(t, adresZablokirovannogo, inoyPort)
	defer udpIno.Close()

	dom := t.TempDir()
	mixedPort := svobodnyPort(t)
	syroy := profilDlyaZhivogo(mixedPort, spisokZablokirovannogo(t, dom))
	// BezSistemnogoProksi: true — иначе Prigotovit включит set_system_proxy
	// на mixed-inbound (см. Prigotovit, ветка Proksi), а щуп не имеет права
	// трогать настройки сети машины, на которой он идёт.
	gotovyy, _, err := Prigotovit(syroy, Vybor{Prava: false, BezSistemnogoProksi: true})
	if err != nil {
		t.Fatalf("Prigotovit: %v", err)
	}

	// Живой конфиг обязан содержать ровно наше правило, и обязан содержать
	// его СУЖЕННЫМ — иначе щуп ниже проверял бы что-то другое.
	pravila := pravilaRoute(t, gotovyy)
	if n := schyotUdp443Pravil(pravila); n != 1 {
		t.Fatalf("в живом конфиге правил network:udp+port:443: %d, хочу 1 — щуп бы проверял не то", n)
	}
	for _, p := range pravila {
		if !praviloUdp443(p) {
			continue
		}
		if _, est := p["rule_set"]; !est {
			t.Fatalf("правило про udp/443 не сужено списками: %#v — щуп проверял бы грубое правило", p)
		}
	}

	putKonfig := filepath.Join(dom, "config.json")
	if err := writeFile(t, putKonfig, gotovyy); err != nil {
		t.Fatal(err)
	}

	proxyAddr := "127.0.0.1:" + strconv.Itoa(mixedPort)
	cmd, log := zapustitYadro(t, bin, putKonfig, mixedPort)
	defer ostanovitYadro(cmd)

	// --- вердикт 1: TCP:443 к заблокированному обязан пройти ---
	tc, kod, err := socks5Connect(t, proxyAddr, adresZablokirovannogo, 443)
	if err != nil {
		t.Fatalf("socks5 CONNECT %s:443: %v\nлог ядра:\n%s", adresZablokirovannogo, err, log.String())
	}
	if kod != 0x00 {
		t.Fatalf("TCP:443 отвергнут сокс-кодом %d, а обязан пройти\nлог ядра:\n%s", kod, log.String())
	}
	tc.SetDeadline(time.Now().Add(3 * time.Second))
	tc.Write([]byte("proba-tcp-443"))
	buf := make([]byte, 64)
	n, err := tc.Read(buf)
	tc.Close()
	if err != nil || string(buf[:n]) != "proba-tcp-443" {
		t.Fatalf("TCP:443 не докатился до эхо-сервера (n=%d err=%v) — вердикт 1 (пройти) НЕ подтверждён\nлог ядра:\n%s", n, err, log.String())
	}
	t.Logf("вердикт 1 [TCP:443 к заблокированному]: сокс-код=%d, эхо %q — ПРОШЁЛ", kod, string(buf[:n]))

	// --- вердикт 2: UDP:443 к заблокированному обязан быть отвергнут ---
	if otvet := probaUdp(t, proxyAddr, adresZablokirovannogo, 443, "proba-udp-443"); otvet != nil {
		t.Fatalf("вердикт 2 [UDP:443 к заблокированному] ПРОВАЛЕН: ядро пропустило пакет (эхо %q)\nлог ядра:\n%s", string(otvet), log.String())
	}
	t.Logf("вердикт 2 [UDP:443 к заблокированному]: ответа нет (таймаут 1.5с) — ОТВЕРГНУТ")

	// --- вердикт 3 (главный после правки 31.08): UDP:443 к адресу ВНЕ
	// списков обязан пройти. Раньше правило резало весь udp/443 машины, и
	// этот вердикт был бы красным — вместе с играми и звонками человека. ---
	otvetPryamoy := probaUdp(t, proxyAddr, adresPryamoy, 443, "proba-udp-pryamoy")
	if otvetPryamoy == nil || string(otvetPryamoy) != "proba-udp-pryamoy" {
		t.Fatalf("вердикт 3 [UDP:443 мимо списков] ПРОВАЛЕН: ядро отбило QUIC к адресу, которому туннель не нужен (ответ=%v) — это и есть срезанные игры и звонки\nлог ядра:\n%s", otvetPryamoy, log.String())
	}
	t.Logf("вердикт 3 [UDP:443 мимо списков]: эхо %q — ПРОШЁЛ", string(otvetPryamoy))

	// --- вердикт 4: UDP на другой порт обязан пройти ---
	otvetIno := probaUdp(t, proxyAddr, adresZablokirovannogo, inoyPort, "proba-udp-inoy")
	if otvetIno == nil || string(otvetIno) != "proba-udp-inoy" {
		t.Fatalf("вердикт 4 [UDP:%d] ПРОВАЛЕН: ядро НЕ пропустило пакет, хотя порт не 443 (ответ=%v)\nлог ядра:\n%s", inoyPort, otvetIno, log.String())
	}
	t.Logf("вердикт 4 [UDP:%d к заблокированному]: эхо %q — ПРОШЁЛ", inoyPort, string(otvetIno))

	ostanovitYadro(cmd)

	// --- контроль порчей: тот же конфиг БЕЗ правила обязан ПРОПУСТИТЬ udp:443 ---
	t.Run("kontrol_porchey_bez_pravila_udp443_prohodit", func(t *testing.T) {
		isporchennyy := bezPravilaUdp443(t, gotovyy)
		if n := schyotUdp443Pravil(pravilaRoute(t, isporchennyy)); n != 0 {
			t.Fatalf("в испорченном конфиге всё ещё %d правил(о) udp:443 — порча не сработала", n)
		}
		portPorchi := svobodnyPort(t)
		var d map[string]any
		if err := json.Unmarshal(isporchennyy, &d); err != nil {
			t.Fatal(err)
		}
		d["inbounds"].([]any)[0].(map[string]any)["listen_port"] = portPorchi
		isporchennyy, _ = json.Marshal(d)

		putPorchi := filepath.Join(t.TempDir(), "config.json")
		if err := writeFile(t, putPorchi, isporchennyy); err != nil {
			t.Fatal(err)
		}
		cmdP, logP := zapustitYadro(t, bin, putPorchi, portPorchi)
		defer ostanovitYadro(cmdP)

		proxyP := "127.0.0.1:" + strconv.Itoa(portPorchi)
		otvetP := probaUdp(t, proxyP, adresZablokirovannogo, 443, "proba-porcha-443")
		if otvetP == nil {
			t.Fatalf("КОНТРОЛЬ ПОРЧЕЙ НЕ СРАБОТАЛ: без правила udp:443 всё равно отвергнут — щуп проверяет не наше правило, а что-то ещё\nлог ядра:\n%s", logP.String())
		}
		t.Logf("контроль порчей: без правила udp:443 ПРОШЁЛ (эхо %q) — щуп краснеет ровно там, где должен", string(otvetP))
	})
}
