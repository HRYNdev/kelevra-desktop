package podpiska

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OshibkaKlyuchaDoma — дверь ответила на открытый ключ дома не 2xx.
// 404 — код, слот или устройство двери не знакомы (мозг отвечает одинаково на
// всё чужое) или входа домой у двери нет (старый мозг); 403 — доступ закрыт.
type OshibkaKlyuchaDoma struct{ Status int }

func (e *OshibkaKlyuchaDoma) Error() string {
	return fmt.Sprintf("дверь не приняла ключ дома (ответ %d)", e.Status)
}

// OtvetKlyuchaDoma — ответ мозга на открытый ключ (KelevraServer
// docs/VHOD-DOMOY-API.md): {"sostoyanie": "zhdet"|"podtverzhden"|"otozvan", "dom": bool}.
type OtvetKlyuchaDoma struct {
	Sostoyanie string `json:"sostoyanie"`
	// Dom — включён ли устройству «Дом»; nil — дверь не сказала.
	Dom *bool `json:"dom"`
}

// OtpravitKlyuchDoma — POST <схема>://<dver>/k/<kod>/dom/klyuch {"slot","pub"}
// (договор «вход домой»: тем же путём и с теми же заголовками устройства, что
// POST /k/<код>/ustroystvo; X-Device-Id мозг требует). klyuchUstroystva
// непустой — уходит заголовком X-Device-Key, как у пропуска в панель: мозг
// сверяет его со слотом. Повторять можно сколько угодно: тот же ключ —
// прежнее состояние.
//
// В теле только открытый ключ: закрытый с этой машины не уходит никогда.
func (k *Klient) OtpravitKlyuchDoma(ctx context.Context, dver, kod, slot, pub, klyuchUstroystva string) (OtvetKlyuchaDoma, error) {
	var o OtvetKlyuchaDoma
	kod = strings.TrimSpace(kod)
	if !kodDopustim.MatchString(kod) {
		return o, fmt.Errorf("нет кода доступа для ключа дома")
	}
	telo, _ := json.Marshal(map[string]string{"slot": slot, "pub": pub})
	req, err := k.zaprosMetodom(ctx, http.MethodPost, fmt.Sprintf("%s://%s/k/%s/dom/klyuch", k.shema(), dver, kod))
	if err != nil {
		return o, SkrytKodVOshibke(err)
	}
	req.Body = io.NopCloser(bytes.NewReader(telo))
	req.ContentLength = int64(len(telo))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(telo)), nil }
	req.Header.Set("Content-Type", "application/json")
	if klyuchUstroystva != "" {
		req.Header.Set("X-Device-Key", klyuchUstroystva)
	}
	otvet, err := k.http().Do(req)
	if err != nil {
		return o, SkrytKodVOshibke(err)
	}
	defer otvet.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(otvet.Body, 64<<10))
	if otvet.StatusCode < 200 || otvet.StatusCode > 299 {
		return o, &OshibkaKlyuchaDoma{Status: otvet.StatusCode}
	}
	_ = json.Unmarshal(b, &o)
	o.Sostoyanie = strings.TrimSpace(o.Sostoyanie)
	return o, nil
}
