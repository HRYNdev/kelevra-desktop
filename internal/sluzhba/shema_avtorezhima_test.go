package sluzhba

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Заказанная схема «одна кнопка» (27.08): нажал «подключить» — программа
// сама определяет обстановку: дома «режим ожидания», вне дома включается.
// Здесь проверяется
// проводка службы: кто включает автомат, кто его выключает и что после этого
// клиент перестаёт решать за человека. Само распознавание обстановки и
// приведение защиты к ней проверены сценариями в internal/avtorezhim
// (shema_odna_knopka_test.go), на подставных зондах и без единого сетевого запроса.

func sostoyanieStenda(t *testing.T, s *Sluzhba) otvetSostoyaniya {
	t.Helper()
	m := s.Obsluzhit()
	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	var o otvetSostoyaniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал /api/sostoyanie: %v", err)
	}
	return o
}

// TestPodklyuchitBezVyboraChelovekaSamVklyuchaetAvtomat — свежая установка:
// человек тумблера авторежима не касался ни разу (Avtorezhim == false,
// RuchnoyVybor == false) и просто жмёт «Подключиться». Дальше должен решать
// клиент: автомат включается сам, а дома защита НЕ поднимается — это и есть
// «режим ожидания».
//
// До этой правки нажатие на свежей установке уходило в безусловный ручной
// подъём: туннель вставал дома и не опускался уже никогда, потому что
// автомат вообще не крутился. Ровно на это жалоба 22.08 («включил впн, авто
// режим, ну нихуя авто режим не увидел что я дома нахожусь и не выключился»).
func TestPodklyuchitBezVyboraChelovekaSamVklyuchaetAvtomat(t *testing.T) {
	a := &avtorezhim.Avtorezhim{
		Dns:       fakeDnsKnopka{doma: true},
		Trafik:    fakeTrafikKnopka{proshel: true},
		Zadvizhka: avtorezhim.NovayaZadvizhka(avtorezhim.Neizvestno),
	}
	s, popytok := podklyuchitStend(t, a, time.Second)
	// podklyuchitStend ставит тумблер включённым — здесь проверяется именно
	// свежая установка, где человек его ещё не трогал.
	s.Nastroyki.Avtorezhim = false
	s.Nastroyki.RuchnoyVybor = false

	postPodklyuchitIProverit(t, s)

	if !s.Nastroyki.Avtorezhim {
		t.Fatal("«Подключиться» на свежей установке обязана включить автомат — дальше решает клиент")
	}
	if *popytok != 0 {
		t.Fatalf("обстановка «дома», а защита всё-таки поднялась (%d раз) — это не режим ожидания", *popytok)
	}

	o := sostoyanieStenda(t, s)
	if !o.OzhidanieDoma {
		t.Fatalf("окну не сказано про режим ожидания: %+v", o)
	}
	if o.AvtorezhimPolozhenie != "дома — режим ожидания" {
		t.Fatalf("положение авторежима = %q, хочу «дома — режим ожидания»", o.AvtorezhimPolozhenie)
	}
	if o.AvtorezhimRuchnoy {
		t.Fatal("человек тумблера не касался — ручным выбором это считать нельзя")
	}
}

// TestOtklyuchitRukamiGasitAvtomatIBolsheNeReshaetSam — «человек выключил
// автомат руками → клиент больше не решает сам».
//
// Без этого нажатие «Отключить» вне дома не значило ничего: приведение
// (avtorezhimKolbek) поднимало защиту обратно на ближайшем же заходе, и кнопка
// выглядела сломанной. Телефон делает то же самое — chooseManually() ставит
// Settings.autoModeEnabled = false и круг перестаёт решать (AutoMode.kt:1035).
func TestOtklyuchitRukamiGasitAvtomatIBolsheNeReshaetSam(t *testing.T) {
	s := gotovStendLestnicy(t)
	t.Cleanup(s.OstanovitAvtorezhim)

	// Человек уже в автоматическом режиме, служитель крутится.
	if err := s.vklyuchitAvtorezhim(avtorezhim.VneDoma); err != nil {
		t.Fatalf("не включил авторежим: %v", err)
	}
	s.avtorezhimZamok.Lock()
	krutitsya := s.avtorezhimOtmena != nil
	s.avtorezhimZamok.Unlock()
	if !krutitsya {
		t.Fatal("служитель авторежима не поднялся — проверять нечего")
	}

	m := s.Obsluzhit()
	r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/otklyuchit", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("POST /api/otklyuchit вернул код %d: %s", w.Code, w.Body.String())
	}

	if s.Nastroyki.Avtorezhim {
		t.Fatal("«Отключить» не выключила автомат — вне дома он поднимет защиту обратно, и кнопка не значит ничего")
	}
	if !s.Nastroyki.RuchnoyVybor {
		t.Fatal("«Отключить» не запомнила ручной выбор — следующее «Подключиться» снова включит автомат за человека")
	}
	s.avtorezhimZamok.Lock()
	krutitsya = s.avtorezhimOtmena != nil
	s.avtorezhimZamok.Unlock()
	if krutitsya {
		t.Fatal("служитель авторежима остался крутиться после ручного «Отключить»")
	}

	// И следующее нажатие «Подключиться» автомат обратно НЕ включает: решение
	// человека держится, пока он сам не вернёт тумблер.
	popytok := 0
	s.zapustitYadro = func(ctx context.Context) error { popytok++; return nil }
	postPodklyuchitIProverit(t, s)
	if s.Nastroyki.Avtorezhim {
		t.Fatal("после ручного «Отключить» кнопка «Подключиться» снова включила автомат за человека")
	}
	if popytok != 1 {
		t.Fatalf("ручной режим: защита обязана подняться безусловно ровно 1 раз, поднята %d раз(а)", popytok)
	}

	o := sostoyanieStenda(t, s)
	if !o.AvtorezhimRuchnoy {
		t.Fatalf("окну не сказано, что решает человек: %+v", o)
	}
	if o.AvtorezhimPolozhenie != "решаете вы: автомат выключен" {
		t.Fatalf("положение авторежима = %q, хочу «решаете вы: автомат выключен»", o.AvtorezhimPolozhenie)
	}
}

