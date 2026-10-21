package sluzhba

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// ETag сводки /info переживает перезапуск службы: до 22.10 он жил только в
// памяти, и первый лёгкий опрос после каждого перезапуска шёл без
// If-None-Match — сервер отдавал сводку целиком, хотя она не менялась.
func TestEtagSvodkiPerezhivaetPerezapusk(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())
	const etag = `W/"svodka-7"`
	var mu sync.Mutex
	polnyh, uslovnyh := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/info") {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("If-None-Match") == etag {
			uslovnyh++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		polnyh++
		w.Header().Set("ETag", etag)
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "owner", "active": true})
	}))
	defer srv.Close()

	novaya := func() *Sluzhba {
		s := &Sluzhba{
			Nastroyki: &hranenie.Nastroyki{Kod: "TESTOVYY_KOD_1234", DeviceID: "dev-1"},
			Podpiska:  &podpiska.Klient{Host: strings.TrimPrefix(srv.URL, "http://"), Shema: "http", DeviceID: "dev-1"},
		}
		s.nachatPult()
		return s
	}

	pervaya := novaya()
	if err := pervaya.ObnovitSvedeniyaLegko(context.Background()); err != nil {
		t.Fatal(err)
	}
	if polnyh != 1 {
		t.Fatalf("первый опрос: полных ответов %d, ждали 1", polnyh)
	}

	// Перезапуск: новая служба на той же папке.
	vtoraya := novaya()
	vtoraya.zamok.Lock()
	byl := vtoraya.etagSvodki
	vtoraya.zamok.Unlock()
	if byl != etag {
		t.Fatalf("ETag после перезапуска %q, ждали %q дословно", byl, etag)
	}
	if err := vtoraya.ObnovitSvedeniyaLegko(context.Background()); err != nil {
		t.Fatal(err)
	}
	if polnyh != 1 || uslovnyh != 1 {
		t.Fatalf("после перезапуска: полных %d, условных %d — ждали 1 и 1 (304 без тела)", polnyh, uslovnyh)
	}
	if vtoraya.svedeniya == nil {
		t.Fatal("карточка подписки после перезапуска пустая")
	}
}
