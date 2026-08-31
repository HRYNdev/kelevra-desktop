package avtorezhim

import (
	"context"
	"errors"
	"testing"
)

// Молчание резолвера — это «не знаю», а не «не дома».
//
// Разбор 28.08 (приборный, телефон хозяина): телефон роумит между репитером
// и роутером, старая точка держит ассоциацию до 480 секунд, резолвер молчит
// 4-7 минут ПРИ ЖИВОМ интернете. До этой правки Avtorezhim.Zahod на строке
// «резолвер не ответил вовсе — не дома, безопасный дефолт» выбрасывал
// различие, которое DnsZond.DomaPoDns честно проводил, Reshit по
// !DnsPriznakDoma отдавал VneDoma, а Zadvizhka пропускала доверенное
// наблюдение с одного раза — и один немой заход поднимал туннель посреди
// дома.
//
// Тесты ниже написаны на СЦЕНАРИИ (что видит человек), а не на реализацию:
// поля наблюдения тут только средство, проверяются вердикт и обстановка.

// nastroyaemyDns — DNS-зонд, ответ которого тест меняет по ходу сценария
// (fakeDns в avtorezhim_test.go — значение, менять его на лету нельзя).
type nastroyaemyDns struct {
	doma bool
	err  error
}

func (d *nastroyaemyDns) DomaPoDns(ctx context.Context) (bool, error) { return d.doma, d.err }

// oshibkaNemogoRezolvera — то же, что возвращает настоящий DnsZond.DomaPoDns,
// когда не ответил ни один контрольный домен (dns_zond.go, otvetili == 0).
var oshibkaNemogoRezolvera = errors.New("резолвер не ответил ни на один из 3 доменов")

// TestMolchaniyeRezolveraDayotNeizvestnoINeMenyaetObstanovku — сценарий 1:
// резолверы молчат, туннель НЕ поднят (режим прокси: TunnelPodnyat отдаёт
// false, значит слепота ZondSlep не сработает никогда), заход доверенный —
// то есть самый опасный случай. Вердикт обязан быть Neizvestno, обстановка —
// та же, что была.
func TestMolchaniyeRezolveraDayotNeizvestnoINeMenyaetObstanovku(t *testing.T) {
	trafik := &fakeTrafik{izmereno: true, proshel: true}
	a := &Avtorezhim{
		Dns:           &nastroyaemyDns{doma: false, err: oshibkaNemogoRezolvera},
		Trafik:        trafik,
		Zadvizhka:     NovayaZadvizhka(Doma), // стояли дома
		TunnelPodnyat: func() bool { return false },
	}

	n, izmenilos, tek := a.Zahod(context.Background(), true, true)

	if n.ZondSlep {
		t.Fatal("режим прокси, туннель не поднят — заход не может быть слепым по ZondSlep; значит немой резолвер обязан ловиться отдельно")
	}
	if got := Reshit(n); got != Neizvestno {
		t.Fatalf("вердикт по немому резолверу = %v, хочу Neizvestno (молчание — не «не дома»)", got)
	}
	if izmenilos {
		t.Fatal("немой резолвер сменил обстановку — а мерить было нечего")
	}
	if tek != Doma {
		t.Fatalf("обстановка = %v, хочу Doma — при «не знаю» состояние не меняется", tek)
	}
	if trafik.zvonkov != 0 {
		t.Fatalf("зонд трафика позван %d раз(а) при немом DNS — мерить нечего", trafik.zvonkov)
	}
}

// TestRezolverOtvetilChuzhoySetyuEtoVneDoma — сценарий 2: резолвер ОТВЕТИЛ и
// домашней подмены в ответе нет. Это честное «не дома», и правка не смеет
// тут ничего менять: обстановка обязана уйти в VneDoma тем же доверенным
// заходом, что и раньше.
func TestRezolverOtvetilChuzhoySetyuEtoVneDoma(t *testing.T) {
	a := &Avtorezhim{
		Dns:       &nastroyaemyDns{doma: false},
		Trafik:    &fakeTrafik{},
		Zadvizhka: NovayaZadvizhka(Doma),
	}

	n, izmenilos, tek := a.Zahod(context.Background(), true, true)

	if got := Reshit(n); got != VneDoma {
		t.Fatalf("вердикт при честном ответе «чужая сеть» = %v, хочу VneDoma", got)
	}
	if !izmenilos || tek != VneDoma {
		t.Fatalf("izmenilos=%v, обстановка=%v, хочу true/VneDoma — доверенный заход с честным ответом работает как раньше", izmenilos, tek)
	}
}

