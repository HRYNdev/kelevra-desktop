package konfig

// Сторож точности правила про QUIC (правка 31.08).
//
// Что было. dobavitPravilomRezhimQuic вставляла в конфиг ядра правило «весь
// UDP на порт 443 — отбить», одинаковое в обоих режимах и ничем не
// ограниченное. В режиме прокси это ещё почти не жгло: системный прокси UDP в
// клиент не заводит, и резать было почти нечего. А в режиме туннеля в клиент
// заходит ВЕСЬ UDP машины — и правило рубило QUIC всему компьютеру: браузер
// молча откатится на обычный протокол, а игра, звонок и всё, что умеет только
// QUIC, просто перестанет работать. В том числе к российским и нейтральным
// адресам, которым туннель не нужен вовсе.
//
// Что должно быть. Правило бьёт только по тому, что и так уводится в туннель
// (rule_set заблокированного из самого профиля). Всё остальное — включая
// весь UDP к прямым адресам — ядро не трогает.
//
// Почему правило не убрано совсем. UDP через наш выход НЕ ХОДИТ: основной
// выход профиля — vless с flow "xtls-rprx-vision", а vless запрещает UDP при
// этом flow на стороне сервера (sing-vmess, vless/service.go:
// `request.Flow == FlowVision && request.Command == vmess.NetworkUDP` →
// «flow does not support UDP»; строка лежит и в самом бинаре ядра). Без
// правила QUIC к заблокированному честно ушёл бы в туннель и умер бы там
// таймаутом — reject честнее и быстрее: браузер откатится на TCP сразу.

import (
	"encoding/json"
	"testing"
)

// praviloQuic — наше правило про udp/443 из готового конфига, и его номер в
// списке. Номер -1, если правила нет вовсе.
func praviloQuic(t *testing.T, gotovyy []byte) (map[string]any, int) {
	t.Helper()
	for i, p := range pravilaRoute(t, gotovyy) {
		if praviloUdp443(p) {
			return p, i
		}
	}
	return nil, -1
}

// tegiPravila — поле rule_set правила списком строк.
func tegiPravila(t *testing.T, pr map[string]any) []string {
	t.Helper()
	v, est := pr["rule_set"]
	if !est {
		return nil
	}
	spisok, ok := v.([]any)
	if !ok {
		t.Fatalf("rule_set правила не список: %#v", v)
	}
	var out []string
	for _, s := range spisok {
		str, ok := s.(string)
		if !ok {
			t.Fatalf("тег rule_set не строка: %#v", s)
		}
		out = append(out, str)
	}
	return out
}

// tunnelnyeTegiProfilya — какие rule_set сам профиль уводит в туннель. Это
// эталон, с которым сверяется правило: не хардкод, а то же самое, что читает
// человек в профиле подписки.
func tunnelnyeTegiProfilya(t *testing.T, syroy []byte) []string {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal(syroy, &d); err != nil {
		t.Fatal(err)
	}
	tegi := tegiZablokirovannogo(d)
	if len(tegi) == 0 {
		t.Fatal("в тестовом профиле нет ни одного rule_set в туннель — сверять не с чем")
	}
	return tegi
}

