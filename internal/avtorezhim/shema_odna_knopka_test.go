package avtorezhim

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Заказанная схема, проверенная СЦЕНАРИЯМИ, а не устройством кода.
// Требование 27.08: нажал «подключить» — программа сама определяет
// обстановку: дома «режим ожидания», вне дома защита включается. И четырежды
// повторённая жалоба (25.08, 28.08, 29.08, 30.08): при возврате на домашний
// Wi-Fi VPN не выключался.
//
// Каждый тест ниже — один человеческий сценарий целиком:
//   - дома при старте → защита не поднимается;
//   - ушёл из дома → поднялась сама;
//   - вернулся домой → опустилась сама;
//   - вернулся домой, но обстановка «дома» уже стояла → всё равно опустилась
//     (это и есть та самая невыполненная жалоба: событийный колбэк здесь
//     молчал вечно);
//   - резолверы молчат на той же сети → не меняется НИЧЕГО;
//   - заход слепой (мерили свой же туннель) → не меняется НИЧЕГО.

// stendShemy — обстановка вокруг авторежима, какой её видит человек: поднята
// ли защита, что отвечает DNS-зонд и молчит ли он вовсе. Он же играет роль
// службы: primenit делает с защитой ровно то, что в бою делает
// Sluzhba.avtorezhimKolbek через OpustitZashchitu/PodnyatZashchitu.
type stendShemy struct {
	mu      sync.Mutex
	tunnel  bool // защита поднята прямо сейчас
	doma    bool // DNS-зонд видит домашнюю подмену
	molchit bool // резолверы не отвечают вовсе

	zvonok     chan struct{} // «заход случился» — синхронизация теста с циклом
	dvizheniya []string      // что приведение сделало с защитой, по порядку
}

func novyyStendShemy() *stendShemy {
	return &stendShemy{zvonok: make(chan struct{}, 64)}
}

func (st *stendShemy) DomaPoDns(ctx context.Context) (bool, error) {
	st.mu.Lock()
	doma, molchit := st.doma, st.molchit
	st.mu.Unlock()
	if st.zvonok != nil {
		st.zvonok <- struct{}{}
	}
	if molchit {
		// Ровно то, что возвращает боевой DnsZond, когда не ответил НИ ОДИН
		// контрольный домен: отсутствие измерения, а не измерение «не дома».
		return false, fmt.Errorf("резолвер молчит")
	}
	return doma, nil
}

func (st *stendShemy) Proshel(ctx context.Context) (bool, bool) { return true, true }

func (st *stendShemy) podnyat() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.tunnel
}

// primenit — то же приведение, что в бою делает Sluzhba.avtorezhimKolbek:
// спрашивает чистое правило Nuzhno и двигает защиту, только если она
// обстановке не отвечает.
func (st *stendShemy) primenit(ctx context.Context, s Sostoyanie, povtor bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	switch Nuzhno(s, st.tunnel) {
	case Opustit:
		st.tunnel = false
		st.dvizheniya = append(st.dvizheniya, "опустил")
	case Podnyat:
		st.tunnel = true
		st.dvizheniya = append(st.dvizheniya, "поднял")
	}
}

func (st *stendShemy) sdelano() []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]string(nil), st.dvizheniya...)
}

func (st *stendShemy) postavit(doma, molchit bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.doma, st.molchit = doma, molchit
}

// sluzhitelShemy — служитель, собранный как в бою: зонды подставные, зато
// приведение защиты (Primenit) и задвижка настоящие. SetevoyAdres отдаёт
// правдоподобный домашний резолвер — при поднятом туннеле боевой заход
// спрашивает именно его, минуя системный (см. Avtorezhim.Zahod).
func sluzhitelShemy(st *stendShemy, nachalo Sostoyanie, sl Sledchik, posle *fakePosle) *Sluzhitel {
	a := &Avtorezhim{
		Dns:           st,
		Trafik:        st,
		Zadvizhka:     NovayaZadvizhka(nachalo),
		TunnelPodnyat: st.podnyat,
		SetevoyAdres: func() (string, string, error) {
			return "192.168.1.1:53", "192.168.1.77", nil
		},
		DnsPryamoy: func(_, _ string) DnsProver { return st },
	}
	return &Sluzhitel{
		Avtorezhim: a,
		Sledchik:   sl,
		Interval:   time.Hour, // страховочный тикер сценариям не нужен
		Posle:      posle.posle,
		Primenit:   st.primenit,
	}
}

