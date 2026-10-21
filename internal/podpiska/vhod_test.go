package podpiska

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dver — подставная выдача: host без схемы и сам сервер.
func dver(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return strings.TrimPrefix(s.URL, "http://")
}

func klientDlyaDverey() *Klient {
	return &Klient{Shema: "http", DeviceID: "test-hwid", HTTP: &http.Client{}}
}

// Дверь своей сети знает вход устройства: отдаёт адрес /u/<ключ>, его и сохраняем.
func TestVoytiVhodUstroystva(t *testing.T) {
	var zagolovok string
	d := dver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/k/KOD1/ustroystvo" {
			zagolovok = r.Header.Get("X-Device-Id")
			w.Write([]byte(`{"profil":"https://node2kv.example/u/klyuch123456","slot":"01-6"}`))
			return
		}
		w.WriteHeader(404)
	})
	adres, err := klientDlyaDverey().Voyti(context.Background(), "KOD1", []string{d})
	if err != nil {
		t.Fatalf("ждал вход, получил: %v", err)
	}
	if adres != "https://node2kv.example/u/klyuch123456" {
		t.Fatalf("не тот адрес: %s", adres)
	}
	if zagolovok != "test-hwid" {
		t.Fatalf("устройство не назвало себя: %q", zagolovok)
	}
}

// Старый сервер семьи входа устройства не знает (404), своя сеть кода не знает (404) —
// код берётся старым путём /k/ у первой двери: семья остаётся там же, где была.
// Старым путём вторую дверь не спрашивают.
func TestVoytiStaryyPutNaPervoyDveri(t *testing.T) {
	staraya := dver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/k/KOD2" {
			w.Write([]byte(primerKonfiga))
			return
		}
		w.WriteHeader(404)
	})
	var novayaZaprosy []string
	novaya := dver(t, func(w http.ResponseWriter, r *http.Request) {
		novayaZaprosy = append(novayaZaprosy, r.Method+" "+r.URL.Path)
		w.WriteHeader(404)
	})
	adres, err := klientDlyaDverey().Voyti(context.Background(), "KOD2", []string{staraya, novaya})
	if err != nil {
		t.Fatalf("ждал вход старым путём, получил: %v", err)
	}
	if adres != "http://"+staraya+"/k/KOD2" {
		t.Fatalf("не тот адрес: %s", adres)
	}
	if strings.Join(novayaZaprosy, ",") != "POST /k/KOD2/ustroystvo" {
		t.Fatalf("у второй двери ждал только вход устройства, были: %v", novayaZaprosy)
	}
}

// Переехавший человек: старый сервер пересылает его старый путь в свою сеть и отдал бы
// общий /k/<код>, но своя сеть (вторая дверь) заводит машину по ключу — берём ключ.
// Иначе на машине не действуют лимит устройств и отключение по одному.
func TestVoytiPereehavshiyPoluchaetKlyuch(t *testing.T) {
	staryyPutZvali := false
	staraya := dver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/k/KOD7" {
			staryyPutZvali = true
			w.Write([]byte(primerKonfiga))
			return
		}
		w.WriteHeader(404)
	})
	novaya := dver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/k/KOD7/ustroystvo" {
			w.Write([]byte(`{"profil":"https://n/u/k7k7k7k7k7k7","slot":"01-7"}`))
			return
		}
		w.WriteHeader(404)
	})
	adres, err := klientDlyaDverey().Voyti(context.Background(), "KOD7", []string{staraya, novaya})
	if err != nil || adres != "https://n/u/k7k7k7k7k7k7" {
		t.Fatalf("ждал ключ своей сети, получил %q, %v", adres, err)
	}
	if staryyPutZvali {
		t.Fatal("старым путём ходили, хотя вход устройства удался")
	}
}

