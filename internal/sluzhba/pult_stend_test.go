package sluzhba

// Сквозная проверка пульта Desktop против НАСТОЯЩЕЙ выдачи (server/app.py) и мозга на локальном стенде
// (KelevraServer, стенд пульта: дверь 127.0.0.1:20900, управление 127.0.0.1:20903). Сеть ПК не трогает:
// служба не поднимается, ядра и туннеля нет — только HTTP до 127.0.0.1. Без стенда — пропуск.
//
//	KELEVRA_PULT_STEND=1 KELEVRA_PULT_STEND_KOD=<код человека> go test ./internal/sluzhba -run TestPultStend -v \
//	  -ldflags "-X github.com/HRYNdev/kelevra-desktop/internal/pult.ProverochnyyKlyuchHex=<проверочный ключ>"

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/pult"
)

const (
	stendDver = "127.0.0.1:20900"
	stendUpr  = "http://127.0.0.1:20903"
)

// perenapravlenie — https://test.example/… стенда уходит на дверь стенда по http.
type perenapravlenie struct{}

func (perenapravlenie) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	if r2.URL.Host == "test.example" {
		r2.URL.Scheme, r2.URL.Host = "http", stendDver
	}
	return (&http.Transport{Proxy: nil}).RoundTrip(r2)
}

func upr(t *testing.T, put string, telo any) map[string]any {
	t.Helper()
	k := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 60 * time.Second}
	var otv *http.Response
	var err error
	if telo == nil {
		otv, err = k.Get(stendUpr + put)
	} else {
		b, _ := json.Marshal(telo)
		otv, err = k.Post(stendUpr+put, "application/json", bytes.NewReader(b))
	}
	if err != nil {
		t.Fatalf("стенд %s: %v", put, err)
	}
	defer otv.Body.Close()
	var d map[string]any
	_ = json.NewDecoder(otv.Body).Decode(&d)
	return d
}

func api(t *testing.T, m, p string, j any) any {
	t.Helper()
	d := upr(t, "/ctl/api", map[string]any{"m": m, "p": p, "j": j})
	if s, _ := d["status"].(float64); s >= 400 {
		t.Fatalf("API %s %s: %v", m, p, d)
	}
	return d["telo"]
}