// TestPolozhenieAvtorezhima — строка схемы для окна таблицей случаев. Именно
// по ней человек видит переход: обстановка сменилась, а ядро ещё не догнало —
// и это сказано словами, а не угадывается по цвету круга (требование 27.08).
func TestPolozhenieAvtorezhima(t *testing.T) {
	rabotaet := string(yadro.Rabotaet)
	stoit := string(yadro.Stoit)
	doma := avtorezhim.Doma.String()
	vne := avtorezhim.VneDoma.String()

	sluchai := []struct {
		nazvanie   string
		vklyuchen  bool
		ruchnoy    bool
		obstanovka string
		sost       string
		slep       string
		hochu      string
	}{
		{"дома, защиты нет — режим ожидания", true, false, doma, stoit, "", "дома — режим ожидания"},
		{"дома, ядро ещё работает — переход виден", true, false, doma, rabotaet, "", "дома — опускаю защиту"},
		{"вне дома под защитой", true, false, vne, rabotaet, "", "вне дома — защита включена"},
		{"вне дома, защиты ещё нет — переход виден", true, false, vne, stoit, "", "вне дома — поднимаю защиту"},
		{"обстановка ещё не известна", true, false, "", stoit, "", "определяю, где вы"},
		{"длящаяся слепота названа человеку", true, false, doma, stoit, "физический сетевой адаптер не найден",
			"не понимаю, где вы: физический сетевой адаптер не найден"},
		{"человек решает сам", false, true, doma, rabotaet, "", "решаете вы: автомат выключен"},
		{"человек решает сам — даже при включённом тумблере это его слово", true, true, doma, rabotaet, "", "решаете вы: автомат выключен"},
		{"автомат не крутится и выбора не было — сказать нечего", false, false, "", stoit, "", ""},
	}
	for _, s := range sluchai {
		t.Run(s.nazvanie, func(t *testing.T) {
			got := polozhenieAvtorezhima(s.vklyuchen, s.ruchnoy, s.obstanovka, s.sost, s.slep)
			if got != s.hochu {
				t.Fatalf("polozhenieAvtorezhima = %q, хочу %q", got, s.hochu)
			}
		})
	}
}

// TestVyhodDlyaOkna — жалоба 27.08: россыпь конкретных узлов на главном
// экране не нужна, нужен простой выбор авторежима. Пока человек в
// список выходов не лазил, выход ровно один и называется «Автоматически»;
// выбранный руками узел показывается как есть, иначе непонятно, почему
// автоматика перестала работать.
func TestVyhodDlyaOkna(t *testing.T) {
	t.Run("человек не выбирал — Автоматически", func(t *testing.T) {
		avto, imya := vyhodDlyaOkna(nil)
		if !avto || imya != ImyaAvtoVyhoda {
			t.Fatalf("получил (%v, %q), хочу (true, %q)", avto, imya, ImyaAvtoVyhoda)
		}
	})
	t.Run("человек выбрал узел — виден его выбор", func(t *testing.T) {
		avto, imya := vyhodDlyaOkna(map[string]string{"Выбор": "Нидерланды прямой"})
		if avto || imya != "Нидерланды прямой" {
			t.Fatalf("получил (%v, %q), хочу (false, «Нидерланды прямой»)", avto, imya)
		}
	})
	t.Run("выбор в нескольких группах — берётся устойчиво один и тот же", func(t *testing.T) {
		uzly := map[string]string{"Ядро": "запасной", "Выбор": "Нидерланды прямой"}
		avto, imya := vyhodDlyaOkna(uzly)
		if avto {
			t.Fatal("выбор человека принят за автоматический")
		}
		for i := 0; i < 20; i++ {
			if _, opyat := vyhodDlyaOkna(uzly); opyat != imya {
				t.Fatalf("имя выхода скачет между заходами: %q и %q", imya, opyat)
			}
		}
	})
}

