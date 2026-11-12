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
	// Заявка ждёт — шаг 30 секунд.
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

func TestOplataIstoriyaIShagPriZhdushchey(t *testing.T) {
	var otvet atomic.Value
	var n, kodPost atomic.Int32
	otvet.Store(`{"platit": true, "cena": 249, "zhdet": true, "srok": "2099-01-01T00:00:00Z",
		"istoriya": [{"data": "2026-11-12T05:00:00Z", "summa": 249, "do": "2026-12-12T07:00:00Z"}]}`)
	s := dverOplaty(t, &otvet, &n, &kodPost)
	o := zaprositOkno(t, s, "GET", "/api/oplata")
	if len(o.Istoriya) != 1 || o.Istoriya[0] != (podpiska.ZapisOplaty{DataUnix: 1794459600, Summa: 249, DoUnix: 1797058800}) {
		t.Fatalf("история: %+v", o.Istoriya)
	}
	s.oplata.kogda = time.Now().Add(-10 * time.Second)
	zaprositOkno(t, s, "GET", "/api/oplata")
	if n.Load() != 1 {
		t.Fatalf("ждущая заявка: спрошено раньше 30 секунд: %d", n.Load())
	}
	s.oplata.kogda = time.Now().Add(-31 * time.Second)
	zaprositOkno(t, s, "GET", "/api/oplata")
	if n.Load() != 2 {
		t.Fatalf("ждущая заявка: через 30 секунд не спрошено: %d", n.Load())
	}
}

func sprositNapominanie(t *testing.T, s *Sluzhba, metod, put string) otvetNapominaniya {
	t.Helper()
	w := httptest.NewRecorder()
	s.napominanieRuchka(w, httptest.NewRequest(metod, put, nil))
	var o otvetNapominaniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("%s: %v %s", put, err, w.Body)
	}
	return o
}

func TestNapominanieOplaty(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())
	var otvet atomic.Value
	var n, kodPost atomic.Int32
	srok := time.Now().Add(48 * time.Hour).UTC()
	otvet.Store(`{"platit": true, "cena": 249, "srok": "` + srok.Format(time.RFC3339) + `"}`)
	s := dverOplaty(t, &otvet, &n, &kodPost)

	o := sprositNapominanie(t, s, "GET", "/api/napominanie")
	if !strings.HasPrefix(o.Klyuch, "skoro:") || !strings.HasPrefix(o.Tekst, "Подписка до ") || n.Load() != 1 {
		t.Fatalf("за двое суток до срока: %+v n=%d", o, n.Load())
	}
	// Пока не отмечено показанным — то же самое; сервер второй раз не спрошен (шаг 6 часов).
	if o2 := sprositNapominanie(t, s, "GET", "/api/napominanie"); o2 != o || n.Load() != 1 {
		t.Fatalf("повтор: %+v n=%d", o2, n.Load())
	}
	sprositNapominanie(t, s, "POST", "/api/napominanie?klyuch="+o.Klyuch)
	if s.Nastroyki.NapomnenoOplaty != o.Klyuch {
		t.Fatalf("отметка: %q", s.Nastroyki.NapomnenoOplaty)
	}
	if o2 := sprositNapominanie(t, s, "GET", "/api/napominanie"); o2.Klyuch != "" {
		t.Fatalf("показанное повторилось: %+v", o2)
	}

	// Срок вышел: новое напоминание; окно на экране — пузыря нет, отметка ставится.
	otvet.Store(`{"platit": true, "cena": 249, "srok": "` + time.Now().Add(-time.Hour).UTC().Format(time.RFC3339) + `"}`)
	s.oplata.kogda = time.Now().Add(-podpiska.ShagOprosaOplatyFon - time.Second)
	s.oknoSprosilo = time.Now()
	if o2 := sprositNapominanie(t, s, "GET", "/api/napominanie"); o2.Klyuch != "" || n.Load() != 2 {
		t.Fatalf("окно на экране: %+v n=%d", o2, n.Load())
	}
	if !strings.HasPrefix(s.Nastroyki.NapomnenoOplaty, "konchilsya:") {
		t.Fatalf("при открытом окне не отмечено: %q", s.Nastroyki.NapomnenoOplaty)
	}

	// Ждущая заявка — напоминаний нет.
	s.Nastroyki.NapomnenoOplaty = ""
	s.oknoSprosilo = time.Time{}
	otvet.Store(`{"platit": true, "cena": 249, "zhdet": true, "srok": "` + time.Now().Add(-time.Hour).UTC().Format(time.RFC3339) + `"}`)
	s.oplata.kogda = time.Now().Add(-podpiska.ShagOprosaOplatyFon - time.Second)
	if o2 := sprositNapominanie(t, s, "GET", "/api/napominanie"); o2.Klyuch != "" || n.Load() != 3 {
		t.Fatalf("ждущая заявка: %+v n=%d", o2, n.Load())
	}
}
