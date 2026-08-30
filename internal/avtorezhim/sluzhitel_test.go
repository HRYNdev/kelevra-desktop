package avtorezhim

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeSledchikSluzhitelya — подставной Sledchik: события шлются тестом через
// Sobytiya (канал без буфера — отправка синхронизирует тест с циклом Krutit,
// подтверждая, что событие уже забрано), Stop только отмечает факт вызова.
type fakeSledchikSluzhitelya struct {
	sobytiya chan struct{}

	mu      sync.Mutex
	ostanov bool
}

func novyyFakeSledchik() *fakeSledchikSluzhitelya {
	return &fakeSledchikSluzhitelya{sobytiya: make(chan struct{})}
}

func (f *fakeSledchikSluzhitelya) Sobytiya() <-chan struct{} { return f.sobytiya }

func (f *fakeSledchikSluzhitelya) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ostanov = true
}

func (f *fakeSledchikSluzhitelya) ostanovlen() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ostanov
}

// schitayushchiyDns — DNS-зонд, который считает каждый вызов (= каждый
// реальный заход Avtorezhim.Zahod, потому что тот дёргает Dns.DomaPoDns на
// каждом заходе с estSet=true) и извещает об этом тест через zvonok, а сам
// ответ подменяется полем doma под мьютексом.
type schitayushchiyDns struct {
	mu     sync.Mutex
	doma   bool
	zvonki int
	zvonok chan struct{} // сигнал "заход случился", буферизован с запасом
}

func (d *schitayushchiyDns) DomaPoDns(ctx context.Context) (bool, error) {
	d.mu.Lock()
	d.zvonki++
	doma := d.doma
	d.mu.Unlock()
	if d.zvonok != nil {
		d.zvonok <- struct{}{}
	}
	return doma, nil
}

func (d *schitayushchiyDns) ustanovit(doma bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.doma = doma
}

func (d *schitayushchiyDns) schyot() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.zvonki
}

// fakeTrafikVsegda — прямой зонд, который всегда отвечает заданным заранее
// исходом (без сети, тестам этого пакета хватает).
type fakeTrafikVsegda struct {
	izmereno bool
	proshel  bool
}

func (f fakeTrafikVsegda) Proshel(ctx context.Context) (bool, bool) { return f.izmereno, f.proshel }

// fakePosle — подставная "время после паузы": каждый вызов создаёт канал,
// кладёт его в poslednyaya (тест дальше сам решает, когда его "сработать") и
// извещает о самом факте вызова через vyzvan, чтобы тест мог синхронизироваться,
// не совершая ни одной настоящей паузы.
type fakePosle struct {
	mu          sync.Mutex
	poslednyaya chan time.Time
	vyzvan      chan struct{}
}

func novyyFakePosle() *fakePosle {
	return &fakePosle{vyzvan: make(chan struct{}, 64)}
}

func (f *fakePosle) posle(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	f.mu.Lock()
	f.poslednyaya = ch
	f.mu.Unlock()
	f.vyzvan <- struct{}{}
	return ch
}

func (f *fakePosle) srabotat() {
	f.mu.Lock()
	ch := f.poslednyaya
	f.mu.Unlock()
	ch <- time.Now()
}

func novyySluzhitelDlyaTesta(dns *schitayushchiyDns, sl *fakeSledchikSluzhitelya, posle *fakePosle, kolbek func(context.Context, Sostoyanie)) *Sluzhitel {
	a := &Avtorezhim{
		Dns:       dns,
		Trafik:    fakeTrafikVsegda{izmereno: true, proshel: true},
		Zadvizhka: NovayaZadvizhka(VneDoma), // стартуем "вне дома", как чаще всего в жизни
	}
	return &Sluzhitel{
		Avtorezhim: a,
		Sledchik:   sl,
		Interval:   time.Hour, // страховочный тикер тестам не нужен — гасим его на "никогда"
		Posle:      posle.posle,
		Kolbek:     kolbek,
	}
}

// zhdatZvonok ждёт сигнала из канала не дольше t (юнит-тест, не боевой цикл:
// таймаут — это ловля зависшего теста, не рабочая пауза служителя).
func zhdatZvonok(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("не дождался ожидаемого захода")
	}
}