// TestRezolverOtvetilDomashneySetyuEtoDoma — сценарий 3: резолвер ответил
// домашней подменой, трафик наружу прошёл. Это дом, как и был.
func TestRezolverOtvetilDomashneySetyuEtoDoma(t *testing.T) {
	a := &Avtorezhim{
		Dns:       &nastroyaemyDns{doma: true},
		Trafik:    &fakeTrafik{izmereno: true, proshel: true},
		Zadvizhka: NovayaZadvizhka(VneDoma),
	}

	n, izmenilos, tek := a.Zahod(context.Background(), true, true)

	if got := Reshit(n); got != Doma {
		t.Fatalf("вердикт при домашней подмене и прошедшем трафике = %v, хочу Doma", got)
	}
	if !izmenilos || tek != Doma {
		t.Fatalf("izmenilos=%v, обстановка=%v, хочу true/Doma", izmenilos, tek)
	}
}

// TestDoverennoyeMolchaniyeNeProhoditSOdnogoPodtverzhdeniya — сценарий 4.
// Роуминг между репитером и роутером — это как раз ДОВЕРЕННОЕ событие
// (Windows шлёт NotifyAddrChange, Sledchik его ловит), и именно доверие
// раньше пропускало один немой заход прямо в переключение защиты.
// Наблюдение, построенное на молчании, обязано набирать полные
// Podtverzhdeniy — как заход страховочного тикера.
func TestDoverennoyeMolchaniyeNeProhoditSOdnogoPodtverzhdeniya(t *testing.T) {
	// Сначала на самой задвижке — там живёт правило.
	z := NovayaZadvizhka(Doma)
	if izm := z.Predlozhit(Neizvestno, true); izm {
		t.Fatal("доверенное «не знаю» сменило обстановку с одного подтверждения")
	}
	if z.Tekushcheye() != Doma {
		t.Fatalf("обстановка = %v, хочу Doma", z.Tekushcheye())
	}
	if izm := z.Predlozhit(Neizvestno, true); izm {
		t.Fatal("двух подтверждений «не знаю» тоже мало")
	}
	if izm := z.Predlozhit(Neizvestno, true); !izm {
		t.Fatalf("на %d-м подряд «не знаю» обстановка обязана стать Neizvestno", Podtverzhdeniy)
	}

	// И тем же путём, каким это идёт в бою: доверенные заходы с немым DNS.
	a := &Avtorezhim{
		Dns:       &nastroyaemyDns{err: oshibkaNemogoRezolvera},
		Trafik:    &fakeTrafik{},
		Zadvizhka: NovayaZadvizhka(Doma),
	}
	for i := 1; i < Podtverzhdeniy; i++ {
		_, izmenilos, tek := a.Zahod(context.Background(), true, true)
		if izmenilos {
			t.Fatalf("заход %d: доверенный немой заход сменил обстановку раньше %d-го", i, Podtverzhdeniy)
		}
		if tek != Doma {
			t.Fatalf("заход %d: обстановка = %v, хочу Doma", i, tek)
		}
	}
	_, _, tek := a.Zahod(context.Background(), true, true)
	if tek != Neizvestno {
		t.Fatalf("после %d немых заходов подряд обстановка = %v, хочу Neizvestno (но НЕ VneDoma — туннель поднимать не на чем)", Podtverzhdeniy, tek)
	}
}

