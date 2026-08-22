package avtorezhim

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"time"
)

// dialFunc — то немногое от net.Dialer.DialContext, что нужно зонду.
// Отдельный тип, чтобы в тестах подменять сеть моком, не открывая ни
// одного настоящего сокета (см. pryamoy_trafik_test.go).
type dialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// PryamoyZond проверяет прямой трафик мимо VPN.
//
// DNS-признак говорит только «нам отвечает домашний резолвер», а не
// «наружу проходит трафик»: в сети с ограничениями (гостевой Wi-Fi с
// белым списком) резолвер может быть тот же домашний, а прямой запрос
// наружу не пройдёт (см. homeCarriesTraffic в AutoMode.kt). Порт 80 и
// youtube.com — тот же выбор, что в эталоне: ответ короткий, TLS не нужен.
type PryamoyZond struct {
	Dial    dialFunc // nil — обычный net.Dialer
	Host    string
	Port    int
	Taimaut time.Duration
}

// NovyyPryamoyZond — зонд с параметрами по умолчанию.
func NovyyPryamoyZond() *PryamoyZond {
	return &PryamoyZond{Host: "www.youtube.com", Port: 80, Taimaut: 4 * time.Second}
}

// Proshel пробует прямое TCP-соединение до контрольного хоста и, встав,
// шлёт по нему минимальный HTTP-запрос: сам факт connect() ничего не
// доказывает — дома контрольный хост резолвится в fake-IP, и TCP-рукопожатие
// с ним терминирует локальный перехватчик роутера ещё до всякого выхода
// наружу. Живой замер: настоящий fake-IP роутера (198.18.3.10:80) даёт
// честный ответ HTTP/1.0 301 за connect=6мс, а заведомо мёртвый адрес того
// же диапазона (198.18.200.200:80) тоже встаёт за connect=2мс и рвётся
// только при чтении. Поэтому успехом считается только непустой прочитанный
// ответ.
//
// izmereno=false значит «проверить не удалось» — например, имя не
// зарезолвилось, а на только что поднятой сети это норма первых секунд, а
// не доказательство мёртвого пути. Это не то же самое, что «трафик не
// идёт», и вызывающая сторона обязана не путать одно с другим — так же,
// как AutoMode.kt отдельно считает measurement.measured и measurement.live.
func (z *PryamoyZond) Proshel(ctx context.Context) (izmereno bool, proshel bool) {
	dial := z.Dial
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	taimaut := z.Taimaut
	if taimaut <= 0 {
		taimaut = 4 * time.Second
	}
	host, port := z.Host, z.Port
	if host == "" {
		host = "www.youtube.com"
	}
	if port == 0 {
		port = 80
	}

	cctx, otmena := context.WithTimeout(ctx, taimaut)
	defer otmena()

	adres := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := dial(cctx, "tcp", adres)
	if err != nil {
		var dnsOshibka *net.DNSError
		if errors.As(err, &dnsOshibka) {
			return false, false // имя не резолвится — не узнали, а не «не идёт»
		}
		return true, false // соединение честно отказало или не встало — трафик не идёт
	}
	defer conn.Close()

	// Общий таймаут — на всю операцию (запись+чтение), а не только на
	// connect: у conn из мока/дозвона может не быть встроенного дедлайна
	// из контекста, а голый connect с fake-IP успешен ВСЕГДА (см. выше).
	if dedlayn, est := cctx.Deadline(); est {
		_ = conn.SetDeadline(dedlayn)
	}

	zapros := "GET / HTTP/1.0\r\nHost: " + host + "\r\n\r\n"
	if _, err := conn.Write([]byte(zapros)); err != nil {
		return true, false // соединение встало, но запрос не ушёл — трафик не идёт
	}

	bufer := make([]byte, 512)
	n, err := conn.Read(bufer)
	if n == 0 || (err != nil && err != io.EOF) {
		return true, false // байты не пришли или соединение сброшено — трафик не идёт
	}
	return true, true
}
