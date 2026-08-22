package sluzhba

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestAvtorezhimRuchkaVklyuchaetIVyklyuchaet проверяет саму проводку: ручка
// /api/avtorezhim обязана сохранить выбор в настройках, поднять служителя на
// "включить" и погасить его на "выключить" — а /api/sostoyanie обязано
// честно отразить оба состояния. Поведение самого распознавания обстановки
// (когда именно зовётся PodnyatZashchitu/OpustitZashchitu) проверено в
// internal/avtorezhim отдельно, на подставных зондах.
func TestAvtorezhimRuchkaVklyuchaetIVyklyuchaet(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()
	// Служитель полез бы за настоящей сетью (пусть и не заваливая тест —
	// SledchikSeti на не-Windows просто поллинг интерфейсов) — гасим его в
	// конце, чтобы не оставить фоновую горутину висеть после теста.
	t.Cleanup(s.OstanovitAvtorezhim)

	postAvtorezhim := func(vklyuchit bool) *httptest.ResponseRecorder {
		telo, _ := json.Marshal(map[string]any{"vklyuchit": vklyuchit})
		r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/avtorezhim", bytes.NewReader(telo))
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		return w
	}
	getSostoyanie := func() otvetSostoyaniya {
		r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil)
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		var o otvetSostoyaniya
		if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
			t.Fatalf("не разобрал /api/sostoyanie: %v", err)
		}
		return o
	}

	if s.Nastroyki.Avtorezhim {
		t.Fatal("по умолчанию авторежим обязан быть выключен")
	}
	if o := getSostoyanie(); o.AvtorezhimVklyuchen {
		t.Fatal("/api/sostoyanie сразу после старта показывает авторежим включённым")
	}

	if w := postAvtorezhim(true); w.Code != 200 {
		t.Fatalf("включение авторежима вернуло код %d: %s", w.Code, w.Body.String())
	}
	if !s.Nastroyki.Avtorezhim {
		t.Fatal("после включения настройка Avtorezhim не сохранилась в памяти")
	}
	s.avtorezhimZamok.Lock()
	otmenaEst := s.avtorezhimOtmena != nil
	ekzEst := s.avtorezhimEkz != nil
	s.avtorezhimZamok.Unlock()
	if !otmenaEst || !ekzEst {
		t.Fatal("после включения служитель авторежима не поднялся")
	}
	if o := getSostoyanie(); !o.AvtorezhimVklyuchen {
		t.Fatal("/api/sostoyanie не отразило включённый авторежим")
	}

	if w := postAvtorezhim(false); w.Code != 200 {
		t.Fatalf("выключение авторежима вернуло код %d: %s", w.Code, w.Body.String())
	}
	if s.Nastroyki.Avtorezhim {
		t.Fatal("после выключения настройка Avtorezhim осталась true")
	}
	s.avtorezhimZamok.Lock()
	otmenaEst = s.avtorezhimOtmena != nil
	s.avtorezhimZamok.Unlock()
	if otmenaEst {
		t.Fatal("после выключения служитель авторежима остался поднят")
	}
	if o := getSostoyanie(); o.AvtorezhimVklyuchen {
		t.Fatal("/api/sostoyanie не отразило выключенный авторежим")
	}
}
