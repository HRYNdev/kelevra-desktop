package sluzhba

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Беда «два хозяина туннеля»: кнопка «Работать без подтверждений»
// (sluzhba_postavit → PostavitSluzhbuWindows) поднимает СЛУЖБУ Windows, а сама
// ставится службой ЗАПУСКАЕТСЯ немедленно (vinsluzhba.Ustanovit зовёт
// s.Start() в конце). Но ЭТА, уже работающая копия, к моменту успеха ручки
// сама держит живое ядро (s.Yadro) — и, в отличие от согласия на права
// администратора (polnayaZashchita → uydiPosleSoglasiyaNaPrava) и установки
// обновления (obnovleniePostavit), эта ветка НЕ гасит своё же ядро. Итог —
// два процесса держат один и тот же адаптер/туннель одновременно.
//
// Тест поднимает НАСТОЯЩЕЕ (подставное) ядро тем же приёмом, что и
// internal/yadro/zapustit_test.go: TestZapustitDvazhdyIdempotenten, — а не
// подделывает состояние вручную, потому что дело не в поле Sost(), а в
// РЕАЛЬНОМ вызове Ostanovit() после успешной установки службы.
func TestSluzhbaPostavitGasitSvoyoYadroPosleUspekha(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("подставное ядро — скрипт /bin/sh; свойство от платформы не зависит (см. zapustit_test.go)")
	}
	s := stend(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	adres := strings.TrimPrefix(srv.URL, "http://")

	papka := t.TempDir()
	bin := filepath.Join(papka, "sing-box")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(papka, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Yadro = &yadro.Yadro{Bin: bin, Papka: papka, Api: adres}
	defer s.Yadro.Ostanovit()

	if err := s.Yadro.Zapustit(context.Background()); err != nil {
		t.Fatalf("подставное ядро не поднялось: %v", err)
	}
	if s.Yadro.Sost() != yadro.Rabotaet {
		t.Fatalf("ядро должно быть живо перед проверкой, а состояние %q", s.Yadro.Sost())
	}

	postavleno := false
	s.PostavitSluzhbuWindows = func() error {
		postavleno = true
		return nil // служба «поставилась» — vinsluzhba.Ustanovit её уже и запустила
	}

	r := httptest.NewRequest("POST", "/api/sluzhba_postavit", nil)
	w := httptest.NewRecorder()
	s.sluzhbaPostavit(w, r)

	if !postavleno {
		t.Fatal("обработчик не дошёл до PostavitSluzhbuWindows — тест ничего не измерил")
	}
	if w.Code != 200 {
		t.Fatalf("успешная установка должна вернуть 200, а код %d: %s", w.Code, w.Body.String())
	}
	if sost := s.Yadro.Sost(); sost != yadro.Stoit {
		t.Fatalf("после успешной установки службы своё ядро обязано быть погашено, а состояние %q — служба Windows подняла своё, эта копия держит своё: два хозяина туннеля", sost)
	}
}
