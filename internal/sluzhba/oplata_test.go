package sluzhba

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// dverOplaty — дверь с /oplata и /oplatil; otvet — что отдать на GET.
func dverOplaty(t *testing.T, otvet *atomic.Value, zaprosov *atomic.Int32, kodPost *atomic.Int32) *Sluzhba {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zaprosov.Add(1)
		if r.Method == http.MethodPost {
			if k := int(kodPost.Load()); k != 0 && k != 200 {
				w.WriteHeader(k)
				return
			}
			w.Write([]byte(`{"platit": true, "cena": 249, "ssylka": "https://t.tb.ru/x", "zhdet": true}`))
			return
		}
		w.Write([]byte(otvet.Load().(string)))
	}))
	t.Cleanup(srv.Close)
	return &Sluzhba{
		Nastroyki: &hranenie.Nastroyki{Podpiska: srv.URL + "/k/ABCD"},
		Podpiska:  &podpiska.Klient{HTTP: srv.Client()},
	}
}

func zaprositOkno(t *testing.T, s *Sluzhba, metod, put string) otvetOplaty {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(metod, put, nil)
	switch {
	case strings.HasPrefix(put, "/api/oplata_qr"):
		s.oplataQrRuchka(w, r)
	case strings.HasPrefix(put, "/api/oplatil"):
		s.oplatilRuchka(w, r)
	default:
		s.oplataRuchka(w, r)
	}
	var o otvetOplaty
	if !strings.HasPrefix(put, "/api/oplata_qr") {
		if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
			t.Fatalf("%s: %v %s", put, err, w.Body)
		}
	}
	return o
}

func TestOplataOprosNeChashcheShaga(t *testing.T) {
	var otvet atomic.Value
	var n, kodPost atomic.Int32
	otvet.Store(`{"platit": true, "cena": 249, "ssylka": "https://t.tb.ru/x", "nomer": "+7 900", "srok": "2099-01-01T00:00:00Z"}`)
	s := dverOplaty(t, &otvet, &n, &kodPost)
	o := zaprositOkno(t, s, "GET", "/api/oplata")
	if !o.Est || o.Cena != 249 || o.Srok != "obychnyj" || o.Nomer != "+7 900" || n.Load() != 1 {
		t.Fatalf("%+v n=%d", o, n.Load())
	}
	zaprositOkno(t, s, "GET", "/api/oplata")
	if n.Load() != 1 {
		t.Fatalf("второй заход окна раньше часа спросил сервер: %d", n.Load())
	}
	zaprositOkno(t, s, "GET", "/api/oplata?sila=1")
	if n.Load() != 2 {
		t.Fatalf("sila: %d", n.Load())
	}
	// Заявка ждёт — шаг 5 минут.
	o = zaprositOkno(t, s, "POST", "/api/oplatil")
	if !o.Zhdet || o.Otpravka != "" {
		t.Fatalf("после «Я оплатил»: %+v", o)
	}
	s.oplata.kogda = time.Now().Add(-6 * time.Minute)
	otvet.Store(`{"platit": false}`)
	o = zaprositOkno(t, s, "GET", "/api/oplata")
	if o.Est {
		t.Fatalf("неплатящему — ничего: %+v", o)
	}
}

func TestOplatilChastoISboy(t *testing.T) {
	var otvet atomic.Value
	var n, kodPost atomic.Int32
	otvet.Store(`{"platit": true, "cena": 249}`)
	s := dverOplaty(t, &otvet, &n, &kodPost)
	zaprositOkno(t, s, "GET", "/api/oplata")
	kodPost.Store(429)
	if o := zaprositOkno(t, s, "POST", "/api/oplatil"); o.Otpravka != "uzhe" || !o.Est {
		t.Fatalf("429: %+v", o)
	}
	kodPost.Store(502)
	if o := zaprositOkno(t, s, "POST", "/api/oplatil"); o.Otpravka != "net_svyazi" || !o.Est {
		t.Fatalf("502: %+v", o)
	}
	if o := zaprositOkno(t, s, "GET", "/api/oplata?zabyt=1"); o.Otpravka != "" {
		t.Fatalf("zabyt: %+v", o)
	}
}

func TestOplataNeOtvetOplatyBolsheNeSprashivaem(t *testing.T) {
	var otvet atomic.Value
	var n, kodPost atomic.Int32
	otvet.Store(`{"outbounds": []}`)
	s := dverOplaty(t, &otvet, &n, &kodPost)
	zaprositOkno(t, s, "GET", "/api/oplata")
	zaprositOkno(t, s, "GET", "/api/oplata?sila=1")
	if n.Load() != 1 {
		t.Fatalf("адрес без оплаты спрошен повторно: %d", n.Load())
	}
}

func TestOplataQR(t *testing.T) {
	var otvet atomic.Value
	var n, kodPost atomic.Int32
	otvet.Store(`{"platit": true, "cena": 249, "ssylka": "https://t.tb.ru/c2c-qr-choose-bank?requisiteNumber=79000000000&bankCode=100000000004"}`)
	s := dverOplaty(t, &otvet, &n, &kodPost)
	w := httptest.NewRecorder()
	s.oplataQrRuchka(w, httptest.NewRequest("GET", "/api/oplata_qr", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("до ответа сервера QR быть не должно: %d", w.Code)
	}
	zaprositOkno(t, s, "GET", "/api/oplata")
	w = httptest.NewRecorder()
	s.oplataQrRuchka(w, httptest.NewRequest("GET", "/api/oplata_qr", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(w.Body.String(), "<path") {
		t.Fatalf("QR: %d %s", w.Code, w.Body.String()[:min(80, w.Body.Len())])
	}
}
