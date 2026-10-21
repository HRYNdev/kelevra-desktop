package pult

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Реестр ключей пульта: что клиент понимает, какого типа и в каких границах.
//
// Зачем реестр, а не россыпь проверок по месту. Пульт принимает только ключи
// из списка в коде (договор KelevraServer docs/PULT.md): незнакомый ключ, не
// тот тип или выход за границы — ключ не принимается и уходит в X-Pult
// словом oshibki. Список один на всё приложение — по нему же проверяется
// пришедшее и пишется таблица docs/PULT-KLYUCHI-desktop.md, так что таблица и
// код разъехаться не могут.

// Tip — тип значения ключа.
type Tip int

const (
	// Celoe — целое в границах [Min, Max].
	Celoe Tip = iota
	// Flag — true/false.
	Flag
	// Vremya — строка ISO-8601 с зоной (RFC 3339).
	Vremya
	// Vybor — одно слово из Varianty.
	Vybor
	// Spisok — список строк, от Min до Max штук, каждая непустая и не длиннее 253 знаков.
	Spisok
	// Stroka — одна непустая строка не длиннее 253 знаков.
	Stroka
)

func (t Tip) String() string {
	switch t {
	case Celoe:
		return "int"
	case Flag:
		return "bool"
	case Vremya:
		return "ISO-время"
	case Vybor:
		return "выбор"
	case Spisok:
		return "список строк"
	case Stroka:
		return "строка"
	}
	return "?"
}

// Klyuch — один ключ реестра.
type Klyuch struct {
	Imya     string
	Tip      Tip
	Min, Max int64
	Varianty []string
	// Umolchanie — значение по умолчанию словами для таблицы (оно же —
	// нынешняя константа в коде; само значение живёт в коде, а не тут).
	Umolchanie string
	// Gde — что подстраивает (место в коде), для таблицы.
	Gde string
}

// Reestr — все ключи клиента.
type Reestr []Klyuch

func (r Reestr) nayti(imya string) (Klyuch, bool) {
	for _, k := range r {
		if k.Imya == imya {
			return k, true
		}
	}
	return Klyuch{}, false
}

// Proverit разбирает пришедшие настройки: принятые значения (в каноническом
// JSON) и ключи, которые не приняты (незнаком, тип, границы), по алфавиту.
func (r Reestr) Proverit(prishlo map[string]json.RawMessage) (map[string]json.RawMessage, []string) {
	prinyato := map[string]json.RawMessage{}
	var oshibki []string
	for imya, syroe := range prishlo {
		k, est := r.nayti(imya)
		if !est {
			oshibki = append(oshibki, imya)
			continue
		}
		kanon, err := k.proverit(syroe)
		if err != nil {
			oshibki = append(oshibki, imya)
			continue
		}
		prinyato[imya] = kanon
	}
	sort.Strings(oshibki)
	return prinyato, oshibki
}

// proverit — значение одного ключа: канонический JSON или отказ.
func (k Klyuch) proverit(syroe json.RawMessage) (json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(syroe))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	switch k.Tip {
	case Celoe:
		n, ok := v.(json.Number)
		if !ok {
			return nil, fmt.Errorf("не число")
		}
		f, err := n.Float64()
		if err != nil || f != math.Trunc(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("не целое")
		}
		c := int64(f)
		if c < k.Min || c > k.Max {
			return nil, fmt.Errorf("вне границ")
		}
		return json.Marshal(c)
	case Flag:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("не bool")
		}
		return json.Marshal(b)
	case Vremya:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("не строка")
		}
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
		if err != nil {
			return nil, err
		}
		return json.Marshal(t.UTC().Format(time.RFC3339))
	case Vybor:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("не строка")
		}
		for _, variant := range k.Varianty {
			if s == variant {
				return json.Marshal(s)
			}
		}
		return nil, fmt.Errorf("не из списка")
	case Stroka:
		s, ok := v.(string)
		s = strings.TrimSpace(s)
		if !ok || s == "" || len(s) > 253 {
			return nil, fmt.Errorf("не строка")
		}
		return json.Marshal(s)
	case Spisok:
		spisok, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("не список")
		}
		if int64(len(spisok)) < k.Min || int64(len(spisok)) > k.Max {
			return nil, fmt.Errorf("длина вне границ")
		}
		var itog []string
		for _, e := range spisok {
			s, ok := e.(string)
			s = strings.TrimSpace(s)
			if !ok || s == "" || len(s) > 253 {
				return nil, fmt.Errorf("плохой элемент")
			}
			itog = append(itog, s)
		}
		if itog == nil {
			itog = []string{}
		}
		return json.Marshal(itog)
	}
	return nil, fmt.Errorf("тип не известен")
}

// Tablica — таблица ключей в markdown (docs/PULT-KLYUCHI-desktop.md).
func (r Reestr) Tablica(zagolovok string) string {
	var b strings.Builder
	b.WriteString(zagolovok)
	b.WriteString("\n| Ключ | Тип | Границы | По умолчанию (= код) | Что подстраивает |\n|---|---|---|---|---|\n")
	for _, k := range r {
		granicy := "—"
		switch k.Tip {
		case Celoe:
			granicy = fmt.Sprintf("%d–%d", k.Min, k.Max)
		case Vybor:
			granicy = strings.Join(k.Varianty, " \\| ")
		case Spisok:
			granicy = fmt.Sprintf("%d–%d шт.", k.Min, k.Max)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", k.Imya, k.Tip, granicy, k.Umolchanie, k.Gde)
	}
	return b.String()
}
