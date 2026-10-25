package sluzhba

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/pravila"
)

// svoiTegiProfilya — свои наборы так, как их выдаёт сервер (две двери у svoi-tunnel).
var svoiTegiProfilya = []string{"svoi-blok", "svoi-mimo", "svoi-razreshit", "svoi-tunnel", "svoi-tunnel-2"}

// vydachaNaborov — подставная раздача наборов: чужие — двоичные «SRS…»,
// свои — source JSON; содержимое своих меняется на ходу.
type vydachaNaborov struct {
	mu     sync.Mutex
	svoi   map[string]string // тег -> домен
	server *httptest.Server
}

func novayaVydachaNaborov(t *testing.T) *vydachaNaborov {
	t.Helper()
	v := &vydachaNaborov{svoi: map[string]string{}}
	v.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		teg := strings.TrimSuffix(filepath.Base(r.URL.Path), ".srs")
		if pravila.Svoy(teg) {
			v.mu.Lock()
			domen := v.svoi[pravila.Gruppa(teg)]
			v.mu.Unlock()
			if domen == "" {
				domen = "svoi.invalid"
			}
			_, _ = fmt.Fprintf(w, `{"version":2,"rules":[{"domain_suffix":[%q]}]}`, domen)
			return
		}
		_, _ = w.Write([]byte("SRS-" + teg))
	}))
	t.Cleanup(v.server.Close)
	return v
}

// profilSoSvoimiNaborami — боевой профиль телефона, где все адреса наборов
// ведут на подставную раздачу (проверки не стучатся на боевой сервер), плюс
// свои наборы в формате source с интервалом 2m.
func profilSoSvoimiNaborami(t *testing.T, adres string) []byte {
	t.Helper()
	syroy, err := os.ReadFile("../konfig/testdata/profil_telefona.json")
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(syroy, &d); err != nil {
		t.Fatal(err)
	}
	r := d["route"].(map[string]any)
	spisok := r["rule_set"].([]any)
	for _, rs := range spisok {
		m := rs.(map[string]any)
		m["url"] = adres + "/rules/" + m["tag"].(string) + ".srs"
	}
	for _, teg := range svoiTegiProfilya {
		spisok = append(spisok, map[string]any{
			"tag": teg, "type": "remote", "format": "source",
			"url": adres + "/u/klyuch/" + teg, "update_interval": "2m",
		})
	}
	r["rule_set"] = spisok
	for _, p := range r["rules"].([]any) {
		pr := p.(map[string]any)
		if rs, est := pr["rule_set"].([]any); est && pr["outbound"] != "direct" && pr["action"] != "reject" {
			pr["rule_set"] = append([]any{"svoi-tunnel", "svoi-tunnel-2"}, rs...)
		}
	}
	r["rules"] = append([]any{map[string]any{"action": "reject", "rule_set": []any{"svoi-blok"}}}, r["rules"].([]any)...)
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// naboryKonfiga — route.rule_set конфига с диска: тег -> запись.
func naboryKonfiga(t *testing.T, s *Sluzhba) map[string]map[string]any {
	t.Helper()
	itog := map[string]map[string]any{}
	for _, rs := range razobratKonfig(t, s)["rule_set"].([]any) {
		m := rs.(map[string]any)
		itog[m["tag"].(string)] = m
	}
	return itog
}

// Профиль со своими наборами, источник правил недоступен, кеша нет:
// повтор с комплектом обязан закрыть КАЖДЫЙ тег. Своих наборов во
// встроенном комплекте нет и быть не может — до 0.6.93 комплект из-за этого
// отвергался целиком, и человек оставался без связи.
func TestSvoiNaboryKomplektNeOtvergaetsya(t *testing.T) {
	v := novayaVydachaNaborov(t)
	s := gotovStendLestnicy(t)
	if err := s.SohranitProfil(profilSoSvoimiNaborami(t, v.server.URL)); err != nil {
		t.Fatal(err)
	}
	popytok := 0
	var nabory map[string]map[string]any
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		if popytok == 1 {
			return fmt.Errorf("%s", oshibkaIstochnikaPravil)
		}
		nabory = naboryKonfiga(t, s)
		return nil
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("комплект со своими наборами в профиле не поднял связь: %v", err)
	}
	if popytok != 2 {
		t.Fatalf("ядро звали %d раз, ждали 2", popytok)
	}
	kesh, _ := filepath.Abs(pravila.PutKesha(hranenie.PapkaYadra()))
	for teg, m := range nabory {
		if m["type"] != "local" {
			t.Fatalf("набор %s остался %v — ядро снова пошло бы в сеть и упало", teg, m["type"])
		}
		if pravila.Svoy(teg) {
			if m["format"] != "source" || filepath.Dir(m["path"].(string)) != kesh {
				t.Fatalf("свой набор %s: %v — ждали заготовку source в кеше", teg, m)
			}
		}
	}
	if len(nabory) != 22+len(svoiTegiProfilya) {
		t.Fatalf("в конфиге %d наборов, ждали %d", len(nabory), 22+len(svoiTegiProfilya))
	}
}

