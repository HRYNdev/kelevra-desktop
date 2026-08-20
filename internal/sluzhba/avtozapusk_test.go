package sluzhba

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"runtime"
	"testing"
)

// stend поднимает настоящую Sluzhba на изолированном каталоге данных: тесты
// не должны трогать %LOCALAPPDATA% того, кто их запускает.
func stend(t *testing.T) *Sluzhba {
	t.Helper()
	t.Setenv("KELEVRA_DIR", t.TempDir())
	s, err := Novaya()
	if err != nil {
		t.Fatalf("не поднял службу: %v", err)
	}
	return s
}

// На линуксовом стенде (тот, где вообще гоняются go test) avtozapusk всегда
// отвечает «только на Windows»: ручка обязана донести это честно, а не
// притвориться, что переключатель сработал.
func TestSostoyanieAvtozapuskNaLinukse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("проверяет ветку не-Windows")
	}
	s := stend(t)
	m := s.Obsluzhit()

	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("код ответа %d", w.Code)
	}
	var o otvetSostoyaniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	if o.AvtozapuskPodderzhivaetsya {
		t.Fatalf("на не-Windows тумблер не должен объявлять себя рабочим: %+v", o)
	}
	if o.AvtozapuskVklyuchen || o.AvtozapuskUstarela || o.AvtozapuskBeda != "" {
		t.Fatalf("при непозаддержке остальные поля автозапуска должны молчать: %+v", o)
	}
}

// Переключение тумблера на не-Windows не должно тихо отвечать «gotovo: true» —
// человеку (или тесту окна) обязана доехать причина текстом, а не потеряться
// в журнале.
func TestAvtozapuskRuchkaChestnoOtkazyvaetNaLinukse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("проверяет ветку не-Windows")
	}
	s := stend(t)
	m := s.Obsluzhit()

	telo, _ := json.Marshal(map[string]any{"vklyuchit": true})
	r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/avtozapusk", bytes.NewReader(telo))
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatalf("на не-Windows включение обязано вернуть ошибку, а не gotovo: %s", w.Body.String())
	}
	var o map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	if o["beda"] == "" {
		t.Fatalf("ответ без текста причины: %s", w.Body.String())
	}
}