// TestSluzhitelKolbekNaSmenuObstanovki — колбэк зовётся, когда обстановка
// реально меняется (после набора Podtverzhdeniy подтверждений подряд), и не
// зовётся, пока заходы подтверждают то же самое, на чём и так стоим.
func TestSluzhitelKolbekNaSmenuObstanovki(t *testing.T) {
	dns := &schitayushchiyDns{zvonok: make(chan struct{}, 64)}
	sl := novyyFakeSledchik()
	posle := novyyFakePosle()

	var mu sync.Mutex
	var vyzovyKolbeka []Sostoyanie
	kolbek := func(ctx context.Context, s Sostoyanie) {
		mu.Lock()
		vyzovyKolbeka = append(vyzovyKolbeka, s)
		mu.Unlock()
	}

	sluzh := novyySluzhitelDlyaTesta(dns, sl, posle, kolbek)
	// Заход, подтвердивший уже принятую обстановку (i>0 ниже), запускает
	// добивающую пачку быстрых опросов независимо от slep (см. Krutit,
	// case <-ozhidaniye) — с одним повтором это ровно один лишний вызов
	// Posle, который тест ниже детерминированно сливает, не подбирая число
	// по факту (гонка недопустима: fakePosle хранит только последний канал).
	sluzh.PovtorovBystrogo = 1
	dns.ustanovit(false) // "вне дома" — совпадает со стартовой обстановкой Zadvizhka

	ctx, otmena := context.WithCancel(context.Background())
	defer otmena()
	gotovo := make(chan struct{})
	go func() { sluzh.Krutit(ctx); close(gotovo) }()

	// Заход сразу на старте: DNS без признака дома == VneDoma == текущая
	// обстановка, изменения нет, колбэк не зовётся.
	zhdatZvonok(t, dns.zvonok)

	// Теперь сеть "дома" — доводим до Podtverzhdeniy подтверждений подряд
	// событиями смены сети. Каждое событие — синхронная отправка (без
	// буфера, значит подтверждает, что Krutit его забрал), дальше ждём вызова
	// Posle и вручную "срабатываем" паузу дребезга.
	dns.ustanovit(true)
	for i := 0; i < Podtverzhdeniy; i++ {
		sl.sobytiya <- struct{}{}
		zhdatZvonok(t, posle.vyzvan)
		posle.srabotat()
		zhdatZvonok(t, dns.zvonok)

		if i > 0 {
			// Первое событие (i==0) сменило обстановку (izmenilos=true) —
			// добивать нечего. Начиная со второго событие лишь ПОДТВЕРЖДАЕТ
			// уже принятую Doma (izmenilos=false), и Krutit сам стартует
			// добивающую пачку из PovtorovBystrogo=1 раунда — сливаем его,
			// иначе неслитый Posle останется в очереди и собьёт
			// синхронизацию со следующим событием цикла.
			zhdatZvonok(t, posle.vyzvan)
			posle.srabotat()
			zhdatZvonok(t, dns.zvonok)
		}
	}

	otmena()
	<-gotovo

	mu.Lock()
	defer mu.Unlock()
	if len(vyzovyKolbeka) != 1 {
		t.Fatalf("колбэк позван %d раз, хочу 1 (только на реальной смене): %+v", len(vyzovyKolbeka), vyzovyKolbeka)
	}
	if vyzovyKolbeka[0] != Doma {
		t.Fatalf("колбэк позван с %v, хочу Doma", vyzovyKolbeka[0])
	}
}

// TestSluzhitelSobytiyeSmenySetiMenyaetSrazu — претензия хозяина «долго и
// странно»: наблюдение по РЕАЛЬНОМУ событию смены сети ([Sledchik]) обязано
// переключить обстановку одним заходом, а не после Podtverzhdeniy (=3)
// одинаковых наблюдений подряд (это раньше означало ~180-260 с, потому что
// подтверждения было неоткуда взять, кроме страховочного тикера раз в
// IntervalTikeraPoUmolchaniyu=2 мин). Перенос AutoModeGate.offer(trust=true)
// с телефона.
func TestSluzhitelSobytiyeSmenySetiMenyaetSrazu(t *testing.T) {
	dns := &schitayushchiyDns{zvonok: make(chan struct{}, 64)}
	sl := novyyFakeSledchik()
	posle := novyyFakePosle()

	var mu sync.Mutex
	var vyzovyKolbeka []Sostoyanie
	kolbek := func(ctx context.Context, s Sostoyanie) {
		mu.Lock()
		vyzovyKolbeka = append(vyzovyKolbeka, s)
		mu.Unlock()
	}

	sluzh := novyySluzhitelDlyaTesta(dns, sl, posle, kolbek)
	dns.ustanovit(false) // "вне дома" — совпадает со стартовой обстановкой Zadvizhka

	ctx, otmena := context.WithCancel(context.Background())
	defer otmena()
	gotovo := make(chan struct{})
	go func() { sluzh.Krutit(ctx); close(gotovo) }()

	zhdatZvonok(t, dns.zvonok) // заход на старте — VneDoma совпадает, без смены

	// Сеть сменилась на "дома" — ОДНО событие смены сети, без набора трёх.
	dns.ustanovit(true)
	sl.sobytiya <- struct{}{}
	zhdatZvonok(t, posle.vyzvan)
	posle.srabotat()
	zhdatZvonok(t, dns.zvonok) // единственный заход после единственного события

	otmena()
	<-gotovo

	mu.Lock()
	defer mu.Unlock()
	if len(vyzovyKolbeka) != 1 {
		t.Fatalf("колбэк позван %d раз, хочу 1 — одно событие смены сети обязано переключить обстановку сразу: %+v", len(vyzovyKolbeka), vyzovyKolbeka)
	}
	if vyzovyKolbeka[0] != Doma {
		t.Fatalf("колбэк позван с %v, хочу Doma", vyzovyKolbeka[0])
	}
}

