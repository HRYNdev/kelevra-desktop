package konfig

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/zapasnoy"
)

// Вход службы (Vybor.VhodSluzhby): запасной путь запросов самой службы через
// туннель (internal/zapasnoy).

func vhodSluzhby(t *testing.T, d map[string]any) map[string]any {
	t.Helper()
	for _, v := range d["inbounds"].([]any) {
		vh := v.(map[string]any)
		if vh["tag"] == zapasnoy.TegVhoda {
			return vh
		}
	}
	return nil
}

func TestVhodSluzhbyVpisanVTunnel(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{VhodSluzhby: &VhodSluzhby{Port: 20125, Parol: "parol-ustroystva"}})
	if err != nil {
		t.Fatal(err)
	}
	if k.VhodSluzhby != "127.0.0.1:20125" {
		t.Fatalf("Kartina.VhodSluzhby = %q", k.VhodSluzhby)
	}
	if got := zapasnoy.AdresVhoda(gotovyy); got != "127.0.0.1:20125" {
		t.Fatalf("адрес входа из готового конфига %q", got)
	}
	d := razobrat(t, gotovyy)
	vh := vhodSluzhby(t, d)
	if vh == nil {
		t.Fatal("входа службы нет")
	}
	if vh["type"] != "socks" || vh["listen"] != "127.0.0.1" {
		t.Fatalf("вход службы должен быть socks только на 127.0.0.1: %v", vh)
	}
	if _, est := vh["set_system_proxy"]; est {
		t.Fatal("системный прокси не ставим никогда")
	}
	polz, _ := vh["users"].([]any)
	if len(polz) != 1 || polz[0].(map[string]any)["password"] != "parol-ustroystva" || polz[0].(map[string]any)["username"] != zapasnoy.Polzovatel {
		t.Fatalf("вход службы только с паролем устройства: %v", vh["users"])
	}
	// Правило входа — первым, и ведёт туда же, куда профиль ведёт заблокированное.
	pravila := d["route"].(map[string]any)["rules"].([]any)
	pervoe := pravila[0].(map[string]any)
	var kudaBlok string
	for _, p := range pravila {
		pr := p.(map[string]any)
		if _, est := pr["rule_set"]; est && pr["action"] == nil && pr["outbound"] != "direct" && pr["outbound"] != nil {
			kudaBlok = pr["outbound"].(string)
			break
		}
	}
	if kudaBlok == "" {
		t.Fatal("в профиле не нашёл правило заблокированного")
	}
	if fmt.Sprint(pervoe["inbound"]) != "["+zapasnoy.TegVhoda+"]" || pervoe["outbound"] != kudaBlok {
		t.Fatalf("первое правило %v, ждал вход службы → %q", pervoe, kudaBlok)
	}
	// Правила про QUIC и локальную сеть — только для входов человека.
	for _, p := range pravila[1:] {
		pr := p.(map[string]any)
		for _, v := range spisokStrok(pr["inbound"]) {
			if v == zapasnoy.TegVhoda {
				t.Fatalf("вход службы попал в чужое правило: %v", pr)
			}
		}
	}
	// Порт прокси-режима старых версий не занимаем.
	if vh["listen_port"].(float64) == 2412 {
		t.Fatal("2412 — порт прокси-режима старых версий")
	}
}

func TestVhodSluzhbyBezSetevyhPravilVedyotVFinal(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{BezSetevyhPravil: true, VhodSluzhby: &VhodSluzhby{Port: 20125, Parol: "p"}})
	if err != nil {
		t.Fatal(err)
	}
	d := razobrat(t, gotovyy)
	r := d["route"].(map[string]any)
	pervoe := r["rules"].([]any)[0].(map[string]any)
	if pervoe["outbound"] != r["final"] {
		t.Fatalf("в упрощённом режиме вход службы ведём в final (туннель): %v, final %v", pervoe, r["final"])
	}
	if vyhodPryamoy(d, pervoe["outbound"].(string)) {
		t.Fatal("вход службы ведёт мимо туннеля")
	}
}

