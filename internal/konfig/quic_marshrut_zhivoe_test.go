package konfig

// Щуп поверх nastoyashchee_yadro_test.go: тот доказывает только, что
// НАСТОЯЩЕЕ ядро согласно ПРИНЯТЬ подготовленный конфиг (`sing-box check`).
// Этот щуп идёт на шаг дальше — поднимает то же настоящее ядро КОМАНДОЙ
// `sing-box run` и пропускает через него живые пакеты SOCKS5 (CONNECT и UDP
// ASSOCIATE), чтобы увидеть, что решает МАРШРУТИЗАТОР ядра, а не то, что
// думает о своей структуре наш собственный код.
//
// Зачем именно так, а не через tun: на этой машине нет /dev/net/tun — tun
// inbound поднять нельзя. mixed-inbound (socks5) даёт то же самое решение
// маршрутизатора (network/port у route.rules) и поддерживает UDP через
// UDP ASSOCIATE — этого достаточно, чтобы пропустить настоящий UDP-пакет с
// портом назначения 443 и настоящий TCP-пакет с портом назначения 443 через
// один и тот же route.rules, который правит dobavitPravilomRezhimQuic.
//
// Контроль порчей встроен в сам тест (не отдельный ручной прогон): второй
// подпрогон запускает ТО ЖЕ ядро на конфиге, из которого наше правило
// вырезано (praviloUdp443 отфильтровывает его же формой), и требует, чтобы
// UDP:443 в этом случае ПРОШЁЛ — если он не проходит, значит блокировку
// создаёт что-то ещё (например default route.final=block), и щуп сам не
// зелёный, а FailNow с честной причиной.

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

// svobodnyPort — берёт свободный TCP-порт на 127.0.0.1 через bind/close;
// гонка с чужим процессом теоретически возможна, но это тот же приём, что
// используют соседние Go-тесты сети в этом репозитории.
func writeFile(t *testing.T, put string, dannye []byte) error {
	t.Helper()
	return os.WriteFile(put, dannye, 0o600)
}

func svobodnyPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("не нашёл свободный порт: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// echoTCP443 — сервер-цель на 127.0.0.1:443: если пакет докатился физически,
// он вернёт его же, и это отличит "докатилось" от "не докатилось" надёжнее,
// чем просто отсутствие ошибки на sing-box.
func echoTCP443(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:443")
	if err != nil {
		t.Skipf("не смог занять 127.0.0.1:443 (нужен root и свободный порт): %v", err)
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

func echoUDP(t *testing.T, port int) net.PacketConn {
	t.Helper()
	pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Skipf("не смог занять udp 127.0.0.1:%d: %v", port, err)
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

// profilDlyaZhivogo — минимальный самодостаточный профиль: mixed-inbound
// (socks5, поддерживает UDP ASSOCIATE) + direct-outbound, БЕЗ remote
// rule_set (сеть наружу для щупа не нужна и не должна быть нужна — цель это
// решение маршрутизатора, а не успех похода в интернет).
func profilDlyaZhivogo(mixedPort int) []byte {
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
		},
		"route": map[string]any{
			"final": "direct",
			"rules": []any{},
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

// socks5Connect — команда CONNECT, возвращает соединение к 127.0.0.1:port,
// уже проведённое через route.rules ядра, и код ответа сервера.
func socks5Connect(proxyAddr string, port int) (net.Conn, byte, error) {
	c, err := net.DialTimeout("tcp", proxyAddr, 3*time.Second)
	if err != nil {
		return nil, 0, err
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if err := socks5Greet(c); err != nil {
		c.Close()
		return nil, 0, err
	}
	req := []byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, 0, 0}
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

// udpCherezReley — шлёт один датаграм на 127.0.0.1:port через relay и ждёт
// ответное эхо. nil, значит ответа не дождались (сеть решила отвергнуть
// пакет раньше, чем он дошёл до эхо-сервера, либо эхо-сервер и правда не
// увидел пакет).
func udpCherezReley(relay *net.UDPAddr, port int, payload []byte, zhdat time.Duration) ([]byte, error) {
	u, err := net.DialUDP("udp", nil, relay)
	if err != nil {
		return nil, err
	}
	defer u.Close()
	hdr := []byte{0, 0, 0, 0x01, 127, 0, 0, 1, 0, 0}
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

// TestMarshrutizatorZhivymYadromRezhetUdp443NeTronuvTcp443 — главный щуп
// задания: настоящее ядро sing-box (`sing-box run`, не `check`) на
// конфиге, прошедшем через Prigotovit, обязано (1) отвергнуть UDP:443,
// (2) пропустить TCP:443, (3) пропустить UDP на другой порт. Контроль
// порчей — тот же конфиг без нашего правила обязан ПРОПУСТИТЬ UDP:443
// (иначе блокирует что-то другое, и щуп сам красный по честной причине).
func TestMarshrutizatorZhivymYadromRezhetUdp443NeTronuvTcp443(t *testing.T) {
	bin := putYadra(t)
	if bin == "" {
		t.Skipf("настоящего ядра рядом нет — щуп пропущен. Искал: %s", mestaPoiskaYadra())
	}

	tcp443 := echoTCP443(t)
	defer tcp443.Close()
	udp443 := echoUDP(t, 443)
	defer udp443.Close()
	inoyPort := 15353
	udpIno := echoUDP(t, inoyPort)
	defer udpIno.Close()

	mixedPort := svobodnyPort(t)
	syroy := profilDlyaZhivogo(mixedPort)
	// BezSistemnogoProksi: true — иначе Prigotovit включит set_system_proxy
	// на mixed-inbound (см. Prigotovit, ветка Proksi), а на этом стенде нет
	// графического окружения — ядро падает на "unsupported desktop
	// environment" ещё до того, как успевает открыть порт.
	gotovyy, _, err := Prigotovit(syroy, Vybor{Prava: false, BezSistemnogoProksi: true})
	if err != nil {
		t.Fatalf("Prigotovit: %v", err)
	}

	// Живой конфиг обязан содержать ровно наше правило — иначе щуп ниже
	// проверял бы что-то другое.
	if n := schyotUdp443Pravil(pravilaRoute(t, gotovyy)); n != 1 {
		t.Fatalf("в живом конфиге правил network:udp+port:443: %d, хочу 1 — щуп бы проверял не то", n)
	}

	dom := t.TempDir()
	putKonfig := dom + "/config.json"
	if err := writeFile(t, putKonfig, gotovyy); err != nil {
		t.Fatal(err)
	}

	proxyAddr := "127.0.0.1:" + strconv.Itoa(mixedPort)
	cmd, log := zapustitYadro(t, bin, putKonfig, mixedPort)
	defer ostanovitYadro(cmd)

	// --- вердикт 1: TCP:443 обязан пройти ---
	tc, kod, err := socks5Connect(proxyAddr, 443)
	if err != nil {
		t.Fatalf("socks5 CONNECT :443: %v\nлог ядра:\n%s", err, log.String())
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
	t.Logf("вердикт 1 [TCP:443]: сокс-код=%d, эхо получено дословно %q — ПРОШЁЛ", kod, string(buf[:n]))

	// --- вердикт 2: UDP:443 обязан быть отвергнут ---
	ctrl443, relay443, err := socks5UdpAssociate(proxyAddr)
	if err != nil {
		t.Fatalf("socks5 UDP ASSOCIATE (для :443): %v\nлог ядра:\n%s", err, log.String())
	}
	otvet443, err := udpCherezReley(relay443, 443, []byte("proba-udp-443"), 1500*time.Millisecond)
	ctrl443.Close()
	if err != nil {
		t.Fatalf("udp:443 через релей: %v", err)
	}
	if otvet443 != nil {
		t.Fatalf("вердикт 2 [UDP:443] ПРОВАЛЕН: ядро пропустило пакет (эхо %q), а правило обязано было его отвергнуть\nлог ядра:\n%s", string(otvet443), log.String())
	}
	t.Logf("вердикт 2 [UDP:443]: ответа от эхо-сервера НЕТ (таймаут 1.5с) — ОТВЕРГНУТ")

	// --- вердикт 3: UDP на другой порт обязан пройти ---
	ctrlIno, relayIno, err := socks5UdpAssociate(proxyAddr)
	if err != nil {
		t.Fatalf("socks5 UDP ASSOCIATE (для :%d): %v\nлог ядра:\n%s", inoyPort, err, log.String())
	}
	otvetIno, err := udpCherezReley(relayIno, inoyPort, []byte("proba-udp-inoy"), 1500*time.Millisecond)
	ctrlIno.Close()
	if err != nil {
		t.Fatalf("udp:%d через релей: %v", inoyPort, err)
	}
	if otvetIno == nil || string(otvetIno) != "proba-udp-inoy" {
		t.Fatalf("вердикт 3 [UDP:%d] ПРОВАЛЕН: ядро НЕ пропустило пакет, хотя порт не 443 (ответ=%v)\nлог ядра:\n%s", inoyPort, otvetIno, log.String())
	}
	t.Logf("вердикт 3 [UDP:%d]: эхо получено дословно %q — ПРОШЁЛ", inoyPort, string(otvetIno))

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

		putPorchi := t.TempDir() + "/config.json"
		if err := writeFile(t, putPorchi, isporchennyy); err != nil {
			t.Fatal(err)
		}
		cmdP, logP := zapustitYadro(t, bin, putPorchi, portPorchi)
		defer ostanovitYadro(cmdP)

		proxyP := "127.0.0.1:" + strconv.Itoa(portPorchi)
		ctrlP, relayP, err := socks5UdpAssociate(proxyP)
		if err != nil {
			t.Fatalf("socks5 UDP ASSOCIATE (порча): %v\nлог ядра:\n%s", err, logP.String())
		}
		otvetP, err := udpCherezReley(relayP, 443, []byte("proba-porcha-443"), 1500*time.Millisecond)
		ctrlP.Close()
		if err != nil {
			t.Fatalf("udp:443 через релей (порча): %v", err)
		}
		if otvetP == nil {
			t.Fatalf("КОНТРОЛЬ ПОРЧЕЙ НЕ СРАБОТАЛ: без правила udp:443 всё равно отвергнут — щуп проверяет не наше правило, а что-то ещё\nлог ядра:\n%s", logP.String())
		}
		t.Logf("контроль порчей: без правила udp:443 ПРОШЁЛ (эхо %q) — щуп краснеет ровно там, где должен", string(otvetP))
	})
}
