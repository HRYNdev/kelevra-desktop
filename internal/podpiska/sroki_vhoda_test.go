package podpiska

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// visyashchayaDver — дверь, которая принимает соединение и молчит: TCP есть, TLS и
// ответа нет (так выглядит дверь, которую режут по дороге).
func visyashchayaDver(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		l.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
	})
	return l.Addr().String()
}

// staryyServer — дверь старой системы по https: входа устройства нет (404), код по
// старому пути знает.
func staryyServer(t *testing.T, kod string) *httptest.Server {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/k/"+kod {
			w.Write([]byte(primerKonfiga))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(s.Close)
	return s
}

// Вторая дверь режется на соединении: человек старой системы входит за срок соединения,
// а не за 20 с срока клиента; с двумя такими дверями вход не падает по сроку.
func TestVoytiVisyashchieDveriNeDerzhatVhod(t *testing.T) {
	stary := staryyServer(t, "KODSTARY1")
	k := &Klient{Shema: "https", HTTP: stary.Client(), DeviceID: "test-hwid",
		SrokSoedineniyaVhoda: 300 * time.Millisecond}
	staryHost := strings.TrimPrefix(stary.URL, "https://")
	for _, dveri := range [][]string{
		{staryHost, visyashchayaDver(t)},
		{visyashchayaDver(t), visyashchayaDver(t), staryHost},
	} {
		ctx, otmena := context.WithTimeout(context.Background(), 30*time.Second)
		nachalo := time.Now()
		adres, err := k.Voyti(ctx, "KODSTARY1", dveri)
		proshlo := time.Since(nachalo)
		otmena()
		if err != nil || adres != stary.URL+"/k/KODSTARY1" {
			t.Fatalf("%v: ждал старый путь, получил %q, %v", dveri, adres, err)
		}
		// Висящие двери стоят по сроку соединения каждая; второй проход их не ждёт,
		// пока не спрошена дверь, ответившая 404.
		if proshlo > 3*time.Second {
			t.Fatalf("%v: вход занял %s", dveri, proshlo)
		}
	}
}

// Дверь соединилась, но молчит с ответом: ждём не дольше срока ответа, весь первый
// проход — не дольше своего срока.
func TestVoytiMolchashchiyOtvetOgranichen(t *testing.T) {
	stop := make(chan struct{})
	molchit := dver(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	})
	defer close(stop)
	staraya := dver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/k/KODSTARY2" {
			w.Write([]byte(primerKonfiga))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	k := klientDlyaDverey()
	k.SrokOtvetaVhoda = 400 * time.Millisecond
	nachalo := time.Now()
	adres, err := k.Voyti(context.Background(), "KODSTARY2", []string{staraya, molchit})
	if err != nil || adres != "http://"+staraya+"/k/KODSTARY2" {
		t.Fatalf("ждал старый путь, получил %q, %v", adres, err)
	}
	if p := time.Since(nachalo); p > 3*time.Second {
		t.Fatalf("вход занял %s", p)
	}

	// Срок всего прохода при долгом сроке ответа: молчащая дверь съедает его целиком,
	// до старой двери первый проход не доходит — она спрашивается старым путём сразу.
	k = klientDlyaDverey()
	k.SrokOtvetaVhoda, k.SrokPervogoProhoda = 10*time.Second, 500*time.Millisecond
	ctx, otmena := context.WithTimeout(context.Background(), 30*time.Second)
	defer otmena()
	nachalo = time.Now()
	adres, err = k.Voyti(ctx, "KODSTARY2", []string{molchit, staraya})
	if err != nil || adres != "http://"+staraya+"/k/KODSTARY2" {
		t.Fatalf("ждал старый путь, получил %q, %v", adres, err)
	}
	if p := time.Since(nachalo); p > 3*time.Second {
		t.Fatalf("первый проход не уложился в свой срок: %s", p)
	}
}

// Дверь не ответила на вход устройства (режут только его или только напрямую), но
// старым путём отвечает — человек входит старым путём, а не получает отказ.
func TestVoytiMolchavshayaDverIdyotStarymPutyom(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	d := dver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			select {
			case <-r.Context().Done():
			case <-stop:
			}
			return
		}
		if r.URL.Path == "/k/KODSTARY3" {
			w.Write([]byte(primerKonfiga))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	k := klientDlyaDverey()
	k.SrokOtvetaVhoda = 300 * time.Millisecond
	adres, err := k.Voyti(context.Background(), "KODSTARY3", []string{d})
	if err != nil || adres != "http://"+d+"/k/KODSTARY3" {
		t.Fatalf("ждал старый путь, получил %q, %v", adres, err)
	}
}
