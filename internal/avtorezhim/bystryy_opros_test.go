package avtorezhim

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// schitayushchiySetevoyAdres — SetevoyAdres, который первые (uspehS-1) раз
// отвечает ошибкой (адаптер ещё не опознан — реальная картина сразу после
// NotifyAddrChange), а начиная с попытки uspehS — успешно.
type schitayushchiySetevoyAdres struct {
	mu      sync.Mutex
	popytok int
	uspehS  int
	zvonok  chan struct{}
}

func (s *schitayushchiySetevoyAdres) adres() (string, string, error) {
	s.mu.Lock()
	s.popytok++
	n := s.popytok
	s.mu.Unlock()
	if s.zvonok != nil {
		s.zvonok <- struct{}{}
	}
	if n < s.uspehS {
		return "", "", errors.New("адаптер ещё не опознан")
	}
	return "192.168.1.1:53", "192.168.1.77", nil
}

func (s *schitayushchiySetevoyAdres) schyot() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.popytok
}

// TestSluzhitelBystrayaPachkaDobivayetSlepoyZahodPosleSobytiya —
// боевой сценарий хозяина 29.08 «после возврата домой туннель висит до 6
// минут»: сразу после события смены сети физический адаптер ещё не опознан
// (Avtorezhim.Zahod уходит в слепой путь, ZondSlep, задвижке нечего
// предложить), и до правки следующий шанс подтвердить обстановку был только
// у страховочного тикера — тот не доверенный, требует Podtverzhdeniy=3
// заходов подряд, то есть до IntervalTikeraPoUmolchaniyu*3 (~6 минут).
//
// Тест поднимает Sluzhitel.Krutit так, что первые заходы (старт и заход по
// событию) остаются слепыми, а физический адаптер опознаётся только на
// третьей попытке — она обязана прийти из ПАЧКИ быстрых опросов после
// события, а не из страховочного тикера (Interval здесь взведён на час —
// если бы подтверждение зависело от тикера, тест просто не дождался бы его
// за отпущенные секунды и упал по таймауту zhdatZvonok).
func TestSluzhitelBystrayaPachkaDobivayetSlepoyZahodPosleSobytiya(t *testing.T) {
	tunnelUp := true

	// Попытка 1 — заход на старте (слепая), попытка 2 — заход по событию
	// (тоже слепая), попытка 3 — первый быстрый опрос пачки: адаптер
	// наконец опознан.
	adr := &schitayushchiySetevoyAdres{uspehS: 3, zvonok: make(chan struct{}, 64)}

	dns := &schitayushchiyDns{zvonok: make(chan struct{}, 64)}
	dns.ustanovit(true) // дом — как только адаптер опознается, DNS это увидит

	sl := novyyFakeSledchik()
	posle := novyyFakePosle()

	var mu sync.Mutex
	var kolbekZvali []Sostoyanie
	kolbek := func(ctx context.Context, s Sostoyanie) {
		mu.Lock()
		kolbekZvali = append(kolbekZvali, s)
		mu.Unlock()
	}

	a := &Avtorezhim{
		Trafik:        fakeTrafikVsegda{izmereno: true, proshel: true},
		Zadvizhka:     NovayaZadvizhka(VneDoma),
		TunnelPodnyat: func() bool { return tunnelUp },
		SetevoyAdres:  adr.adres,
		DnsPryamoy:    func(string, string) DnsProver { return dns },
	}

	sluzh := &Sluzhitel{
		Avtorezhim: a,
		Sledchik:   sl,
		// Тикер намеренно огромный: подтверждение обязано прийти из пачки
		// быстрых опросов, а не из страховочной подстраховки.
		Interval: time.Hour,
		Posle:    posle.posle,
		Kolbek:   kolbek,
	}

	ctx, otmena := context.WithCancel(context.Background())
	defer otmena()
	gotovo := make(chan struct{})
	go func() { sluzh.Krutit(ctx); close(gotovo) }()

	// Заход на старте — попытка 1, слепая.
	zhdatZvonok(t, adr.zvonok)

	// Событие смены сети — заход по событию (после паузы дребезга), попытка
	// 2, ещё слепая (адаптер опознаётся только с 3-й попытки).
	sl.sobytiya <- struct{}{}
	zhdatZvonok(t, posle.vyzvan)
	posle.srabotat()
	zhdatZvonok(t, adr.zvonok)

	// Первый быстрый опрос пачки — попытка 3: адаптер опознан, DNS видит
	// "дом", обстановка меняется одним доверенным наблюдением.
	zhdatZvonok(t, posle.vyzvan)
	posle.srabotat()
	zhdatZvonok(t, dns.zvonok)

	otmena()
	<-gotovo

	mu.Lock()
	defer mu.Unlock()
	if len(kolbekZvali) != 1 || kolbekZvali[0] != Doma {
		t.Fatalf("колбэк(и): %+v, хочу ровно один вызов с Doma — пачка быстрых опросов после события обязана добить слепой заход, не дожидаясь страховочного тикера", kolbekZvali)
	}
	if got := adr.schyot(); got < 3 {
		t.Fatalf("SetevoyAdres спрошен %d раз, хочу минимум 3 — событие смены сети обязано повлечь несколько опросов подряд с коротким шагом", got)
	}
}
