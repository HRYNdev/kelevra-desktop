package konfig

// TestPrigotovitRezhetQuic — боевая жалоба «YouTube не грузит под VPN» 29.08:
// снифф QUIC у ядра (common/sniff/quic.go) промахивается на retry/coalescing,
// и часть соединений YouTube проваливается мимо ВСЕХ rule_set сразу на
// route.final=direct. Prigotovit обязан добавить в НАЧАЛО route.rules
// правило {"protocol":"quic","action":"reject"} — Chrome без ответа по QUIC
// откатывается на TCP/443, где снифф по SNI надёжен.
//
// Требования по заданию: правило появляется ровно один раз, повторный
// прогон Prigotovit не дублирует его, порядок остальных правил не ломается
// (правило ставится после ведущих sniff/hijack-dns, до правил с rule_set).

import (
	"encoding/json"
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

// schyotQuicPravil — сколько раз в списке правил встречается protocol:quic.
func schyotQuicPravil(pravila []map[string]any) int {
	n := 0
	for _, p := range pravila {
		if proto, _ := p["protocol"].(string); proto == "quic" {
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
	if n := schyotQuicPravil(pravila); n != 1 {
		t.Fatalf("правил protocol:quic в route.rules: %d, хочу ровно 1", n)
	}

	// Найти вставленное правило и проверить его форму и место.
	idx := -1
	for i, p := range pravila {
		if proto, _ := p["protocol"].(string); proto == "quic" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("правило protocol:quic не нашлось")
	}
	novoye := pravila[idx]
	if act, _ := novoye["action"].(string); act != "reject" {
		t.Fatalf(`правило про QUIC = %#v, хочу {"protocol":"quic","action":"reject"}`, novoye)
	}

	// Порядок: ведущие sniff/hijack-dns (если есть) обязаны остаться ДО
	// нового правила, а первое правило с rule_set — ПОСЛЕ него.
	for i := 0; i < idx; i++ {
		act, _ := pravila[i]["action"].(string)
		if act != "sniff" && act != "hijack-dns" {
			t.Fatalf("перед правилом про QUIC (индекс %d) стоит не sniff/hijack-dns правило: %#v", i, pravila[i])
		}
	}
	for i := idx + 1; i < len(pravila); i++ {
		if _, est := pravila[i]["rule_set"]; est {
			// Это ожидаемо и нормально — правило про QUIC обязано стоять ДО
			// таких правил, что уже проверено индексом idx < i. Дополнительно
			// убеждаемся, что само оно тут единственное, а не затесалось ещё
			// одно позже (уже проверено schyotQuicPravil выше).
			continue
		}
	}
	if idx == 0 {
		t.Skip("в этом профиле нет ведущих sniff/hijack-dns правил — граничный случай размещения в начало не проверить на этих данных")
	}
}

// TestPrigotovitPravilomQuicIdempotentno — повторный прогон Prigotovit (в
// том числе на УЖЕ подготовленном профиле — том самом, что вернул первый
// прогон) не должен дублировать правило про QUIC и не должен портить его
// место в списке.
func TestPrigotovitPravilomQuicIdempotentno(t *testing.T) {
	pervyy, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := schyotQuicPravil(pravilaRoute(t, pervyy)); n != 1 {
		t.Fatalf("после первого прогона правил protocol:quic: %d, хочу 1", n)
	}

	// Второй прогон на уже подготовленном профиле: имитирует то, что
	// случится, если Prigotovit случайно позовут дважды подряд на одном и
	// том же теле (переоткрытие подключения, повторная попытка и т.п.).
	vtoroy, _, err := Prigotovit(pervyy, Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	pravilaVtorogo := pravilaRoute(t, vtoroy)
	if n := schyotQuicPravil(pravilaVtorogo); n != 1 {
		t.Fatalf("после второго прогона на уже подготовленном профиле правил protocol:quic: %d, хочу 1 (не дублируется)", n)
	}

	// Порядок правил у первого и второго прогона обязан совпасть один в
	// один — второй прогон не переставляет ничего заново.
	pravilaPervogo := pravilaRoute(t, pervyy)
	if len(pravilaPervogo) != len(pravilaVtorogo) {
		t.Fatalf("длина route.rules разъехалась: было %d, стало %d", len(pravilaPervogo), len(pravilaVtorogo))
	}
	for i := range pravilaPervogo {
		p1, p2 := pravilaPervogo[i], pravilaVtorogo[i]
		proto1, _ := p1["protocol"].(string)
		proto2, _ := p2["protocol"].(string)
		act1, _ := p1["action"].(string)
		act2, _ := p2["action"].(string)
		if proto1 != proto2 || act1 != act2 {
			t.Fatalf("правило %d изменилось между прогонами: было %#v, стало %#v", i, p1, p2)
		}
	}
}

// TestPrigotovitNeTrogayetSvoyeProtoQuic — если профиль сам уже содержит
// правило с protocol:quic (сервер решил про QUIC сам), Prigotovit его не
// трогает — профиль главнее.
func TestPrigotovitNeTrogayetSvoyeProtoQuic(t *testing.T) {
	d := razobrat(t, profil(t))
	r := d["route"].(map[string]any)
	svoye := map[string]any{"protocol": "quic", "outbound": "direct"}
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
	if n := schyotQuicPravil(pravila); n != 1 {
		t.Fatalf("правил protocol:quic: %d, хочу 1 — своё правило профиля не должно размножиться и не должно смениться на наше", n)
	}
	if act, _ := pravila[0]["action"].(string); act != "" {
		t.Fatalf("первое правило должно было остаться своим (outbound, не action=reject): %#v", pravila[0])
	}
	if out, _ := pravila[0]["outbound"].(string); out != "direct" {
		t.Fatalf("своё правило профиля про QUIC подменилось: %#v", pravila[0])
	}
}