// TestVTunneleQuicRezhetsyaTolkoUZablokirovannogo — режим полной защиты.
// Главная проверка задания: правила «отбить ВЕСЬ udp/443» в туннельном
// конфиге быть не должно. Правило есть, но привязано к спискам
// заблокированного — значит QUIC игр и звонков к прямым адресам живой.
func TestVTunneleQuicRezhetsyaTolkoUZablokirovannogo(t *testing.T) {
	syroy := profil(t)
	gotovyy, k, err := Prigotovit(syroy, Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	if k.Rezhim != Tunnel {
		t.Fatalf("режим %q, а щуп про туннельный", k.Rezhim)
	}

	pr, idx := praviloQuic(t, gotovyy)
	if idx < 0 {
		t.Fatal("правила про udp/443 нет вовсе — QUIC к заблокированному уйдёт в туннель, где UDP не работает (flow does not support UDP), и умрёт таймаутом")
	}
	tegi := tegiPravila(t, pr)
	if len(tegi) == 0 {
		t.Fatalf("правило про udp/443 = %#v — оно режет ВЕСЬ UDP машины: у человека отвалятся игры, звонки и всё, что умеет только QUIC", pr)
	}
	hochu := tunnelnyeTegiProfilya(t, syroy)
	if len(tegi) != len(hochu) {
		t.Fatalf("rule_set правила = %v, а в туннель профиль уводит %v", tegi, hochu)
	}
	for i := range hochu {
		if tegi[i] != hochu[i] {
			t.Fatalf("rule_set правила = %v, а в туннель профиль уводит %v", tegi, hochu)
		}
	}
	if act, _ := pr["action"].(string); act != "reject" {
		t.Fatalf("action правила = %q, ожидал reject", act)
	}
}

// TestVProksiQuicPrivyazanKZablokirovannomu — запасной режим (нет прав).
// Здесь правило имеет смысл (без него YouTube по QUIC ушёл бы мимо клиента),
// но и здесь оно обязано бить точечно — по тем же спискам, а не по всему UDP
// машины.
func TestVProksiQuicPrivyazanKZablokirovannomu(t *testing.T) {
	syroy := profil(t)
	gotovyy, k, err := Prigotovit(syroy, Vybor{Prava: false})
	if err != nil {
		t.Fatal(err)
	}
	if k.Rezhim != Proksi {
		t.Fatalf("режим %q, а щуп про прокси", k.Rezhim)
	}

	pr, idx := praviloQuic(t, gotovyy)
	if idx < 0 {
		t.Fatal("правила про udp/443 в прокси-режиме нет — YouTube по QUIC уйдёт мимо клиента к провайдеру")
	}
	if tegi := tegiPravila(t, pr); len(tegi) == 0 {
		t.Fatalf("правило про udp/443 = %#v — оно режет весь UDP машины, а должно только списки заблокированного", pr)
	}
}

// TestFinalOstayotsyaDirectVOboihRezhimah — ожог 31.08 (коммит 654c8d4): по
// умолчанию весь трафик ушёл в туннель, «русские приложения начали ругаться
// на впн». Правка про QUIC не имеет права повторить это в другой форме:
// route.final остаётся direct, в туннель уходит только то, что в правилах.
func TestFinalOstayotsyaDirectVOboihRezhimah(t *testing.T) {
	for imya, v := range map[string]Vybor{
		"туннель": {Prava: true},
		"прокси":  {Prava: false},
	} {
		t.Run(imya, func(t *testing.T) {
			gotovyy, _, err := Prigotovit(profil(t), v)
			if err != nil {
				t.Fatal(err)
			}
			d := razobrat(t, gotovyy)
			r, _ := d["route"].(map[string]any)
			if final, _ := r["final"].(string); final != "direct" {
				t.Fatalf("route.final = %q, ожидал direct — по умолчанию всё идёт напрямую", final)
			}
		})
	}
}

// TestQuicPraviloNeTrogaetSpisokReklamy — список рекламы профиль отбивает сам
// (action:reject), и в сужение нашего правила он попадать не должен: там и
// так reject, а лишний тег — лишний матч.
func TestQuicPraviloNeTrogaetSpisokReklamy(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	pr, idx := praviloQuic(t, gotovyy)
	if idx < 0 {
		t.Fatal("правила про udp/443 нет")
	}
	for _, teg := range tegiPravila(t, pr) {
		if teg == "ads" {
			t.Fatal("в сужение правила попал список рекламы — он отбивается своим правилом профиля, наше сюда лезть не должно")
		}
	}
}

// TestQuicPraviloStoitDoPravilProfilya — порядок: после ведущих
// sniff/hijack-dns (снифф должен успеть отработать) и ДО первого правила
// профиля с rule_set, иначе решение профиля перехватит трафик раньше среза.
func TestQuicPraviloStoitDoPravilProfilya(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	pravila := pravilaRoute(t, gotovyy)
	_, idx := praviloQuic(t, gotovyy)
	if idx < 0 {
		t.Fatal("правила про udp/443 нет")
	}
	for i := 0; i < idx; i++ {
		act, _ := pravila[i]["action"].(string)
		// Наше правило про локальную сеть стоит здесь нарочно — см.
		// TestPraviloLokalkiStoitRanshheObshchih.
		if act != "sniff" && act != "hijack-dns" && !praviloProLokalnuyuSet(pravila[i]) {
			t.Fatalf("перед правилом про udp/443 стоит чужое правило [%d]: %#v", i, pravila[i])
		}
	}
	pervoeSRuleSet := -1
	for i, p := range pravila {
		if praviloUdp443(p) {
			continue
		}
		if _, est := p["rule_set"]; est {
			pervoeSRuleSet = i
			break
		}
	}
	if pervoeSRuleSet >= 0 && pervoeSRuleSet < idx {
		t.Fatalf("правило профиля с rule_set [%d] стоит раньше нашего [%d] — оно перехватит трафик первым", pervoeSRuleSet, idx)
	}
}

// TestBezTunnelnyhPravilQuicNeRezhetsyaVovse — чужой профиль, в котором в
// туннель не уходит вообще ничего (final=direct, ни одного rule_set в
// туннель). Резать в таком нечего, и правила быть не должно: иначе мы
// отобрали бы у человека QUIC просто так.
func TestBezTunnelnyhPravilQuicNeRezhetsyaVovse(t *testing.T) {
	syroy := []byte(`{
		"inbounds":[{"type":"mixed","tag":"mixed-in","listen":"127.0.0.1","listen_port":2412}],
		"outbounds":[{"type":"direct","tag":"direct"}],
		"route":{"final":"direct","rules":[{"action":"sniff"}]}}`)
	gotovyy, _, err := Prigotovit(syroy, Vybor{BezSistemnogoProksi: true})
	if err != nil {
		t.Fatal(err)
	}
	if pr, idx := praviloQuic(t, gotovyy); idx >= 0 {
		t.Fatalf("в профиле без туннельных правил появилось правило про udp/443: %#v — резать тут нечего", pr)
	}
}

// TestUproshchennyyRezhimRezhetVesUdp443 — единственный случай, где правило
// остаётся широким. BezSetevyhPravil выбрасывает rule_set целиком и
// переставляет route.final на туннель: в туннель идёт ВСЁ, сужать не по чему,
// а UDP там не работает. Широкий reject тут честнее тишины — иначе весь UDP
// уходил бы в туннель и умирал по таймауту.
func TestUproshchennyyRezhimRezhetVesUdp443(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true, BezSetevyhPravil: true})
	if err != nil {
		t.Fatal(err)
	}
	pr, idx := praviloQuic(t, gotovyy)
	if idx < 0 {
		t.Fatal("в упрощённом режиме правила про udp/443 нет — весь UDP уйдёт в туннель, где он не работает")
	}
	if tegi := tegiPravila(t, pr); len(tegi) > 0 {
		t.Fatalf("правило ссылается на rule_set %v, а их в упрощённом режиме в конфиге больше нет — ядро такой конфиг не примет", tegi)
	}
}

