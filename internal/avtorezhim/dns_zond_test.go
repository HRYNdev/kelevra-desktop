package avtorezhim

import (
	"context"
	"errors"
	"net"
	"testing"
)

// fakeResolver — подставной резолвер: отвечает по имени домена без единого
// настоящего DNS-запроса.
type fakeResolver struct {
	otvety  map[string][]net.IP
	oshibki map[string]error
}

func (f fakeResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	if err, ok := f.oshibki[host]; ok {
		return nil, err
	}
	return f.otvety[host], nil
}

func fakeAdres(a, b, c, d byte) net.IP { return net.IPv4(a, b, c, d) }

func TestDnsZondDvaIzTryohFake(t *testing.T) {
	z := &DnsZond{
		Resolver: fakeResolver{otvety: map[string][]net.IP{
			"youtube.com":   {fakeAdres(198, 18, 3, 9)},   // подменный
			"discord.com":   {fakeAdres(198, 19, 1, 1)},   // подменный
			"rutracker.org": {fakeAdres(93, 158, 134, 3)}, // настоящий
		}},
		Domeny: DomenyPoUmolchaniyu,
		Nuzhno: 2,
	}
	doma, err := z.DomaPoDns(context.Background())
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if !doma {
		t.Fatal("2 из 3 подменных — признак дома обязан быть true")
	}
}

func TestDnsZondOdinIzTryohNeHvataet(t *testing.T) {
	z := &DnsZond{
		Resolver: fakeResolver{otvety: map[string][]net.IP{
			"youtube.com":   {fakeAdres(198, 18, 3, 9)},
			"discord.com":   {fakeAdres(140, 82, 121, 1)},
			"rutracker.org": {fakeAdres(93, 158, 134, 3)},
		}},
		Domeny: DomenyPoUmolchaniyu,
		Nuzhno: 2,
	}
	doma, err := z.DomaPoDns(context.Background())
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if doma {
		t.Fatal("1 из 3 подменных — признака дома быть не должно")
	}
}

func TestDnsZondMolchaniyeOdnogoDomenaNeMeshayet(t *testing.T) {
	z := &DnsZond{
		Resolver: fakeResolver{
			otvety: map[string][]net.IP{
				"youtube.com": {fakeAdres(198, 18, 3, 9)},
				"discord.com": {fakeAdres(198, 19, 1, 1)},
			},
			oshibki: map[string]error{
				"rutracker.org": errors.New("таймаут"),
			},
		},
		Domeny: DomenyPoUmolchaniyu,
		Nuzhno: 2,
	}
	doma, err := z.DomaPoDns(context.Background())
	if err != nil {
		t.Fatalf("молчание одного домена не должно превращаться в ошибку: %v", err)
	}
	if !doma {
		t.Fatal("2 из 2 ответивших подменных — признак дома обязан быть true, молчание третьего не в счёт")
	}
}

func TestDnsZondVseMolchatEtoOshibka(t *testing.T) {
	z := &DnsZond{
		Resolver: fakeResolver{oshibki: map[string]error{
			"youtube.com":   errors.New("сеть недоступна"),
			"discord.com":   errors.New("сеть недоступна"),
			"rutracker.org": errors.New("сеть недоступна"),
		}},
		Domeny: DomenyPoUmolchaniyu,
		Nuzhno: 2,
	}
	_, err := z.DomaPoDns(context.Background())
	if err == nil {
		t.Fatal("ни один домен не ответил — ждал ошибку, получил nil")
	}
}

