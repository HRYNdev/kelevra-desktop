package sluzhba

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// Гейт на баг с ноута Вовы 26.10.2026: первая сводка /info на старте службы
// падает без сети (DNS/коннект) и раньше НЕ повторялась вовсе — ждала первого
// тика обновления профиля (от получаса), и человек не видел новую версию
// пульта часами. obnovitSvedeniyaPriStarte обязана повторить сама, короткой
// лестницей (povtor_seti.go), и остановиться, как только сводка принята.
func TestSvodkaPriStartePovtoryaetPosleSetevogoOtkaza(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/info") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"owner","active":true,"limit_bytes":0,"used_bytes":1,
			"person":{"name":"Хозяин"},"device":{"name":"Ноутбук"},"devices":[]}`))
	}))
	defer srv.Close()

	s := &Sluzhba{
		Nastroyki: &hranenie.Nastroyki{Kod: "TESTOVYY_KOD_5678"},
		Podpiska:  &podpiska.Klient{Host: "127.0.0.1:1", Shema: "http"}, // сети нет: порт закрыт
	}

	var zhurnal syncBuffer
	bylo := log.Writer()
	log.SetOutput(&zhurnal)
	t.Cleanup(func() { log.SetOutput(bylo) })

	pauzy := make(chan time.Duration, 10)
	kanaly := make(chan chan time.Time, 10)
	posle := func(d time.Duration) <-chan time.Time {
		k := make(chan time.Time, 1)
		pauzy <- d
		kanaly <- k
		return k
	}

	gotovo := make(chan struct{})
	go func() {
		s.obnovitSvedeniyaPriStarteStend(context.Background(), posle, func() float64 { return 0.5 })
		close(gotovo)
	}()

	select {
	case d := <-pauzy:
		if d != 15*time.Second {
			t.Fatalf("первый повтор назначен через %s, ждал 15с", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("после сетевого отказа повтор не назначен — баг 26.10 не исправлен")
	}

	// Сеть «появилась»: подставляем рабочий сервер и отпускаем назначенную паузу.
	s.Podpiska.Host = strings.TrimPrefix(srv.URL, "http://")
	k := <-kanaly
	k <- time.Time{}

	select {
	case <-gotovo:
	case <-time.After(2 * time.Second):
		t.Fatal("после удачной попытки лестница не остановилась")
	}

	if s.svedeniya == nil || !s.svedeniya.Aktivna {
		t.Fatal("сводка после восстановления сети так и не принята")
	}
}
