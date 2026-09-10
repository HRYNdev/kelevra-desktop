package avtorezhim

import (
	"context"
	"errors"
	"testing"
)

// Беда с машины человека 09.09.2026, дословно из его журнала на мобильной
// точке (ноутбук, 0.6.64):
//
//	15:59:16 поднят туннель (адаптер "tun125")
//	15:59:17 авторежим: спрашивал 10.184.67.105:53 — подмен 3
//	         [youtube.com=198.18.0.118 discord.com=198.18.0.119 ...]
//	15:59:18 авторежим: обстановка сменилась на дома
//	15:59:18 авторежим: обстановка «дома» — опускаю защиту
//
// 198.18.х — это фейковые адреса НАШЕГО ЖЕ ядра: через секунду после подъёма
// туннеля оно перехватывает порт 53 независимо от того, куда адресован
// запрос, и зонд видит собственную подмену вместо чужой. Так восемь раз
// подряд, период девять секунд. Единственный заход, ушедший через три с
// половиной минуты после подъёма, ответил правильно — «вне дома».
//
// Лечение: дом опознаётся по аппаратному номеру шлюза, а он берётся с
// канального уровня, куда туннель не дотягивается вовсе.

func TestShlyuzReshaetRanshePodmenyDns(t *testing.T) {
	// Ровно та мобильная точка: DNS-поля говорят «дома» (подмена есть,
	// трафик прошёл) — именно то сочетание, которое давало ложное «дома».
	pravda := true
	n := Nablyudeniye{
		EstSet:         true,
		DnsPriznakDoma: true,
		TrafikPryamoy:  &pravda,
		ShlyuzOpoznan:  true,
		ShlyuzDoma:     false,
	}
	if got := Reshit(n); got != VneDoma {
		t.Fatalf("вердикт %v, хочу %v: шлюз не домашний, подмена DNS — наша собственная", got, VneDoma)
	}
}

func TestShlyuzDomaPobezhdaetMolchaniyeDns(t *testing.T) {
	// Дома резолвер молчит по 4-7 минут при переходе между репитером и
	// роутером (замер 28.08). Раньше это давало Neizvestno и «не знаю где
	// мы»; со шлюзом такой заход отвечает.
	n := Nablyudeniye{
		EstSet:        true,
		DnsMolchit:    true,
		ShlyuzOpoznan: true,
		ShlyuzDoma:    true,
	}
	if got := Reshit(n); got != Doma {
		t.Fatalf("вердикт %v, хочу %v: шлюз домашний, молчание DNS роли не играет", got, Doma)
	}
}

func TestBezShlyuzaReshayutPrezhniePravila(t *testing.T) {
	// Номер шлюза не прочитан (сеть только что сменилась, ARP пуст) —
	// правила остаются прежними, ни одно из них не тронуто.
	n := Nablyudeniye{EstSet: true, DnsPriznakDoma: false}
	if got := Reshit(n); got != VneDoma {
		t.Fatalf("вердикт %v, хочу %v", got, VneDoma)
	}
	n = Nablyudeniye{EstSet: true, DnsMolchit: true}
	if got := Reshit(n); got != Neizvestno {
		t.Fatalf("вердикт %v, хочу %v", got, Neizvestno)
	}
}

func TestEtoDomashniyShlyuzNeSporitOZapisi(t *testing.T) {
	domashnie := []string{"80:af:ca:82:17:91"}
	for _, zapis := range []string{
		"80:af:ca:82:17:91",
		"80-AF-CA-82-17-91", // так пишет Windows
		"80AF.CA82.1791",    // так пишут сетевые железки
		"80afca821791",
	} {
		if !EtoDomashniyShlyuz(zapis, domashnie) {
			t.Errorf("запись %q не опознана как домашний шлюз", zapis)
		}
	}
	for _, chuzhoy := range []string{
		"80:af:ca:82:17:92", // соседний номер того же роутера (радио 5 ГГц)
		"02:1a:11:f0:00:01", // телефон в режиме модема
		"",
		"мусор",
		"80:af:ca:82:17", // обрезок
	} {
		if EtoDomashniyShlyuz(chuzhoy, domashnie) {
			t.Errorf("запись %q ошибочно принята за домашний шлюз", chuzhoy)
		}
	}
}

// Заход, прочитавший шлюз, не должен трогать DNS вовсе: это и есть та
// экономия, ради которой признак и меняли — ни одного запроса в сеть,
// которую мы ещё не опознали.
func TestZahodSoShlyuzomNeSprashivaetDns(t *testing.T) {
	sprosili := false
	a := &Avtorezhim{
		Dns:            zondDns{doma: true, sprosili: &sprosili},
		Trafik:         zondTrafika{proshel: true},
		Zadvizhka:      NovayaZadvizhka(Neizvestno),
		MakiShlyuzovFunc: func() ([]string, error) { return []string{"80:af:ca:82:17:91"}, nil },
	}
	n, _, sost := a.Zahod(context.Background(), true, true)
	if !n.ShlyuzOpoznan || !n.ShlyuzDoma {
		t.Fatalf("наблюдение %+v — шлюз обязан быть опознан домашним", n)
	}
	if sost != Doma {
		t.Fatalf("обстановка %v, хочу %v", sost, Doma)
	}
	if sprosili {
		t.Fatal("шлюз уже ответил на вопрос, а заход всё равно сходил в DNS")
	}
}