// TestSKomplektomQuicVsyoRavnoTochechnyy — правила из встроенного комплекта:
// теги rule_set остаются те же (меняется только источник, remote→local),
// значит и сужение обязано остаться на месте.
func TestSKomplektomQuicVsyoRavnoTochechnyy(t *testing.T) {
	syroy := profil(t)
	komplekt := map[string]string{}
	for _, teg := range tunnelnyeTegiProfilya(t, syroy) {
		komplekt[teg] = "C:/pravila/" + teg + ".srs"
	}
	komplekt["ads"] = "C:/pravila/ads.srs"

	gotovyy, _, err := Prigotovit(syroy, Vybor{Prava: true, PravilaIzKomplekta: komplekt, PravilaKomplektData: "2026-08-23"})
	if err != nil {
		t.Fatal(err)
	}
	pr, idx := praviloQuic(t, gotovyy)
	if idx < 0 {
		t.Fatal("с комплектом правила про udp/443 нет")
	}
	if tegi := tegiPravila(t, pr); len(tegi) == 0 {
		t.Fatalf("с комплектом правило стало широким: %#v", pr)
	}
}

// TestQuicPraviloSsylaetsyaTolkoNaZhivyeRuleSet — правило, ссылающееся на
// rule_set, которого в конфиге нет, ядро не примет (та же беда, что
// pochistitPravila чинит для входов). Проверяем оба режима и комплект.
func TestQuicPraviloSsylaetsyaTolkoNaZhivyeRuleSet(t *testing.T) {
	for imya, v := range map[string]Vybor{
		"туннель":    {Prava: true},
		"прокси":     {Prava: false},
		"упрощённый": {Prava: true, BezSetevyhPravil: true},
	} {
		t.Run(imya, func(t *testing.T) {
			gotovyy, _, err := Prigotovit(profil(t), v)
			if err != nil {
				t.Fatal(err)
			}
			d := razobrat(t, gotovyy)
			zhivye := map[string]bool{}
			if r, ok := d["route"].(map[string]any); ok {
				spisok, _ := r["rule_set"].([]any)
				for _, rs := range spisok {
					if m, ok := rs.(map[string]any); ok {
						if teg, _ := m["tag"].(string); teg != "" {
							zhivye[teg] = true
						}
					}
				}
			}
			pr, idx := praviloQuic(t, gotovyy)
			if idx < 0 {
				return
			}
			for _, teg := range tegiPravila(t, pr) {
				if !zhivye[teg] {
					t.Fatalf("правило ссылается на rule_set %q, которого в конфиге нет — ядро такой конфиг не примет", teg)
				}
			}
		})
	}
}
