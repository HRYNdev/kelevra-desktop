package konfig

import (
	"testing"
)

// Сторож локальной сети.
//
// Беда 31.08 на стенде: с поднятым туннелем машина теряет локальную сеть —
// SSH отработал около шести команд, потом передача файла зависла и связь
// пропала совсем при живой Windows. Дома у человека на той же сети NAS,
// Home Assistant, роутер и принтер, и терять их при включённом VPN нельзя.
//
// Тесты здесь бьются о НАСТОЯЩИЙ профиль с сервера (testdata/profil_telefona
// .json) — тот самый, где в секции tun стоит strict_route: true, а в
// route.rules уже есть ip_is_private, который от беды не спас.

// pravilaTunnelnogo — route.rules готового конфига в туннельном режиме.
func pravilaTunnelnogo(t *testing.T) []map[string]any {
	t.Helper()
	gotovyy, k, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	if k.Rezhim != Tunnel {
		t.Fatalf("ожидал туннельный режим, получил %q", k.Rezhim)
	}
	return pravilaRoute(t, gotovyy)
}

// nomerPravilaLokalki — на каком месте в route.rules стоит наше правило.
// -1 — его нет вовсе.
func nomerPravilaLokalki(pravila []map[string]any) int {
	for i, pr := range pravila {
		if praviloProLokalnuyuSet(pr) {
			return i
		}
	}
	return -1
}

// TestTunnelnyyKonfigUvoditLokalnuyuSetNapryamuyu — главное требование:
// в собранном туннельном конфиге ЕСТЬ правило, уводящее локальные подсети
// напрямую.
func TestTunnelnyyKonfigUvoditLokalnuyuSetNapryamuyu(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	pravila := pravilaRoute(t, gotovyy)
	n := nomerPravilaLokalki(pravila)
	if n < 0 {
		t.Fatalf("в туннельном конфиге нет правила про локальные подсети: %v", pravila)
	}
	d := razobrat(t, gotovyy)
	teg := tegPryamogoVyhoda(d)
	if got, _ := pravila[n]["outbound"].(string); got != teg {
		t.Fatalf("правило локалки ведёт в %q, а прямой выход профиля — %q", got, teg)
	}
	// Каждая подсеть из списка обязана быть на месте: пропавшая строка —
	// это отвалившийся дома прибор, и заметить это по зелёному тесту нельзя.
	est := map[string]bool{}
	for _, s := range spisokStrok(pravila[n]["ip_cidr"]) {
		est[s] = true
	}
	for _, nado := range LokalnyePodseti() {
		if !est[nado] {
			t.Fatalf("в правиле локалки нет подсети %s", nado)
		}
	}
}

// TestPraviloLokalkiStoitRanshheObshchih — правило обязано стоять ПЕРЕД
// общими: перед срезом QUIC, перед правилами с rule_set и, разумеется, перед
// route.final. Иначе подсеть, попавшая в чей-нибудь rule_set, уедет в туннель
// раньше, чем дойдёт до нашего правила.
func TestPraviloLokalkiStoitRanshheObshchih(t *testing.T) {
	pravila := pravilaTunnelnogo(t)
	n := nomerPravilaLokalki(pravila)
	if n < 0 {
		t.Fatal("правила про локальные подсети нет вовсе")
	}
	for i, pr := range pravila {
		if i == n {
			continue
		}
		if _, est := pr["rule_set"]; est && i < n {
			t.Fatalf("правило с rule_set стоит на %d, раньше локалки (%d): %v", i, n, pr)
		}
		if praviloUdp443(pr) && i < n {
			t.Fatalf("срез QUIC стоит на %d, раньше локалки (%d)", i, n)
		}
	}
	// ...но ПОЗЖЕ hijack-dns: запрос к DNS домашнего роутера обязан
	// по-прежнему заворачиваться в ядро, иначе в туннельном режиме имена
	// начнут разрешать двое сразу.
	for i, pr := range pravila {
		if act, _ := pr["action"].(string); act == "hijack-dns" || act == "sniff" {
			if i > n {
				t.Fatalf("%s стоит на %d, позже локалки (%d)", act, i, n)
			}
		}
	}
}

