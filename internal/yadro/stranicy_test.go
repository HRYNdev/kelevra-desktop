package yadro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// Сборка ядра на второй странице списка релизов находится. Живая — 91-я из
// 92 при странице в 100: ещё несколько выпусков, и она ушла бы на вторую.
func TestYadroNaVtoroyStranice(t *testing.T) {
	const vsego, per = 150, 100
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
		if stranica*per < vsego {
			w.Header().Set("Link", fmt.Sprintf(`<%s/spisok?per_page=%d&page=%d>; rel="next"`, srv.URL, per, stranica+1))
		}
		json.NewEncoder(w).Encode(otvet)
	}))
	t.Cleanup(srv.Close)
	y := &Yadro{Spisok: srv.URL + "/spisok?per_page=100"}
	ssylka, _, err := y.ssylkaNaYadro(context.Background())
	if err != nil || ssylka != "https://example.invalid/a" {
		t.Fatalf("ядро на второй странице не найдено: %q, %v", ssylka, err)
	}
	if zaprosov != 2 {
		t.Fatalf("страниц запрошено %d, ждал 2", zaprosov)
	}
}
