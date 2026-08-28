package sluzhba

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
)

// Диагноз 28.08 (площадка): контейнер, в котором гоняются стенды, сам живёт
// за резолвером, отвечающим на контрольные домены (youtube.com, discord.com,
// rutracker.org) fake-ip подменой — тем же диапазоном 198.18.0.0/15, что и
// собственное ядро (см. stend/zond_doma.sh). Без подмены domaSeychas
// (podklyuchit, #78) честно, но ЛОЖНО решает «дома» через системный резолвер,
// и «Подключиться» молча не поднимает защиту — стенды proksi.sh,
// pervyy_ekran.sh, prava_avtozapros.sh, vybor_uzla.sh, zhivoy_trafik.sh
// красные не из-за продукта, а из-за площадки.
//
// Лечение — KELEVRA_AVTOREZHIM_DNS (Novaya, avtorezhimDnsAdres,
// avtorezhimBoevoy): стенд явно указывает недостижимый резолвер, зонд честно
// решает «не дома». Эти тесты доказывают ОБА конца этой подмены на
// настоящем Novaya() (не через avtorezhimDlyaKnopki, как
// podklyuchit_doma_test.go, — там подмена целиком ручная и не поймала бы
// регресс в чтении переменной окружения или в avtorezhimBoevoy):
//  1. KELEVRA_AVTOREZHIM_DNS пуст — a.Dns остаётся дефолтным DnsZond без
//     AdresResolvera (системный путь, поведение до этой правки).
//  2. KELEVRA_AVTOREZHIM_DNS задан — и a.Dns (domaSeychas), и a.DnsPryamoy
//     (тот же путь для фонового авторежима в TUN) уходят именно на него.
func TestAvtorezhimBoevoyBezPodmenyIspolzuetSistemnyPut(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())
	s, err := Novaya()
	if err != nil {
		t.Fatalf("не поднял службу: %v", err)
	}
	a := s.avtorezhimBoevoy()
	z, ok := a.Dns.(*avtorezhim.DnsZond)
	if !ok {
		t.Fatalf("a.Dns не *avtorezhim.DnsZond: %T", a.Dns)
	}
	if z.AdresResolvera != "" {
		t.Fatalf("KELEVRA_AVTOREZHIM_DNS не задан, а AdresResolvera=%q — резолвер подменён без настройки", z.AdresResolvera)
	}
}

func TestAvtorezhimBoevoySPodmenoyUhoditNaZadannyResolver(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())
	t.Setenv("KELEVRA_AVTOREZHIM_DNS", "127.0.0.1:1")
	s, err := Novaya()
	if err != nil {
		t.Fatalf("не поднял службу: %v", err)
	}
	a := s.avtorezhimBoevoy()
	z, ok := a.Dns.(*avtorezhim.DnsZond)
	if !ok {
		t.Fatalf("a.Dns не *avtorezhim.DnsZond: %T", a.Dns)
	}
	if z.AdresResolvera != "127.0.0.1:1" {
		t.Fatalf("a.Dns.AdresResolvera=%q, ждали 127.0.0.1:1 — KELEVRA_AVTOREZHIM_DNS не дошёл до зонда", z.AdresResolvera)
	}
	if a.DnsPryamoy == nil {
		t.Fatal("a.DnsPryamoy пуст — фоновый авторежим в TUN-режиме уйдёт на системный путь, а не на подмену")
	}
	pryamoy, ok := a.DnsPryamoy("не-важно-какой-адрес", "").(*avtorezhim.DnsZond)
	if !ok {
		t.Fatalf("a.DnsPryamoy(...) не *avtorezhim.DnsZond")
	}
	if pryamoy.AdresResolvera != "127.0.0.1:1" {
		t.Fatalf("a.DnsPryamoy(...).AdresResolvera=%q, ждали 127.0.0.1:1 (адрес физ. адаптера обязан игнорироваться подменой)", pryamoy.AdresResolvera)
	}
}

// TestPodklyuchitSPodmenoyDnsChestnoReshaetVneDoma — сквозной прогон через
// настоящую HTTP-ручку /api/podklyuchit и настоящий Novaya(): с
// KELEVRA_AVTOREZHIM_DNS, указывающим на адрес, который никто не слушает
// (127.0.0.1:1, мгновенный ECONNREFUSED), domaSeychas обязана честно решить
// «не дома» и поднять защиту — ровно то поведение, которого площадке стендов
// не хватало без этой переменной.
func TestPodklyuchitSPodmenoyDnsChestnoReshaetVneDoma(t *testing.T) {
	t.Setenv("KELEVRA_AVTOREZHIM_DNS", "127.0.0.1:1")
	s := gotovStendLestnicy(t) // KELEVRA_DIR, KELEVRA_PRAVA=net, профиль и лже-ядро
	// gotovStendLestnicy сам подставляет avtorezhimDlyaKnopki — тут нужен
	// именно ДЕФОЛТНЫЙ (nil) путь, чтобы проверить настоящий avtorezhimBoevoy.
	s.avtorezhimDlyaKnopki = nil
	popytok := 0
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		return nil
	}

	m := s.Obsluzhit()
	r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/podklyuchit", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("POST /api/podklyuchit код %d: %s", w.Code, w.Body.String())
	}
	var otvet map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &otvet); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	if popytok != 1 {
		t.Fatalf("резолвер подставлен недостижимым, обязаны честно решить «не дома» и поднять защиту (zapustitYadro), а позвано %d раз(а): %v", popytok, otvet)
	}
}