// TestSostoyanieOtdayotVyborVyhoda — то же самое, но через настоящую ручку:
// окну есть что нарисовать в строке выхода, не спрашивая /api/uzly.
func TestSostoyanieOtdayotVyborVyhoda(t *testing.T) {
	s := stend(t)
	if o := sostoyanieStenda(t, s); !o.VyhodAvto || o.VyhodImya != ImyaAvtoVyhoda {
		t.Fatalf("свежая установка: vyhod_avto=%v vyhod_imya=%q, хочу true/%q", o.VyhodAvto, o.VyhodImya, ImyaAvtoVyhoda)
	}
	s.zapomnitUzel("Выбор", "Нидерланды запасной")
	if o := sostoyanieStenda(t, s); o.VyhodAvto || o.VyhodImya != "Нидерланды запасной" {
		t.Fatalf("после ручного выбора: vyhod_avto=%v vyhod_imya=%q", o.VyhodAvto, o.VyhodImya)
	}
}

// TestSlovoChelovekaByotResheniyeAvtomataNachatoyeRanshe — гонка, найденная
// приёмкой: человек жмёт «Отключить», а заход авторежима, УЖЕ ушедший в полёт,
// доводит своё решение до конца и поднимает защиту обратно. Со стороны это
// «нажал отключить — а оно само включилось».
//
// Проверяется сам механизм, а не везение планировщика: заход поколения,
// которое человек уже перебил, зовётся напрямую и обязан не сделать НИЧЕГО.
// Сценарный тест (TestOtklyuchitRukamiGasitAvtomatIBolsheNeReshaetSam) ловит
// ту же беду через настоящие ручки, но зависит от того, кто кого обгонит;
// этот — не зависит ни от чего.
func TestSlovoChelovekaByotResheniyeAvtomataNachatoyeRanshe(t *testing.T) {
	s := gotovStendLestnicy(t)
	t.Cleanup(s.OstanovitAvtorezhim)
	podyomov := 0
	s.zapustitYadro = func(ctx context.Context) error { podyomov++; return nil }

	// Человек включил автомат — служитель заведён на своём поколении.
	if err := s.vklyuchitAvtorezhim(avtorezhim.VneDoma); err != nil {
		t.Fatalf("не включил авторежим: %v", err)
	}
	s.avtorezhimZamok.Lock()
	stroePokolenie := s.avtorezhimPokolenie
	s.avtorezhimZamok.Unlock()

	// ...и тут же передумал.
	s.OstanovitAvtorezhim()
	podyomov = 0

	// Заход, начатый ДО нажатия, добрался до защиты только сейчас.
	s.avtorezhimPrimenit(context.Background(), stroePokolenie, avtorezhim.VneDoma, false)
	if podyomov != 0 {
		t.Fatalf("заход старого поколения поднял защиту %d раз(а) после того, как человек выключил автомат", podyomov)
	}

	// Никакого залипания: человек снова включил автомат — свежее поколение
	// работает как ни в чём не бывало.
	if err := s.vklyuchitAvtorezhim(avtorezhim.VneDoma); err != nil {
		t.Fatalf("не включил авторежим второй раз: %v", err)
	}
	s.avtorezhimZamok.Lock()
	svezheePokolenie := s.avtorezhimPokolenie
	s.avtorezhimZamok.Unlock()
	if svezheePokolenie == stroePokolenie {
		t.Fatal("поколение не выросло — сверять будет нечего")
	}
	podyomov = 0
	s.avtorezhimPrimenit(context.Background(), svezheePokolenie, avtorezhim.VneDoma, false)
	if podyomov != 1 {
		t.Fatalf("свежее поколение подняло защиту %d раз(а), хочу 1 — появилось залипание", podyomov)
	}

	// Отменённый ctx служителя бьёт так же, как выросшее поколение: гасить
	// служителя и оставлять его решения в силе нельзя.
	otmenyonnyy, otmena := context.WithCancel(context.Background())
	otmena()
	podyomov = 0
	if err := s.OpustitZashchitu(); err != nil {
		t.Logf("опускание защиты стенда: %v", err)
	}
	s.avtorezhimPrimenit(otmenyonnyy, svezheePokolenie, avtorezhim.VneDoma, false)
	if podyomov != 0 {
		t.Fatalf("заход с отменённым ctx поднял защиту %d раз(а)", podyomov)
	}
}
