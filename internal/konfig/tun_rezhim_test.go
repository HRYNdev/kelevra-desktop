package konfig

import (
	"encoding/json"
	"strings"
	"testing"
)

// Сторожа двух режимов и DNS-связки в каждом из них.
//
// Диагноз 31.08 на машине человека, приборно: в собранном yadro\config.json
// оказался ровно ОДИН вход — mixed на порту 2412; туннель не поднимался ни
// разу. Всё, что попадало в клиент, уходило правильно (в журнале ядра
// edge.microsoft.com ушёл через outbound/vless за 136 мс), но мимо клиента
// шёл весь QUIC и UDP: системный прокси их не перехватывает. YouTube ходит по
// QUIC — отсюда «впн не выполняет свою основную функцию».
//
// Второй половиной той же беды был DNS: route.final=direct вместе с
// резолвером tcp://1.1.1.1, который в журнале умирал («lookup www.bing.com:
// exchange4: use of closed network connection»), — и сайты не открывались при
// зелёном круге «подключено».

// serveryDns — dns.servers готового конфига, тегом к самому объекту.
func serveryDns(t *testing.T, d map[string]any) map[string]map[string]any {
	t.Helper()
	dns, _ := d["dns"].(map[string]any)
	if dns == nil {
		t.Fatal("в готовом конфиге нет dns")
	}
	out := map[string]map[string]any{}
	spisok, _ := dns["servers"].([]any)
	for _, sv := range spisok {
		m, ok := sv.(map[string]any)
		if !ok {
			continue
		}
		teg, _ := m["tag"].(string)
		out[teg] = m
	}
	return out
}

// storeFakeip — experimental.cache_file.store_fakeip готового конфига.
func storeFakeip(t *testing.T, d map[string]any) (bool, bool) {
	t.Helper()
	e, _ := d["experimental"].(map[string]any)
	if e == nil {
		return false, false
	}
	c, _ := e["cache_file"].(map[string]any)
	if c == nil {
		return false, false
	}
	v, est := c["store_fakeip"].(bool)
	return v, est
}

// estDeystvieMarshruta — есть ли в route.rules правило с таким action.
func estDeystvieMarshruta(d map[string]any, deystvie string) bool {
	r, _ := d["route"].(map[string]any)
	if r == nil {
		return false
	}
	pravila, _ := r["rules"].([]any)
	for _, p := range pravila {
		pr, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if a, _ := pr["action"].(string); a == deystvie {
			return true
		}
	}
	return false
}

// TestSPravamiTunnelOstayotsyaILishnegoVhodaNet — сценарий «права есть».
// Проверяется не только то, что туннель уцелел, но и то, что локальный прокси
// УШЁЛ. Оставленный вход означал бы открытый порт 2412, на который может
// смотреть системный прокси в реестре, — то есть ровно авария 31.08, только в
// туннельном режиме: прокси висит на порту, за которым в этом конфиге никого
// нет, и у человека не грузится ни один сайт при живом туннеле.
func TestSPravamiTunnelOstayotsyaILishnegoVhodaNet(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	if k.Rezhim != Tunnel {
		t.Fatalf("права есть, а режим %q", k.Rezhim)
	}
	if k.Chastichnaya {
		t.Fatal("туннель поднят, а картина говорит о половинной защите — окно нарисует жёлтое «частично» на полной защите")
	}
	if k.PochemuChastichnaya != "" {
		t.Fatalf("защита полная, а причина половинчатости не пуста: %q", k.PochemuChastichnaya)
	}
	if k.TunImya != "tun125" {
		t.Fatalf("имя адаптера туннеля = %q, в профиле interface_name=tun125 — "+
			"без него след на диске (internal/tunnel) не сможет проверить адаптер после аварии", k.TunImya)
	}
	if k.ProksiAdres != "" {
		t.Fatalf("в туннельном режиме отдан адрес прокси %q — по нему sluzhba.PodnyatZashchitu "+
			"напишет метку «системный прокси поставили мы» и оставит её висеть", k.ProksiAdres)
	}

	d := razobrat(t, gotovyy)
	estTun := false
	for _, vh := range vhody(d) {
		switch tip, _ := vh["type"].(string); tip {
		case "tun":
			estTun = true
			for _, p := range androidPolyaTun {
				if _, lishnee := vh[p]; lishnee {
					t.Fatalf("во входе-туннеле осталось android-поле %q", p)
				}
			}
		case "mixed", "http", "socks":
			t.Fatalf("в туннельном режиме остался лишний вход %q — это открытый порт, "+
				"на который может смотреть системный прокси", tip)
		}
	}
	if !estTun {
		t.Fatal("права есть, а вход-туннель из конфига пропал — клиент молча стал прокси")
	}
}

