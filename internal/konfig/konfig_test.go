package konfig

import (
	"encoding/json"
	"maps"
	"os"
	"reflect"
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

// route.final боевого профиля — "direct" и в режиме полной защиты (Prava:
// true) остаётся direct: переключение final на туннельный выход (правка
// 7bf3374) отправляло в туннель ВСЁ неопознанное, включая приложения
// пользователя, не связанные с VPN, — откачено. route.rules (whitelist того,
// что рулится в туннель через rule_set, и правило
// {"ip_is_private":true,"outbound":"direct"} в нём — прямая дорога домой)
// эта правка не трогает вовсе.
func TestSPolnoyZashchitoyFinalOstaetsyaDirect(t *testing.T) {
	// Базой берём тот же Prigotovit в режиме прокси (Prava:false), а не сырой
	// профиль: dobavitPravilomRezhimQuic вставляет своё udp/443-правило
	// независимо от Prava, и это не имеет отношения к нашей правке —
	// сравнивать нужно только то, что мог поменять именно final-переключатель.
	baza, _, err := Prigotovit(profil(t), Vybor{Prava: false})
	if err != nil {
		t.Fatal(err)
	}
	dDo := razobrat(t, baza)
	pravilaDo, _ := dDo["route"].(map[string]any)["rules"].([]any)
	chisloPravilDo := len(pravilaDo)

	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	d := razobrat(t, gotovyy)
	r, ok := d["route"].(map[string]any)
	if !ok {
		t.Fatal("route пропал из конфига")
	}
	final, _ := r["final"].(string)
	if final != "direct" {
		t.Fatalf("route.final = %q, ожидал direct как в профиле подписки — final трогать не должны", final)
	}

	pravila, _ := r["rules"].([]any)
	if len(pravila) != chisloPravilDo {
		t.Fatalf("число route.rules изменилось: было %d, стало %d — правку нельзя трогать список правил", chisloPravilDo, len(pravila))
	}
	var nashlosIpPrivate bool
	for i, p := range pravila {
		pr, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if pr["ip_is_private"] == true {
			nashlosIpPrivate = true
			if pr["outbound"] != "direct" {
				t.Fatalf("правило ip_is_private[%d].outbound = %v, ожидал direct — домашняя сеть должна остаться прямой", i, pr["outbound"])
			}
		}
		prDo, ok := pravilaDo[i].(map[string]any)
		if !ok {
			continue
		}
		// "inbound" правила про udp/443 естественно отличается набором тегов
		// входов (с правами остаётся tun-in) — это dobavitPravilomRezhimQuic,
		// чужое поведение, не наша правка. Сравниваем без него.
		prCopy, prDoCopy := maps.Clone(pr), maps.Clone(prDo)
		delete(prCopy, "inbound")
		delete(prDoCopy, "inbound")
		if !reflect.DeepEqual(prCopy, prDoCopy) {
			t.Fatalf("route.rules[%d] изменилось: было %v, стало %v — порядок и содержимое правил трогать нельзя", i, prDo, pr)
		}
	}
	if !nashlosIpPrivate {
		t.Fatal("правило ip_is_private→direct пропало — страховка для домашней сети исчезла")
	}
}

// Режим прокси (нет прав) route.final не трогаем — там как и раньше "direct":
// решает уже система через set_system_proxy, а не route.final ядра.
func TestBezPravFinalOstayotsyaDirect(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: false})
	if err != nil {
		t.Fatal(err)
	}
	d := razobrat(t, gotovyy)
	r, _ := d["route"].(map[string]any)
	final, _ := r["final"].(string)
	if final != "direct" {
		t.Fatalf("route.final = %q, в режиме прокси (без прав) ожидал direct как раньше", final)
	}
}

// Комплект (BezSetevyhPravil) главнее и без него взводится независимо —
// проверяем, что при Prava:true+BezSetevyhPravil final по-прежнему туннель
// (это поведение уже было, новая правка его не должна задеть).
func TestSPolnymiPravamiIBezSetevyhPravilFinalTunnel(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true, BezSetevyhPravil: true})
	if err != nil {
		t.Fatal(err)
	}
	d := razobrat(t, gotovyy)
	r, _ := d["route"].(map[string]any)
	final, _ := r["final"].(string)
	if final != "Соединение" {
		t.Fatalf("route.final = %q, ожидал тег селектора «Соединение»", final)
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
	// Своё правило про QUIC/udp443 (dobavitPravilomRezhimQuic) к этой
	// проверке отношения не имеет — считаем только правила профиля.
	svoih := 0
	for _, p := range pravila {
		if pr, ok := p.(map[string]any); ok {
			if praviloUdp443(pr) {
				continue
			}
		}
		svoih++
	}
	if svoih != 2 {
		t.Fatalf("ожидал 2 правила (одно выброшено целиком), получил %d", svoih)
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

// BezSetevyhPravil — упрощённый режим, взводится, когда сервер правил
// недоступен (см. Vybor.BezSetevyhPravil). Ожидаем: ни rule_set, ни ссылок на
// него в правилах не осталось, а route.final указывает на туннельный выход,
// а не на "direct" (иначе трафик молча пошёл бы мимо VPN — боевой профиль
// живёт именно с route.final=="direct").
func TestBezSetevyhPravilUbiraetRuleSetIStavitFinal(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{BezSetevyhPravil: true})
	if err != nil {
		t.Fatal(err)
	}
	if k.Zametka != ZametkaBezSetevyhPravil {
		t.Fatalf("заметка не про упрощённый режим: %q", k.Zametka)
	}
	d := razobrat(t, gotovyy)
	r, _ := d["route"].(map[string]any)
	if _, est := r["rule_set"]; est {
		t.Fatal("route.rule_set остался — источник правил больше не спросят, ядро на это упадёт")
	}
	if strings.Contains(string(gotovyy), `"rule_set"`) {
		t.Fatal(`ссылка "rule_set" осталась в правилах (route.rules или dns.rules)`)
	}
	final, _ := r["final"].(string)
	if final == "" || final == "direct" {
		t.Fatalf("route.final = %q — весь трафик пойдёт мимо туннеля", final)
	}
	if final != "Соединение" {
		t.Fatalf("ожидал тег селектора «Соединение» (первый outbounds[].type==selector), получил %q", final)
	}
}

// Профиль без единого туннельного выхода: молчаливое "final": "direct" хуже
// падения (тихая утечка трафика), поэтому это должно быть ошибкой.
func TestBezSetevyhPravilBezTunnelnogoVyhodaEtoOshibka(t *testing.T) {
	syroy := []byte(`{"route":{"final":"direct","rule_set":[{"tag":"ads"}],
		"rules":[{"outbound":"direct","rule_set":["ads"]}]},
		"outbounds":[{"type":"direct","tag":"direct"},{"type":"block","tag":"block"}],
		"inbounds":[{"type":"mixed","tag":"mixed-in","listen":"127.0.0.1","listen_port":2412}]}`)
	if _, _, err := Prigotovit(syroy, Vybor{BezSetevyhPravil: true}); err == nil {
		t.Fatal("без туннельного выхода упрощённый режим не должен молча оставлять final=direct")
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
