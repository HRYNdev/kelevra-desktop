package podpiska

// Оплата подписки (KelevraServer docs/OPLATA.md): тот же хост и та же база
// профиля, что у /info:
//
//	GET  <база>/oplata   -> {"platit": false} либо {"platit": true, "cena", "ssylka", "nomer", "srok", "zhdet"}
//	POST <база>/oplatil  -> то же (zhdet: true); чаще раза в 10 минут — 429
//
// где база — https://<дверь>/k/<код> или /u/<ключ>. Неизвестный код и
// неплатящий на POST — 404. Образец поведения — Android-клиент (Oplata.kt,
// OplataSostoyanie.kt).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Oplata — что дверь говорит про оплату.
type Oplata struct {
	// Platit — показывать ли оплату вовсе; false — остальных полей нет.
	Platit bool `json:"platit"`
	// Cena — рублей в месяц.
	Cena int `json:"cena"`
	// Ssylka — ссылка на перевод (только https); "" — не задана.
	Ssylka string `json:"ssylka"`
	// Nomer — номер для перевода по СБП; "" — не задан.
	Nomer string `json:"nomer"`
	// SrokUnix — доступ до (секунды Unix); 0 — бессрочно или не прислан.
	SrokUnix int64 `json:"srok_unix"`
	// Zhdet — «Я оплатил» нажато, срок ещё не продлён.
	Zhdet bool `json:"zhdet"`
}

// ErrNeOplata — ответ двери не про оплату (старая выдача на этом адресе
// отдаёт профиль или страницу): оплату не показываем.
var ErrNeOplata = errors.New("ответ двери не про оплату")

// ErrChasto — 429: заявка уже ушла недавно (или подбор кода).
var ErrChasto = errors.New("сервер: слишком часто")

// RazobratOplatu — ответ /oplata и /oplatil. Ключа platit нет — ErrNeOplata.
func RazobratOplatu(telo []byte) (*Oplata, error) {
	var syroe map[string]json.RawMessage
	if err := json.Unmarshal(telo, &syroe); err != nil {
		return nil, ErrNeOplata
	}
	if _, est := syroe["platit"]; !est {
		return nil, ErrNeOplata
	}
	var d struct {
		Platit bool     `json:"platit"`
		Cena   *float64 `json:"cena"`
		Ssylka *string  `json:"ssylka"`
		Nomer  *string  `json:"nomer"`
		Srok   *string  `json:"srok"`
		Zhdet  bool     `json:"zhdet"`
	}
	// Поле неожиданного вида не роняет ответ целиком: разбираем по одному.
	razobrat := func(k string, v any) { _ = json.Unmarshal(syroe[k], v) }
	razobrat("platit", &d.Platit)
	if !d.Platit {
		return &Oplata{}, nil
	}
	razobrat("cena", &d.Cena)
	razobrat("ssylka", &d.Ssylka)
	razobrat("nomer", &d.Nomer)
	razobrat("srok", &d.Srok)
	razobrat("zhdet", &d.Zhdet)
	o := &Oplata{Platit: true, Zhdet: d.Zhdet}
	if d.Cena != nil && *d.Cena > 0 && *d.Cena < 1e9 {
		o.Cena = int(*d.Cena)
	}
	if d.Ssylka != nil {
		o.Ssylka = ssylkaOplaty(*d.Ssylka)
	}
	if d.Nomer != nil {
		o.Nomer = chistyTekst(*d.Nomer, 40)
	}
	if d.Srok != nil {
		o.SrokUnix = srokOplatyUnix(*d.Srok)
	}
	return o, nil
}

// ssylkaOplaty — ссылку открывает системный браузер: только https с хостом,
// без пробелов и управляющих знаков; всё прочее — как нет ссылки.
func ssylkaOplaty(s string) string {
	s = strings.TrimSpace(s)
	if !SsylkaOplatyGodna(s) {
		return ""
	}
	return s
}

// SsylkaOplatyGodna — можно ли отдать ссылку системе на открытие.
func SsylkaOplatyGodna(s string) bool {
	if len(s) > 2048 || !strings.HasPrefix(strings.ToLower(s), "https://") {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == 0x7f || r == '"' {
			return false
		}
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}

func chistyTekst(s string, potolok int) string {
	s = strings.TrimSpace(s)
	if s == "null" {
		return ""
	}
	r := []rune(s)
	if len(r) > potolok {
		return ""
	}
	for _, c := range r {
		if c < ' ' {
			return ""
		}
	}
	return s
}

// srokOplatyUnix — ISO с зоной («2026-12-12T07:00:00Z»); голая дата —
// полночь UTC; пусто или мусор — 0.
func srokOplatyUnix(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix()
	}
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t.Unix() // без зоны — UTC, как пишет мозг
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Unix()
	}
	return 0
}