// TestNuzhnoPravilo — чистое правило приведения таблицей: что нужно сделать с
// защитой, чтобы она отвечала обстановке. Главное здесь — что Neizvestno не
// требует НИЧЕГО ни при какой защите: неизвестность не повод дёргать туннель.
func TestNuzhnoPravilo(t *testing.T) {
	sluchai := []struct {
		nazvanie string
		sost     Sostoyanie
		podnyata bool
		hochu    Deystvie
	}{
		{"дома, защита поднята — опустить", Doma, true, Opustit},
		{"дома, защиты нет — режим ожидания, не трогать", Doma, false, NeTrogat},
		{"вне дома, защиты нет — поднять", VneDoma, false, Podnyat},
		{"вне дома, защита поднята — не трогать", VneDoma, true, NeTrogat},
		{"неизвестно, защита поднята — не трогать", Neizvestno, true, NeTrogat},
		{"неизвестно, защиты нет — не трогать", Neizvestno, false, NeTrogat},
	}
	for _, s := range sluchai {
		t.Run(s.nazvanie, func(t *testing.T) {
			if got := Nuzhno(s.sost, s.podnyata); got != s.hochu {
				t.Fatalf("Nuzhno(%v, %v) = %v, хочу %v", s.sost, s.podnyata, got, s.hochu)
			}
		})
	}
}

// TestDomaPriStarteZashchitaNePodnimaetsya — дома «режим ожидания»
// (требование 27.08). Человек дома, защиты нет: авторежим обязан опознать дом и НЕ
// поднимать туннель — обход блокировок дома уже делает роутер.
func TestDomaPriStarteZashchitaNePodnimaetsya(t *testing.T) {
	st := novyyStendShemy()
	st.postavit(true, false)

	sluzh := sluzhitelShemy(st, Neizvestno, novyyFakeSledchik(), novyyFakePosle())
	if _, slep := sluzh.zahod(context.Background(), true); slep {
		t.Fatal("заход вышел слепым, хотя туннель опущен и зонды честны")
	}

	if got := sluzh.Avtorezhim.Zadvizhka.Tekushcheye(); got != Doma {
		t.Fatalf("обстановка %v, хочу «дома» — окно обязано показать режим ожидания", got)
	}
	if st.podnyat() {
		t.Fatal("дома при старте защита всё-таки поднялась — это не режим ожидания")
	}
	if d := st.sdelano(); len(d) != 0 {
		t.Fatalf("дома при старте авторежим тронул защиту: %v", d)
	}
}

// TestUshelIzDomaZashchitaPodnimaetsyaSama — вне дома защита включается
// (требование 27.08). Человек был дома в режиме ожидания, ушёл, Windows прислала событие
// смены сети — защита обязана подняться сама, без единого нажатия.
func TestUshelIzDomaZashchitaPodnimaetsyaSama(t *testing.T) {
	st := novyyStendShemy()
	st.postavit(true, false) // пока дома

	sl := novyyFakeSledchik()
	posle := novyyFakePosle()
	sluzh := sluzhitelShemy(st, Doma, sl, posle)

	ctx, otmena := context.WithCancel(context.Background())
	defer otmena()
	gotovo := make(chan struct{})
	go func() { sluzh.Krutit(ctx); close(gotovo) }()

	zhdatZvonok(t, st.zvonok) // заход на старте: подтвердил дом, защиты нет

	// Человек ушёл: домашней подмены больше нет, сеть сменилась.
	st.postavit(false, false)
	sl.sobytiya <- struct{}{}
	zhdatZvonok(t, posle.vyzvan)
	posle.srabotat()
	zhdatZvonok(t, st.zvonok)

	otmena()
	<-gotovo

	if !st.podnyat() {
		t.Fatal("ушли из дома, а защита не поднялась сама")
	}
	if d := st.sdelano(); len(d) != 1 || d[0] != "поднял" {
		t.Fatalf("движения защиты: %v, хочу ровно одно «поднял»", d)
	}
}

