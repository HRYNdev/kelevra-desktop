package sluzhba

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// Гейт на дыру: карточка «Подписка» писала «Пока неизвестно» вечно.
// Сведения добывались РОВНО ОДИН РАЗ — в ручке приёма кода доступа. Код
// вводят однажды, дальше приложение перезапускается сотни раз, и после
// каждого перезапуска сведения пустые. На сервере это видно как нулевое
// число запросов /info при исправно выдаваемом конфиге.
//
// Тест держит ровно это: служба обязана спросить сервер САМА, без повторного
// ввода кода.
func TestSvedeniyaBerutsyaBezPovtornogoVvodaKoda(t *testing.T) {
	var sprosili int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/info") {
			http.NotFound(w, r)
			return
		}
		sprosili++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"Vova","active":true,"limit_bytes":0,"used_bytes":42,
			"person":{"name":"Хозяин"},"device":{"name":"Настольный компьютер"},
			"devices":[{"self":true,"name":"Настольный компьютер","kind":"desktop","app_version":"0.6.47"}]}`))
	}))
	defer srv.Close()

	s := &Sluzhba{
		Nastroyki: &hranenie.Nastroyki{Kod: "TESTOVYY_KOD_1234"},
		Podpiska: &podpiska.Klient{
			Host:  strings.TrimPrefix(srv.URL, "http://"),
			Shema: "http",
		},
	}

	s.ObnovitSvedeniya(context.Background())

	if sprosili != 1 {
		t.Fatalf("сервер спросили %d раз, ждали ровно один: без запроса окно вечно пишет «Пока неизвестно»", sprosili)
	}
	if s.svedeniya == nil {
		t.Fatal("сведения не легли в службу — окно их не увидит")
	}
	if !s.svedeniya.Aktivna {
		t.Error("подписка пришла активной, а в службе лежит неактивная")
	}
	if got := len(s.svedeniya.Ustroystva); got != 1 {
		t.Errorf("устройств в сведениях %d, ждали 1 — список для шторки подписки потерян", got)
	}
}

// Без кода доступа спрашивать нечего и некого: приложение только что
// поставили, человек ещё ничего не вводил. Молча выходим, а не бьёмся в
// сервер с пустым кодом.
func TestBezKodaServerNeTrogaem(t *testing.T) {
	var sprosili int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sprosili++
	}))
	defer srv.Close()

	s := &Sluzhba{
		Nastroyki: &hranenie.Nastroyki{Kod: ""},
		Podpiska:  &podpiska.Klient{Host: strings.TrimPrefix(srv.URL, "http://"), Shema: "http"},
	}
	s.ObnovitSvedeniya(context.Background())

	if sprosili != 0 {
		t.Fatalf("сервер дёрнули %d раз при пустом коде", sprosili)
	}
	if s.svedeniya != nil {
		t.Error("без кода в службе появились сведения — взяться им неоткуда")
	}
}

// Сервер молчит или лежит — прежние сведения не должны превращаться в
// «Пока неизвестно»: подписка не пропала, пропала связь.
func TestUpavshiyServerNeStiraetPrezhniyeSvedeniya(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "лежу", http.StatusBadGateway)
	}))
	defer srv.Close()

	bylo := &podpiska.Svedeniya{Imya: "hozyain", Aktivna: true}
	s := &Sluzhba{
		Nastroyki: &hranenie.Nastroyki{Kod: "TESTOVYY_KOD_1234"},
		Podpiska:  &podpiska.Klient{Host: strings.TrimPrefix(srv.URL, "http://"), Shema: "http"},
		svedeniya: bylo,
	}
	s.ObnovitSvedeniya(context.Background())

	if s.svedeniya != bylo {
		t.Fatal("ответ 502 стёр прежние сведения — окно написало бы «Пока неизвестно» на живой подписке")
	}
}
