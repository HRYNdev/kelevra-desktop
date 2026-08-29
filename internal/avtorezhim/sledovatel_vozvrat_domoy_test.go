package avtorezhim

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestSluzhitelVozvratDomoyVyklyuchayetTunnelPoslePodnyatiya —
// СЛЕДОВАТЕЛЬСКИЙ тест: боевой сценарий хозяина 29.08 «при переходе обратно
// на вайфай авто режим не выключает впн», прогнанный через ТОТ ЖЕ путь, что
// в бою — Sluzhitel.Krutit + avtorezhimKolbek-подобный колбэк, а не голый
// Avtorezhim.Zahod, как в slepota_test.go.
//
// Отличие от TestZahodVTunneleSPrivatnymAdapteromNeSlep: там TunnelPodnyat
// приходит в Avtorezhim.Zahod уже готовым булем, здесь — живой замкнутый
// цикл через Sluzhitel: колбэк реально гасит tunnelUp (как avtorezhimKolbek
// реально гасит ядро через OpustitZashchitu), и TunnelPodnyat на СЛЕДУЮЩЕМ
// заходе обязан это увидеть.
func TestSluzhitelVozvratDomoyVyklyuchayetTunnelPoslePodnyatiya(t *testing.T) {
	var mu sync.Mutex
	tunnelUp := true // человек вне дома, авторежим уже поднял VPN

	dns := &schitayushchiyDns{zvonok: make(chan struct{}, 64)}
	dns.ustanovit(false) // вне дома: DNS не видит домашней подмены

	sl := novyyFakeSledchik()
	posle := novyyFakePosle()

	var kolbekZvali []Sostoyanie
	kolbek := func(ctx context.Context, s Sostoyanie) {
		mu.Lock()
		kolbekZvali = append(kolbekZvali, s)
		if s == Doma {
			tunnelUp = false // ровно то, что в бою делает OpustitZashchitu
		}
		mu.Unlock()
	}

	a := &Avtorezhim{
		Trafik:    fakeTrafikVsegda{izmereno: true, proshel: true},
		Zadvizhka: NovayaZadvizhka(VneDoma),
		TunnelPodnyat: func() bool {
			mu.Lock()
			defer mu.Unlock()
			return tunnelUp
		},
		SetevoyAdres: func() (string, string, error) {
			return "192.168.1.192:53", "192.168.1.77", nil
		},
		DnsPryamoy: func(adresResolvera, lokalnyAdres string) DnsProver {
			return dns
		},
	}

	sluzh := &Sluzhitel{
		Avtorezhim: a,
		Sledchik:   sl,
		Interval:   time.Hour, // страховочный тикер тестам не нужен
		Posle:      posle.posle,
		Kolbek:     kolbek,
	}

	ctx, otmena := context.WithCancel(context.Background())
	defer otmena()
	gotovo := make(chan struct{})
	go func() { sluzh.Krutit(ctx); close(gotovo) }()

	// Заход на старте: туннель поднят, DNS через адаптер видит "не дома" —
	// подтверждает то, на чём и так стоим, колбэк не зовётся.
	zhdatZvonok(t, dns.zvonok)

	// Человек вернулся на домашний Wi-Fi: DNS-зонд физического адаптера
	// теперь видит домашнюю подмену. Windows шлёт NotifyAddrChange — событие
	// Sledchik — авторежим обязан поверить с первого раза (dovereno=true) и
	// выключить туннель.
	dns.ustanovit(true)
	sl.sobytiya <- struct{}{}
	zhdatZvonok(t, posle.vyzvan)
	posle.srabotat()
	zhdatZvonok(t, dns.zvonok)

	otmena()
	<-gotovo

	mu.Lock()
	defer mu.Unlock()
	if len(kolbekZvali) != 1 || kolbekZvali[0] != Doma {
		t.Fatalf("колбэк(и): %+v, хочу ровно один вызов с Doma", kolbekZvali)
	}
	if tunnelUp {
		t.Fatal("вернулись домой одним событием смены сети, а туннель всё ещё поднят — воспроизведена жалоба хозяина 29.08")
	}
}
