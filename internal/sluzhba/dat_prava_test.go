package sluzhba

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// «Дать права»: ручка просит права и ставит службу Windows — тот же путь,
// что запрос при первом запуске. Отказ — человеку официальная строка про
// права, а не сырой код ShellExecute.
func TestDatPravaStavitSluzhbuIliGovoritProPrava(t *testing.T) {
	s := stend(t)
	s.pravaDlyaStenda = func() bool { return false }
	m := s.Obsluzhit()

	zvali := 0
	s.PostavitSluzhbuWindows = func() error { zvali++; return nil }
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("POST", "/"+s.klyuch+"/api/dat_prava", nil))
	if w.Code != 200 || zvali != 1 {
		t.Fatalf("согласие: код %d, установку звали %d раз: %s", w.Code, zvali, w.Body.String())
	}

	s.PostavitSluzhbuWindows = func() error { return errors.New("права не выданы") }
	w = httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("POST", "/"+s.klyuch+"/api/dat_prava", nil))
	var o map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &o)
	if w.Code == 200 || o["beda"] != OshibkaNetPrav.Error() {
		t.Fatalf("отказ: код %d, ответ %s — ждали строку «%s»", w.Code, w.Body.String(), OshibkaNetPrav.Error())
	}

	// Старые имена ручки (окна прошлых версий) ведут туда же.
	for _, put := range []string{"polnaya_zashchita", "sluzhba_postavit"} {
		zvali = 0
		s.PostavitSluzhbuWindows = func() error { zvali++; return nil }
		w = httptest.NewRecorder()
		m.ServeHTTP(w, httptest.NewRequest("POST", "/"+s.klyuch+"/api/"+put, nil))
		if w.Code != 200 || zvali != 1 {
			t.Fatalf("/api/%s: код %d, установку звали %d раз", put, w.Code, zvali)
		}
	}
}

// Права уже есть — ставить нечего и спрашивать нечего.
func TestDatPravaSPravamiNichegoNeSprashivaet(t *testing.T) {
	s := stend(t)
	s.pravaDlyaStenda = func() bool { return true }
	s.PostavitSluzhbuWindows = func() error {
		t.Fatal("права есть, а окно Windows всё равно показано")
		return nil
	}
	w := httptest.NewRecorder()
	s.Obsluzhit().ServeHTTP(w, httptest.NewRequest("POST", "/"+s.klyuch+"/api/dat_prava", nil))
	if w.Code != 200 {
		t.Fatalf("код %d", w.Code)
	}
}

// Служба только что поставлена по просьбе человека — она сама поднимает
// связь (решение 17.10: «дал права админа, ВСЁ, РАБОТАЕТ»), ровно один раз.
func TestSluzhbaPosleUstanovkiPodklyuchaetsyaOdinRaz(t *testing.T) {
	s := stendSPravami(t)
	s.Nastroyki.Kod = "stend"
	s.Nastroyki.RuchnoyVybor = true // ручной режим: без захода обстановки
	zapuskov := 0
	s.zapustitYadro = func(ctx context.Context) error { zapuskov++; return nil }

	if err := hranenie.OtmetitPodklyuchitPosleUstanovki(); err != nil {
		t.Fatal(err)
	}
	s.PodklyuchitPosleUstanovki(context.Background())
	if zapuskov != 1 {
		t.Fatalf("после установки службы ядро запускали %d раз — ждали подключение", zapuskov)
	}
	s.PodklyuchitPosleUstanovki(context.Background())
	if zapuskov != 1 {
		t.Fatal("флаг не снят — служба подключалась бы на каждом своём старте")
	}
}

// Без флага (обычный старт службы) ничего не подключается само.
func TestSluzhbaBezFlagaNePodklyuchaetsya(t *testing.T) {
	s := stendSPravami(t)
	s.Nastroyki.Kod = "stend"
	s.zapustitYadro = func(ctx context.Context) error {
		t.Fatal("флага не было, а служба сама подняла связь")
		return nil
	}
	s.PodklyuchitPosleUstanovki(context.Background())
}