func TestPultStend(t *testing.T) {
	if os.Getenv("KELEVRA_PULT_STEND") == "" {
		t.Skip("нет стенда пульта (KELEVRA_PULT_STEND)")
	}
	kod := os.Getenv("KELEVRA_PULT_STEND_KOD")
	imyaCheloveka := os.Getenv("KELEVRA_PULT_STEND_IMYA")
	if pult.ProverochnyyKlyuchHex == "" {
		t.Fatal("сборка без проверочного ключа: -ldflags -X …/internal/pult.ProverochnyyKlyuchHex=…")
	}
	papka := t.TempDir()
	t.Setenv("KELEVRA_DIR", papka)
	ctx := context.Background()
	klient := &http.Client{Transport: perenapravlenie{}, Timeout: 90 * time.Second}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	did := "desk-e2e-" + hex.EncodeToString(b)

	novaya := func(n *hranenie.Nastroyki) *Sluzhba {
		s := &Sluzhba{Nastroyki: n,
			Podpiska: &podpiska.Klient{Host: "test.example", Shema: "https", DeviceID: did, HTTP: klient}}
		s.nachatPult()
		s.obnovitPuls(true, true, "Финляндия")
		return s
	}
	s := novaya(&hranenie.Nastroyki{Kod: kod, DeviceID: did})
	zhurnal := func(f string, a ...any) { t.Logf(f, a...) }

	// --- (а) пульт с настройкой ---
	s.ObnovitSvedeniya(ctx)
	upr(t, "/ctl/cikl", map[string]any{})
	slot := ""
	for _, u := range api(t, "GET", "/pult/ustroystva", nil).([]any) {
		d := u.(map[string]any)
		if d["platforma"] == "desktop" && d["imya"] == imyaCheloveka {
			slot = d["slot"].(string)
		}
	}
	if slot == "" {
		t.Fatal("стенд не видит устройство Desktop")
	}
	api(t, "POST", "/pult/nastroyki", map[string]any{"sloy": "ustroystvo", "kto": slot, "klyuch": "zhurnal.otpravka_min", "znachenie": 45})
	V := int64(upr(t, "/ctl/cikl", map[string]any{})["versiya"].(float64))
	s.ObnovitSvedeniya(ctx)
	if s.pult.Versiya() != V || s.periodZhurnalov(time.Now()) != 45*time.Minute {
		t.Fatalf("(а) версия %d (ждали %d), период журнала %s", s.pult.Versiya(), V, s.periodZhurnalov(time.Now()))
	}
	if sv, err := s.Podpiska.SvedeniyaEsliNovoe(ctx, s.adresPodpiski(), s.etagSvodki); err != nil || sv != nil {
		t.Fatalf("(а) повтор ETag сервера дословно — ждали 304: %v %v (ETag %q)", sv, err, s.etagSvodki)
	}
	otch := upr(t, "/ctl/fayl?imya=pult_otchety.jsonl", nil)["tekst"].(string)
	if !strings.Contains(otch, fmt.Sprintf(`"device_id": "%s", "slot": "", "umeet": 1, "versiya": %d`, did, V)) &&
		!strings.Contains(otch, fmt.Sprintf(`"device_id": "%s", "slot": "%s", "umeet": 1, "versiya": %d`, did, slot, V)) {
		t.Fatalf("(а) сервер не записал X-Pult versiya=%d", V)
	}
	zhurnal("(а) прошёл: X-Pult %q, ETag %s → 304", s.pult.Zagolovok(), s.etagSvodki)

	// --- (б) prishli_zhurnal ---
	if err := os.WriteFile(hranenie.PutZhurnala(), []byte("2026-10-21 проба журнала Desktop для пульта, устройство "+did+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ot := s.otpravshchikZhurnalov()
	ot.HTTP = klient
	s.otpravshchikZhurnalovDlyaStenda = ot
	k1 := api(t, "POST", "/pult/komandy", map[string]any{"slot": slot, "chto": "prishli_zhurnal"}).(map[string]any)["id"].(string)
	upr(t, "/ctl/cikl", map[string]any{})
	s.ObnovitSvedeniya(ctx)
	zhdatDo(t, 60*time.Second, "(б) квитанция "+k1, func() bool { return strings.Contains(s.pult.Zagolovok(), k1+":ok") })
	_ = s.ObnovitSvedeniyaLegko(ctx)
	upr(t, "/ctl/cikl", map[string]any{})
	if st := statusKomandy(t, slot, k1); st != "zakryta:ok" {
		t.Fatalf("(б) команда на сервере: %s", st)
	}
	fayly := fmt.Sprint(upr(t, "/ctl/zhurnaly", nil)["fayly"])
	if !strings.Contains(fayly, slot+"__") {
		t.Fatalf("(б) журнал не пришёл: %s", fayly)
	}
	zhurnal("(б) прошёл: журнал на сервере %s, команда %s закрыта ok", fayly, k1)

	// --- (в) испорченная подпись ---
	upr(t, "/ctl/pauza", map[string]any{"da": true})
	api(t, "POST", "/pult/nastroyki", map[string]any{"sloy": "ustroystvo", "kto": slot, "klyuch": "zhurnal.otpravka_min", "znachenie": 50})
	upr(t, "/ctl/cikl", map[string]any{})
	upr(t, "/ctl/isportit", map[string]any{"device_id": did})
	byla := s.pult.Versiya()
	s.ObnovitSvedeniya(ctx)
	if !strings.Contains(s.pult.Zagolovok(), "pult:podpis") || s.pult.Versiya() != byla || s.periodZhurnalov(time.Now()) != 45*time.Minute {
		t.Fatalf("(в) испорченный пульт: X-Pult %q, версия %d, период %s", s.pult.Zagolovok(), s.pult.Versiya(), s.periodZhurnalov(time.Now()))
	}
	upr(t, "/ctl/pauza", map[string]any{"da": false})
	upr(t, "/ctl/cikl", map[string]any{})
	s.ObnovitSvedeniya(ctx)
	zhurnal("(в) прошёл: отказ %q, держится версия %d; после пересборки — версия %d", "pult:podpis", byla, s.pult.Versiya())

	// --- (г) сервер выключен, служба перезапущена ---
	s.PultVyhod()
	upr(t, "/ctl/app", map[string]any{"vkl": false})
	n2, err := hranenie.Zagruzit()
	if err != nil {
		t.Fatal(err)
	}
	s2 := novaya(n2)
	s2.ObnovitSvedeniya(ctx) // выдача лежит — сводка не обновится
	if s2.pult.Versiya() != s.pult.Versiya() || s2.periodZhurnalov(time.Now()) != 50*time.Minute || s2.svedeniya == nil || s2.svedeniya.Imya == "" {
		t.Fatalf("(г) после перезапуска без сервера: версия %d, период %s, сводка %+v", s2.pult.Versiya(), s2.periodZhurnalov(time.Now()), s2.svedeniya)
	}
	upr(t, "/ctl/app", map[string]any{"vkl": true})
	zhurnal("(г) прошёл: с диска версия %d, период журнала %s, сводка «%s»", s2.pult.Versiya(), s2.periodZhurnalov(time.Now()), s2.svedeniya.Imya)
	s = s2

	// --- (д) блок obnovlenie с неверным sha256 ---
	sborka := make([]byte, 150_000)
	_, _ = rand.Read(sborka)
	verImya := fmt.Sprintf("0.7.%d", int(b[0])+int(b[1])*256)
	imya := "Kelevra-" + verImya + ".exe"
	put := filepath.Join(os.Getenv("KELEVRA_PULT_STEND_SBORKI"), imya)
	if err := os.WriteFile(put, sborka, 0o644); err != nil {
		t.Fatalf("(д) файл сборки для стенда: %v", err)
	}
	pravilnyy := sha256.Sum256(sborka)
	nevernyy := strings.Repeat("0", 64)
	if hex.EncodeToString(pravilnyy[:]) == nevernyy {
		t.Fatal("невероятно")
	}
	// Тихую установку служба повела бы своим клиентом сети (klientSeti) мимо стенда — до test.example ему не
	// дойти; гасим её пультом и ставим тем же путём (naytiObnovlenie → PostavitSHodom) через стенд.
	api(t, "POST", "/pult/nastroyki", map[string]any{"sloy": "ustroystvo", "kto": slot, "klyuch": "obnovleniya.tiho", "znachenie": false})
	api(t, "POST", "/pult/sborki", map[string]any{"platforma": "windows", "versiya_imya": verImya,
		"url": "https://test.example/sborki/" + imya, "sha256": nevernyy, "razmer": len(sborka)})
	api(t, "POST", "/pult/sborki/naznachit", map[string]any{"kuda": "ustroystvo", "kto": slot, "versiya_imya": verImya})
	upr(t, "/ctl/cikl", map[string]any{})
	s.ObnovitSvedeniya(ctx)
	o := s.pult.Obnovlenie()
	if o == nil || o.Nomer() != verImya || o.VersiyaKod != 0 {
		t.Fatalf("(д) блок obnovlenie: %+v", o)
	}
	nayd, err := s.naytiObnovlenie(ctx, klient)
	if err != nil || nayd == nil || nayd.Sha256 != nevernyy {
		t.Fatalf("(д) naytiObnovlenie: %+v %v", nayd, err)
	}
	papkaExe := t.TempDir()
	exe := filepath.Join(papkaExe, "Kelevra.exe")
	_ = os.WriteFile(exe, []byte("старая версия"), 0o755)
	err = obnovlenie.PostavitSHodom(ctx, klient, *nayd, exe, nil)
	if !errors.Is(err, obnovlenie.ErrSha256) {
		t.Fatalf("(д) установка с неверной суммой: %v", err)
	}
	s.pult.OshibkaObnovleniya(prichinaObnovleniya(err)) // как postavitTiho
	ostalos, _ := os.ReadDir(papkaExe)
	staroe, _ := os.ReadFile(exe)
	if len(ostalos) != 1 || string(staroe) != "старая версия" {
		t.Fatalf("(д) рядом с exe осталось %d файлов / exe тронут", len(ostalos))
	}
	_ = s.ObnovitSvedeniyaLegko(ctx)
	if !strings.Contains(s.pult.Zagolovok(), "obnovlenie:hesh") {
		t.Fatalf("(д) X-Pult: %q", s.pult.Zagolovok())
	}
	zhurnal("(д) прошёл: %v; временный файл удалён, exe не тронут; X-Pult %q", err, s.pult.Zagolovok())

	// --- (е) долгий опрос ---
	api(t, "POST", "/pult/nastroyki", map[string]any{"sloy": "ustroystvo", "kto": slot, "klyuch": "kanal.dolgiy_opros", "znachenie": true})
	upr(t, "/ctl/cikl", map[string]any{})
	s.ObnovitSvedeniya(ctx)
	if !s.pult.FlagIli("kanal.dolgiy_opros", false) {
		t.Fatal("(е) kanal.dolgiy_opros не встал")
	}
	dctx, stop := context.WithCancel(ctx)
	defer stop()
	go s.SleditZaDolgimOprosom(dctx)
	time.Sleep(2 * time.Second)
	nachalo := time.Now()
	k2 := api(t, "POST", "/pult/komandy", map[string]any{"slot": slot, "chto": "sbros_avtomata"}).(map[string]any)["id"].(string)
	upr(t, "/ctl/cikl", map[string]any{})
	zhdatDo(t, 30*time.Second, "(е) команда долгим опросом", func() bool { return strings.Contains(s.pult.Zagolovok(), k2+":ok") })
	zhurnal("(е) прошёл: команда %s выполнена через %s после постановки", k2, time.Since(nachalo).Round(100*time.Millisecond))
	stop()

	// --- (ж) pereydi_na_klyuch: профиль по общему коду → свой ключ на месте ---
	if s.Nastroyki.Podpiska != "" {
		t.Fatalf("(ж) ждали установку по общему коду, адрес %q", s.Nastroyki.Podpiska)
	}
	k3 := api(t, "POST", "/pult/komandy", map[string]any{"slot": slot, "chto": "pereydi_na_klyuch"}).(map[string]any)["id"].(string)
	upr(t, "/ctl/cikl", map[string]any{})
	s.ObnovitSvedeniya(ctx)
	zhdatDo(t, 60*time.Second, "(ж) квитанция", func() bool { return strings.Contains(s.pult.Zagolovok(), k3+":") })
	if !strings.Contains(s.pult.Zagolovok(), k3+":ok") || !strings.HasPrefix(s.Nastroyki.Podpiska, "https://test.example/u/") {
		t.Fatalf("(ж) X-Pult %q, адрес %q", s.pult.Zagolovok(), s.Nastroyki.Podpiska)
	}
	if n, _ := hranenie.Zagruzit(); n.Podpiska != s.Nastroyki.Podpiska {
		t.Fatal("(ж) новый адрес не на диске")
	}
	s.ObnovitSvedeniya(ctx) // теперь /u/<ключ>/info
	if s.svedeniya == nil || s.svedeniya.Imya == "" {
		t.Fatal("(ж) сводка по ключу устройства не пришла")
	}
	upr(t, "/ctl/cikl", map[string]any{})
	if st := statusKomandy(t, slot, k3); st != "zakryta:ok" {
		t.Fatalf("(ж) команда на сервере: %s", st)
	}
	chel := api(t, "GET", "/lyudi/"+imyaCheloveka, nil)
	zhurnal("(ж) прошёл: адрес %s…, сводка «%s», команда закрыта; у человека на сервере: %s", s.Nastroyki.Podpiska[:30],
		s.svedeniya.Imya, kratko(chel))
}

func statusKomandy(t *testing.T, slot, id string) string {
	for _, k := range api(t, "GET", "/pult/komandy?slot="+slot, nil).([]any) {
		d := k.(map[string]any)
		if d["id"] == id {
			return fmt.Sprintf("%s:%s", d["status"], d["kvit"])
		}
	}
	return "нет"
}

func kratko(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if i := strings.Index(s, `"ustroystva"`); i >= 0 {
		s = s[i:]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

func zhdatDo(t *testing.T, srok time.Duration, chto string, uslovie func() bool) {
	t.Helper()
	konec := time.Now().Add(srok)
	for time.Now().Before(konec) {
		if uslovie() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("не дождался: %s", chto)
}

var _ = io.Discard