// TestBezPravOstayotsyaMixedIChestnyyStatus — сценарий «прав нет». Прокси
// остаётся (иначе подключаться нечем), но подпись обязана быть честной: до
// 31.08 окно рисовало в этом режиме ровно тот же зелёный круг «подключено»,
// что и при поднятом туннеле.
func TestBezPravOstayotsyaMixedIChestnyyStatus(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{Prava: false})
	if err != nil {
		t.Fatal(err)
	}
	if k.Rezhim != Proksi {
		t.Fatalf("прав нет, а режим %q", k.Rezhim)
	}
	if !k.Chastichnaya {
		t.Fatal("прав нет, защищены только браузеры и только TCP — а картина молчит о половинной защите")
	}
	if k.PochemuChastichnaya != PrichinaBezPrav {
		t.Fatalf("причина половинчатости = %q, ждали %q", k.PochemuChastichnaya, PrichinaBezPrav)
	}
	if k.ProksiAdres != "127.0.0.1:2412" {
		t.Fatalf("адрес прокси = %q, ждали 127.0.0.1:2412", k.ProksiAdres)
	}

	d := razobrat(t, gotovyy)
	estMixed := false
	for _, vh := range vhody(d) {
		switch tip, _ := vh["type"].(string); tip {
		case "tun":
			t.Fatal("прав нет, а вход-туннель остался — ядро упадёт на старте")
		case "mixed":
			estMixed = true
			if sp, _ := vh["set_system_proxy"].(bool); !sp {
				t.Fatal("прокси-режим без просьбы прописать себя в системе — ядро работает, а трафик идёт мимо")
			}
		}
	}
	if !estMixed {
		t.Fatal("прав нет и входа mixed тоже нет — подключаться нечем")
	}
}

// Профиль без туннеля вообще: нажимать нечего, просить права незачем, и
// причина должна быть другой — иначе окно позовёт человека на кнопку, которая
// ничего не изменит.
func TestBezTunnelyaVProfilePrichinaDrugaya(t *testing.T) {
	d := razobrat(t, profil(t))
	var bezTunnelya []any
	for _, v := range d["inbounds"].([]any) {
		vh := v.(map[string]any)
		if tip, _ := vh["type"].(string); tip == "tun" {
			continue
		}
		bezTunnelya = append(bezTunnelya, vh)
	}
	d["inbounds"] = bezTunnelya
	syroy, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}

	// Права есть, а туннеля в ключе нет — режим всё равно половинный.
	_, k, err := Prigotovit(syroy, Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	if k.EstTunnel {
		t.Fatal("туннель из профиля убран, а картина его видит")
	}
	if !k.Chastichnaya || k.PochemuChastichnaya != PrichinaBezTunnelya {
		t.Fatalf("картина = (частичная %v, причина %q), ждали (true, %q)",
			k.Chastichnaya, k.PochemuChastichnaya, PrichinaBezTunnelya)
	}
}

// TestVRezhimeTunnelyaFakeipZhivoyISvyazkaTselaya — fakeip оживает только в
// туннельном режиме, и связка обязана быть целой с ОБЕИХ сторон: сервер, на
// который ссылаются из dns.rules, и перехват DNS в route.rules, без которого
// запросы программ до ядра не доходят и выдуманные адреса не выдаются никогда.
func TestVRezhimeTunnelyaFakeipZhivoyISvyazkaTselaya(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	d := razobrat(t, gotovyy)
	servery := serveryDns(t, d)
	if servery["fakeip"] == nil {
		t.Fatal("в туннельном режиме сервер fakeip выброшен — маршрутизация по домену для двух десятков rule_set умерла")
	}
	if tip, _ := servery["fakeip"]["type"].(string); tip != "fakeip" {
		t.Fatalf("сервер с тегом fakeip имеет тип %q", tip)
	}
	if !naFakeipSsylayutsya(d, "fakeip") {
		t.Fatal("сервер fakeip есть, а ссылаться на него некому — заряженная мина без применения")
	}
	if !estDeystvieMarshruta(d, "hijack-dns") {
		t.Fatal("fakeip есть, а перехвата DNS нет: Windows спросит свой резолвер, ядро об этом не узнает, " +
			"и выдуманные адреса не выдадутся ни разу — ровно то, что показал ноль записей 198.18 в базе ядра")
	}
}

// Тот же туннельный режим, но профиль без hijack-dns: связку надо ДОБАВИТЬ, а
// не оставить наполовину.
func TestVRezhimeTunnelyaPerehvatDnsDobavlyaetsyaEsliEgoNet(t *testing.T) {
	d := razobrat(t, profil(t))
	r := d["route"].(map[string]any)
	var bezPerehvata []any
	for _, p := range r["rules"].([]any) {
		pr, ok := p.(map[string]any)
		if ok {
			if a, _ := pr["action"].(string); a == "hijack-dns" {
				continue
			}
		}
		bezPerehvata = append(bezPerehvata, p)
	}
	r["rules"] = bezPerehvata
	syroy, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}

	gotovyy, _, err := Prigotovit(syroy, Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	if !estDeystvieMarshruta(razobrat(t, gotovyy), "hijack-dns") {
		t.Fatal("перехват DNS не добавлен — fakeip остался половиной связки")
	}
}

