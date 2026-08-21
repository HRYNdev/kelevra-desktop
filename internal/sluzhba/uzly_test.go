package sluzhba

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
)

// До 21.08 в отключённом состоянии /api/uzly отдавал пустой список ("пока
// ядро стоит, спрашивать некого"), и окно показывало 300px пустоты вместо
// списка узлов (Вова, снимок 2_otklyucheno.png). Список — часть конфига на
// диске, спрашивать живое ядро для него не обязательно.
func TestUzlySoStatikaPokaYadroStoit(t *testing.T) {
	s := stend(t)
	profil, err := os.ReadFile("../konfig/testdata/profil_telefona.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SohranitProfil(profil); err != nil {
		t.Fatalf("не сохранил профиль: %v", err)
	}
	m := s.Obsluzhit()

	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/uzly", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("код ответа %d: %s", w.Code, w.Body.String())
	}
	var o struct {
		Gruppy []struct {
			Imya    string `json:"imya"`
			Seychas string `json:"seychas"`
			Uzly    []struct {
				Imya      string `json:"imya"`
				Zaderzhka int    `json:"zaderzhka"`
			} `json:"uzly"`
		} `json:"gruppy"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	if len(o.Gruppy) != 1 || o.Gruppy[0].Imya != "Соединение" {
		t.Fatalf("список узлов пуст или не тот, хотя ядро стоит: %s", w.Body.String())
	}
	if o.Gruppy[0].Seychas != "Нидерланды" {
		t.Fatalf("без сохранённого выбора обязан взять default из профиля: %+v", o.Gruppy[0])
	}
	for _, u := range o.Gruppy[0].Uzly {
		if u.Zaderzhka != 0 {
			t.Errorf("задержка без живого ядра не может быть измерена: %+v", u)
		}
	}
}

// Выбор узла ДО подключения обязан сохраниться и реально примениться на
// «Подключить» — иначе список в отключённом состоянии декорация, а не выбор
// (условие задачи 21.08).
func TestVybratSohranyaetVyborPokaYadroStoitIPerezhivaetPerezapusk(t *testing.T) {
	s := stend(t)
	profil, err := os.ReadFile("../konfig/testdata/profil_telefona.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SohranitProfil(profil); err != nil {
		t.Fatalf("не сохранил профиль: %v", err)
	}
	m := s.Obsluzhit()

	telo, _ := json.Marshal(map[string]string{"gruppa": "Соединение", "uzel": "Комната"})
	r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/vybrat", bytes.NewReader(telo))
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("выбор узла без работающего ядра обязан приниматься, а не отказывать: %s", w.Body.String())
	}

	r2 := httptest.NewRequest("GET", "/"+s.klyuch+"/api/uzly", nil)
	w2 := httptest.NewRecorder()
	m.ServeHTTP(w2, r2)
	var o struct {
		Gruppy []struct {
			Seychas string `json:"seychas"`
		} `json:"gruppy"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	if len(o.Gruppy) != 1 || o.Gruppy[0].Seychas != "Комната" {
		t.Fatalf("выбор не отразился в /api/uzly: %s", w2.Body.String())
	}

	// Переживает перезапуск процесса приложения: новый Sluzhba на той же папке
	// данных обязан прочитать сохранённый выбор из nastroyki.json.
	s2, err := Novaya()
	if err != nil {
		t.Fatalf("не поднял вторую службу: %v", err)
	}
	if s2.Nastroyki.Uzly["Соединение"] != "Комната" {
		t.Fatalf("выбор узла не пережил перезапуск: %+v", s2.Nastroyki.Uzly)
	}
}
