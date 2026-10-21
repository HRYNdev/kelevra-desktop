package sluzhba

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Машина на своём ключе называет его журналам — выдача кладёт журнал в папку слота
// устройства. Подписка по общему коду ключа не знает; стенд с подменённым хостом
// (KELEVRA_PODPISKA) ключ не получает — он только той двери, что его выдала.
func TestZhurnalyNesutKlyuchUstroystva(t *testing.T) {
	s := stend(t)
	zagolovki := func() http.Header {
		h := http.Header{}
		s.otpravshchikZhurnalov().Zagolovki(h, "dev-1", "0.0.0-test")
		return h
	}

	s.Podpiska.Host = ""
	s.Nastroyki.Podpiska = "https://node2kv.example/u/KLYUCH_USTROYSTVA_1"
	h := zagolovki()
	if h.Get("X-Device-Key") != "KLYUCH_USTROYSTVA_1" {
		t.Fatalf("журнал без ключа устройства: %v", h)
	}
	if h.Get("X-Device-Id") != "dev-1" {
		t.Fatalf("журнал потерял заголовки устройства: %v", h)
	}

	s.Nastroyki.Podpiska = "https://subkv.example/k/OBSHCHIY_KOD"
	if k := zagolovki().Get("X-Device-Key"); k != "" {
		t.Fatalf("подписка по коду, а ключ ушёл: %q", k)
	}

	s.Podpiska.Host = "127.0.0.1:1"
	s.Nastroyki.Podpiska = "https://node2kv.example/u/KLYUCH_USTROYSTVA_1"
	if k := zagolovki().Get("X-Device-Key"); k != "" {
		t.Fatalf("ключ ушёл на чужой хост журналов: %q", k)
	}
}

// Ввод кода в окне: код группами через пробел — вход устройства по чистому коду,
// в настройки ложатся чистый код и адрес /u/<ключ>.
func TestVvodKodaSohranyaetKlyuch(t *testing.T) {
	s := stend(t)
	profil, err := os.ReadFile("../konfig/testdata/profil_telefona.json")
	if err != nil {
		t.Fatal(err)
	}
	var vhod string
	var dver *httptest.Server
	dver = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/k/K7QM2XRT9PLA/ustroystvo":
			vhod = r.Header.Get("X-Device-Id")
			w.Write([]byte(`{"profil":"` + dver.URL + `/u/KLYUCH_USTROYSTVA_2","slot":"01-2"}`))
		case r.URL.Path == "/u/KLYUCH_USTROYSTVA_2":
			w.Write(profil)
		case r.URL.Path == "/u/KLYUCH_USTROYSTVA_2/info":
			w.Write([]byte(`{"name":"test","active":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer dver.Close()
	t.Setenv("KELEVRA_PODPISKA", strings.TrimPrefix(dver.URL, "http://"))

	telo, _ := json.Marshal(map[string]string{"kod": " K7QM 2XRT 9PLA "})
	w := httptest.NewRecorder()
	s.Obsluzhit().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/"+s.klyuch+"/api/kod", bytes.NewReader(telo)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "gotovo") {
		t.Fatalf("вход не удался: %d %s", w.Code, w.Body.String())
	}
	if vhod == "" {
		t.Fatal("вход устройства без X-Device-Id")
	}
	if s.Nastroyki.Kod != "K7QM2XRT9PLA" {
		t.Fatalf("в настройках не чистый код: %q", s.Nastroyki.Kod)
	}
	if s.Nastroyki.Podpiska != dver.URL+"/u/KLYUCH_USTROYSTVA_2" {
		t.Fatalf("в настройках не адрес ключа: %q", s.Nastroyki.Podpiska)
	}
}
