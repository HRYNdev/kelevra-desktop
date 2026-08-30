package konfig

// TestPrigotovitRezhetQuic — боевая жалоба «YouTube не грузит под VPN» 29.08,
// не закрытая коммитом от 29.08 (правило было завязано на снифф quic, тоже
// промахивающийся на retry/coalescing) и переделанная 30.08: Prigotovit
// обязан добавить в НАЧАЛО route.rules правило
// {"network":"udp","port":443,"action":"reject"} — оно не зависит от того,
// узнал ли снифф пакет как QUIC, потому что матчит по транспорту и порту
// напрямую. HTTP/3 всегда ходит по udp/443, поэтому это тот же смысл
// (форсировать fallback на TCP), но без слепого пятна сниффа. Chrome без
// ответа по QUIC откатывается на TCP/443, где снифф по SNI надёжен.
//
// Требования по заданию: правило появляется ровно один раз, повторный
// прогон Prigotovit не дублирует его, порядок остальных правил не ломается
// (правило ставится после ведущих sniff/hijack-dns, до правил с rule_set).

import (
	"encoding/json"
	"strings"
	"testing"
)

// pravilaRoute — route.rules подготовленного профиля как []map[string]any.
func pravilaRoute(t *testing.T, gotovyy []byte) []map[string]any {
	t.Helper()
	d := razobrat(t, gotovyy)
	r, ok := d["route"].(map[string]any)
	if !ok {
		t.Fatal("в подготовленном профиле нет route")
	}
	spisok, ok := r["rules"].([]any)
	if !ok {
		t.Fatal("в route нет rules")
	}
	var out []map[string]any
	for _, p := range spisok {
		m, ok := p.(map[string]any)
		if !ok {
			t.Fatalf("правило не объект: %#v", p)
		}
		out = append(out, m)
	}
	return out
}

// schyotUdp443Pravil — сколько раз в списке правил встречается network:udp
// + port:443 (форма нашего защитного правила, см. praviloUdp443).
func schyotUdp443Pravil(pravila []map[string]any) int {
	n := 0
	for _, p := range pravila {
		if praviloUdp443(p) {
			n++
		}
	}
	return n
}

func TestPrigotovitDobavlyaetPraviloRezhaSchayeyeQuicOdinRaz(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}

	pravila := pravilaRoute(t, gotovyy)
	if n := schyotUdp443Pravil(pravila); n != 1 {
		t.Fatalf("правил network:udp+port:443 в route.rules: %d, хочу ровно 1", n)
	}

	// Найти вставленное правило и проверить его форму и место.
	idx := -1
	for i, p := range pravila {
		if praviloUdp443(p) {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("правило network:udp+port:443 не нашлось")
	}
	novoye := pravila[idx]
	if act, _ := novoye["action"].(string); act != "reject" {
		t.Fatalf(`правило про udp/443 = %#v, хочу {"network":"udp","port":443,"action":"reject"}`, novoye)
	}
	if network, _ := novoye["network"].(string); network != "udp" {
		t.Fatalf(`правило про udp/443 = %#v, поле network должно быть "udp"`, novoye)
	}
	if port := portChislom(novoye["port"]); port != 443 {
		t.Fatalf(`правило про udp/443 = %#v, поле port должно быть 443`, novoye)
	}
	// Не должно зависеть от protocol — это ровно то поле, которое
	// подставлял ненадёжный снифф и от которого мы уходим.
	if _, est := novoye["protocol"]; est {
		t.Fatalf(`правило про udp/443 = %#v, не должно содержать protocol — оно не зависит от сниффа`, novoye)
	}

	// Порядок: ведущие sniff/hijack-dns (если есть) обязаны остаться ДО
	// нового правила, а первое правило с rule_set — ПОСЛЕ него.
	for i := 0; i < idx; i++ {
		act, _ := pravila[i]["action"].(string)
		if act != "sniff" && act != "hijack-dns" {
			t.Fatalf("перед правилом про udp/443 (индекс %d) стоит не sniff/hijack-dns правило: %#v", i, pravila[i])
		}
	}
	for i := idx + 1; i < len(pravila); i++ {
		if _, est := pravila[i]["rule_set"]; est {
			// Это ожидаемо и нормально — правило про udp/443 обязано стоять
			// ДО таких правил, что уже проверено индексом idx < i.
			// Дополнительно убеждаемся, что само оно тут единственное, а не
			// затесалось ещё одно позже (уже проверено schyotUdp443Pravil
			// выше).
			continue
		}
	}
	if idx == 0 {
		t.Skip("в этом профиле нет ведущих sniff/hijack-dns правил — граничный случай размещения в начало не проверить на этих данных")
	}
}

