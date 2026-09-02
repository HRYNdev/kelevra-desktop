package sluzhba

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// lzheClash — Clash API, показания которого задаёт тест. Настоящее ядро для
// проверки учёта не нужно: учёт видит от него ровно две цифры.
func lzheClash(t *testing.T, pokazanie func() (int64, int64)) *yadro.Yadro {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connections" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		vverh, vniz := pokazanie()
		fmt.Fprintf(w, `{"uploadTotal":%d,"downloadTotal":%d,"connections":[]}`, vverh, vniz)
	}))
	t.Cleanup(srv.Close)
	return &yadro.Yadro{Api: strings.TrimPrefix(srv.URL, "http://"), Klient: srv.Client()}
}

func sluzhbaSYadrom(t *testing.T, y *yadro.Yadro) *Sluzhba {
	t.Helper()
	t.Setenv("KELEVRA_DIR", t.TempDir())
	n, err := hranenie.Zagruzit()
	if err != nil {
		t.Fatalf("настройки не загрузились: %v", err)
	}
	return &Sluzhba{Nastroyki: n, Yadro: y}
}

// Учёт копит расход между тиками и кладёт итог на диск: без диска «расход за
// всё время» обнулялся бы каждым перезапуском приложения.
func TestUchetTrafikaKopitIKladetNaDisk(t *testing.T) {
	var vverh, vniz int64
	s := sluzhbaSYadrom(t, lzheClash(t, func() (int64, int64) { return vverh, vniz }))

	vverh, vniz = 100, 400
	s.UchestTrafik()
	if got := s.Nastroyki.TrafikUstroystva(); got != 500 {
		t.Fatalf("после первого тика расход %d, ждали 500", got)
	}
	vverh, vniz = 150, 900
	s.UchestTrafik()
	if got := s.Nastroyki.TrafikUstroystva(); got != 1050 {
		t.Fatalf("после второго тика расход %d, ждали 1050", got)
	}

	naDiske, err := hranenie.Zagruzit()
	if err != nil {
		t.Fatalf("не перечитались: %v", err)
	}
	if naDiske.TrafikUstroystva() != 1050 {
		t.Fatalf("на диске %d, а в памяти 1050 — итог не переживёт перезапуск", naDiske.TrafikUstroystva())
	}
}

// Ядро перезапустилось посреди счёта: его счётчик пошёл с нуля. Итог обязан
// прирасти на новое показание, а не уйти в минус на разности.
func TestPerezapuskYadraPosrediSchetaNeUmenshaetItog(t *testing.T) {
	var vverh, vniz int64
	s := sluzhbaSYadrom(t, lzheClash(t, func() (int64, int64) { return vverh, vniz }))

	vverh, vniz = 1_000, 9_000
	s.UchestTrafik()
	vverh, vniz = 10, 90 // ядро упало и поднялось заново
	s.UchestTrafik()

	if got := s.Nastroyki.TrafikUstroystva(); got != 10_100 {
		t.Fatalf("расход %d, ждали 10100 (10000 + 100)", got)
	}
}

// Clash API недоступен — обычное дело: ядро стоит бо́льшую часть суток. Это не
// ошибка приложения и не повод портить сохранённый итог.
func TestMertvyyClashNePortitItog(t *testing.T) {
	// Порт 1 на петле: слушать там некому, соединение отказывают сразу.
	y := &yadro.Yadro{Api: "127.0.0.1:1", Klient: &http.Client{Timeout: time.Second}}
	s := sluzhbaSYadrom(t, y)
	s.Nastroyki.UchestPokazanieYadra(5_000)
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		t.Fatalf("не сохранились: %v", err)
	}

	s.UchestTrafik() // не должно ни паниковать, ни менять цифры

	if got := s.Nastroyki.TrafikUstroystva(); got != 5_000 {
		t.Fatalf("после недоступного Clash расход %d, ждали 5000", got)
	}
	if s.Nastroyki.TrafikYadraBayt != 5_000 {
		t.Fatalf("сбита отметка показания ядра: %d", s.Nastroyki.TrafikYadraBayt)
	}
	naDiske, err := hranenie.Zagruzit()
	if err != nil {
		t.Fatalf("не перечитались: %v", err)
	}
	if naDiske.TrafikUstroystva() != 5_000 {
		t.Fatalf("на диске %d, ждали 5000", naDiske.TrafikUstroystva())
	}
}

// Ядра нет вовсе (служба собрана без него) — учёт обязан промолчать, а не
// уронить копию по nil.
func TestUchetBezYadraNePadaet(t *testing.T) {
	s := sluzhbaSYadrom(t, nil)
	s.Yadro = nil
	s.UchestTrafik()
	if got := s.Nastroyki.TrafikUstroystva(); got != 0 {
		t.Fatalf("расход %d, ждали 0", got)
	}
}