func TestZahodBezShlyuzaPadaetNaDns(t *testing.T) {
	sprosili := false
	a := &Avtorezhim{
		Dns:            zondDns{doma: false, sprosili: &sprosili},
		Zadvizhka:      NovayaZadvizhka(Neizvestno),
		MakiShlyuzovFunc: func() ([]string, error) { return nil, errors.New("ARP пуст") },
	}
	n, _, sost := a.Zahod(context.Background(), true, true)
	if n.ShlyuzOpoznan {
		t.Fatalf("наблюдение %+v — шлюз не читался, опознанным он быть не может", n)
	}
	if !sprosili {
		t.Fatal("шлюз не прочитан, а DNS так и не спросили")
	}
	if sost != VneDoma {
		t.Fatalf("обстановка %v, хочу %v", sost, VneDoma)
	}
}

// Боевой конструктор обязан ставить чтение шлюза: тесты собирают Avtorezhim
// литералом и остаются на DNS-пути, поэтому забытая строчка в Novyy() не
// уронила бы ни одной проверки, кроме этой.
func TestNovyyBeryotShlyuz(t *testing.T) {
	if Novyy().MakiShlyuzovFunc == nil {
		t.Fatal("боевой авторежим не читает номер шлюза — главный признак дома выключен")
	}
}

// zondDns — подставной DNS-зонд, отмечающий сам факт обращения.
type zondDns struct {
	doma     bool
	sprosili *bool
}

func (z zondDns) DomaPoDns(context.Context) (bool, error) {
	if z.sprosili != nil {
		*z.sprosili = true
	}
	return z.doma, nil
}

type zondTrafika struct{ proshel bool }

func (z zondTrafika) Proshel(context.Context) (bool, bool) { return true, z.proshel }

// Беда 09.09.2026 живьём: человек сидит дома, домашний роутер в списке
// шлюзов ВТОРОЙ (первым идёт виртуальный адаптер Radmin VPN), и авторежим
// объявляет «вне дома» и поднимает туннель посреди квартиры.
//
// Проверка идёт по обоим порядкам нарочно: беда была именно в том, что
// вердикт выносил ПЕРВЫЙ найденный шлюз, поэтому «домашний первым» проходил
// и раньше, а «домашний вторым» — нет. Проверять только удобный порядок
// значило бы повторить ту же слепоту в тесте.
func TestDomashniyShlyuzOpoznayotsyaNaLyubomMeste(t *testing.T) {
	domashnie := []string{"80:af:ca:82:17:91"}
	radmin := "02:00:00:00:51:00" // Famatech Radmin VPN, замер с машины человека
	dom := "80:af:ca:82:17:91"

	for _, sluchay := range []struct {
		imya string
		maki []string
		hochu bool
	}{
		{"домашний первым", []string{dom, radmin}, true},
		{"домашний вторым", []string{radmin, dom}, true},
		{"домашний третьим", []string{radmin, "02:1a:11:f0:00:01", dom}, true},
		{"один домашний", []string{dom}, true},
		{"домашнего нет", []string{radmin, "02:1a:11:f0:00:01"}, false},
		{"список пуст", nil, false},
	} {
		if got := EstDomashniyShlyuz(sluchay.maki, domashnie); got != sluchay.hochu {
			t.Errorf("%s: %v — вердикт «дома» %v, хочу %v", sluchay.imya, sluchay.maki, got, sluchay.hochu)
		}
	}
}

// Тот же случай, но целым заходом: важно не только правило, но и то, что
// Zahod до него доносит ВЕСЬ список, а не первый номер.
func TestZahodVidytDomashniyShlyuzZaChuzhim(t *testing.T) {
	sprosili := false
	a := &Avtorezhim{
		Dns:       zondDns{doma: false, sprosili: &sprosili},
		Zadvizhka: NovayaZadvizhka(Neizvestno),
		MakiShlyuzovFunc: func() ([]string, error) {
			// Порядок ровно как у Windows на машине человека 09.09.
			return []string{"02:00:00:00:51:00", "80:af:ca:82:17:91"}, nil
		},
	}
	n, _, sost := a.Zahod(context.Background(), true, true)
	if !n.ShlyuzOpoznan || !n.ShlyuzDoma {
		t.Fatalf("наблюдение %+v — домашний шлюз есть в списке, он обязан быть опознан", n)
	}
	if sost != Doma {
		t.Fatalf("обстановка %v, хочу %v: человек дома, туннель поднимать не за чем", sost, Doma)
	}
	if sprosili {
		t.Fatal("шлюз ответил на вопрос, а заход всё равно сходил в DNS")
	}
}