// SrokSkoro — сколько до конца срока считается «скоро»: трое суток.
const SrokSkoro = 3 * 24 * time.Hour

// SostoyanieSroka — «obychnyj», «skoro» (≤ SrokSkoro) или «konchilsya».
func SostoyanieSroka(srokUnix int64, seychas time.Time) string {
	switch {
	case srokUnix <= 0:
		return "obychnyj"
	case srokUnix <= seychas.Unix():
		return "konchilsya"
	case time.Unix(srokUnix, 0).Sub(seychas) <= SrokSkoro:
		return "skoro"
	}
	return "obychnyj"
}

// ShagOprosaOplaty — как часто спрашивать /oplata, пока окно на экране: час;
// заявка ждёт подтверждения — 5 минут.
func ShagOprosaOplaty(zhdet bool) time.Duration {
	if zhdet {
		return 5 * time.Minute
	}
	return time.Hour
}

// BazaOplaty — база для /oplata из сохранённого адреса подписки (/k/<код>,
// /u/<ключ>; старый префикс /s/ сервер оплаты не знает — тот же код по /k/).
// Адреса нет — по коду на хосте клиента. "" — спрашивать нечего.
func (k *Klient) BazaOplaty(adres, kod string) string {
	if adres != "" {
		u, err := url.Parse(strings.TrimSuffix(strings.TrimSpace(adres), "/"))
		if err != nil || u.Host == "" {
			return ""
		}
		chasti := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(chasti) != 2 || !kodDopustim.MatchString(chasti[1]) {
			return ""
		}
		switch chasti[0] {
		case "k", "u":
		case "s":
			chasti[0] = "k"
		default:
			return ""
		}
		return fmt.Sprintf("%s://%s/%s/%s", u.Scheme, u.Host, chasti[0], chasti[1])
	}
	kod = ChistyyKod(kod)
	if kod == "" || !kodDopustim.MatchString(kod) {
		return ""
	}
	return fmt.Sprintf("%s://%s/k/%s", k.shema(), k.host(), kod)
}

// Oplata — GET <база>/oplata. 404 — *OshibkaKoda, 429 — ErrChasto, не ответ
// оплаты — ErrNeOplata; сеть — обычная ошибка.
func (k *Klient) Oplata(ctx context.Context, baza string) (*Oplata, error) {
	return k.oplataZapros(ctx, http.MethodGet, strings.TrimSuffix(baza, "/")+"/oplata")
}

// Oplatil — POST <база>/oplatil («Я оплатил»). Ответы — как у Oplata.
func (k *Klient) Oplatil(ctx context.Context, baza string) (*Oplata, error) {
	return k.oplataZapros(ctx, http.MethodPost, strings.TrimSuffix(baza, "/")+"/oplatil")
}

func (k *Klient) oplataZapros(ctx context.Context, metod, adres string) (*Oplata, error) {
	req, err := k.zaprosMetodom(ctx, metod, adres)
	if err != nil {
		return nil, SkrytKodVOshibke(err)
	}
	if metod == http.MethodPost {
		req.Body = http.NoBody
		req.ContentLength = 0
		// Повтор безопасен: ждущая заявка второй не создаётся (ответ 200), и
		// запасной путь (internal/zapasnoy) может повторить POST через туннель.
		req.Header.Set("Idempotency-Key", "oplatil")
	}
	otvet, err := k.http().Do(req)
	if err != nil {
		return nil, SkrytKodVOshibke(err)
	}
	defer otvet.Body.Close()
	telo, err := io.ReadAll(io.LimitReader(otvet.Body, 64<<10))
	if err != nil {
		return nil, SkrytKodVOshibke(err)
	}
	switch {
	case otvet.StatusCode == http.StatusNotFound:
		return nil, &OshibkaKoda{Kod: otvet.StatusCode}
	case otvet.StatusCode == http.StatusTooManyRequests:
		return nil, ErrChasto
	case otvet.StatusCode < 200 || otvet.StatusCode > 299:
		return nil, fmt.Errorf("сервер ответил %d", otvet.StatusCode)
	}
	return RazobratOplatu(telo)
}
