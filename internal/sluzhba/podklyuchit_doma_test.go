package sluzhba

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// Заказ 28.08 (первая беда): при нажатии «Подключиться» программа не
// определяла, дома человек или нет. Кнопка «Подключиться» (podklyuchit,
// sluzhba.go)
// обязана сама одним доверенным заходом авторежима (domaSeychas) спросить
// обстановку и решить, поднимать ли защиту — а не делать это безусловно, но
// ТОЛЬКО пока человек уже выбрал авторежим (тумблер /api/avtorezhim
// включён). Эти тесты проверяют РОВНО эту развилку через настоящую
// HTTP-ручку /api/podklyuchit на включённом тумблере (podklyuchitStend),
// считая вызовы PodnyatZashchitu через s.zapustitYadro — тем же приёмом, что
// уже использует TestPervoePodklyuchenieSamoSprashivaetPrava
// (prava_avto_test.go).
//
// Вторая беда (28.08, тот же день): «когда программа определила что я дома,
// она не даёт включить защиту вручную» — ручной режим (тумблер выключен)
// вообще не спрашивает обстановку и подключает безусловно, см.
// TestPodklyuchitRuchnojRezhimPodnimaetBezuslovnoDazheDoma в конце файла.

// fakeDnsKnopka — подставной DNS-зонд для одиночного захода кнопки: тот же
// приём, что fakeDns в internal/avtorezhim/avtorezhim_test.go, но заведён
// здесь заново — тип оттуда не экспортирован.
type fakeDnsKnopka struct {
	doma bool
	err  error
}

func (f fakeDnsKnopka) DomaPoDns(ctx context.Context) (bool, error) { return f.doma, f.err }

// fakeTrafikKnopka — подставной зонд прямого трафика.
type fakeTrafikKnopka struct{ proshel bool }

func (f fakeTrafikKnopka) Proshel(ctx context.Context) (bool, bool) { return true, f.proshel }

// visjachiyDnsKnopka — зонд, который не отвечает вовсе: DomaPoDns виснет,
// пока не истечёт ctx (симулирует зависшую сеть), и тогда возвращает ошибку —
// ровно то, что и настоящий DnsZond при исчерпанном таймауте.
type visjachiyDnsKnopka struct{}

func (visjachiyDnsKnopka) DomaPoDns(ctx context.Context) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

// podklyuchitStend поднимает Sluzhba со стендовым ядром (как gotovStendLestnicy,
// без реальной сети), подменяет одиночный заход авторежима кнопки на a и
// считает через zapustitYadro, сколько раз реально позвана PodnyatZashchitu.
//
// Тумблер авторежима (s.Nastroyki.Avtorezhim) стенд ставит ВКЛЮЧЁННЫМ ещё до
// нажатия: с 28.08 (беда «когда программа определила что я дома, она не
// даёт включить защиту вручную») развилка «спросить обстановку или
// подключить безусловно» смотрит именно на этот тумблер, а весь этот файл
// проверяет путь, где человек авторежим уже выбрал — см.
// TestPodklyuchitRuchnojRezhimPodnimaetBezuslovno (manual-режим, тумблер
// выключен) отдельно.
func podklyuchitStend(t *testing.T, a *avtorezhim.Avtorezhim, taimaut time.Duration) (s *Sluzhba, popytok *int) {
	t.Helper()
	s = gotovStendLestnicy(t)
	n := 0
	popytok = &n
	s.zapustitYadro = func(ctx context.Context) error {
		*popytok++
		return nil
	}
	s.avtorezhimDlyaKnopki = func() *avtorezhim.Avtorezhim { return a }
	s.avtorezhimKnopkaTaimaut = taimaut
	s.Nastroyki.Avtorezhim = true
	// «Подключиться» с 28.08 сама включает фоновый авторежим (требование
	// 27.08: нажал — и программа определяет обстановку, а по возвращении
	// домой сама возвращается в положение «дома») — гасим служителя в
	// конце теста, как и TestAvtorezhimRuchkaVklyuchaetIVyklyuchaet.
	t.Cleanup(s.OstanovitAvtorezhim)
	return s, popytok
}

func postPodklyuchitIProverit(t *testing.T, s *Sluzhba) *httptest.ResponseRecorder {
	t.Helper()
	m := s.Obsluzhit()
	r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/podklyuchit", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("POST /api/podklyuchit вернул код %d: %s", w.Code, w.Body.String())
	}
	return w
}