// Туннельный режим плюс BezSetevyhPravil: та выбрасывает из dns.rules всё, что
// ссылается на rule_set, а правило про fakeip в боевом профиле именно такое.
// Оставшийся без единой ссылки сервер fakeip — мина, которая оживёт от любого
// будущего правила, написанного не глядя; вместе с ним обязано погаснуть и
// хранение его карты между запусками.
func TestVRezhimeTunnelyaFakeipBezSsylokUhoditVmesteSHranilishchem(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true, BezSetevyhPravil: true})
	if err != nil {
		t.Fatal(err)
	}
	d := razobrat(t, gotovyy)
	if serveryDns(t, d)["fakeip"] != nil {
		t.Fatal("на fakeip больше никто не ссылается, а сервер остался в конфиге")
	}
	if v, est := storeFakeip(t, d); !est || v {
		t.Fatalf("store_fakeip = (%v, задан %v), ждали (false, true): ядро продолжит "+
			"переживать между запусками карту адресов, которую теперь неоткуда взять", v, est)
	}
}

// TestVRezhimeProksiFakeipObezvrezhen — запасной режим. fakeip тут не работает
// вовсе (DNS Windows делает мимо ядра), но опасен он не бездействием, а тем,
// что может сработать НАПОЛОВИНУ: программа, спросившая DNS через сам прокси,
// получит 198.18.x.x и пойдёт по этому адресу напрямую, мимо прокси, — в
// никуда. Поэтому сервер убирается, а ссылки на него переводятся на обычный
// резолвер: ядро на ссылку в никуда попросту не запустится.
func TestVRezhimeProksiFakeipObezvrezhen(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: false})
	if err != nil {
		t.Fatal(err)
	}
	d := razobrat(t, gotovyy)
	if serveryDns(t, d)["fakeip"] != nil {
		t.Fatal("в прокси-режиме сервер fakeip остался — он может сработать наполовину и увести программу в никуда")
	}
	if naFakeipSsylayutsya(d, "fakeip") {
		t.Fatal("сервера fakeip нет, а правила dns.rules всё ещё на него ссылаются — ядро не запустится")
	}
	if v, est := storeFakeip(t, d); !est || v {
		t.Fatalf("store_fakeip = (%v, задан %v), ждали (false, true)", v, est)
	}
	// Ссылки должны быть переведены на обычный резолвер (dns.final), а не
	// стёрты: правило без сервера означало бы другой маршрут запроса.
	perevedeno := false
	perebratDnsPravila(d, func(pr map[string]any) {
		if srv, _ := pr["server"].(string); srv == "local" {
			perevedeno = true
		}
	})
	if !perevedeno {
		t.Fatal("ни одно правило dns.rules не переведено на обычный резолвер — правила про два десятка rule_set потеряли сервер")
	}
}

// TestVRezhimeProksiPryamoyResolverCherezSistemu — вторая половина беды 31.08.
// route.final в профиле "direct", и всё, что не попало ни в один rule_set,
// идёт напрямую; резолвит эти домены сервер "local", который на самом деле
// tcp://1.1.1.1. В журнале ядра он умирал, и половина интернета переставала
// открываться при зелёном круге. В прокси-режиме туннеля нет, системный
// резолвер НЕ перехвачен — значит он по определению тот, который на этой сети
// работает (домашний роутер человека, гостевой портал, корпоративный DNS).
func TestVRezhimeProksiPryamoyResolverCherezSistemu(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: false})
	if err != nil {
		t.Fatal(err)
	}
	local := serveryDns(t, razobrat(t, gotovyy))["local"]
	if local == nil {
		t.Fatal("резолвер прямого выхода исчез из конфига целиком")
	}
	if tip, _ := local["type"].(string); tip != "local" {
		t.Fatalf("резолвер прямого выхода = %q, ждали \"local\" (системный): зашитый tcp://1.1.1.1 — "+
			"первая мишень провайдера, и когда он падает, вместе с ним падает всё, что не попало в rule_set", tip)
	}
	// Тип local полей tcp-сервера не знает, и ядро на них ругается: объект
	// обязан быть новым, а не подправленным старым.
	for _, lishnee := range []string{"server", "server_port", "detour"} {
		if _, est := local[lishnee]; est {
			t.Fatalf("у резолвера типа local осталось поле %q от прежнего tcp-сервера — ядро на нём заругается", lishnee)
		}
	}
	if strings.Contains(string(gotovyy), "1.1.1.1") {
		t.Fatal("в готовом конфиге прокси-режима остался адрес 1.1.1.1 — тот самый резолвер, который умирал в журнале")
	}
}

// В туннельном режиме подменять резолвер прямого выхода на системный НЕЛЬЗЯ:
// с поднятым tun системный резолвер сам завёрнут в ядро, и спрашивать его —
// замкнуть петлю на себя.
func TestVRezhimeTunnelyaPryamoyResolverNeTrogaem(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	local := serveryDns(t, razobrat(t, gotovyy))["local"]
	if local == nil {
		t.Fatal("резолвер прямого выхода исчез из туннельного конфига")
	}
	if tip, _ := local["type"].(string); tip != "tcp" {
		t.Fatalf("в туннельном режиме резолвер прямого выхода стал %q — с поднятым tun это петля на себя", tip)
	}
}
