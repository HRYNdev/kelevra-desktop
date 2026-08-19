// Пакет sluzhba: вся работа приложения, выставленная наружу как маленький
// локальный HTTP API, и интерфейс поверх него.
//
// Интерфейс — обычная страница в окне приложения. Такой стык даёт две вещи:
// окно можно проверить в браузере на сервере, где Windows нет, а логика
// проверяется без окна вовсе.
package sluzhba

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

//go:embed oblik/*
var oblik embed.FS

// Sluzhba — приложение целиком, без оболочки окна.
type Sluzhba struct {
	Nastroyki *hranenie.Nastroyki
	Yadro     *yadro.Yadro
	Podpiska  *podpiska.Klient

	zamok      sync.Mutex
	svedeniya  *podpiska.Svedeniya
	klyuch     string
	kachaemBin bool // идёт скачивание ядра
}

// Novaya собирает службу на настоящих путях приложения.
// KELEVRA_PODPISKA и KELEVRA_SHEMA переопределяют сервер подписки — это нужно
// только для проверки приложения на стенде, у пользователя они не заданы.
func Novaya() (*Sluzhba, error) {
	n, err := hranenie.Zagruzit()
	if err != nil {
		return nil, err
	}
	if err := hranenie.Sohranit(n); err != nil { // закрепляем device_id при первом запуске
		return nil, err
	}
	return &Sluzhba{
		Nastroyki: n,
		Yadro:     &yadro.Yadro{Bin: hranenie.PutYadra(), Papka: hranenie.PapkaYadra()},
		Podpiska:  &podpiska.Klient{DeviceID: n.DeviceID, Host: os.Getenv("KELEVRA_PODPISKA"), Shema: os.Getenv("KELEVRA_SHEMA")},
		klyuch:    sluchaynyy(),
	}, nil
}

// Adres — на чём слушать: только петля, наружу приложение не смотрит.
func (s *Sluzhba) Slushat() (net.Listener, string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	url := fmt.Sprintf("http://%s/%s/", l.Addr().String(), s.klyuch)
	return l, url, nil
}

// Obsluzhit — маршруты приложения. Всё висит под случайным ключом в пути,
// чтобы чужая программа на этой же машине не дёргала наше API.
func (s *Sluzhba) Obsluzhit() http.Handler {
	m := http.NewServeMux()
	pref := "/" + s.klyuch
	stranicy, _ := fsPodpapki()
	m.Handle(pref+"/", http.StripPrefix(pref+"/", http.FileServer(http.FS(stranicy))))
	m.HandleFunc(pref+"/api/sostoyanie", s.sostoyanie)
	m.HandleFunc(pref+"/api/kod", s.kod)
	m.HandleFunc(pref+"/api/podklyuchit", s.podklyuchit)
	m.HandleFunc(pref+"/api/otklyuchit", s.otklyuchit)
	return m
}

type otvetSostoyaniya struct {
	Sost       string `json:"sost"`
	KodEst     bool   `json:"kod_est"`
	YadroEst   bool   `json:"yadro_est"`
	KachaemBin bool   `json:"kachaem_yadro"`
	Beda       string `json:"beda,omitempty"`
	VverhBayt  int64  `json:"vverh_bayt"`
	VnizBayt   int64  `json:"vniz_bayt"`
	Imya       string `json:"imya,omitempty"`
	DoUnix     int64  `json:"do_unix,omitempty"`
	PID        string `json:"pid,omitempty"`
}