// TestPrigotovitPravilomQuicIdempotentno — повторный прогон Prigotovit (в
// том числе на УЖЕ подготовленном профиле — том самом, что вернул первый
// прогон) не должен дублировать правило про udp/443 и не должен портить его
// место в списке.
func TestPrigotovitPravilomQuicIdempotentno(t *testing.T) {
	pervyy, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := schyotUdp443Pravil(pravilaRoute(t, pervyy)); n != 1 {
		t.Fatalf("после первого прогона правил network:udp+port:443: %d, хочу 1", n)
	}

	// Второй прогон на уже подготовленном профиле: имитирует то, что
	// случится, если Prigotovit случайно позовут дважды подряд на одном и
	// том же теле (переоткрытие подключения, повторная попытка и т.п.).
	vtoroy, _, err := Prigotovit(pervyy, Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	pravilaVtorogo := pravilaRoute(t, vtoroy)
	if n := schyotUdp443Pravil(pravilaVtorogo); n != 1 {
		t.Fatalf("после второго прогона на уже подготовленном профиле правил network:udp+port:443: %d, хочу 1 (не дублируется)", n)
	}

	// Порядок правил у первого и второго прогона обязан совпасть один в
	// один — второй прогон не переставляет ничего заново.
	pravilaPervogo := pravilaRoute(t, pervyy)
	if len(pravilaPervogo) != len(pravilaVtorogo) {
		t.Fatalf("длина route.rules разъехалась: было %d, стало %d", len(pravilaPervogo), len(pravilaVtorogo))
	}
	for i := range pravilaPervogo {
		p1, p2 := pravilaPervogo[i], pravilaVtorogo[i]
		net1, _ := p1["network"].(string)
		net2, _ := p2["network"].(string)
		act1, _ := p1["action"].(string)
		act2, _ := p2["action"].(string)
		if net1 != net2 || act1 != act2 || portChislom(p1["port"]) != portChislom(p2["port"]) {
			t.Fatalf("правило %d изменилось между прогонами: было %#v, стало %#v", i, p1, p2)
		}
	}
}

// TestPrigotovitNeTrogayetSvoyeUdp443 — если профиль сам уже содержит
// правило с network:udp+port:443 (сервер решил про QUIC/443 сам, пусть и не
// action:reject), Prigotovit его не трогает — профиль главнее.
func TestPrigotovitNeTrogayetSvoyeUdp443(t *testing.T) {
	d := razobrat(t, profil(t))
	r := d["route"].(map[string]any)
	svoye := map[string]any{"network": "udp", "port": 443, "outbound": "direct"}
	r["rules"] = append([]any{svoye}, r["rules"].([]any)...)
	syroy, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}

	gotovyy, _, err := Prigotovit(syroy, Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	pravila := pravilaRoute(t, gotovyy)
	if n := schyotUdp443Pravil(pravila); n != 1 {
		t.Fatalf("правил network:udp+port:443: %d, хочу 1 — своё правило профиля не должно размножиться и не должно смениться на наше", n)
	}
	if act, _ := pravila[0]["action"].(string); act != "" {
		t.Fatalf("первое правило должно было остаться своим (outbound, не action=reject): %#v", pravila[0])
	}
	if out, _ := pravila[0]["outbound"].(string); out != "direct" {
		t.Fatalf("своё правило профиля про udp/443 подменилось: %#v", pravila[0])
	}
}

// tegiMnozhestvom — inbound-поле правила ([]any со строками после
// json-круга) как множество, для сравнения без оглядки на порядок.
func tegiMnozhestvom(t *testing.T, v any) map[string]bool {
	t.Helper()
	spisok, ok := v.([]any)
	if !ok {
		t.Fatalf("поле inbound не список: %#v", v)
	}
	out := map[string]bool{}
	for _, s := range spisok {
		str, ok := s.(string)
		if !ok {
			t.Fatalf("тег inbound не строка: %#v", s)
		}
		out[str] = true
	}
	return out
}

// TestPraviloUdp443OgranichenoVhodamiPolzovatelya — риск самоотстрела:
// сервер выхода (vless+reality, TCP/443) в testdata/profil_telefona.json сам
// по себе не пострадал бы даже без этого ограничения (route.rules матчит
// только вошедший через inbound трафик, а не собственный dial outbound'а к
// его серверу), но по заданию правило обязано явно ссылаться только на
// входы пользователя, взятые ИЗ профиля, а не хардкодиться — и не должно
// пережить фильтрацию входов машины устаревшей ссылкой.
func TestPraviloUdp443OgranichenoVhodamiPolzovatelya(t *testing.T) {
	// С правами: остаются оба входа профиля — tun-in и mixed-in.
	sPravami, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	pravila := pravilaRoute(t, sPravami)
	idx := -1
	for i, p := range pravila {
		if praviloUdp443(p) {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("правило про udp/443 не нашлось")
	}
	hochu := map[string]bool{"tun-in": true, "mixed-in": true}
	if got := tegiMnozhestvom(t, pravila[idx]["inbound"]); !mapsRavny(got, hochu) {
		t.Fatalf("inbound правила про udp/443 = %v, хочу %v (оба входа профиля, взятые динамически)", got, hochu)
	}

	// Без прав: tun-in выбрасывается целиком (Prigotovit, случай !estPrava).
	// Если бы теги брались один раз ДО фильтрации входов, правило осталось
	// бы со ссылкой на уже несуществующий tun-in — ровно та беда, которую
	// pochistitPravila чинит для правил профиля.
	bezPrav, _, err := Prigotovit(profil(t), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	pravilaBezPrav := pravilaRoute(t, bezPrav)
	idx = -1
	for i, p := range pravilaBezPrav {
		if praviloUdp443(p) {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("правило про udp/443 не нашлось (без прав)")
	}
	hochuBezPrav := map[string]bool{"mixed-in": true}
	if got := tegiMnozhestvom(t, pravilaBezPrav[idx]["inbound"]); !mapsRavny(got, hochuBezPrav) {
		t.Fatalf("inbound правила про udp/443 без прав = %v, хочу %v — tun-in выброшен, ссылки на него быть не должно", got, hochuBezPrav)
	}
	if strings.Contains(string(bezPrav), `"tun-in"`) {
		t.Fatal("выброшенный tun-in всё ещё где-то упомянут в готовом конфиге")
	}
}

func mapsRavny(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
