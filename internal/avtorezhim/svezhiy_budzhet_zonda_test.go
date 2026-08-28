package avtorezhim

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"
)

// Заказ хозяина дословно (28.08): «при включении нажатии кнопку подключится, он
// *** не определят дома или нет, а когда выключен определяет ИНОГДа что
// дома и то не всегда». Замер следователя (синтетический стенд, 3/3
// детерминированно): «DNS съел 50мс -> дома; DNS съел 280мс (тот же честный
// ответ, просто дольше) -> вне дома» — прямой зонд отвечал одинаково честно
// в обоих случаях, вердикт перевернула только задержка DNS-подэтапа. Корень:
// Avtorezhim.Zahod раньше звал оба зонда на ОДНОМ каскадном ctx
// (DomaPoDns/Proshel сами оборачивают его в context.WithTimeout) — честно
// медленный DNS съедал часть родительского бюджета, и трафик-зонду
// доставался урезанный остаток вместо своего полного номинала.
//
// Этот тест воспроизводит ровно тот сценарий на подставном DNS-резолвере
// (отвечает честно «дома», но с управляемой задержкой — два случая, быстрый
// и медленный) и на настоящем локальном TCP-сервере вместо трафик-зонда
// (отвечает честно и ОДИНАКОВО долго в обоих случаях, как «тот же честный
// ответ» у следователя). Итог обоих прогонов обязан быть «дома» — сравни с
// откатом правки ниже (TestPryamoyZondNeStradaetOtKaskadaDoPravki показывает
// красный на старом каскадном вызове).

// fakeResolverSZaderzhkoy — подставной резолвер: отвечает честно (тем же
// набором адресов, что и настоящий домашний роутер), но не мгновенно — общий
// DNS-подэтап занимает zaderzhka. DomaPoDns спрашивает НЕСКОЛЬКО доменов
// подряд (3 контрольных + 1 отдельный), поэтому задержка отрабатывает только
// на первом запросе (как холодный резолв первого имени — остальные идут по
// уже прогретому пути) — иначе умножилась бы на число доменов и не отражала
// бы «DNS-подэтап честно занял X мс» из замера следователя. Уважает
// ctx.Done(), как и настоящий net.Resolver.
type fakeResolverSZaderzhkoy struct {
	otvety     map[string][]net.IP
	zaderzhka  time.Duration
	zaderzhano *bool // общий на все вызовы одного резолвера — задержка только один раз
}

func (f fakeResolverSZaderzhkoy) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	if !*f.zaderzhano {
		select {
		case <-time.After(f.zaderzhka):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		*f.zaderzhano = true
	}
	return f.otvety[host], nil
}

// medlennySluzhitelTrafika — настоящий локальный TCP-сервер (петлевой сокет,
// не мок net.Conn): честно принимает соединение, честно читает запрос и
// честно отвечает — но только после zaderzhka, как настоящий контрольный
// хост за небыстрым домашним роутером. Задержка тут ОДНА И ТА ЖЕ что для
// быстрого, что для медленного DNS-случая — прямой зонд ведёт себя одинаково
// честно в обоих, ровно как в замере следователя.
func medlennySluzhitelTrafika(t *testing.T, zaderzhka time.Duration) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("поднять локальный слушатель: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = bufio.NewReader(c).ReadString('\n')
				time.Sleep(zaderzhka)
				_, _ = c.Write([]byte("HTTP/1.0 301 Moved Permanently\r\n\r\n"))
			}(c)
		}
	}()
	return l
}