// TestVernulsyaDomoyZashchitaOpuskaetsyaSama — та самая жалоба, повторённая
// четырежды (25.08: «при переключении обратно на вайфай впн не выключился»).
// Человек вне дома под поднятым туннелем, вернулся на домашний Wi-Fi — защита
// обязана опуститься сама.
func TestVernulsyaDomoyZashchitaOpuskaetsyaSama(t *testing.T) {
	st := novyyStendShemy()
	st.mu.Lock()
	st.tunnel = true // вне дома авторежим уже поднял защиту
	st.mu.Unlock()
	st.postavit(false, false)

	sl := novyyFakeSledchik()
	posle := novyyFakePosle()
	sluzh := sluzhitelShemy(st, VneDoma, sl, posle)

	ctx, otmena := context.WithCancel(context.Background())
	defer otmena()
	gotovo := make(chan struct{})
	go func() { sluzh.Krutit(ctx); close(gotovo) }()

	zhdatZvonok(t, st.zvonok) // заход на старте: подтвердил «вне дома»

	// Вернулся домой: резолвер физического адаптера снова показывает подмену.
	st.postavit(true, false)
	sl.sobytiya <- struct{}{}
	zhdatZvonok(t, posle.vyzvan)
	posle.srabotat()
	zhdatZvonok(t, st.zvonok)

	otmena()
	<-gotovo

	if st.podnyat() {
		t.Fatal("вернулись домой, а защита всё ещё поднята — жалоба 25/28/29/30.08 жива")
	}
	if d := st.sdelano(); len(d) != 1 || d[0] != "опустил" {
		t.Fatalf("движения защиты: %v, хочу ровно одно «опустил»", d)
	}
}

// TestDomaUzheStoitAZashchitaPodnyataOpuskaetsyaVsyoRavno — ЯДРО невыполненной
// жалобы, и именно этого сценария не хватало.
//
// Обстановка «дома» УЖЕ принята (так её заводит кнопка «Подключиться» дома —
// Sluzhba.podklyuchit → vklyuchitAvtorezhim(Doma); так же остаётся, если
// опустить защиту не удалось с первого раза или её подняли руками поверх
// работающего автомата), а защита при этом поднята. Смены обстановки больше
// не будет НИКОГДА — значит событийный колбэк молчит вечно, и туннель висит
// до перезапуска приложения. Приведение обязано опустить его на ближайшем же
// заходе, ничего не дожидаясь.
func TestDomaUzheStoitAZashchitaPodnyataOpuskaetsyaVsyoRavno(t *testing.T) {
	st := novyyStendShemy()
	st.mu.Lock()
	st.tunnel = true
	st.mu.Unlock()
	st.postavit(true, false)

	sluzh := sluzhitelShemy(st, Doma, novyyFakeSledchik(), novyyFakePosle())
	izmenilos, slep := sluzh.zahod(context.Background(), false)

	if izmenilos {
		t.Fatal("обстановка не менялась (стояла на «дома») — тест проверяет не тот случай")
	}
	if slep {
		t.Fatal("заход вышел слепым — тест проверяет не тот случай")
	}
	if st.podnyat() {
		t.Fatal("обстановка «дома» стоит, а защита осталась поднятой — та самая яма, из-за которой VPN не гас")
	}
	if d := st.sdelano(); len(d) != 1 || d[0] != "опустил" {
		t.Fatalf("движения защиты: %v, хочу ровно одно «опустил»", d)
	}
}

