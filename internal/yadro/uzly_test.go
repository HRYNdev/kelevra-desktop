package yadro

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// otvetProxies повторяет форму ответа настоящего sing-box: регистр типов там
// «Selector»/«URLTest», а не как в конфиге.
const otvetProxies = `{"proxies":{
 "GLOBAL":{"type":"Selector","now":"Соединение","all":["Соединение","direct"]},
 "Соединение":{"type":"Selector","now":"Комната","all":["Нидерланды","Комната"]},
 "Нидерланды":{"type":"URLTest","now":"Нидерланды · прямой","all":["Нидерланды · прямой"]},
 "Нидерланды · прямой":{"type":"Direct","history":[{"delay":41},{"delay":37}]},
 "Комната":{"type":"Direct","history":[]},
 "direct":{"type":"Direct"}}}`

func stend(t *testing.T, h http.HandlerFunc) *Yadro {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return &Yadro{Api: strings.TrimPrefix(s.URL, "http://")}
}

func TestGruppyBeretSelectoryINeGLOBAL(t *testing.T) {
	y := stend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies" {
			t.Errorf("спросили не тот путь: %s", r.URL.Path)
		}
		io.WriteString(w, otvetProxies)
	})
	g, err := y.Gruppy()
	if err != nil {
		t.Fatal(err)
	}
	// «Нидерланды» — автоматика с единственным вариантом: переключать нечего,
	// в окне это была бы лишняя строка. GLOBAL — служебная группа Clash API.
	if len(g) != 1 {
		t.Fatalf("групп %d, ждал одну («Соединение»): %+v", len(g), g)
	}
	s := g[0]
	if s.Imya != "Соединение" || s.Seychas != "Комната" || s.Sam {
		t.Fatalf("группа выбора разобрана неверно: %+v", s)
	}
	if len(s.Uzly) != 2 || s.Uzly[0].Imya != "Нидерланды" || !s.Uzly[0].Gruppa {
		t.Fatalf("узлы группы разобраны неверно: %+v", s.Uzly)
	}
	if s.Uzly[1].Zaderzhka != 0 {
		t.Errorf("у узла без истории задержки быть не должно: %+v", s.Uzly[1])
	}
	// У «Нидерландов» своей истории нет: показываем задержку того выхода,
	// который автоматика в них выбрала, и берём последний замер, а не первый.
	if s.Uzly[0].Zaderzhka != 37 {
		t.Errorf("задержка вложенной группы потеряна: %+v", s.Uzly[0])
	}
}

func TestVybratShlyotPutSImenem(t *testing.T) {
	var put, telo string
	y := stend(t, func(w http.ResponseWriter, r *http.Request) {
		put = r.Method + " " + r.URL.Path
		b, _ := io.ReadAll(r.Body)
		telo = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
	if err := y.Vybrat("Соединение", "Нидерланды"); err != nil {
		t.Fatal(err)
	}
	if put != "PUT /proxies/Соединение" {
		t.Errorf("не тот запрос: %s", put)
	}
	var v map[string]string
	if json.Unmarshal([]byte(telo), &v); v["name"] != "Нидерланды" {
		t.Errorf("не то тело: %s", telo)
	}
}

func TestVybratOtkazYadraNeMolchit(t *testing.T) {
	y := stend(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadRequest) })
	if err := y.Vybrat("Соединение", "Марс"); err == nil {
		t.Fatal("отказ ядра проглочен как успех")
	}
}

func TestZamerit(t *testing.T) {
	y := stend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies/Комната/delay" {
			t.Errorf("не тот путь: %s", r.URL.Path)
		}
		if r.URL.Query().Get("url") != ProbaAdres {
			t.Errorf("адрес пробы не дошёл: %s", r.URL.RawQuery)
		}
		io.WriteString(w, `{"delay":42}`)
	})
	ms, err := y.Zamerit(context.Background(), "Комната")
	if err != nil || ms != 42 {
		t.Fatalf("замер: %d, %v", ms, err)
	}
}

// Узел, который не отвечает, ядро отдаёт кодом 503 и словами. Их и надо
// показать человеку — «ошибка» ему ничего не говорит.
func TestZameritMertvyyUzelOtdayotPrichinu(t *testing.T) {
	y := stend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"message":"An error occurred in the delay test"}`)
	})
	if _, err := y.Zamerit(context.Background(), "Комната"); err == nil ||
		!strings.Contains(err.Error(), "delay test") {
		t.Fatalf("причина отказа потеряна: %v", err)
	}
}

// Форма конфига ядра (outbounds сингбокса), а не ответа Clash API — та же,
// что кладёт konfig.Prigotovit на диск.
const konfigOutbounds = `{"outbounds":[
 {"type":"selector","tag":"Соединение","outbounds":["Нидерланды","Комната"],"default":"Нидерланды"},
 {"type":"urltest","tag":"Нидерланды","outbounds":["Нидерланды · прямой"]},
 {"type":"direct","tag":"direct"},
 {"type":"vless","tag":"Нидерланды · прямой"},
 {"type":"socks","tag":"Комната"}]}`

// GruppyStatik — единственный способ показать список узлов, пока ядро стоит:
// спросить Clash API некого. Раньше в этом состоянии окно отдавало пустой
// список (снимок 21.08 — 300px пустоты вместо списка).
func TestGruppyStatikBeretIzKonfigaBezZadershki(t *testing.T) {
	g, err := GruppyStatik([]byte(konfigOutbounds), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 1 {
		t.Fatalf("групп %d, ждал одну («Соединение»): %+v", len(g), g)
	}
	s := g[0]
	if s.Imya != "Соединение" || s.Sam {
		t.Fatalf("группа выбора разобрана неверно: %+v", s)
	}
	if s.Seychas != "Нидерланды" {
		t.Fatalf("без сохранённого выбора обязан взять default из конфига: %+v", s)
	}
	if len(s.Uzly) != 2 || s.Uzly[0].Imya != "Нидерланды" || !s.Uzly[0].Gruppa {
		t.Fatalf("узлы группы разобраны неверно: %+v", s.Uzly)
	}
	for _, u := range s.Uzly {
		if u.Zaderzhka != 0 {
			// Задержка — это запрос ЧЕРЕЗ ядро; пока оно стоит, спрашивать
			// некого. Ноль тут — omitempty, окно печатает его как «—», а не
			// как «0 мс» (0 читался бы как «быстрее всех» — неправда).
			t.Errorf("у статического узла не может быть замеренной задержки: %+v", u)
		}
	}
}

// Сохранённый человеком выбор (Nastroyki.Uzly) обязан победить default из
// конфига: иначе выбор «до Подключить» был бы декорацией.
func TestGruppyStatikSohranennyyVyborPobezhdaetDefault(t *testing.T) {
	g, err := GruppyStatik([]byte(konfigOutbounds), map[string]string{"Соединение": "Комната"})
	if err != nil {
		t.Fatal(err)
	}
	if g[0].Seychas != "Комната" {
		t.Fatalf("сохранённый выбор не применился: %+v", g[0])
	}
}