// TestDnsZondTriIzTryohNoKontrolnyyTozhePodmenenEtoNeDom — живой замер:
// сеть с ТОТАЛЬНЫМ перехватом DNS подменяет вообще все домены, включая
// контрольный (gosuslugi.ru), который дома всегда резолвится по-настоящему.
// 3 из 3 обычных доменов подменены, но раз подменён и контрольный —
// это перехватчик (например, публичный Wi-Fi), а не дом.
func TestDnsZondTriIzTryohNoKontrolnyyTozhePodmenenEtoNeDom(t *testing.T) {
	z := &DnsZond{
		Resolver: fakeResolver{otvety: map[string][]net.IP{
			"youtube.com":   {fakeAdres(198, 18, 3, 9)},
			"discord.com":   {fakeAdres(198, 19, 1, 1)},
			"rutracker.org": {fakeAdres(198, 18, 2, 210)},
			"gosuslugi.ru":  {fakeAdres(198, 18, 5, 5)}, // тотальный перехват подменяет и его
		}},
		Domeny:          DomenyPoUmolchaniyu,
		Nuzhno:          2,
		KontrolnyyDomen: "gosuslugi.ru",
	}
	doma, err := z.DomaPoDns(context.Background())
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if doma {
		t.Fatal("контрольный домен тоже подменён — это перехватчик, признака дома быть не должно")
	}
}

// TestDnsZondDvaIzTryohKontrolnyyNastoyashchiyEtoDom — живой замер дома:
// youtube/discord подменены, rutracker настоящий (обычный шум), контрольный
// gosuslugi.ru резолвится в настоящий адрес — признак дома должен устоять.
func TestDnsZondDvaIzTryohKontrolnyyNastoyashchiyEtoDom(t *testing.T) {
	z := &DnsZond{
		Resolver: fakeResolver{otvety: map[string][]net.IP{
			"youtube.com":   {fakeAdres(198, 18, 3, 10)},
			"discord.com":   {fakeAdres(198, 18, 9, 93)},
			"rutracker.org": {fakeAdres(93, 158, 134, 3)},
			"gosuslugi.ru":  {net.IPv4(213, 59, 254, 7)},
		}},
		Domeny:          DomenyPoUmolchaniyu,
		Nuzhno:          2,
		KontrolnyyDomen: "gosuslugi.ru",
	}
	doma, err := z.DomaPoDns(context.Background())
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if !doma {
		t.Fatal("2 из 3 подменных, контрольный настоящий — признак дома обязан быть true")
	}
}

// TestDnsZondKontrolnyyMolchitNeMeshayet — контрольный домен промолчал
// (таймаут/сбой) — молчание не довод ни за, ни против, признак не блокируется.
func TestDnsZondKontrolnyyMolchitNeMeshayet(t *testing.T) {
	z := &DnsZond{
		Resolver: fakeResolver{
			otvety: map[string][]net.IP{
				"youtube.com":   {fakeAdres(198, 18, 3, 10)},
				"discord.com":   {fakeAdres(198, 18, 9, 93)},
				"rutracker.org": {fakeAdres(93, 158, 134, 3)},
			},
			oshibki: map[string]error{
				"gosuslugi.ru": errors.New("таймаут"),
			},
		},
		Domeny:          DomenyPoUmolchaniyu,
		Nuzhno:          2,
		KontrolnyyDomen: "gosuslugi.ru",
	}
	doma, err := z.DomaPoDns(context.Background())
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if !doma {
		t.Fatal("контрольный домен промолчал — это не довод, признак дома по 2 из 3 обязан устоять")
	}
}

func TestFakeIPGranitsy(t *testing.T) {
	cases := []struct {
		ip   net.IP
		fake bool
	}{
		{fakeAdres(198, 17, 255, 255), false}, // ниже диапазона
		{fakeAdres(198, 18, 0, 0), true},      // нижняя граница
		{fakeAdres(198, 19, 255, 255), true},  // верхняя граница
		{fakeAdres(198, 20, 0, 0), false},     // выше диапазона
		{fakeAdres(8, 8, 8, 8), false},
	}
	for _, c := range cases {
		if got := fakeIP(c.ip); got != c.fake {
			t.Fatalf("fakeIP(%v) = %v, хочу %v", c.ip, got, c.fake)
		}
	}
}