// TestRoumingMezhduRepiteromIRouteromNePodnimaetTunnel — сценарий 5, ради
// которого правка и делалась: «дома → молчание 5 минут → снова дома».
// Замкнутый цикл, как в бою: колбэк делает ровно то, что
// Sluzhba.avtorezhimKolbek (Doma — опустить защиту, VneDoma — поднять,
// Neizvestno — ничего). Туннель не смеет подняться НИ РАЗУ.
func TestRoumingMezhduRepiteromIRouteromNePodnimaetTunnel(t *testing.T) {
	dns := &nastroyaemyDns{doma: true}
	tunnelUp := false
	a := &Avtorezhim{
		Dns:       dns,
		Trafik:    &fakeTrafik{izmereno: true, proshel: true},
		Zadvizhka: NovayaZadvizhka(Doma),
		// Режим прокси: туннель не поднят, слепота ZondSlep недостижима —
		// значит немой резолвер обязан лечиться сам по себе, а не через неё.
		TunnelPodnyat: func() bool { return tunnelUp },
	}

	podyomov := 0
	var kolbekZvali []Sostoyanie
	zahod := func(dovereno bool) {
		_, izmenilos, tek := a.Zahod(context.Background(), true, dovereno)
		if !izmenilos {
			return
		}
		kolbekZvali = append(kolbekZvali, tek)
		switch tek {
		case Doma:
			tunnelUp = false
		case VneDoma:
			tunnelUp = true
			podyomov++
		}
	}

	// Человек дома, всё честно.
	zahod(false)
	if a.Zadvizhka.Tekushcheye() != Doma {
		t.Fatalf("до роуминга обстановка = %v, хочу Doma", a.Zadvizhka.Tekushcheye())
	}

	// Роуминг: ассоциация висит на старой точке, резолвер нем 5 минут при
	// живом интернете. Быстрые опросы идут раз в PauzaBystrogoOprosa (5 с) и
	// каждый доверенный — 60 заходов, худший возможный случай.
	dns.err = oshibkaNemogoRezolvera
	dns.doma = false
	for i := 0; i < 60; i++ {
		zahod(true)
		if tunnelUp {
			t.Fatalf("немой заход %d поднял туннель посреди домашней сети — авария 28.08 воспроизведена", i+1)
		}
	}

	// Ассоциация наконец переехала на роутер, резолвер ожил и снова видит дом.
	dns.err = nil
	dns.doma = true
	for i := 0; i < Podtverzhdeniy; i++ {
		zahod(true)
	}

	if podyomov != 0 {
		t.Fatalf("туннель поднимался %d раз(а) за сценарий роуминга, хочу 0; колбэки: %v", podyomov, kolbekZvali)
	}
	if tunnelUp {
		t.Fatal("после возврата домой туннель всё ещё поднят")
	}
	if a.Zadvizhka.Tekushcheye() != Doma {
		t.Fatalf("после возврата домой обстановка = %v, хочу Doma", a.Zadvizhka.Tekushcheye())
	}
	for _, s := range kolbekZvali {
		if s == VneDoma {
			t.Fatalf("колбэк хоть раз позван с VneDoma за весь роуминг: %v", kolbekZvali)
		}
	}
}

// TestReshitMolchaniyeOtdelnoOtChestnogoNeDoma — таблица на самом решателе:
// одно и то же DnsPriznakDoma=false обязано читаться по-разному в
// зависимости от того, было ли измерение вообще.
func TestReshitMolchaniyeOtdelnoOtChestnogoNeDoma(t *testing.T) {
	cases := []struct {
		imya  string
		n     Nablyudeniye
		hochu Sostoyanie
	}{
		{"резолвер ответил, дома нет — честное «не дома»", Nablyudeniye{EstSet: true}, VneDoma},
		{"резолвер промолчал — «не знаю»", Nablyudeniye{EstSet: true, DnsMolchit: true}, Neizvestno},
		{"сети нет вовсе — «не знаю» и без DNS", Nablyudeniye{EstSet: false, DnsMolchit: true}, Neizvestno},
		{"слепой заход перевешивает всё", Nablyudeniye{EstSet: true, ZondSlep: true, DnsMolchit: true}, Neizvestno},
		{"молчание перевешивает случайный признак дома", Nablyudeniye{EstSet: true, DnsMolchit: true, DnsPriznakDoma: true, TrafikPryamoy: bul(true)}, Neizvestno},
	}
	for _, c := range cases {
		t.Run(c.imya, func(t *testing.T) {
			if got := Reshit(c.n); got != c.hochu {
				t.Fatalf("Reshit(%+v) = %v, хочу %v", c.n, got, c.hochu)
			}
		})
	}
}