// TestPodklyuchitDomaNePodnimaetZashchitu — обстановка «дома»: защиту
// поднимать не нужно, обход блокировок уже делает роутер.
func TestPodklyuchitDomaNePodnimaetZashchitu(t *testing.T) {
	a := &avtorezhim.Avtorezhim{
		Dns:       fakeDnsKnopka{doma: true},
		Trafik:    fakeTrafikKnopka{proshel: true},
		Zadvizhka: avtorezhim.NovayaZadvizhka(avtorezhim.Neizvestno),
	}
	s, popytok := podklyuchitStend(t, a, time.Second)
	postPodklyuchitIProverit(t, s)
	if *popytok != 0 {
		t.Fatalf("обстановка «дома», а PodnyatZashchitu всё равно позвана %d раз(а)", *popytok)
	}

	m := s.Obsluzhit()
	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	var o otvetSostoyaniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал /api/sostoyanie: %v", err)
	}
	if o.AvtorezhimObstanovka != "дома" {
		t.Fatalf("/api/sostoyanie не показал обстановку «дома» (тумблер выключен, а видно быть обязано): %+v", o)
	}
}

// TestPodklyuchitVneDomaPodnimaetZashchitu — обстановка «не дома»: защита
// поднимается как обычно.
func TestPodklyuchitVneDomaPodnimaetZashchitu(t *testing.T) {
	a := &avtorezhim.Avtorezhim{
		Dns:       fakeDnsKnopka{doma: false},
		Zadvizhka: avtorezhim.NovayaZadvizhka(avtorezhim.Neizvestno),
	}
	s, popytok := podklyuchitStend(t, a, time.Second)
	postPodklyuchitIProverit(t, s)
	if *popytok != 1 {
		t.Fatalf("обстановка «не дома», а PodnyatZashchitu позвана %d раз(а), хочу ровно 1", *popytok)
	}
}

// TestPodklyuchitTaimautVsyoRavnoPodnimaetZashchitu — заход авторежима
// повис (зонд не ответил вовсе): неизвестность не должна оставить человека
// без VPN, это дороже лишнего VPN дома — значит защита поднимается.
func TestPodklyuchitTaimautVsyoRavnoPodnimaetZashchitu(t *testing.T) {
	a := &avtorezhim.Avtorezhim{
		Dns:       visjachiyDnsKnopka{},
		Zadvizhka: avtorezhim.NovayaZadvizhka(avtorezhim.Neizvestno),
	}
	// Короткий таймаут — не ждать боевые 5с на каждый прогон теста.
	s, popytok := podklyuchitStend(t, a, 30*time.Millisecond)
	postPodklyuchitIProverit(t, s)
	if *popytok != 1 {
		t.Fatalf("заход авторежима повис, а PodnyatZashchitu позвана %d раз(а), хочу ровно 1", *popytok)
	}
}

// avtorezhimVklyuchenPoSostoyaniyu — читает /api/sostoyanie и возвращает
// avtorezhim_vklyuchen, тем же путём, что видит окно.
func avtorezhimVklyuchenPoSostoyaniyu(t *testing.T, s *Sluzhba) bool {
	t.Helper()
	m := s.Obsluzhit()
	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	var o otvetSostoyaniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал /api/sostoyanie: %v", err)
	}
	return o.AvtorezhimVklyuchen
}

// Заказ 27.08: человек включает VPN нажатием, дальше программа сама
// определяет обстановку, а по возвращении домой сама же возвращается в
// положение «дома», — нажатие «Подключиться» обязано включить
// автомат НАВСЕГДА, а не один раз спросить обстановку и забыть про неё.
// Ниже — доказательство ровно этого требования: тумблер, увиденный
// службой (s.Nastroyki.Avtorezhim), тот же тумблер по HTTP
// (/api/sostoyanie) и настройка, реально долетевшая до диска (переживёт
// перезапуск — hranenie.Zagruzit читает тот же KELEVRA_DIR).

// TestPodklyuchitDomaVklyuchaetAvtorezhim — обстановка «дома»: защита не
// поднимается (это и есть режим ожидания), но автомат обязан включиться, а
// не остаться разовым определением.
func TestPodklyuchitDomaVklyuchaetAvtorezhim(t *testing.T) {
	a := &avtorezhim.Avtorezhim{
		Dns:       fakeDnsKnopka{doma: true},
		Trafik:    fakeTrafikKnopka{proshel: true},
		Zadvizhka: avtorezhim.NovayaZadvizhka(avtorezhim.Neizvestno),
	}
	s, popytok := podklyuchitStend(t, a, time.Second)
	postPodklyuchitIProverit(t, s)
	if *popytok != 0 {
		t.Fatalf("обстановка «дома», а PodnyatZashchitu всё равно позвана %d раз(а)", *popytok)
	}
	if !s.Nastroyki.Avtorezhim {
		t.Fatal("после «Подключиться» дома автомат не включился (s.Nastroyki.Avtorezhim=false)")
	}
	if !avtorezhimVklyuchenPoSostoyaniyu(t, s) {
		t.Fatal("/api/sostoyanie не показал авторежим включённым после «Подключиться» дома")
	}
	n, err := hranenie.Zagruzit()
	if err != nil {
		t.Fatalf("не перечитал настройки с диска: %v", err)
	}
	if !n.Avtorezhim {
		t.Fatal("настройка авторежима не долетела до диска — перезапуск потерял бы выбор (требование 27.08: сам вернётся в положение «дома»)")
	}
}