// 200 без адреса /u/<ключ> (пустой, общий /k/, чужая схема) — не вход: такой адрес не
// сохраняется, идём к следующей двери.
func TestVoytiNegodnyyAdresProfilya(t *testing.T) {
	for _, plohoy := range []string{`{}`, `{"profil":"https://n/k/OBSHCHIY_KOD"}`, `{"profil":"ftp://n/u/k8k8k8k8k8k8"}`, `{"profil":"https://n/u/k8/../x"}`} {
		d1 := dver(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				w.Write([]byte(plohoy))
				return
			}
			w.WriteHeader(404)
		})
		d2 := dver(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				w.Write([]byte(`{"profil":"https://n/u/k9k9k9k9k9k9"}`))
				return
			}
			w.WriteHeader(404)
		})
		adres, err := klientDlyaDverey().Voyti(context.Background(), "KOD8", []string{d1, d2})
		if err != nil || adres != "https://n/u/k9k9k9k9k9k9" {
			t.Errorf("%s: ждал вторую дверь, получил %q, %v", plohoy, adres, err)
		}
	}
}

// Код, перепечатанный группами через пробел, уходит без пробелов; знаки, которых в коде
// не бывает, до сети не доходят.
func TestVoytiKodIzVvoda(t *testing.T) {
	var put string
	d := dver(t, func(w http.ResponseWriter, r *http.Request) {
		put = r.URL.Path
		w.Write([]byte(`{"profil":"https://n/u/kAkAkAkAkAkA"}`))
	})
	if _, err := klientDlyaDverey().Voyti(context.Background(), " K7QM 2XRT\t9PLA ", []string{d}); err != nil {
		t.Fatalf("вход: %v", err)
	}
	if put != "/k/K7QM2XRT9PLA/ustroystvo" {
		t.Fatalf("не тот путь: %q", put)
	}
	put = ""
	for _, plohoy := range []string{"abc/../../logs", "K7QM?x=1", "КОД12345", "ab"} {
		if _, err := klientDlyaDverey().Voyti(context.Background(), plohoy, []string{d}); err == nil {
			t.Errorf("%q: ждал отказ", plohoy)
		}
	}
	if put != "" {
		t.Fatalf("негодный код ушёл в сеть: %q", put)
	}
}

func TestKlyuchUstroystva(t *testing.T) {
	for adres, ozhid := range map[string]string{
		"https://node2kv.example/u/AbC_123-xyz0":  "AbC_123-xyz0",
		"https://node2kv.example/u/AbC_123-xyz0/": "AbC_123-xyz0",
		"https://subkv.example/k/KOD12345":        "",
		"":                                        "",
		"https://n/u/k/info":                      "",
	} {
		if got := KlyuchUstroystva(adres); got != ozhid {
			t.Errorf("KlyuchUstroystva(%q) = %q, ждал %q", adres, got, ozhid)
		}
	}
}

// Код своей сети: старая дверь его не знает, вторая выдаёт ключ.
func TestVoytiVtorayaDver(t *testing.T) {
	staraya := dver(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	novaya := dver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Write([]byte(`{"profil":"https://n/u/k2k2k2k2k2k2","slot":"01-3"}`))
			return
		}
		w.WriteHeader(404)
	})
	adres, err := klientDlyaDverey().Voyti(context.Background(), "KOD3", []string{staraya, novaya})
	if err != nil || adres != "https://n/u/k2k2k2k2k2k2" {
		t.Fatalf("ждал адрес второй двери, получил %q, %v", adres, err)
	}
}

