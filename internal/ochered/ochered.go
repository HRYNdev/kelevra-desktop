// Пакет ochered: переход по очереди узлов своей сети (решение 17.10.2026).
//
// Профиль своей сети кладёт в селектор «Соединение» группу (urltest) на каждый узел — по
// порядку, который задал мозг для человека: первый основной, дальше запасные. Между узлами
// переключает клиент, не urltest: иначе один аккаунт скакал бы между странами от каждой
// флуктуации задержки. Так же устроено на телефоне (Box: AutoMode.switchToReserve /
// maybeReturnToPrimary).
//
// Что делает Shag (зовут раз в ShagMezhduZamerami, пока ядро работает):
//   - текущий узел мерится самим ядром (Clash API delay — запрос идёт ИМЕННО через этот узел,
//     выбор при этом не переставляется, живые соединения не рвутся);
//   - ProvalovDoPerehoda провалов подряд — идём по очереди с начала и встаём на первый живой;
//     ни один не ответил — остаёмся, где были (дальше решает человек или следующий шаг);
//   - стоим на запасном — мерим и первый узел; жив непрерывно VozvratCherez — возвращаемся;
//   - выбор в группе сделал человек в окне (не первый узел) — не вмешиваемся.
//
// Профиль с одним узлом очереди не имеет: Shag ничего не делает, поведение прежнее.
package ochered

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

const (
	ShagMezhduZamerami = 30 * time.Second
	ProvalovDoPerehoda = 2
	VozvratCherez      = 4 * time.Minute
)

// Yadro — то, что очереди нужно от работающего ядра.
type Yadro interface {
	Vybrano(gruppa string) (string, error)
	Vybrat(gruppa, uzel string) error
	Zamerit(ctx context.Context, uzel string) (int, error)
}

// Ochered — состояние перехода между узлами одного профиля.
type Ochered struct {
	Gruppa string   // селектор, в котором лежат узлы («Соединение»)
	Uzly   []string // узлы по порядку: первый основной
	Ya     Yadro
	// Ruchnoy — выбор в группе сделал человек (окно): очередь не вмешивается.
	Ruchnoy func() bool
	Log     func(format string, args ...any)
	Seychas func() time.Time
	// Porog и Vozvrat — пороги очереди, которые может подстроить пульт
	// (uzel.porog_otkazov, uzel.vozvrat_na_pervyy_min); 0 — константы выше.
	// Функциями: пульт меняется на ходу, а очередь живёт, пока не сменится конфиг.
	Porog   func() int
	Vozvrat func() time.Duration

	provalov    int
	pervyyZhivS time.Time
}

func (o *Ochered) porog() int {
	if o.Porog != nil {
		if p := o.Porog(); p > 0 {
			return p
		}
	}
	return ProvalovDoPerehoda
}

func (o *Ochered) vozvrat() time.Duration {
	if o.Vozvrat != nil {
		if v := o.Vozvrat(); v > 0 {
			return v
		}
	}
	return VozvratCherez
}

func (o *Ochered) seychas() time.Time {
	if o.Seychas != nil {
		return o.Seychas()
	}
	return time.Now()
}

func (o *Ochered) log(f string, a ...any) {
	if o.Log != nil {
		o.Log(f, a...)
	}
}

// IzKonfiga — очередь из конфига ядра: селектор, в котором лежат группы-узлы (urltest), и эти
// группы по порядку. Узлов меньше двух — очереди нет (ok=false).
func IzKonfiga(syroy []byte) (gruppa string, uzly []string, ok bool) {
	var d struct {
		Outbounds []struct {
			Type      string   `json:"type"`
			Tag       string   `json:"tag"`
			Outbounds []string `json:"outbounds"`
		} `json:"outbounds"`
	}
	if json.Unmarshal(syroy, &d) != nil {
		return "", nil, false
	}
	tipy := make(map[string]string, len(d.Outbounds))
	for _, o := range d.Outbounds {
		tipy[o.Tag] = strings.ToLower(o.Type)
	}
	for _, o := range d.Outbounds {
		if !strings.EqualFold(o.Type, "selector") {
			continue
		}
		var gr []string
		for _, n := range o.Outbounds {
			if tipy[n] == "urltest" {
				gr = append(gr, n)
			}
		}
		if len(gr) >= 2 {
			return o.Tag, gr, true
		}
	}
	return "", nil, false
}

func (o *Ochered) indeks(uzel string) int {
	for i, u := range o.Uzly {
		if u == uzel {
			return i
		}
	}
	return -1
}

func (o *Ochered) zhiv(ctx context.Context, uzel string) bool {
	_, err := o.Ya.Zamerit(ctx, uzel)
	return err == nil
}

func (o *Ochered) vstat(i int, pochemu string) bool {
	if err := o.Ya.Vybrat(o.Gruppa, o.Uzly[i]); err != nil {
		// Ядро могло переключить, но не успеть ответить (стенд 17.10: PUT по таймауту, а выбор встал):
		// верим не ответу, а тому, что выбрано на деле.
		if tek, e := o.Ya.Vybrano(o.Gruppa); e != nil || tek != o.Uzly[i] {
			o.log("очередь узлов: встать на «%s» не вышло: %v", o.Uzly[i], err)
			return false
		}
		o.log("очередь узлов: ядро ответило с ошибкой (%v), но «%s» встал", err, o.Uzly[i])
	}
	o.log("очередь узлов: узел сменён на «%s» (%s)", o.Uzly[i], pochemu)
	o.provalov = 0
	o.pervyyZhivS = time.Time{}
	return true
}

// Shag — один замер и, если надо, одно переключение. Возвращает, переключились ли.
func (o *Ochered) Shag(ctx context.Context) bool {
	if len(o.Uzly) < 2 || o.Ya == nil {
		return false
	}
	if o.Ruchnoy != nil && o.Ruchnoy() {
		return false
	}
	tek, err := o.Ya.Vybrano(o.Gruppa)
	if err != nil {
		return false
	}
	i := o.indeks(tek)
	if i < 0 {
		// В «Соединении» стоит не узел (комната, ручной выход) — это не наше дело.
		return false
	}
	if o.zhiv(ctx, tek) {
		o.provalov = 0
		if i == 0 {
			o.pervyyZhivS = time.Time{}
			return false
		}
		// На запасном: жив ли первый, и как долго.
		if !o.zhiv(ctx, o.Uzly[0]) {
			o.pervyyZhivS = time.Time{}
			return false
		}
		if o.pervyyZhivS.IsZero() {
			o.pervyyZhivS = o.seychas()
			o.log("очередь узлов: первый узел «%s» снова отвечает — вернусь, если продержится", o.Uzly[0])
			return false
		}
		if o.seychas().Sub(o.pervyyZhivS) < o.vozvrat() {
			return false
		}
		return o.vstat(0, "первый узел жив "+o.seychas().Sub(o.pervyyZhivS).Round(time.Second).String())
	}
	o.provalov++
	if o.provalov < o.porog() {
		o.log("очередь узлов: узел «%s» не ответил (%d из %d)", tek, o.provalov, o.porog())
		return false
	}
	for j := range o.Uzly {
		if j == i {
			continue
		}
		if o.zhiv(ctx, o.Uzly[j]) {
			return o.vstat(j, "узел «"+tek+"» не отвечает")
		}
	}
	o.log("очередь узлов: ни один узел %v не отвечает", o.Uzly)
	return false
}