func TestVhodSluzhbyNeVpisyvaetsya(t *testing.T) {
	for imya, v := range map[string]*VhodSluzhby{
		"без входа":       nil,
		"без пароля":      {Port: 20125},
		"без порта":       {Parol: "p"},
		"порт Clash API":  {Port: 9090, Parol: "p"},
		"порт вне границ": {Port: 70000, Parol: "p"},
		"отрицательный":   {Port: -1, Parol: "p"},
	} {
		t.Run(imya, func(t *testing.T) {
			gotovyy, k, err := Prigotovit(profil(t), Vybor{VhodSluzhby: v})
			if err != nil {
				t.Fatal(err)
			}
			if k.VhodSluzhby != "" || zapasnoy.AdresVhoda(gotovyy) != "" {
				t.Fatalf("вход службы вписан, а не должен: %q", k.VhodSluzhby)
			}
			d := razobrat(t, gotovyy)
			for _, p := range d["route"].(map[string]any)["rules"].([]any) {
				if fmt.Sprint(p.(map[string]any)["inbound"]) == "["+zapasnoy.TegVhoda+"]" {
					t.Fatal("правило входа без самого входа — ядро такой конфиг не примет")
				}
			}
		})
	}
}

// TestVhodSluzhbyZhivoeYadro — настоящее ядро (`sing-box run`, без tun, всё на
// 127.0.0.1): запрос через вход службы с паролем устройства доходит, и
// доходит ИМЕННО по правилу входа — route.final в этом профиле мёртв.
// Контроль порчей: без правила входа тот же запрос умирает в final; с чужим
// паролем ядро не пускает.
func TestVhodSluzhbyZhivoeYadro(t *testing.T) {
	bin := putYadra(t)
	if bin == "" {
		t.Skipf("настоящего ядра рядом нет — щуп пропущен. Искал: %s", mestaPoiskaYadra())
	}
	cel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "dver-otvetila")
	}))
	defer cel.Close()

	dom := t.TempDir()
	putSpiska := spisokZablokirovannogo(t, dom)
	syroy := profilDlyaZhivogo(0, putSpiska)
	d := razobrat(t, syroy)
	// final — мёртвый выход: всё, что не попало в правила, не доходит.
	d["outbounds"] = append(d["outbounds"].([]any), map[string]any{
		"type": "socks", "tag": "mertvyy", "server": "127.0.0.1", "server_port": 1,
	})
	d["route"].(map[string]any)["final"] = "mertvyy"
	syroy, _ = json.Marshal(d)

	port := svobodnyPort(t)
	gotovyy, k, err := Prigotovit(syroy, Vybor{VhodSluzhby: &VhodSluzhby{Port: port, Parol: "parol-ustroystva"}})
	if err != nil {
		t.Fatal(err)
	}
	if k.VhodSluzhby == "" {
		t.Fatal("вход службы не вписан")
	}

	zapros := func(t *testing.T, konfig []byte, parol string) (string, error) {
		t.Helper()
		dd := razobrat(t, konfig)
		// tun щупу не поднять — оставляем один вход службы.
		dd["inbounds"] = []any{vhodSluzhby(t, dd)}
		b, _ := json.Marshal(dd)
		put := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(put, b, 0o600); err != nil {
			t.Fatal(err)
		}
		cmd, log := zapustitYadro(t, bin, put, port)
		defer ostanovitYadro(cmd)
		tr := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "socks5",
			User: url.UserPassword(zapasnoy.Polzovatel, parol), Host: fmt.Sprintf("127.0.0.1:%d", port)})}
		defer tr.CloseIdleConnections()
		otvet, err := (&http.Client{Transport: tr, Timeout: 5 * time.Second}).Get(cel.URL + "/k/KOD/info")
		if err != nil {
			return "", fmt.Errorf("%w (лог ядра: %s)", err, log.String())
		}
		defer otvet.Body.Close()
		telo, _ := io.ReadAll(otvet.Body)
		return string(telo), nil
	}

	t.Run("через вход службы доходит", func(t *testing.T) {
		telo, err := zapros(t, gotovyy, "parol-ustroystva")
		if err != nil {
			t.Fatalf("запрос через вход службы не дошёл: %v", err)
		}
		if telo != "dver-otvetila" {
			t.Fatalf("ответ %q", telo)
		}
	})
	t.Run("чужой пароль не пускает", func(t *testing.T) {
		if _, err := zapros(t, gotovyy, "chuzhoy"); err == nil {
			t.Fatal("вход службы пустил без пароля устройства")
		}
	})
	t.Run("контроль: без правила входа не доходит", func(t *testing.T) {
		dd := razobrat(t, gotovyy)
		r := dd["route"].(map[string]any)
		r["rules"] = r["rules"].([]any)[1:] // правило входа — первое
		b, _ := json.Marshal(dd)
		if _, err := zapros(t, b, "parol-ustroystva"); err == nil {
			t.Fatal("без правила входа запрос всё равно дошёл — щуп ничего не доказывает (final не мёртв?)")
		}
	})
}