// TestStrictRouteVyklyuchenIMarshrutyLokalkiIsklyucheny — два других слоя
// починки, на самом входе tun. Профиль присылает strict_route: true, и это
// не пожелание, а правила фаервола WFP, которые режут трафик мимо туннеля.
func TestStrictRouteVyklyuchenIMarshrutyLokalkiIsklyucheny(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	var tun map[string]any
	for _, vh := range vhody(razobrat(t, gotovyy)) {
		if tip, _ := vh["type"].(string); tip == "tun" {
			tun = vh
		}
	}
	if tun == nil {
		t.Fatal("в туннельном конфиге нет входа tun")
	}
	if v, est := tun["strict_route"].(bool); !est || v {
		t.Fatalf("strict_route = %v (есть=%v), а должен быть явный false: "+
			"с true машина теряет локальную сеть", tun["strict_route"], est)
	}
	est := map[string]bool{}
	for _, s := range spisokStrok(tun["route_exclude_address"]) {
		est[s] = true
	}
	for _, nado := range []string{"192.168.0.0/16", "10.0.0.0/8", "172.16.0.0/12", "169.254.0.0/16"} {
		if !est[nado] {
			t.Fatalf("route_exclude_address не выводит %s из маршрутов туннеля: %v",
				nado, tun["route_exclude_address"])
		}
	}
}

// TestFinalOstalsyaPryamym — по умолчанию всё по-прежнему идёт напрямую.
// Починка локалки не имеет права заодно переставить route.final: это увело
// бы в туннель ВЕСЬ неопознанный трафик, включая сервер подписки.
func TestFinalOstalsyaPryamymPosleLokalki(t *testing.T) {
	for _, prava := range []bool{true, false} {
		gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: prava})
		if err != nil {
			t.Fatal(err)
		}
		d := razobrat(t, gotovyy)
		r, _ := d["route"].(map[string]any)
		if final, _ := r["final"].(string); final != "direct" {
			t.Fatalf("права=%v: route.final стал %q вместо direct", prava, final)
		}
	}
}

// TestPraviloLokalkiNePloditsya — конфиг собирается заново на каждом
// подключении, и профиль на диске может оказаться уже нашим готовым
// конфигом. Второй проход не имеет права добавить второе такое же правило.
func TestPraviloLokalkiNePloditsya(t *testing.T) {
	odin, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	dva, _, err := Prigotovit(odin, Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	skolko := 0
	for _, pr := range pravilaRoute(t, dva) {
		if praviloProLokalnuyuSet(pr) {
			skolko++
		}
	}
	if skolko != 1 {
		t.Fatalf("после второго прогона правил про локалку %d, а должно быть 1", skolko)
	}
}

// TestLokalkaZashchishchenaIBezPrav — в прокси-режиме туннеля нет, но
// правило маршрутизации остаётся: гарантия «дома всё доступно» не должна
// зависеть от того, дали права или нет.
func TestLokalkaZashchishchenaIBezPrav(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	if k.Rezhim != Proksi {
		t.Fatalf("ожидал прокси-режим, получил %q", k.Rezhim)
	}
	if nomerPravilaLokalki(pravilaRoute(t, gotovyy)) < 0 {
		t.Fatal("в прокси-режиме правила про локальные подсети нет")
	}
}

// TestPryamoyVyhodNahoditsyaPoTipu — тег прямого выхода берётся по ТИПУ, а не
// по имени: чужой профиль вправе назвать direct как угодно, а правило со
// ссылкой на несуществующий выход ядро отвергнет целиком.
func TestPryamoyVyhodNahoditsyaPoTipu(t *testing.T) {
	d := map[string]any{"outbounds": []any{
		map[string]any{"type": "vless", "tag": "Соединение"},
		map[string]any{"type": "direct", "tag": "мимо"},
	}}
	if teg := tegPryamogoVyhoda(d); teg != "мимо" {
		t.Fatalf("прямой выход найден как %q, а он назван «мимо»", teg)
	}
	// Прямого выхода нет вовсе — заводим свой, иначе гарантия зависела бы
	// от щедрости сервера подписки.
	pustoy := map[string]any{"outbounds": []any{
		map[string]any{"type": "vless", "tag": "Соединение"},
	}}
	teg := tegPryamogoVyhoda(pustoy)
	if teg == "" {
		t.Fatal("в профиле без прямого выхода мы его не завели")
	}
	nashli := false
	for _, v := range pustoy["outbounds"].([]any) {
		vh := v.(map[string]any)
		if vh["tag"] == teg && vh["type"] == "direct" {
			nashli = true
		}
	}
	if !nashli {
		t.Fatalf("выход %q обещан, но в outbounds его нет: %v", teg, pustoy["outbounds"])
	}
}
