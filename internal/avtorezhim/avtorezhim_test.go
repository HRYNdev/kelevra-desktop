package avtorezhim

import (
	"context"
	"testing"
)

func bul(b bool) *bool { return &b }

func TestReshit(t *testing.T) {
	cases := []struct {
		imya  string
		n     Nablyudeniye
		hochu Sostoyanie
	}{
		{"сети нет вовсе", Nablyudeniye{EstSet: false}, Neizvestno},
		{"сеть есть, DNS без признака дома", Nablyudeniye{EstSet: true, DnsPriznakDoma: false}, VneDoma},
		{
			"DNS признак есть, трафик не проверяли",
			Nablyudeniye{EstSet: true, DnsPriznakDoma: true, TrafikPryamoy: nil},
			VneDoma, // не доказано — безопасный дефолт: полный туннель
		},
		{
			"DNS признак есть, трафик не прошёл (белый список)",
			Nablyudeniye{EstSet: true, DnsPriznakDoma: true, TrafikPryamoy: bul(false)},
			VneDoma,
		},
		{
			"DNS признак есть, трафик прошёл — настоящий дом",
			Nablyudeniye{EstSet: true, DnsPriznakDoma: true, TrafikPryamoy: bul(true)},
			Doma,
		},
		{
			// DNS без признака перевешивает даже подтверждённый трафик —
			// такого сочетания в жизни не бывает (трафик проверяется только
			// когда DNS уже сказал "дома"), но Reshit обязан быть честным
			// и без единого условия на порядок вызова.
			"DNS без признака, но трафик почему-то true",
			Nablyudeniye{EstSet: true, DnsPriznakDoma: false, TrafikPryamoy: bul(true)},
			VneDoma,
		},
	}
	for _, c := range cases {
		t.Run(c.imya, func(t *testing.T) {
			if got := Reshit(c.n); got != c.hochu {
				t.Fatalf("Reshit(%+v) = %v, хочу %v", c.n, got, c.hochu)
			}
		})
	}
}

// fakeDns — подставной DNS-зонд: отвечает тем, что в него положили,
// без единого сетевого запроса.
type fakeDns struct {
	doma bool
	err  error
}

func (f fakeDns) DomaPoDns(ctx context.Context) (bool, error) { return f.doma, f.err }

// fakeTrafik — подставной зонд трафика.
type fakeTrafik struct {
	izmereno bool
	proshel  bool
	zvonkov  int
}

func (f *fakeTrafik) Proshel(ctx context.Context) (bool, bool) {
	f.zvonkov++
	return f.izmereno, f.proshel
}

// TestAvtorezhimZahodDomaPoslePodtverzhdeniy проверяет сборку целиком:
// три захода подряд с одинаковым наблюдением "дома" переключают задвижку
// только на третьем — гистерезис работает через Avtorezhim.Zahod, а не
// только в изоляции у Zadvizhka.
func TestAvtorezhimZahodDomaPoslePodtverzhdeniy(t *testing.T) {
	trafik := &fakeTrafik{izmereno: true, proshel: true}
	a := &Avtorezhim{
		Dns:       fakeDns{doma: true},
		Trafik:    trafik,
		Zadvizhka: NovayaZadvizhka(Neizvestno),
	}

	for i := 1; i < Podtverzhdeniy; i++ {
		_, izmenilos, tek := a.Zahod(context.Background(), true, false)
		if izmenilos {
			t.Fatalf("заход %d: обстановка сменилась раньше срока", i)
		}
		if tek != Neizvestno {
			t.Fatalf("заход %d: текущая = %v, ждал Neizvestno до набора подтверждений", i, tek)
		}
	}

	_, izmenilos, tek := a.Zahod(context.Background(), true, false)
	if !izmenilos {
		t.Fatalf("на %d-м подтверждении обстановка обязана смениться", Podtverzhdeniy)
	}
	if tek != Doma {
		t.Fatalf("текущая = %v, хочу Doma", tek)
	}
	if trafik.zvonkov != Podtverzhdeniy {
		t.Fatalf("зонд трафика позван %d раз, ждал %d", trafik.zvonkov, Podtverzhdeniy)
	}
}