// Лимит устройств: слова сервера доходят до человека, дальше по дверям не идём.
func TestVoytiLimit(t *testing.T) {
	d := dver(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"oshibka":"limit_ustroystv","tekst":"Достигнут лимит устройств"}`))
	})
	_, err := klientDlyaDverey().Voyti(context.Background(), "KOD4", []string{d})
	var o *OshibkaVhoda
	if !errors.As(err, &o) || o.Tekst != "Достигнут лимит устройств" {
		t.Fatalf("ждал отказ с текстом сервера, получил %v", err)
	}
}

// Ни одна дверь кода не знает — это «нет такого кода», а не «нет связи».
func TestVoytiNeizvestnyyKod(t *testing.T) {
	d1 := dver(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	d2 := dver(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	_, err := klientDlyaDverey().Voyti(context.Background(), "NETU", []string{d1, d2})
	var o *OshibkaKoda
	if !errors.As(err, &o) {
		t.Fatalf("ждал «код неизвестен», получил %v", err)
	}
}

// Первая дверь недоступна — идём ко второй, а не сдаёмся.
func TestVoytiPervayaDverLezhit(t *testing.T) {
	novaya := dver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Write([]byte(`{"profil":"https://n/u/k3k3k3k3k3k3"}`))
			return
		}
		w.WriteHeader(404)
	})
	adres, err := klientDlyaDverey().Voyti(context.Background(), "KOD5", []string{"127.0.0.1:1", novaya})
	if err != nil || adres != "https://n/u/k3k3k3k3k3k3" {
		t.Fatalf("ждал вторую дверь, получил %q, %v", adres, err)
	}
}

func TestRazobratSsylku(t *testing.T) {
	sluchai := []struct {
		vvod, adres, hvost string
		ok                 bool
	}{
		{"https://node2kv.chickenkiller.com/u/AbC_123-xyz", "https://node2kv.chickenkiller.com/u/AbC_123-xyz", "AbC_123-xyz", true},
		{"  https://subkv.chickenkiller.com/k/KOD12345/  ", "https://subkv.chickenkiller.com/k/KOD12345", "KOD12345", true},
		{"https://node2kv.chickenkiller.com/u/klyuch99/info?device=x", "https://node2kv.chickenkiller.com/u/klyuch99", "klyuch99", true},
		{"VS4EMH9PQUMX", "", "", false},
		{"https://example.com/panel/", "", "", false},
	}
	for _, s := range sluchai {
		a, h, ok := RazobratSsylku(s.vvod)
		if a != s.adres || h != s.hvost || ok != s.ok {
			t.Errorf("%q: получил (%q, %q, %v), ждал (%q, %q, %v)", s.vvod, a, h, ok, s.adres, s.hvost, s.ok)
		}
	}
}

// Профиль и сведения по сохранённому адресу: /info приклеивается к адресу, двери из сведений читаются.
func TestPoAdresu(t *testing.T) {
	d := dver(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/u/klyuch1234":
			w.Write([]byte(primerKonfiga))
		case "/u/klyuch1234/info":
			if r.URL.Query().Get("device") != "test-hwid" {
				w.WriteHeader(400)
				return
			}
			w.Write([]byte(`{"name":"owner","active":true,"dveri":["a.example","b.example"]}`))
		default:
			w.WriteHeader(404)
		}
	})
	k := klientDlyaDverey()
	adres := "http://" + d + "/u/klyuch1234"
	if _, err := k.KonfigPoAdresu(context.Background(), adres); err != nil {
		t.Fatalf("профиль по адресу: %v", err)
	}
	sv, err := k.SvedeniyaPoAdresu(context.Background(), adres)
	if err != nil || sv.Imya != "owner" || len(sv.Dveri) != 2 {
		t.Fatalf("сведения по адресу: %+v, %v", sv, err)
	}
	if _, err := k.KonfigPoAdresu(context.Background(), "http://"+d+"/u/chuzhoy0000"); !errors.As(err, new(*OshibkaKoda)) {
		t.Fatalf("неизвестный ключ должен дать OshibkaKoda, получил %v", err)
	}
}

// Пропуск в панель: ключ из адреса /u/<ключ> уходит заголовком, ответ — одноразовая ссылка.
func TestPropuskPaneli(t *testing.T) {
	var klyuch string
	d := dver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/panel/api/v1/vhod/ustroystvo" {
			klyuch = r.Header.Get("X-Device-Key")
			if klyuch != "klyuch5555" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Write([]byte(`{"ssylka":"https://n/panel/vhod/abc"}`))
			return
		}
		w.WriteHeader(404)
	})
	k := klientDlyaDverey()
	s, err := k.PropuskPaneli(context.Background(), "http://"+d+"/u/klyuch5555")
	if err != nil || s != "https://n/panel/vhod/abc" || klyuch != "klyuch5555" {
		t.Fatalf("ждал ссылку, получил %q, %v (ключ %q)", s, err, klyuch)
	}
	if _, err := k.PropuskPaneli(context.Background(), "http://"+d+"/u/chuzhoy0000"); err == nil {
		t.Fatal("чужой ключ дал ссылку")
	}
	if _, err := k.PropuskPaneli(context.Background(), "https://subkv.example/k/KOD"); err == nil {
		t.Fatal("с подписки по коду (без ключа устройства) панель открываться не должна")
	}
}