// Обычный подъём без кеша: свои наборы — заготовки в кеше, чужие ядро
// качает само. Сверка кладёт настоящие наборы на место заготовок, и ядро
// перечитывает их на ходу: «ждут подключения» только чужие, которые сейчас
// качает само ядро. Человек добавил домен — следующая сверка подменяет
// файл, без перезапуска ядра.
func TestSvoiNaboryPodhvatyvayutsyaNaHodu(t *testing.T) {
	v := novayaVydachaNaborov(t)
	s := gotovStendLestnicy(t)
	if err := s.SohranitProfil(profilSoSvoimiNaborami(t, v.server.URL)); err != nil {
		t.Fatal(err)
	}
	var nabory map[string]map[string]any
	popytok := 0
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		nabory = naboryKonfiga(t, s)
		return nil
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatal(err)
	}
	for teg, m := range nabory {
		zhdali := "remote"
		if pravila.Svoy(teg) {
			zhdali = "local"
		}
		if m["type"] != zhdali {
			t.Fatalf("набор %s при пустом кеше %v, ждали %s", teg, m["type"], zhdali)
		}
	}

	v.mu.Lock()
	v.svoi["svoi-tunnel"] = "pervyy.example"
	v.mu.Unlock()
	if err := s.ObnovitKeshPravil(context.Background()); err != nil {
		t.Fatalf("сверка: %v", err)
	}
	putTunnelya := nabory["svoi-tunnel"]["path"].(string)
	if b, _ := os.ReadFile(putTunnelya); !strings.Contains(string(b), "pervyy.example") {
		t.Fatalf("сверка не положила свой набор туда, откуда его читает ядро: %s", b)
	}
	if b, _ := os.ReadFile(nabory["svoi-tunnel-2"]["path"].(string)); !strings.Contains(string(b), "pervyy.example") {
		t.Fatalf("набор второй двери не обновился: %s", b)
	}
	s.zamok.Lock()
	zhdut := s.svezhihPravil
	s.zamok.Unlock()
	if zhdut != 22 {
		t.Fatalf("ждут подключения %d наборов, ждали 22 чужих (свои ядро перечитывает само)", zhdut)
	}

	// Человек добавил домен в панели: через сверку он в том же файле.
	v.mu.Lock()
	v.svoi["svoi-tunnel"] = "vtoroy.example"
	v.mu.Unlock()
	sved := pravila.ChitatSvedeniya(hranenie.PapkaYadra())
	for teg, n := range sved.Nabory {
		if pravila.Svoy(teg) {
			n.Svereno = n.Svereno.Add(-5 * 60 * 1e9) // пять минут назад: срок 2m прошёл
			n.ETag = ""
			sved.Nabory[teg] = n
		}
	}
	b, _ := json.Marshal(sved)
	if err := os.WriteFile(filepath.Join(pravila.PutKesha(hranenie.PapkaYadra()), "svedeniya.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.ObnovitKeshPravil(context.Background()); err != nil {
		t.Fatalf("вторая сверка: %v", err)
	}
	if b, _ := os.ReadFile(putTunnelya); !strings.Contains(string(b), "vtoroy.example") {
		t.Fatalf("новый домен не дошёл до файла ядра: %s", b)
	}
	s.zamok.Lock()
	zhdut = s.svezhihPravil
	s.zamok.Unlock()
	if zhdut != 22 {
		t.Fatalf("после смены своего набора ждут %d — свои не должны требовать переподключения", zhdut)
	}
	if popytok != 1 {
		t.Fatalf("ядро перезапускали (%d запусков) — свои наборы должны подхватываться на ходу", popytok)
	}

	// Следующий подъём: всё уже в кеше — ядру качать нечего.
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatal(err)
	}
	for teg, m := range nabory {
		if m["type"] != "local" {
			t.Fatalf("после сверки набор %s всё ещё %v", teg, m["type"])
		}
	}
	if s.shagSverki(ShagSverkiPravil) != pravila.SrokNabora(2*60*1e9) {
		t.Fatalf("часы сверки идут раз в %s, а свои наборы просят 2m", s.shagSverki(ShagSverkiPravil))
	}
}

// Профиль сменился и принёс новые наборы — часы сверки заходят сразу, не
// дожидаясь получасового тика: иначе свои наборы (2m) впервые скачались бы
// только через полчаса после того, как сервер начал их выдавать.
func TestNovyeNaboryVProfilePinayutSverku(t *testing.T) {
	v := novayaVydachaNaborov(t)
	s := gotovStendLestnicy(t)
	select {
	case <-s.pinokSverki:
	default:
	}
	syroy, err := os.ReadFile("../konfig/testdata/profil_telefona.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SohranitProfil(syroy); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.pinokSverki:
		t.Fatal("тот же профиль пнул сверку — наборы не менялись")
	default:
	}
	if err := s.SohranitProfil(profilSoSvoimiNaborami(t, v.server.URL)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.pinokSverki:
	default:
		t.Fatal("в профиле появились свои наборы, а сверку не позвали — ждать бы ей получасового тика")
	}
}