// TestAvtorezhimZahodNeSprashivaetTrafikBezDns: если DNS уже сказал "не
// дома", прямой трафик спрашивать незачем — Reshit всё равно вернёт VneDoma.
func TestAvtorezhimZahodNeSprashivaetTrafikBezDns(t *testing.T) {
	trafik := &fakeTrafik{izmereno: true, proshel: true}
	a := &Avtorezhim{
		Dns:       fakeDns{doma: false},
		Trafik:    trafik,
		Zadvizhka: NovayaZadvizhka(Neizvestno),
	}
	_, _, _ = a.Zahod(context.Background(), true, false)
	if trafik.zvonkov != 0 {
		t.Fatalf("зонд трафика позван %d раз без признака DNS — не должен звониться вовсе", trafik.zvonkov)
	}
}

// TestAvtorezhimZahodOshibkaDnsEtoNeZnayu: резолвер отказал целиком —
// признака дома нет И измерения не было. Раньше здесь стоял «безопасный
// дефолт: не дома», и ровно из него вырастала авария 28.08 (роуминг между
// репитером и роутером, немой резолвер при живом интернете) — теперь ошибка
// зонда даёт «не знаю», см. molchaniye_rezolvera_test.go.
func TestAvtorezhimZahodOshibkaDnsEtoNeZnayu(t *testing.T) {
	a := &Avtorezhim{
		Dns:       fakeDns{doma: false, err: context.DeadlineExceeded},
		Trafik:    &fakeTrafik{},
		Zadvizhka: NovayaZadvizhka(Neizvestno),
	}
	n, _, _ := a.Zahod(context.Background(), true, false)
	if n.DnsPriznakDoma {
		t.Fatalf("наблюдение при ошибке DNS: %+v, ждал DnsPriznakDoma=false", n)
	}
	if !n.DnsMolchit {
		t.Fatalf("наблюдение при ошибке DNS: %+v, ждал DnsMolchit=true — измерения не было", n)
	}
	if got := Reshit(n); got != Neizvestno {
		t.Fatalf("Reshit при ошибке DNS = %v, хочу Neizvestno", got)
	}
}

// TestAvtorezhimZahodBezSeti: сети физически нет — Neizvestno, зонды не зовём.
func TestAvtorezhimZahodBezSeti(t *testing.T) {
	trafik := &fakeTrafik{}
	dnsZvali := false
	a := &Avtorezhim{
		Dns:       dnsSchitalka{&dnsZvali},
		Trafik:    trafik,
		Zadvizhka: NovayaZadvizhka(Doma), // стояли дома
	}
	_, izmenilos, tek := a.Zahod(context.Background(), false, false)
	if dnsZvali {
		t.Fatal("сети нет, а DNS-зонд всё равно позвали")
	}
	if trafik.zvonkov != 0 {
		t.Fatal("сети нет, а зонд трафика всё равно позвали")
	}
	// Стояли дома, наблюдение Neizvestno — задвижка не переключает сразу
	// (нужны Podtverzhdeniy подряд), так что первым заходом ничего не меняется.
	if izmenilos {
		t.Fatal("одно наблюдение без сети не должно сразу менять обстановку")
	}
	if tek != Doma {
		t.Fatalf("текущая = %v, до набора подтверждений должна остаться Doma", tek)
	}
}

type dnsSchitalka struct{ zvali *bool }

func (d dnsSchitalka) DomaPoDns(ctx context.Context) (bool, error) {
	*d.zvali = true
	return true, nil
}

// seqDns — DNS-зонд с заранее заданной последовательностью ответов
// (последний повторяется, если заходов больше, чем элементов).
type seqDns struct {
	otvety []bool
	i      int
}

func (d *seqDns) DomaPoDns(ctx context.Context) (bool, error) {
	v := d.otvety[d.i]
	if d.i < len(d.otvety)-1 {
		d.i++
	}
	return v, nil
}

// seqTrafik — зонд трафика с заранее заданной последовательностью ответов,
// считает звонки.
type seqTrafik struct {
	otvety []struct{ izmereno, proshel bool }
	i      int
	zvonki int
}

func (f *seqTrafik) Proshel(ctx context.Context) (bool, bool) {
	f.zvonki++
	r := f.otvety[f.i]
	if f.i < len(f.otvety)-1 {
		f.i++
	}
	return r.izmereno, r.proshel
}

