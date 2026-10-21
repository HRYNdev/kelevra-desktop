package yadro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
)

// stendStranic — «GitHub» со списком релизов по per страниц и заголовком
// Link, как у настоящего. Сборка ядра — последней (как у живой: 91-я из 92).
func stendStranic(t *testing.T, vsego, per int, beskonechno bool) (*httptest.Server, *int) {
	t.Helper()
	zaprosov := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zaprosov++
		stranica, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if stranica < 1 {
			stranica = 1
		}
		var otvet []map[string]any
		for i := (stranica - 1) * per; i < stranica*per && i < vsego; i++ {
			if i == vsego-1 {
				otvet = append(otvet, map[string]any{"tag_name": "core-v1.14.0-beta.4-1", "assets": []map[string]string{
					{"name": ImyaArhiva(), "browser_download_url": "https://example.invalid/a"}}})
				continue
			}
			otvet = append(otvet, map[string]any{"tag_name": fmt.Sprintf("app-v0.6.%d", i), "assets": []any{}})
		}
		if beskonechno {
			otvet = []map[string]any{{"tag_name": "app-v0.0.1", "assets": []any{}}}
		}
		if beskonechno || stranica*per < vsego {
			w.Header().Set("Link", fmt.Sprintf(`<%s/spisok?per_page=%d&page=%d>; rel="next", <%s/spisok?page=99>; rel="last"`, srv.URL, per, stranica+1, srv.URL))
		}
		json.NewEncoder(w).Encode(otvet)
	}))
	t.Cleanup(srv.Close)
	return srv, &zaprosov
}

// Сборка ядра на второй странице списка находится (живая — 91-я из 92 при
// странице в 100: ещё восемь релизов, и она уехала бы на вторую).
func TestYadroNaVtoroyStranice(t *testing.T) {
	srv, zaprosov := stendStranic(t, 150, 100, false)
	y := &Yadro{Spisok: srv.URL + "/spisok?per_page=100"}
	s, err := y.vybratSborku(context.Background(), http.DefaultClient, false)
	if err != nil || s.Teg != "core-v1.14.0-beta.4-1" {
		t.Fatalf("ядро на второй странице не найдено: %q, %v", s.Teg, err)
	}
	if *zaprosov != 2 {
		t.Fatalf("страниц запрошено %d, ждал 2", *zaprosov)
	}
}

// Бесконечный rel="next" не уводит в бесконечный обход.
func TestObkhodStranicOgranichen(t *testing.T) {
	srv, zaprosov := stendStranic(t, 0, 1, true)
	y := &Yadro{Spisok: srv.URL + "/spisok"}
	if _, err := y.vybratSborku(context.Background(), http.DefaultClient, false); err == nil {
		t.Fatal("сборка найдена там, где её нет")
	}
	if *zaprosov != obnovlenie.PredelStranicRelizov {
		t.Fatalf("страниц запрошено %d, ждал предел %d", *zaprosov, obnovlenie.PredelStranicRelizov)
	}
}

// Ссылка на следующую страницу на чужой сервер не берётся.
func TestStranicaNaChuzhoyServerNeBeretsya(t *testing.T) {
	zaprosov := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zaprosov++
		w.Header().Set("Link", `<https://example.invalid/spisok?page=2>; rel="next"`)
		fmt.Fprint(w, `[{"tag_name":"app-v1","assets":[]}]`)
	}))
	t.Cleanup(srv.Close)
	y := &Yadro{Spisok: srv.URL + "/spisok"}
	_, _ = y.vybratSborku(context.Background(), http.DefaultClient, false)
	if zaprosov != 1 {
		t.Fatalf("запросов %d", zaprosov)
	}
}