// TestRezolveryMolchatNaTojZheSetiZashchituNeTrogayut — броня 28.08 (телефон
// хозяина роумил между репитером и роутером, резолвер молчал 4-7 минут ПРИ
// ЖИВОМ интернете). Молчание — это «не знаю», а не «не дома»: защиту оно не
// двигает НИ В ОДНУ сторону, сколько бы немых заходов подряд ни случилось.
//
// Обстановка при этом после Podtverzhdeniy немых заходов честно становится
// Neizvestno — так задвижка устроена нарочно (см.
// TestDoverennoyeMolchaniyeNeProhoditSOdnogoPodtverzhdeniya): вещь признаётся,
// что не знает, вместо того чтобы врать про дом или про чужую сеть. Важно
// ровно то, что на «не знаю» приведение не делает НИЧЕГО — Nuzhno(Neizvestno)
// возвращает NeTrogat при любой защите.
//
// Проверяются обе стороны сразу: молчание вне дома не роняет защиту, молчание
// дома не поднимает её.
func TestRezolveryMolchatNaTojZheSetiZashchituNeTrogayut(t *testing.T) {
	proverit := func(t *testing.T, nachalo Sostoyanie, tunnelSnachala bool) {
		t.Helper()
		st := novyyStendShemy()
		st.mu.Lock()
		st.tunnel = tunnelSnachala
		st.mu.Unlock()
		st.postavit(false, true) // резолверы молчат

		sluzh := sluzhitelShemy(st, nachalo, novyyFakeSledchik(), novyyFakePosle())

		// Пока немых заходов меньше Podtverzhdeniy, не двигается вообще
		// ничего — даже при доверенном заходе по событию смены сети.
		for i := 0; i < Podtverzhdeniy-1; i++ {
			if izmenilos, _ := sluzh.zahod(context.Background(), true); izmenilos {
				t.Fatalf("заход %d: молчание резолверов сдвинуло обстановку раньше срока", i+1)
			}
			if got := sluzh.Avtorezhim.Zadvizhka.Tekushcheye(); got != nachalo {
				t.Fatalf("заход %d: обстановка уехала с %v на %v из-за молчания", i+1, nachalo, got)
			}
		}

		// Дальше молчание длится и длится — вещь честно говорит «не знаю», но
		// защиту не трогает ни разу.
		for i := 0; i < Podtverzhdeniy+3; i++ {
			sluzh.zahod(context.Background(), true)
		}
		if got := sluzh.Avtorezhim.Zadvizhka.Tekushcheye(); got == VneDoma && nachalo == Doma {
			t.Fatal("молчание превратилось в «не дома» — та самая авария 28.08")
		}

		if st.podnyat() != tunnelSnachala {
			t.Fatalf("защита сдвинулась (была %v, стала %v) из-за молчания резолверов", tunnelSnachala, st.podnyat())
		}
		if d := st.sdelano(); len(d) != 0 {
			t.Fatalf("молчание резолверов подвигало защиту: %v", d)
		}
	}

	t.Run("вне дома под защитой — защиту не роняем", func(t *testing.T) {
		proverit(t, VneDoma, true)
	})
	t.Run("дома в режиме ожидания — защиту не поднимаем", func(t *testing.T) {
		proverit(t, Doma, false)
	})
}

// TestSlepoyZahodZashchituNeTrogaet — вторая половина брони: заход, который
// мерил НАШ ЖЕ туннель (физический адаптер не опознан), не наблюдение вовсе.
// Обстановка на задвижке при этом может стоять любая, в том числе «дома», —
// приводить к ней защиту нельзя, иначе слепой заход погасит VPN по догадке.
func TestSlepoyZahodZashchituNeTrogaet(t *testing.T) {
	st := novyyStendShemy()
	st.mu.Lock()
	st.tunnel = true
	st.mu.Unlock()
	st.postavit(true, false)

	sluzh := sluzhitelShemy(st, Doma, novyyFakeSledchik(), novyyFakePosle())
	// Адаптер не опознан — при поднятом туннеле это и есть слепота.
	sluzh.Avtorezhim.SetevoyAdres = func() (string, string, error) {
		return "", "", fmt.Errorf("физического адаптера нет")
	}

	for i := 0; i < PodryadDoPrichiny+1; i++ {
		if _, slep := sluzh.zahod(context.Background(), true); !slep {
			t.Fatalf("заход %d обязан быть слепым — тест проверяет не тот случай", i+1)
		}
	}

	if !st.podnyat() {
		t.Fatal("слепой заход опустил защиту — броня против ложных срабатываний сломана")
	}
	if d := st.sdelano(); len(d) != 0 {
		t.Fatalf("слепой заход подвигал защиту: %v", d)
	}
	if sluzh.Avtorezhim.PrichinaSlepoty() == "" {
		t.Fatal("слепота длится дольше PodryadDoPrichiny, а человеку про неё не сказано")
	}
}