// TestAvtorezhimPriznakPamyatDerzhitDoOprovergnutiyaIlZabyvaet — перенос
// HomeSign.stands/rememberHomeSign/forgetHomeSign с телефона: DNS, честно
// не нашедший подмены сразу после того, как её видели, признак не
// отменяет — трафик всё равно спрашивается, — а вот трафик, который
// доказанно не прошёл, память стирает, и следующий такой же заход трафик
// уже не спрашивает вовсе.
func TestAvtorezhimPriznakPamyatDerzhitDoOprovergnutiyaIlZabyvaet(t *testing.T) {
	dns := &seqDns{otvety: []bool{true, false, false, false}}
	trafik := &seqTrafik{otvety: []struct{ izmereno, proshel bool }{
		{true, true},  // заход 1: подтвердил
		{true, true},  // заход 2: подтвердил — память ещё в силе
		{true, false}, // заход 3: не прошёл — память опровергнута делом
	}}
	a := &Avtorezhim{
		Dns:       dns,
		Trafik:    trafik,
		Zadvizhka: NovayaZadvizhka(Neizvestno),
		Priznak:   NovyyPriznakPamyat(),
	}

	n1, _, _ := a.Zahod(context.Background(), true, false)
	if !n1.DnsPriznakDoma || trafik.zvonki != 1 {
		t.Fatalf("заход 1: %+v, звонков трафику %d — ждал признак=true, звонок 1", n1, trafik.zvonki)
	}

	n2, _, _ := a.Zahod(context.Background(), true, false)
	if !n2.DnsPriznakDoma {
		t.Fatalf("заход 2: DNS сейчас не нашёл подмены, но признак видели только что — ждал DnsPriznakDoma=true по памяти, получил %+v", n2)
	}
	if trafik.zvonki != 2 {
		t.Fatalf("заход 2: звонков трафику %d, ждал 2 — трафик обязан спрашиваться, пока признак стоит по памяти", trafik.zvonki)
	}

	n3, _, _ := a.Zahod(context.Background(), true, false)
	if !n3.DnsPriznakDoma || trafik.zvonki != 3 {
		t.Fatalf("заход 3: %+v, звонков трафику %d — память ещё стояла ДО этого захода", n3, trafik.zvonki)
	}

	n4, _, _ := a.Zahod(context.Background(), true, false)
	if n4.DnsPriznakDoma {
		t.Fatalf("заход 4: %+v — заход 3 не прошёл трафиком, память обязана была стереться", n4)
	}
	if trafik.zvonki != 3 {
		t.Fatalf("заход 4: звонков трафику %d, ждал 3 — памяти нет, DNS сказал «не дома», трафик спрашивать незачем", trafik.zvonki)
	}
}

// TestAvtorezhimPriznakPamyatSteretsyaDoverennymZahodom — сигнал смены сети
// (dovereno==true, см. Sledchik) стирает память признака ДО этого захода:
// здесь такого объекта, как Network на телефоне, нет, и это единственный
// маркер «сеть уже не та» (см. Avtorezhim.Zahod).
func TestAvtorezhimPriznakPamyatSteretsyaDoverennymZahodom(t *testing.T) {
	dns := &seqDns{otvety: []bool{true, false}}
	trafik := &seqTrafik{otvety: []struct{ izmereno, proshel bool }{{true, true}}}
	a := &Avtorezhim{
		Dns:       dns,
		Trafik:    trafik,
		Zadvizhka: NovayaZadvizhka(Neizvestno),
		Priznak:   NovyyPriznakPamyat(),
	}

	if n1, _, _ := a.Zahod(context.Background(), true, false); !n1.DnsPriznakDoma {
		t.Fatalf("заход 1: %+v, ждал признак=true", n1)
	}

	n2, _, _ := a.Zahod(context.Background(), true, true) // dovereno — сеть сменилась
	if n2.DnsPriznakDoma {
		t.Fatalf("заход 2 (доверенный): %+v — сигнал смены сети обязан был стереть память признака старой сети", n2)
	}
	if trafik.zvonki != 1 {
		t.Fatalf("заход 2: звонков трафику %d, ждал 1 — памяти нет, трафик спрашивать незачем", trafik.zvonki)
	}
}