// zahodSZaderzhkoyDns собирает Avtorezhim с подставным DNS (честный, но с
// задержкой dnsZaderzhka) и с настоящим локальным TCP как трафик-зонд
// (честный, с постоянной задержкой trafikZaderzhka — та же в обоих
// сценариях), и делает один доверенный заход с общим бюджетом obshchiyBudzhet.
func zahodSZaderzhkoyDns(t *testing.T, dnsZaderzhka, trafikZaderzhka, obshchiyBudzhet time.Duration) Sostoyanie {
	t.Helper()

	zaderzhano := false
	resolver := fakeResolverSZaderzhkoy{
		zaderzhka:  dnsZaderzhka,
		zaderzhano: &zaderzhano,
		otvety: map[string][]net.IP{
			"youtube.com":   {fakeAdres(198, 18, 3, 9)},   // подменный — как у настоящего домашнего роутера
			"discord.com":   {fakeAdres(198, 19, 1, 1)},   // подменный
			"rutracker.org": {fakeAdres(93, 158, 134, 3)}, // настоящий
			"gosuslugi.ru":  {fakeAdres(213, 59, 254, 7)}, // контрольный домен, настоящий
		},
	}
	dns := &DnsZond{
		Resolver: resolver,
		Domeny:   DomenyPoUmolchaniyu,
		Nuzhno:   2,
		Taimaut:  300 * time.Millisecond, // номинал DNS-подэтапа в этом тесте (в бою — 3с)
	}

	l := medlennySluzhitelTrafika(t, trafikZaderzhka)
	trafik := &PryamoyZond{
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, l.Addr().String())
		},
		Host:    "www.youtube.com",
		Port:    80,
		Taimaut: 400 * time.Millisecond, // номинал трафик-подэтапа в этом тесте (в бою — 4с)
	}

	a := &Avtorezhim{
		Dns:       dns,
		Trafik:    trafik,
		Zadvizhka: NovayaZadvizhka(Neizvestno),
	}

	ctx, otmena := context.WithTimeout(context.Background(), obshchiyBudzhet)
	defer otmena()
	_, _, tekushcheye := a.Zahod(ctx, true, true)
	return tekushcheye
}

// TestPryamoyZondNeStradaetOtChestnoMedlennogoDns — основной стенд задачи.
// Общий бюджет захода (800мс) вмещает СУММУ номиналов подзондов (300мс DNS +
// 400мс трафик = 700мс) — ровно то условие, которое в бою нарушалось: таймаут
// кнопки был 5с при номиналах 3с+4с=7с, и честно медленный DNS съедал остаток
// прямого зонда. DNS здесь честно отвечает «дома» что за 50мс, что за 280мс,
// прямой зонд в обоих случаях получает одну и ту же честную задержку 260мс.
// Ответ обязан быть «дома» в обоих случаях: разница в скорости ЧЕСТНОГО
// DNS-подэтапа не имеет права переворачивать вердикт.
//
// Стенд краснеет, если кто-то снова урежет бюджет ниже суммы номиналов —
// именно поэтому бюджет тут параметр, а не константа внутри захода. Второй
// сторож, со стороны боевых чисел кнопки, — TestKnopkaVmeshchaetSummuNominalovZondov
// в internal/sluzhba.
func TestPryamoyZondNeStradaetOtChestnoMedlennogoDns(t *testing.T) {
	const (
		obshchiyBudzhet = 800 * time.Millisecond
		trafikZaderzhka = 260 * time.Millisecond
	)

	t.Run("DNS ответил быстро (50мс)", func(t *testing.T) {
		got := zahodSZaderzhkoyDns(t, 50*time.Millisecond, trafikZaderzhka, obshchiyBudzhet)
		if got != Doma {
			t.Fatalf("DNS быстрый и честный, трафик честный — хочу «дома», получил %v", got)
		}
	})

	t.Run("DNS ответил медленно, но так же честно (280мс)", func(t *testing.T) {
		got := zahodSZaderzhkoyDns(t, 280*time.Millisecond, trafikZaderzhka, obshchiyBudzhet)
		if got != Doma {
			t.Fatalf("DNS честный, просто дольше (280мс вместо 50мс) — тот же самый честный трафик-зонд обязан дать тот же вердикт «дома», получил %v (общий бюджет %v меньше суммы номиналов подзондов 300мс+400мс: каскад обрезал прямому зонду остаток вместо его полного номинала)", got, obshchiyBudzhet)
		}
	})
}
