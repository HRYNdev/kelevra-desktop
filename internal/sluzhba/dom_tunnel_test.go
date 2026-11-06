package sluzhba

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// «Дом» молчит, но и обычный туннель плох (очередь узлов только что мерила
// текущий узел — не ответил): ядро не перезапускается, сколько бы проб ни
// провалилось. Туннель ожил — лечение на первой же неудачной пробе «Дома».
// Поле 31.10–07.11: в 21 из 33 серий «Дом: нет связи» туннель был плох, а
// самолечение перезапускало ядро — TUN моргал, «Дом» не возвращался.
func TestDomNeLechitPriPlohomTunnele(t *testing.T) {
	st := novyyStendLecheniya(t)
	z := zhurnalDlyaProverki(t)
	for i := 0; i < 6; i++ {
		st.s.otmetitTunnelPoOcheredi(false) // очередь: текущий узел не ответил
		st.sejchas = st.sejchas.Add(ShagSlezhkiZaDomom)
		st.proba()
	}
	st.zhdat(t, 0, "обычный туннель плох")
	if !strings.Contains(z.String(), "не лечу, пока не оживёт обычный туннель") {
		t.Fatalf("нет строки о плохом туннеле:\n%s", z.String())
	}
	st.s.otmetitTunnelPoOcheredi(true)
	st.sejchas = st.sejchas.Add(ShagSlezhkiZaDomom)
	st.proba()
	st.zhdat(t, 1, "туннель ожил, «Дом» молчит")
}

// Итог очереди старше svezhestOcheredi не в счёт: решает замер моста.
func TestDomStaryyItogOcherediNeVSchet(t *testing.T) {
	st := novyyStendLecheniya(t)
	st.s.otmetitTunnelPoOcheredi(false)
	st.sejchas = st.sejchas.Add(svezhestOcheredi + time.Second)
	st.proba()
	st.proba()
	st.proba()
	st.zhdat(t, 1, "итог очереди устарел, мост жив")
}

// Замер выхода моста: нужно два удачных подряд; второй не ответил —
// туннель плох. Clash API молчит вовсе (ядро зависло) — «жив»: перезапуск
// лечит и зависшее ядро.
func TestDomMostDvaZamera(t *testing.T) {
	s, priv := stendDoma(t)
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 1, "100.76.0.1:51820")})
	s.dom.perezapuski.Wait()
	var zamerov, udachnyh atomic.Int32
	udachnyh.Store(1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/delay") {
			w.WriteHeader(http.StatusOK)
			return
		}
		n := zamerov.Add(1)
		if n <= udachnyh.Load() {
			_, _ = w.Write([]byte(`{"delay":42}`))
			return
		}
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte(`{"message":"timeout"}`))
	}))
	t.Cleanup(srv.Close)
	s.Yadro.Api = strings.TrimPrefix(srv.URL, "http://")

	if zhiv, _ := s.mostOtvechaet(context.Background()); zhiv || zamerov.Load() != 2 {
		t.Fatalf("первый замер удачный, второй нет: жив %v, замеров %d", zhiv, zamerov.Load())
	}
	zamerov.Store(0)
	udachnyh.Store(2)
	if zhiv, _ := s.mostOtvechaet(context.Background()); !zhiv || zamerov.Load() != 2 {
		t.Fatalf("два удачных: жив %v, замеров %d", zhiv, zamerov.Load())
	}
	srv.Close()
	if zhiv, _ := s.mostOtvechaet(context.Background()); !zhiv {
		t.Fatal("Clash API молчит (ядро зависло) — ждали «жив»: перезапуск лечит и его")
	}
}

// По смене сети — то же условие: туннель плох по очереди — ядро не трогаем.
func TestDomPoSmeneSetiPriPlohomTunnele(t *testing.T) {
	bezPauzyPoSmene(t)
	s, priv := stendDoma(t)
	s.dom.rabotaetDlyaStenda = func() bool { return true }
	s.dom.proba = func(context.Context, string, string) error { return errors.New("молчит") }
	s.dom.most = func(context.Context) (bool, string) { return true, "Соединение" }
	var perezapuski []string
	s.dom.perezapustit = func(p string, _ uint64) { perezapuski = append(perezapuski, p) }
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 1, "100.76.0.1:51820")})
	s.dom.perezapuski.Wait()

	s.otmetitTunnelPoOcheredi(false)
	s.proveritDomPoSmeneSeti(context.Background(), "другая сеть")
	s.dom.perezapuski.Wait()
	if len(perezapuski) != 0 {
		t.Fatalf("туннель плох, а по смене сети ядро перезапущено: %v", perezapuski)
	}
	s.otmetitTunnelPoOcheredi(true)
	s.dom.proverkaPoSmene = time.Time{}
	s.proveritDomPoSmeneSeti(context.Background(), "другая сеть")
	s.dom.perezapuski.Wait()
	if len(perezapuski) != 1 {
		t.Fatalf("туннель жив, «Дом» молчит — ждали перезапуск: %v", perezapuski)
	}
}
