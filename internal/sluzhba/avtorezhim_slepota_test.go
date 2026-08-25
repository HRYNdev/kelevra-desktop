package sluzhba

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
)

// TestSostoyanieNesyotPrichinuSlepotyDoUI — забор 2: слой, который рисует
// окно (ручка /api/sostoyanie, oblik/index.html читает поле напрямую),
// обязан увидеть причину слепоты, а не только внутренний пакет avtorezhim.
// Физический адаптер не находится ни разу — тот же сценарий, что доказан
// диагнозом (человек вернулся домой, туннель висел). После трёх слепых
// заходов подряд /api/sostoyanie обязано отдать avtorezhim_slep_prichina, а
// обстановка — по-прежнему не "дома" (защита не снята вслепую).
func TestSostoyanieNesyotPrichinuSlepotyDoUI(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()

	// Собираем авторежим руками, как ZapustitAvtorezhim, но без настоящего
	// служителя/сети — три захода делаем напрямую, детерминированно.
	a := avtorezhim.Novyy()
	a.TunnelPodnyat = func() bool { return true }
	a.SetevoyAdres = func() (string, string, error) {
		return "", "", errors.New("не нашёл подходящий физический адаптер")
	}
	s.avtorezhimZamok.Lock()
	s.avtorezhimEkz = a
	s.avtorezhimZamok.Unlock()

	for i := 0; i < avtorezhim.PodryadDoPrichiny; i++ {
		a.Zahod(context.Background(), true, false)
	}

	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("код ответа %d: %s", w.Code, w.Body.String())
	}
	var o otvetSostoyaniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал /api/sostoyanie: %v", err)
	}
	if o.AvtorezhimSlepPrichina == "" {
		t.Fatal("/api/sostoyanie не донесло причину слепоты до слоя, который рисует UI")
	}
	if o.AvtorezhimObstanovka == avtorezhim.Doma.String() {
		t.Fatalf("обстановка = %q при не найденном адаптере — защита не должна сниматься вслепую", o.AvtorezhimObstanovka)
	}
}
