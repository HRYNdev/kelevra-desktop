package obnovlenie

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Самая свежая версия приложения — на второй странице: находится.
func TestProveritVidiitVtoruyuStranicu(t *testing.T) {
	zaprosov := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zaprosov++
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprintf(w, `[{"tag_name":"app-v0.9.0","assets":[{"name":"Kelevra.exe","size":10,"browser_download_url":"%s/exe"}]}]`, srv.URL)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/spisok?page=2>; rel="next", <%s/spisok?page=2>; rel="last"`, srv.URL, srv.URL))
		fmt.Fprintf(w, `[{"tag_name":"app-v0.6.1","assets":[{"name":"Kelevra.exe","size":10,"browser_download_url":"%s/exe"}]}]`, srv.URL)
	}))
	t.Cleanup(srv.Close)
	n, err := Proverit(context.Background(), srv.Client(), srv.URL+"/spisok", "0.6.0")
	if err != nil || n == nil || n.Versiya != "0.9.0" {
		t.Fatalf("свежая версия со второй страницы не найдена: %+v, %v", n, err)
	}
	if zaprosov != 2 {
		t.Fatalf("запросов %d, ждал 2", zaprosov)
	}
}

// Бесконечный rel="next" обрывается на пределе страниц.
func TestObkhodStranicOgranichen(t *testing.T) {
	zaprosov := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zaprosov++
		w.Header().Set("Link", fmt.Sprintf(`<%s/spisok?page=%d>; rel="next"`, srv.URL, zaprosov+1))
		fmt.Fprint(w, `[{"tag_name":"core-v1-1","assets":[]}]`)
	}))
	t.Cleanup(srv.Close)
	relizy, err := SobratStranicy[relizJSON](context.Background(), srv.Client(), srv.URL+"/spisok", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if zaprosov != PredelStranicRelizov || len(relizy) != PredelStranicRelizov {
		t.Fatalf("запросов %d, релизов %d, ждал %d", zaprosov, len(relizy), PredelStranicRelizov)
	}
}

// Ссылка на следующую страницу на чужой сервер не берётся.
func TestStranicaNaChuzhoyServerNeBeretsya(t *testing.T) {
	zaprosov := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zaprosov++
		w.Header().Set("Link", `<https://example.invalid/spisok?page=2>; rel="next"`)
		fmt.Fprint(w, `[]`)
	}))
	t.Cleanup(srv.Close)
	if _, err := SobratStranicy[relizJSON](context.Background(), srv.Client(), srv.URL+"/spisok", 1<<20); err != nil {
		t.Fatal(err)
	}
	if zaprosov != 1 {
		t.Fatalf("запросов %d", zaprosov)
	}
}

// Ответ не 200 на второй странице — та же ошибка статуса, что и на первой.
func TestOshibkaNaVtoroyStranice(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/spisok?page=2>; rel="next"`, srv.URL))
		fmt.Fprint(w, `[{"tag_name":"app-v0.6.1","assets":[]}]`)
	}))
	t.Cleanup(srv.Close)
	_, err := SobratStranicy[relizJSON](context.Background(), srv.Client(), srv.URL+"/spisok", 1<<20)
	if s, ok := err.(*OshibkaStatusa); !ok || s.Kod != http.StatusForbidden {
		t.Fatalf("ошибка %v, ждал OshibkaStatusa 403", err)
	}
}

func TestSleduyushchayaStranica(t *testing.T) {
	link := `<https://api.github.com/repositories/1/releases?per_page=100&page=2>; rel="next", <https://api.github.com/repositories/1/releases?per_page=100&page=2>; rel="last"`
	if got := SleduyushchayaStranica(link); got != "https://api.github.com/repositories/1/releases?per_page=100&page=2" {
		t.Fatalf("next = %q", got)
	}
	if got := SleduyushchayaStranica(`<https://x/?page=1>; rel="prev"`); got != "" {
		t.Fatalf("next без next = %q", got)
	}
}
