// Пакет podpiska: разговор с сервером Kelevra по коду доступа.
//
// Тот же протокол, что у мобильного клиента (kelevra-box):
//
//	GET https://<хост>/k/<код>       -> готовый config.json для sing-box
//	GET https://<хост>/k/<код>/info  -> сведения о подписке (срок, трафик)
//
// Мобильный ходит на /k/, десктопный форк ходил на /s/ — поэтому пробуем оба
// пути по очереди, пока сервер не ответит.
package podpiska

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Host — сервер подписок Kelevra.
const Host = "subkv.chickenkiller.com"

// Puti — известные префиксы кода доступа, в порядке предпочтения.
var Puti = []string{"k", "s"}

// Versiya подставляется при сборке (-ldflags "-X ...Versiya=…").
var Versiya = "0.1.0-rabota"

// Klient — с кем и как разговариваем. Пустой Klient работоспособен.
type Klient struct {
	Host     string
	Shema    string // "https" в бою; "http" оставлен только для проверок на своём стенде
	HTTP     *http.Client
	DeviceID string
}

func (k *Klient) shema() string {
	if k.Shema != "" {
		return k.Shema
	}
	return "https"
}

func (k *Klient) host() string {
	if k.Host != "" {
		return k.Host
	}
	return Host
}

func (k *Klient) http() *http.Client {
	if k.HTTP != nil {
		return k.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (k *Klient) zapros(ctx context.Context, url string) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("User-Agent", "Kelevra-Desktop/"+Versiya+" (windows)")
	if k.DeviceID != "" {
		r.Header.Set("x-hwid", k.DeviceID)
		r.Header.Set("x-device-os", "windows")
	}
	return r, nil
}

// OshibkaKoda — сервер ответил, но код доступа он не знает.
type OshibkaKoda struct{ Kod int }

func (e *OshibkaKoda) Error() string {
	return fmt.Sprintf("сервер не знает такой код доступа (ответ %d)", e.Kod)
}

// Konfig качает конфиг ядра по коду доступа.
// Возвращает сырой JSON — ровно то, что нужно скормить sing-box.
func (k *Klient) Konfig(ctx context.Context, kod string) ([]byte, error) {
	kod = strings.TrimSpace(kod)
	if kod == "" {
		return nil, fmt.Errorf("пустой код доступа")
	}
	var posledn error
	for _, p := range Puti {
		telo, status, err := k.vzyat(ctx, fmt.Sprintf("%s://%s/%s/%s", k.shema(), k.host(), p, kod))
		if err != nil {
			posledn = err
			continue
		}
		if status == http.StatusNotFound || status == http.StatusForbidden {
			posledn = &OshibkaKoda{Kod: status}
			continue // может быть просто не тот префикс пути
		}
		if status != http.StatusOK {
			posledn = fmt.Errorf("сервер ответил %d", status)
			continue
		}
		if err := ProverKonfig(telo); err != nil {
			posledn = err
			continue
		}
		return telo, nil
	}
	return nil, posledn
}

// Svedeniya — что сервер рассказывает о подписке.
type Svedeniya struct {
	Imya      string `json:"name"`
	Aktivna   bool   `json:"active"`
	Do        int64  `json:"expires"`
	LimitBayt int64  `json:"limit_bytes"`
	SyedenoB  int64  `json:"used_bytes"`
	Otvet     string `json:"reply"`
}

// Svedeniya спрашивает состояние подписки. Ошибка здесь не мешает подключению.
func (k *Klient) Svedeniya(ctx context.Context, kod string) (*Svedeniya, error) {
	kod = strings.TrimSpace(kod)
	if kod == "" {
		return nil, fmt.Errorf("пустой код доступа")
	}
	var posledn error
	for _, p := range Puti {
		url := fmt.Sprintf("%s://%s/%s/%s/info", k.shema(), k.host(), p, kod)
		if k.DeviceID != "" {
			url += "?device=" + k.DeviceID
		}
		telo, status, err := k.vzyat(ctx, url)
		if err != nil {
			posledn = err
			continue
		}
		if status != http.StatusOK {
			posledn = fmt.Errorf("сервер ответил %d", status)
			continue
		}
		s := &Svedeniya{}
		if err := json.Unmarshal(telo, s); err != nil {
			posledn = fmt.Errorf("сведения о подписке нечитаемы: %w", err)
			continue
		}
		return s, nil
	}
	return nil, posledn
}

func (k *Klient) vzyat(ctx context.Context, url string) ([]byte, int, error) {
	req, err := k.zapros(ctx, url)
	if err != nil {
		return nil, 0, err
	}
	otvet, err := k.http().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer otvet.Body.Close()
	// Потолок в 8 МБ: конфиг заведомо меньше, а битый сервер не должен съесть память.
	telo, err := io.ReadAll(io.LimitReader(otvet.Body, 8<<20))
	if err != nil {
		return nil, otvet.StatusCode, err
	}
	return telo, otvet.StatusCode, nil
}

// ProverKonfig — дешёвая проверка, что нам вернули конфиг ядра, а не страницу с ошибкой.
// Настоящую проверку делает само ядро при запуске.
func ProverKonfig(telo []byte) error {
	var v map[string]any
	if err := json.Unmarshal(telo, &v); err != nil {
		return fmt.Errorf("ответ сервера не похож на конфиг: %w", err)
	}
	if _, есть := v["outbounds"]; !есть {
		return fmt.Errorf("в ответе сервера нет ни одного узла (outbounds)")
	}
	return nil
}