// TestPodklyuchitVneDomaVklyuchaetAvtorezhim — обстановка «не дома»: защита
// поднимается как обычно, и автомат ТОЖЕ включается — тот же автомат потом
// сам опустит защиту, когда человек вернётся домой.
func TestPodklyuchitVneDomaVklyuchaetAvtorezhim(t *testing.T) {
	a := &avtorezhim.Avtorezhim{
		Dns:       fakeDnsKnopka{doma: false},
		Zadvizhka: avtorezhim.NovayaZadvizhka(avtorezhim.Neizvestno),
	}
	s, popytok := podklyuchitStend(t, a, time.Second)
	postPodklyuchitIProverit(t, s)
	if *popytok != 1 {
		t.Fatalf("обстановка «не дома», а PodnyatZashchitu позвана %d раз(а), хочу ровно 1", *popytok)
	}
	if !s.Nastroyki.Avtorezhim {
		t.Fatal("после «Подключиться» вне дома автомат не включился (s.Nastroyki.Avtorezhim=false)")
	}
	if !avtorezhimVklyuchenPoSostoyaniyu(t, s) {
		t.Fatal("/api/sostoyanie не показал авторежим включённым после «Подключиться» вне дома")
	}
	n, err := hranenie.Zagruzit()
	if err != nil {
		t.Fatalf("не перечитал настройки с диска: %v", err)
	}
	if !n.Avtorezhim {
		t.Fatal("настройка авторежима не долетела до диска — перезапуск потерял бы выбор")
	}
}

// TestPodklyuchitRuchnojRezhimPodnimaetBezuslovnoDazheDoma — вторая беда
// 28.08: определив, что человек дома, программа переставала давать
// включить защиту вручную. Человек САМ отказался от автомата (выключил
// тумблер или нажал «Отключить» — hranenie.Nastroyki.RuchnoyVybor) —
// «Подключиться» обязана поднять защиту безусловно, не спрашивая обстановку
// (эталон — мобильный AutoMode.kt.chooseManually, который обстановку не
// спрашивает вовсе) и не включая авторежим сам.
//
// Правка 31.08 (схема «одна кнопка», заказ 27.08: нажал «подключить» —
// программа сама определяет обстановку) сдвинула ровно одну вещь: ручной
// режим теперь опознаётся не по выключенному тумблеру, а по ЯВНОМУ отказу
// человека. Выключенный
// тумблер у свежей установки значит «ещё не выбирал», и там кнопка обязана
// включить автомат сама (TestPodklyuchitBezVyboraCheloveka...). Гарантия,
// которую этот тест сторожит, не тронута: как только человек сказал «нет»,
// кнопка поднимает защиту безусловно и молча.
//
// Зонд заведён на «дома», но НАРОЧНО не даёт Zahod завершиться раньше
// таймаута заведомо долгим — если бы правка регрессировала и код всё ещё
// звал domaSeychas, тест поймал бы это как через popytok=0, так и через
// зависший ответ. На старом коде (домашняя обстановка → gotovo без
// PodnyatZashchitu) этот тест красный — см. отчёт правки, откат условия
// подтверждён вручную.
func TestPodklyuchitRuchnojRezhimPodnimaetBezuslovnoDazheDoma(t *testing.T) {
	a := &avtorezhim.Avtorezhim{
		Dns:       fakeDnsKnopka{doma: true},
		Trafik:    fakeTrafikKnopka{proshel: true},
		Zadvizhka: avtorezhim.NovayaZadvizhka(avtorezhim.Neizvestno),
	}
	s := gotovStendLestnicy(t)
	popytok := 0
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		return nil
	}
	s.avtorezhimDlyaKnopki = func() *avtorezhim.Avtorezhim { return a }
	s.avtorezhimKnopkaTaimaut = time.Second
	// Тумблер выключен И человек выключил его сам — это и есть ручной режим
	// под проверкой (gotovStendLestnicy тумблер не трогает, нулевое значение
	// false; RuchnoyVybor ставим здесь, изображая нажатый человеком «нет»).
	if s.Nastroyki.Avtorezhim {
		t.Fatal("стенд обязан стартовать с выключенным авторежимом (ручной режим)")
	}
	s.Nastroyki.RuchnoyVybor = true
	t.Cleanup(s.OstanovitAvtorezhim)

	postPodklyuchitIProverit(t, s)

	if popytok != 1 {
		t.Fatalf("ручной режим, обстановка «дома» — PodnyatZashchitu обязана быть позвана ровно 1 раз, а позвана %d раз(а)", popytok)
	}
	if s.Nastroyki.Avtorezhim {
		t.Fatal("ручной режим: «Подключиться» не должна была включить авторежим сама")
	}
}
