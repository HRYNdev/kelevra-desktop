package avtorezhim

import (
	"bufio"
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// TestPryamoyZondUspekh: соединение встало И пришёл настоящий HTTP-ответ —
// трафик идёт. Слушаем настоящий петлевой сокет (127.0.0.1), но подменяем
// адрес назначения через Dial — в интернет зонд не ходит, это не реальная
// сеть, а локальный мок. Сервер честно читает запрос и отвечает, как это
// делает контрольный хост за настоящим fake-IP в живом замере.
func TestPryamoyZondUspekh(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("поднять локальный слушатель: %v", err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = bufio.NewReader(c).ReadString('\n')
				_, _ = c.Write([]byte("HTTP/1.0 301 Moved Permanently\r\n\r\n"))
			}(c)
		}
	}()

	z := &PryamoyZond{
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, l.Addr().String())
		},
		Host: "www.youtube.com", Port: 80, Taimaut: 2 * time.Second,
	}
	izmereno, proshel := z.Proshel(context.Background())
	if !izmereno || !proshel {
		t.Fatalf("izmereno=%v proshel=%v, ждал true/true", izmereno, proshel)
	}
}

// TestPryamoyZondConnectUspeshenNoSbros — регрессия на живой замер в
// домашней сети: заведомо мёртвый адрес fake-IP-диапазона (198.18.200.200:80)
// принимает TCP-соединение за 2мс (перехватчик роутера отвечает на connect
// сам, ещё до всякого выхода наружу), а при попытке прочитать ответ рвёт
// соединение (ConnectionResetError). До этой правки успехом считался сам
// факт dial() без ошибки — то есть такой мёртвый путь ложно засчитывался
// как «трафик идёт». Здесь connect нарочно успешен (fake conn без единого
// реального сокета), а Read обязан вернуть ошибку сброса.
func TestPryamoyZondConnectUspeshenNoSbros(t *testing.T) {
	z := &PryamoyZond{
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return &sbrosPriChtenii{}, nil // connect "успешен", как в живом замере на мёртвом fake-IP
		},
		Host: "www.youtube.com", Port: 80, Taimaut: 2 * time.Second,
	}
	izmereno, proshel := z.Proshel(context.Background())
	if !izmereno {
		t.Fatal("connect встал, значит измерили — izmereno обязан быть true")
	}
	if proshel {
		t.Fatal("соединение сброшено при чтении, а зонд говорит, что трафик прошёл")
	}
}

// sbrosPriChtenii — фейковый net.Conn: запись (сам HTTP-запрос) проходит
// без ошибки, а чтение ответа всегда рвётся, как настоящий
// ConnectionResetError с fake-IP роутера домашней сети.
type sbrosPriChtenii struct{}

func (c *sbrosPriChtenii) Read(b []byte) (int, error) {
	return 0, errors.New("connection reset by peer")
}
func (c *sbrosPriChtenii) Write(b []byte) (int, error)        { return len(b), nil }
func (c *sbrosPriChtenii) Close() error                       { return nil }
func (c *sbrosPriChtenii) LocalAddr() net.Addr                { return nil }
func (c *sbrosPriChtenii) RemoteAddr() net.Addr               { return nil }
func (c *sbrosPriChtenii) SetDeadline(t time.Time) error      { return nil }
func (c *sbrosPriChtenii) SetReadDeadline(t time.Time) error  { return nil }
func (c *sbrosPriChtenii) SetWriteDeadline(t time.Time) error { return nil }

// TestPryamoyZondOtkaz: соединение честно отказало (порт никто не слушает) —
// это "измерили, трафик не идёт", а не "не узнали".
func TestPryamoyZondOtkaz(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("поднять локальный слушатель, чтобы узнать свободный порт: %v", err)
	}
	adres := l.Addr().String()
	l.Close() // порт свободен, но никто не слушает — соединение будет отказано

	z := &PryamoyZond{
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, adres)
		},
		Host: "www.youtube.com", Port: 80, Taimaut: 2 * time.Second,
	}
	izmereno, proshel := z.Proshel(context.Background())
	if !izmereno {
		t.Fatal("отказ в соединении — это измеренный результат, а не «не узнали»")
	}
	if proshel {
		t.Fatal("порт никто не слушает, а зонд говорит, что трафик прошёл")
	}
}

// TestPryamoyZondDnsOshibkaEtoNeUznali: имя не резолвится — это "не узнали",
// а не "трафик не идёт" (см. AutoMode.kt: measured vs live).
func TestPryamoyZondDnsOshibkaEtoNeUznali(t *testing.T) {
	z := &PryamoyZond{
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, &net.DNSError{Err: "имя не найдено", Name: "www.youtube.com", IsNotFound: true}
		},
		Host: "www.youtube.com", Port: 80, Taimaut: 2 * time.Second,
	}
	izmereno, proshel := z.Proshel(context.Background())
	if izmereno {
		t.Fatal("ошибка резолва DNS обязана давать izmereno=false")
	}
	if proshel {
		t.Fatal("не узнали — не может значить «прошло»")
	}
}