// TestSluzhitelPachkaSobytiySkhlopyvaetsyaVOdinZahod — несколько событий
// смены сети подряд (без паузы между ними) обязаны дать ОДИН заход после
// затишья, а не по заходу на каждое событие.
func TestSluzhitelPachkaSobytiySkhlopyvaetsyaVOdinZahod(t *testing.T) {
	dns := &schitayushchiyDns{zvonok: make(chan struct{}, 64)}
	sl := novyyFakeSledchik()
	posle := novyyFakePosle()
	sluzh := novyySluzhitelDlyaTesta(dns, sl, posle, nil)

	ctx, otmena := context.WithCancel(context.Background())
	defer otmena()
	gotovo := make(chan struct{})
	go func() { sluzh.Krutit(ctx); close(gotovo) }()

	zhdatZvonok(t, dns.zvonok) // заход на старте — 1

	const pachka = 5
	for i := 0; i < pachka; i++ {
		sl.sobytiya <- struct{}{}
		zhdatZvonok(t, posle.vyzvan) // каждое событие обязано отодвинуть паузу
	}
	// Ни одного захода за пачку событий быть не должно — они ждут паузы.
	select {
	case <-dns.zvonok:
		t.Fatal("заход случился раньше, чем сработала пауза схлопывания дребезга")
	default:
	}

	posle.srabotat() // "прошла секунда тишины" — единственный отложенный заход
	zhdatZvonok(t, dns.zvonok)

	otmena()
	<-gotovo

	if got := dns.schyot(); got != 2 {
		t.Fatalf("заходов случилось %d (старт + пачка), хочу 2 — пачка из %d событий должна была схлопнуться в один заход", got, pachka)
	}
}

// TestSluzhitelCtxDoneOstanavlivayetSledchik — отмена контекста завершает
// Krutit и останавливает Sledchik.
func TestSluzhitelCtxDoneOstanavlivayetSledchik(t *testing.T) {
	dns := &schitayushchiyDns{zvonok: make(chan struct{}, 64)}
	sl := novyyFakeSledchik()
	posle := novyyFakePosle()
	sluzh := novyySluzhitelDlyaTesta(dns, sl, posle, nil)

	ctx, otmena := context.WithCancel(context.Background())
	gotovo := make(chan struct{})
	go func() { sluzh.Krutit(ctx); close(gotovo) }()

	zhdatZvonok(t, dns.zvonok) // дождались захода на старте, цикл точно уже крутится

	otmena()
	select {
	case <-gotovo:
	case <-time.After(5 * time.Second):
		t.Fatal("Krutit не завершился после отмены контекста")
	}

	if !sl.ostanovlen() {
		t.Fatal("Sledchik.Stop() не был позван при выходе из Krutit")
	}
}

// TestSluzhitelHolodnyyStartMenyaetSrazu — вторая половина претензии «долго и
// странно», та, что бьёт по КАЖДОМУ запуску: в бою задвижка заводится на
// Neizvestno (avtorezhim.go:187), а на Neizvestno колбэк ничего не включает.
// Значит до правки человек включал Kelevra и ждал Podtverzhdeniy заходов
// страховочного тикера (~180-260 с), прежде чем защита вообще шевельнётся.
// Заход на старте помечен доверенным — обстановка обязана определиться с
// первого раза, без единого события Sledchik и без тикера.
//
// Остальные тесты служителя заводят задвижку на VneDoma (см.
// novyySluzhitelDlyaTesta) — там старт совпадает с наблюдением и эту дыру
// не видно, поэтому здесь стартовая обстановка ставится как в бою.
func TestSluzhitelHolodnyyStartMenyaetSrazu(t *testing.T) {
	dns := &schitayushchiyDns{zvonok: make(chan struct{}, 64)}
	sl := novyyFakeSledchik()
	posle := novyyFakePosle()

	pozvali := make(chan Sostoyanie, 8)
	kolbek := func(ctx context.Context, s Sostoyanie) { pozvali <- s }

	sluzh := novyySluzhitelDlyaTesta(dns, sl, posle, kolbek)
	sluzh.Avtorezhim.Zadvizhka = NovayaZadvizhka(Neizvestno) // как в бою
	dns.ustanovit(true)                                      // сеть домашняя

	ctx, otmena := context.WithCancel(context.Background())
	defer otmena()
	gotovo := make(chan struct{})
	go func() { sluzh.Krutit(ctx); close(gotovo) }()

	// Ни одного события Sledchik и ни одного тика (Interval=час) — только
	// заход на старте.
	select {
	case s := <-pozvali:
		if s != Doma {
			t.Fatalf("колбэк на холодном старте позван с %v, хочу Doma", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("колбэк на холодном старте не позван: обстановка так и осталась Neizvestno")
	}

	otmena()
	<-gotovo
}
