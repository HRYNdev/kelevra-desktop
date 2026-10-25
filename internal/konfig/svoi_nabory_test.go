package konfig

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

// svoiTegi — свои наборы так, как их выдаёт сервер (две двери у svoi-tunnel).
var svoiTegi = []string{"svoi-blok", "svoi-mimo", "svoi-razreshit", "svoi-tunnel", "svoi-tunnel-2"}

// profilSoSvoimi — боевой профиль телефона, в который вписаны свои наборы и
// правила с ними в порядке единого списка: свой блок → реклама → своё
// «напрямую» → туннель (свои + чужие) → остальное напрямую.
func profilSoSvoimi(t *testing.T) []byte {
	t.Helper()
	d := razobrat(t, profil(t))
	r := d["route"].(map[string]any)
	spisok := r["rule_set"].([]any)
	for _, teg := range svoiTegi {
		spisok = append(spisok, map[string]any{
			"tag": teg, "type": "remote", "format": "source",
			"url":             "https://dver.example/u/klyuch/" + teg,
			"update_interval": "2m",
			"http_client":     map[string]any{"detour": "direct"},
		})
	}
	r["rule_set"] = spisok
	var pravila []any
	for _, p := range r["rules"].([]any) {
		pr := p.(map[string]any)
		if act, _ := pr["action"].(string); act == "reject" {
			pravila = append(pravila, map[string]any{"action": "reject", "rule_set": []any{"svoi-blok"}})
			pravila = append(pravila, pr)
			pravila = append(pravila, map[string]any{"action": "route", "outbound": "direct", "rule_set": []any{"svoi-mimo"}})
			continue
		}
		if rs, est := pr["rule_set"].([]any); est && pr["outbound"] != "direct" {
			pr["rule_set"] = append([]any{"svoi-tunnel", "svoi-tunnel-2"}, rs...)
		}
		pravila = append(pravila, pr)
	}
	r["rules"] = pravila
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// klyuchPravila — правило как строка, чтобы сравнивать порядок.
func klyuchPravila(t *testing.T, p any) string {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Порядок правил — как прислал сервер, клиент свой не придумывает.
//
// Единый список правил сети держит порядок «свой блок → реклама → своё
// напрямую → туннель» одинаковым на всех устройствах. Переставь клиент хоть
// одно правило — и домен из «напрямую» внутри туннельного суффикса ушёл бы
// у него в туннель, а у всех остальных напрямую. Правила профиля обязаны
// дойти до ядра в том же порядке, а своих правил про свои наборы клиент не
// добавляет.
func TestPoryadokPravilKakNaServere(t *testing.T) {
	syroy := profilSoSvoimi(t)
	ishodnye := razobrat(t, syroy)["route"].(map[string]any)["rules"].([]any)
	dir := t.TempDir()
	puti := map[string]string{}
	for _, teg := range svoiTegi {
		puti[teg] = filepath.Join(dir, teg+".json")
	}
	gotovyy, _, err := Prigotovit(syroy, Vybor{PravilaIzKomplekta: puti})
	if err != nil {
		t.Fatal(err)
	}
	itogovye := razobrat(t, gotovyy)["route"].(map[string]any)["rules"].([]any)

	ishodnyeKlyuchi := map[string]bool{}
	for _, p := range ishodnye {
		ishodnyeKlyuchi[klyuchPravila(t, p)] = true
	}
	var poryadok []string
	for _, p := range itogovye {
		k := klyuchPravila(t, p)
		if ishodnyeKlyuchi[k] {
			poryadok = append(poryadok, k)
			continue
		}
		// Правило клиента (QUIC, локальная сеть, вход службы): про «свой
		// блок», «напрямую» и исключения оно знать не должно.
		pr, _ := p.(map[string]any)
		for _, teg := range spisokStrok(pr["rule_set"]) {
			if teg == "svoi-blok" || teg == "svoi-mimo" || teg == "svoi-razreshit" {
				t.Fatalf("клиент добавил своё правило со своим набором %s: %s", teg, k)
			}
		}
	}
	if len(poryadok) != len(ishodnye) {
		t.Fatalf("из %d правил профиля в конфиг дошло %d без изменений", len(ishodnye), len(poryadok))
	}
	for i, p := range ishodnye {
		if poryadok[i] != klyuchPravila(t, p) {
			t.Fatalf("правило профиля №%d переставлено: ждали %s, стоит %s", i, klyuchPravila(t, p), poryadok[i])
		}
	}
}

// Интервал своих наборов (2m) доходит до сверки; «1d» sing-box понимает, а
// time.ParseDuration — нет, поэтому разбор свой.
func TestIntervalyPravilIzProfilya(t *testing.T) {
	intervaly := IntervalyPravil(profilSoSvoimi(t))
	if intervaly["svoi-tunnel"] != 2*time.Minute || intervaly["svoi-tunnel-2"] != 2*time.Minute {
		t.Fatalf("интервал своих наборов %v, ждали 2m", intervaly["svoi-tunnel"])
	}
	if intervaly["ads"] != time.Hour {
		t.Fatalf("интервал ads %v, ждали 1h", intervaly["ads"])
	}
	for vhod, zhdali := range map[string]time.Duration{
		"2m": 2 * time.Minute, "1h": time.Hour, "1d": 24 * time.Hour, "1d12h": 36 * time.Hour,
		"": 0, "чушь": 0, "-1h": 0,
	} {
		if poluchili := RazobratInterval(vhod); poluchili != zhdali {
			t.Fatalf("RazobratInterval(%q) = %v, ждали %v", vhod, poluchili, zhdali)
		}
	}
	if adresa := AdresaPravil(profilSoSvoimi(t)); adresa["svoi-mimo"] != "https://dver.example/u/klyuch/svoi-mimo" {
		t.Fatalf("адрес своего набора не найден: %v", adresa["svoi-mimo"])
	}
}