func (s *Sluzhba) sostoyanie(w http.ResponseWriter, r *http.Request) {
	o := otvetSostoyaniya{
		Sost:     string(s.Yadro.Sost()),
		KodEst:   s.Nastroyki.Kod != "",
		YadroEst: s.Yadro.EstBinar(),
		Beda:     s.Yadro.PoslednyayaBeda(),
		PID:      s.Yadro.PID(),
	}
	if t, err := s.Yadro.Trafik(); err == nil {
		o.VverhBayt, o.VnizBayt = t.VverhBayt, t.VnizBayt
	}
	s.zamok.Lock()
	o.KachaemBin = s.kachaemBin
	if s.svedeniya != nil {
		o.Imya, o.DoUnix = s.svedeniya.Imya, s.svedeniya.Do
	}
	s.zamok.Unlock()
	otdat(w, o, nil)
}

// kod принимает код доступа, качает по нему конфиг и запоминает код,
// только если конфиг настоящий: иначе приложение запомнит нерабочий код.
func (s *Sluzhba) kod(w http.ResponseWriter, r *http.Request) {
	var vhod struct {
		Kod string `json:"kod"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&vhod); err != nil {
		otdat(w, nil, fmt.Errorf("не разобрал запрос"))
		return
	}
	kod := strings.TrimSpace(vhod.Kod)
	ctx, otmena := context.WithTimeout(r.Context(), 30*time.Second)
	defer otmena()
	konfig, err := s.Podpiska.Konfig(ctx, kod)
	if err != nil {
		otdat(w, nil, err)
		return
	}
	if err := s.Yadro.ZapisatKonfig(konfig); err != nil {
		otdat(w, nil, err)
		return
	}
	s.Nastroyki.Kod = kod
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		otdat(w, nil, err)
		return
	}
	if sv, err := s.Podpiska.Svedeniya(ctx, kod); err == nil {
		s.zamok.Lock()
		s.svedeniya = sv
		s.zamok.Unlock()
	}
	otdat(w, map[string]any{"gotovo": true}, nil)
}

// podklyuchit поднимает ядро, а если ядра на машине ещё нет — сначала
// приносит его сам. Пользователь ничего не устанавливает руками.
func (s *Sluzhba) podklyuchit(w http.ResponseWriter, r *http.Request) {
	if !s.Yadro.EstBinar() {
		s.zamok.Lock()
		uzhe := s.kachaemBin
		s.kachaemBin = true
		s.zamok.Unlock()
		if uzhe {
			otdat(w, nil, fmt.Errorf("ядро уже качается"))
			return
		}
		ctx, otmena := context.WithTimeout(context.Background(), 15*time.Minute)
		err := s.Yadro.Zagruzit(ctx)
		otmena()
		s.zamok.Lock()
		s.kachaemBin = false
		s.zamok.Unlock()
		if err != nil {
			otdat(w, nil, err)
			return
		}
	}
	ctx, otmena := context.WithTimeout(context.Background(), 40*time.Second)
	defer otmena()
	otdat(w, map[string]any{"gotovo": true}, s.Yadro.Zapustit(ctx))
}

func (s *Sluzhba) otklyuchit(w http.ResponseWriter, r *http.Request) {
	otdat(w, map[string]any{"gotovo": true}, s.Yadro.Ostanovit())
}

// ObnovlyatProfil перекачивает конфиг по расписанию, как это делает мобильный клиент.
func (s *Sluzhba) ObnovlyatProfil(ctx context.Context) {
	shag := time.Duration(s.Nastroyki.ObnovlyatMin) * time.Minute
	if shag < 15*time.Minute {
		shag = 15 * time.Minute
	}
	t := time.NewTicker(shag)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.Nastroyki.Kod == "" {
				continue
			}
			k, err := s.Podpiska.Konfig(ctx, s.Nastroyki.Kod)
			if err != nil {
				continue // сеть могла лечь; работаем на прежнем конфиге
			}
			_ = s.Yadro.ZapisatKonfig(k)
		}
	}
}

func otdat(w http.ResponseWriter, telo any, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"beda": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(telo)
}

func sluchaynyy() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// fsPodpapki отдаёт встроенные страницы так, будто они лежат в корне.
func fsPodpapki() (fs.FS, error) { return fs.Sub(oblik, "oblik") }
