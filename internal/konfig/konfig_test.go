package konfig

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// profil — настоящий профиль с сервера подписки (личное в нём заменено выдумкой).
// Тесты бьются именно о него: выдуманный профиль не покажет, чем телефонный
// конфиг отличается от компьютерного.
func profil(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/profil_telefona.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func razobrat(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func vhody(d map[string]any) []map[string]any {
	var out []map[string]any
	for _, v := range d["inbounds"].([]any) {
		out = append(out, v.(map[string]any))
	}
	return out
}

// Главное: поле, из-за которого ядро на компьютере не стартует вообще.
func TestAndroidnoePoleUhodit(t *testing.T) {
	for _, prava := range []bool{false, true} {
		gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: prava})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(gotovyy), "override_android_vpn") {
			t.Fatalf("права=%v: осталось android-поле, ядро не запустится", prava)
		}
	}
}

func TestBezPravOstayotsyaProksi(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	if k.Rezhim != Proksi {
		t.Fatalf("без прав ожидал прокси-режим, получил %q", k.Rezhim)
	}
	if !k.EstTunnel {
		t.Fatal("в профиле есть туннель, картина о нём молчит")
	}
	if k.ProksiAdres != "127.0.0.1:2412" {
		t.Fatalf("адрес прокси %q", k.ProksiAdres)
	}
	var estMixed bool
	for _, vh := range vhody(razobrat(t, gotovyy)) {
		if vh["type"] == "tun" {
			t.Fatal("туннель остался, а прав на него нет: ядро упадёт при старте")
		}
		if vh["type"] == "mixed" {
			estMixed = true
			if vh["set_system_proxy"] != true {
				t.Fatal("прокси есть, но система о нём не узнает — трафик пойдёт мимо")
			}
		}
	}
	if !estMixed {
		t.Fatal("не осталось ни одного входа")
	}
}

func TestSPravamiOstayotsyaTunnel(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	if k.Rezhim != Tunnel {
		t.Fatalf("с правами ожидал туннель, получил %q", k.Rezhim)
	}
	var estTun bool
	for _, vh := range vhody(razobrat(t, gotovyy)) {
		if vh["type"] != "tun" {
			continue
		}
		estTun = true
		for _, p := range androidPolyaTun {
			if _, est := vh[p]; est {
				t.Fatalf("в туннеле осталось поле телефона %q", p)
			}
		}
	}
	if !estTun {
		t.Fatal("туннель пропал, хотя права есть")
	}
}

func TestClashAdresBerytsyaIzProfilya(t *testing.T) {
	_, k, err := Prigotovit(profil(t), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	if k.ClashAdres != "127.0.0.1:9090" {
		t.Fatalf("адрес Clash API %q, а в профиле 127.0.0.1:9090 — приложение решит, что ядро мертво", k.ClashAdres)
	}
	k2, err := Razobrat(profil(t))
	if err != nil {
		t.Fatal(err)
	}
	if k2.ClashAdres != k.ClashAdres {
		t.Fatalf("разбор готового конфига дал другой адрес: %q", k2.ClashAdres)
	}
}

// Правило, ссылающееся на выброшенный вход, ядро не примет.
func TestPravilaBezVybroshennogoVhoda(t *testing.T) {
	syroy := []byte(`{"route":{"rules":[{"inbound":["tun-in"],"outbound":"direct"},{"inbound":["tun-in","mixed-in"],"outbound":"direct"},{"outbound":"direct"}]},
	"inbounds":[{"type":"tun","tag":"tun-in"},{"type":"mixed","tag":"mixed-in","listen":"127.0.0.1","listen_port":2412}]}`)
	gotovyy, _, err := Prigotovit(syroy, Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	d := razobrat(t, gotovyy)
	pravila := d["route"].(map[string]any)["rules"].([]any)
	if len(pravila) != 2 {
		t.Fatalf("ожидал 2 правила (одно выброшено целиком), получил %d", len(pravila))
	}
	if strings.Contains(string(gotovyy), "tun-in") {
		t.Fatal("ссылка на выброшенный вход осталась в правилах")
	}
}

func TestProfilBezVhodaEtoOshibka(t *testing.T) {
	if _, _, err := Prigotovit([]byte(`{"inbounds":[{"type":"tun","tag":"tun-in"}]}`), Vybor{}); err == nil {
		t.Fatal("без прав и без прокси подключаться нечем — это должно быть ошибкой, а не тихим запуском")
	}
}

func TestBityyProfil(t *testing.T) {
	if _, _, err := Prigotovit([]byte("не json"), Vybor{Prava: true}); err == nil {
		t.Fatal("битый профиль обязан быть ошибкой")
	}
}

// Отказ системы настроить прокси не должен ронять ядро: ставим прокси-вход
// без просьбы к системе и честно говорим человеку адрес.
func TestOtstuplenieBezSistemnogoProksi(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{BezSistemnogoProksi: true})
	if err != nil {
		t.Fatal(err)
	}
	if !k.RuchnoyProksi || k.Rezhim != Proksi {
		t.Fatalf("картина не отражает отступление: %+v", k)
	}
	if !strings.Contains(k.Zametka, k.ProksiAdres) {
		t.Fatalf("человеку не сказали адрес прокси: %q", k.Zametka)
	}
	for _, vh := range vhody(razobrat(t, gotovyy)) {
		if vh["type"] == "mixed" && vh["set_system_proxy"] != false {
			t.Fatal("ядро снова попросят настроить системный прокси — оно упадёт так же")
		}
	}
}

// Выбор узла человеком должен пережить перезапуск ядра, а хранит выбор
// cache_file. Поля store_selected тут быть НЕ ДОЛЖНО: ядро 1.14 на нём падает.
func TestPrigotovitVelitYadruPomnitVybor(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(gotovyy, &d); err != nil {
		t.Fatal(err)
	}
	c, _ := d["experimental"].(map[string]any)["cache_file"].(map[string]any)
	if c == nil || c["enabled"] != true {
		t.Fatalf("ядру негде хранить выбор: cache_file = %+v", c)
	}
	if _, est := c["store_selected"]; est {
		t.Errorf("store_selected вернулся в конфиг — ядро 1.14 с ним не стартует")
	}
	if c["path"] != "remnawave.db" {
		t.Errorf("путь кэша из профиля потерян: %v", c["path"])
	}
}

// Профиль без cache_file: хранилище должно появиться само, иначе выбор узла
// сбросится при первом же перезапуске ядра.
func TestPrigotovitDobavlyaetHranilishcheEsliEgoNet(t *testing.T) {
	d := razobrat(t, profil(t))
	delete(d["experimental"].(map[string]any), "cache_file")
	syroy, _ := json.Marshal(d)
	gotovyy, _, err := Prigotovit(syroy, Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := razobrat(t, gotovyy)["experimental"].(map[string]any)["cache_file"].(map[string]any)
	if c == nil || c["enabled"] != true || c["path"] == "" {
		t.Fatalf("хранилище не появилось: %+v", c)
	}
}
