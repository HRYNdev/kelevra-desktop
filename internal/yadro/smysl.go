package yadro

import (
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/HRYNdev/kelevra-desktop/internal/tunnel"
	"github.com/HRYNdev/kelevra-desktop/internal/zapasnoy"
)

// Нужен ли перезапуск ядра, если конфиг сменился: одно правило на все пути.
//
// Зачем. До 07.11.2026 правил было два, и они противоречили друг другу:
// ежечасный новый профиль ядро не перезапускал вовсе (оно часами работало на
// прежнем), а при обновлении приложения та же разница вдруг гасила туннель
// (приём живого ядра, «конфиг устарел»). Сверка шла по отпечатку почти
// побайтно: ядро, поднятое на tun126 со сдвинутым адресом (tun125 был занят),
// считалось «устаревшим» при каждом обновлении. Каждый такой перезапуск гасит
// TUN на 1–30 с.
//
// Теперь живой конфиг ядра и новый сравниваются ПО СМЫСЛУ (SravnitKonfigi), и
// ответ один из трёх:
//
//	NeNuzhen — разница ядру не важна или оно подхватывает её само:
//	  имя адаптера туннеля и сдвиг его адресов вслед за именем (подбор
//	  свободного имени), вход службы (internal/zapasnoy, порт выбирается на
//	  каждой сборке), наборы правил route.rule_set, которые ядро подхватит
//	  само (тот же файл — fswatch, стенд 25.10; адрес сервера сменился на
//	  файл кеша; вид и частота обновления), журнал ядра (log), порядок
//	  выходов, входов, серверов DNS и эндпоинтов, порядок членов группы
//	  (у селектора без default — кроме первого);
//	Otlozhit — ядру нужен перезапуск, но связь работает и на прежнем: новые
//	  или убранные выходы (кроме того, на котором сидим), другие параметры
//	  узлов, вход tun (адреса сверх сдвига, маршруты, стек, MTU, auto_route),
//	  DNS, правила маршрутизации и final, набор правил, который ядро читает
//	  не из того файла (встроенный комплект, а свежий лёг в кеш), прочие
//	  разделы. Перезапуск ждёт
//	  «естественного» момента (sluzhba, otlozhennyy_perezapusk.go);
//	Srazu — на прежнем конфиге связь сломана или скоро сломается: узел (или
//	  группа), на котором ядро сидит сейчас, из профиля пропал; сменился вход
//	  домой (эндпоинты — «Дом» читается ядром только на старте); сменился
//	  служебный порт ядра (experimental.clash_api) — служба перестала бы
//	  его слышать.
//
// Новое ядро core-* — тоже Otlozhit, но это не разница конфигов, её решает
// служба (smena_yadra.go).
type Nuzhda int

const (
	NeNuzhen Nuzhda = iota
	Otlozhit
	Srazu
)

func (n Nuzhda) String() string {
	switch n {
	case Otlozhit:
		return "отложить"
	case Srazu:
		return "сразу"
	}
	return "не нужен"
}

// Reshenie — ответ сверки: что делать и почему (строки для журнала).
type Reshenie struct {
	Nuzhda   Nuzhda
	Prichiny []string
}

// Slovami — причины одной строкой для журнала.
func (r Reshenie) Slovami() string {
	if len(r.Prichiny) == 0 {
		return "разницы по смыслу нет"
	}
	return strings.Join(r.Prichiny, "; ")
}

func (r *Reshenie) dobavit(n Nuzhda, prichina string) {
	if n > r.Nuzhda {
		r.Nuzhda = n
	}
	r.Prichiny = append(r.Prichiny, prichina)
}

// SravnitKonfigi — нужен ли перезапуск ядра, работающего по rabota, чтобы
// оно работало по novyy.
//
// vybrano — что сейчас выбрано в группах живого ядра (группа → узел, Clash
// API now); nil — не знаем, и пропажу выбранного узла сверка не ловит.
//
// Не разобрался какой-то из конфигов — Otlozhit: судить о смысле нечем, а
// сломать работающую связь из-за неизвестности хуже, чем отложить.
func SravnitKonfigi(rabota, novyy []byte, vybrano map[string]string) Reshenie {
	var r Reshenie
	var a, b map[string]any
	if err := json.Unmarshal(rabota, &a); err != nil || a == nil {
		r.dobavit(Otlozhit, "конфиг живого ядра не разобрать")
		return r
	}
	if err := json.Unmarshal(novyy, &b); err != nil || b == nil {
		r.dobavit(Otlozhit, "новый конфиг не разобрать")
		return r
	}
	naboryA, naboryB := istochnikiNaborov(a), istochnikiNaborov(b)
	normalizovat(a)
	normalizovat(b)
	vyrovnyatTun(a, b)

	// Срочное.
	if !reflect.DeepEqual(clashApi(a), clashApi(b)) {
		r.dobavit(Srazu, "сменился служебный порт ядра (clash_api)")
	}
	if izm := raznicaPoTegam(a["endpoints"], b["endpoints"]); izm != "" {
		r.dobavit(Srazu, "сменился вход домой (эндпоинты: "+izm+")")
	}
	proveritVybrannye(b, vybrano, &r)

	// Отложенное.
	if izm := raznicaPoTegam(a["outbounds"], b["outbounds"]); izm != "" {
		r.dobavit(Otlozhit, "выходы: "+izm)
	}
	if izm := raznicaPoTegam(a["inbounds"], b["inbounds"]); izm != "" {
		r.dobavit(Otlozhit, "входы: "+izm)
	}
	if !reflect.DeepEqual(a["dns"], b["dns"]) {
		r.dobavit(Otlozhit, "DNS")
	}
	if !reflect.DeepEqual(a["route"], b["route"]) {
		r.dobavit(Otlozhit, "правила маршрутизации")
	}
	if izm := raznicaNaborov(naboryA, naboryB); izm != "" {
		r.dobavit(Otlozhit, "наборы правил не из кеша: "+izm)
	}
	if !reflect.DeepEqual(bezClashApi(a["experimental"]), bezClashApi(b["experimental"])) {
		r.dobavit(Otlozhit, "experimental")
	}
	ostalnye := map[string]bool{}
	for k := range a {
		ostalnye[k] = true
	}
	for k := range b {
		ostalnye[k] = true
	}
	var klyuchi []string
	for k := range ostalnye {
		switch k {
		case "endpoints", "outbounds", "inbounds", "dns", "route", "experimental":
			continue
		}
		if !reflect.DeepEqual(a[k], b[k]) {
			klyuchi = append(klyuchi, k)
		}
	}
	sort.Strings(klyuchi)
	if len(klyuchi) > 0 {
		r.dobavit(Otlozhit, "разделы: "+strings.Join(klyuchi, ", "))
	}
	return r
}

// SravnitPoOtpechatku — запасная сверка, когда конфига живого ядра нет
// (ядро поднято версией до 07.11.2026, снимка konfig_v_rabote.json нет):
// только отпечаток из записки. Совпал — не нужен; иначе — отложить: что
// именно сменилось, не узнать, а на прежнем конфиге ядро работало.
func SravnitPoOtpechatku(otpechatokRaboty string, novyy []byte) Reshenie {
	if otpechatokRaboty == "" || otpechatokRaboty == OtpechatokKonfigaBayt(novyy) {
		return Reshenie{}
	}
	return Reshenie{Nuzhda: Otlozhit, Prichiny: []string{"конфиг живого ядра неизвестен, отпечаток другой"}}
}

// istochnikNabora — откуда ядро читает набор правил.
type istochnikNabora struct {
	lokalnyy bool
	gde      string // путь (local) или адрес (remote)
}

// istochnikiNaborov — тег → откуда ядро читает набор (route.rule_set).
func istochnikiNaborov(d map[string]any) map[string]istochnikNabora {
	r, _ := d["route"].(map[string]any)
	itog := map[string]istochnikNabora{}
	for _, x := range spisok(r["rule_set"]) {
		o, ok := x.(map[string]any)
		if !ok {
			continue
		}
		teg, _ := o["tag"].(string)
		if o["type"] == "local" {
			put, _ := o["path"].(string)
			// Путь Windows сравнивается без учёта регистра и вида разделителя
			// (на любой платформе: проверки гоняются и на линуксе).
			put = path.Clean(strings.ReplaceAll(strings.ToLower(put), "\\", "/"))
			itog[teg] = istochnikNabora{lokalnyy: true, gde: put}
			continue
		}
		adres, _ := o["url"].(string)
		itog[teg] = istochnikNabora{gde: adres}
	}
	return itog
}

// raznicaNaborov — наборы, которые живое ядро на ходу НЕ подхватит.
//
// Файл набора ядро перечитывает само (fswatch), поэтому новое содержимое по
// тому же пути перезапуска не требует, как и переход с адреса сервера на
// файл кеша (ядро и так качает набор само). Требует — только смена файла
// (ядро читает встроенный комплект, а свежий набор лёг в кеш) и смена адреса
// сервера у набора, который ядро качает само. Новые и убранные теги ловят
// правила маршрутизации.
func raznicaNaborov(a, b map[string]istochnikNabora) string {
	var izm []string
	for teg, bylo := range a {
		stalo, est := b[teg]
		if !est || bylo == stalo {
			continue
		}
		if bylo.lokalnyy || stalo.lokalnyy == bylo.lokalnyy {
			izm = append(izm, teg)
		}
	}
	sort.Strings(izm)
	return strings.Join(izm, ", ")
}

// normalizovat убирает из конфига то, что перезапуска не требует никогда.
func normalizovat(d map[string]any) {
	delete(d, "log")
	zapasnoy.UbratVhod(d)
	if r, ok := d["route"].(map[string]any); ok {
		// Наборы правил ядро перечитывает само; путь из кеша вместо адреса
		// сервера (кеш правил, заготовка svoi-*) — тоже их дело.
		delete(r, "rule_set")
	}
	for _, v := range spisok(d["outbounds"]) {
		if vh, ok := v.(map[string]any); ok {
			uporyadochitGruppu(vh)
		}
	}
}

// uporyadochitGruppu — порядок членов группы не важен: urltest выбирает по
// замеру, у селектора без default первым членом определяется выбор по
// умолчанию — его сохраняем отдельным полем.
func uporyadochitGruppu(vh map[string]any) {
	chleny, ok := vh["outbounds"].([]any)
	if !ok {
		return
	}
	stroki := make([]string, 0, len(chleny))
	for _, c := range chleny {
		s, _ := c.(string)
		stroki = append(stroki, s)
	}
	if _, est := vh["default"]; !est && len(stroki) > 0 && vh["type"] == "selector" {
		vh["default"] = stroki[0]
	}
	sort.Strings(stroki)
	novye := make([]any, len(stroki))
	for i, s := range stroki {
		novye[i] = s
	}
	vh["outbounds"] = novye
}

// vyrovnyatTun — имя адаптера туннеля и сдвиг его адресов вслед за именем
// (tunnel.Sdvig) — деталь того, как связь поднята, а не того, как она
// работает: у старшего из двух имён адреса возвращаются на сдвиг назад
// (двигаем младшие вперёд тем же шагом), имя стирается у обоих.
func vyrovnyatTun(a, b map[string]any) {
	ta, tb := tunVhod(a), tunVhod(b)
	if ta != nil && tb != nil {
		ia, _ := ta["interface_name"].(string)
		ib, _ := tb["interface_name"].(string)
		if s := tunnel.Sdvig(ib, ia); s > 0 {
			sdvinutTun(tb, s)
		} else if s := tunnel.Sdvig(ia, ib); s > 0 {
			sdvinutTun(ta, s)
		}
	}
	for _, t := range []map[string]any{ta, tb} {
		if t != nil {
			delete(t, "interface_name")
		}
	}
}

func tunVhod(d map[string]any) map[string]any {
	for _, v := range spisok(d["inbounds"]) {
		if vh, ok := v.(map[string]any); ok && vh["type"] == "tun" {
			return vh
		}
	}
	return nil
}

func sdvinutTun(vh map[string]any, shag int) {
	for _, pole := range []string{"address", "inet4_address", "inet6_address"} {
		switch z := vh[pole].(type) {
		case string:
			if s := tunnel.SdvinutAdresa([]string{z}, shag); len(s) == 1 {
				vh[pole] = s[0]
			}
		case []any:
			var stroki []string
			for _, x := range z {
				s, _ := x.(string)
				stroki = append(stroki, s)
			}
			stali := tunnel.SdvinutAdresa(stroki, shag)
			novye := make([]any, len(stali))
			for i, s := range stali {
				novye[i] = s
			}
			vh[pole] = novye
		}
	}
}

func spisok(v any) []any {
	l, _ := v.([]any)
	return l
}

// poTegam — раздел-список как карта тег → объект (порядок не важен).
func poTegam(v any) map[string]any {
	m := map[string]any{}
	for i, x := range spisok(v) {
		teg := fmt.Sprintf("#%d", i)
		if o, ok := x.(map[string]any); ok {
			if t, _ := o["tag"].(string); t != "" {
				teg = t
			}
		}
		m[teg] = x
	}
	return m
}

// raznicaPoTegam — словами, что добавлено, убрано и изменено; пусто — ничего.
func raznicaPoTegam(a, b any) string {
	ma, mb := poTegam(a), poTegam(b)
	var plus, minus, tilda []string
	for t, x := range mb {
		y, est := ma[t]
		switch {
		case !est:
			plus = append(plus, "+"+t)
		case !reflect.DeepEqual(x, y):
			tilda = append(tilda, "~"+t)
		}
	}
	for t := range ma {
		if _, est := mb[t]; !est {
			minus = append(minus, "-"+t)
		}
	}
	sort.Strings(plus)
	sort.Strings(minus)
	sort.Strings(tilda)
	return strings.Join(append(append(plus, minus...), tilda...), " ")
}

func clashApi(d map[string]any) any {
	e, _ := d["experimental"].(map[string]any)
	return e["clash_api"]
}

func bezClashApi(v any) any {
	e, ok := v.(map[string]any)
	if !ok {
		return v
	}
	kopiya := make(map[string]any, len(e))
	for k, x := range e {
		if k != "clash_api" {
			kopiya[k] = x
		}
	}
	return kopiya
}

// proveritVybrannye — выбранное в группах живого ядра есть ли в новом
// конфиге: сама группа, узел среди её членов и узел как выход. Нет — Srazu:
// ядро сидит на том, чего у сервера больше нет.
func proveritVybrannye(b map[string]any, vybrano map[string]string, r *Reshenie) {
	if len(vybrano) == 0 {
		return
	}
	vyhody := poTegam(b["outbounds"])
	for t, x := range poTegam(b["endpoints"]) {
		vyhody[t] = x
	}
	gruppy := make([]string, 0, len(vybrano))
	for g := range vybrano {
		gruppy = append(gruppy, g)
	}
	sort.Strings(gruppy)
	for _, g := range gruppy {
		uzel := vybrano[g]
		if uzel == "" {
			continue
		}
		gr, ok := vyhody[g].(map[string]any)
		if !ok {
			r.dobavit(Srazu, fmt.Sprintf("группы «%s», в которой выбран «%s», в профиле больше нет", g, uzel))
			continue
		}
		est := false
		for _, c := range spisok(gr["outbounds"]) {
			if c == uzel {
				est = true
				break
			}
		}
		if _, vyhodEst := vyhody[uzel]; !est || !vyhodEst {
			r.dobavit(Srazu, fmt.Sprintf("узел «%s», на котором сидим (группа «%s»), из профиля пропал", uzel, g))
		}
	}
}

// GruppyKonfiga — теги групп (selector, urltest) в конфиге: у них есть «что
// выбрано сейчас» (Clash API now).
func GruppyKonfiga(syroy []byte) []string {
	var d struct {
		Outbounds []struct {
			Type string `json:"type"`
			Tag  string `json:"tag"`
		} `json:"outbounds"`
	}
	if json.Unmarshal(syroy, &d) != nil {
		return nil
	}
	var itog []string
	for _, o := range d.Outbounds {
		if (o.Type == "selector" || o.Type == "urltest") && o.Tag != "" {
			itog = append(itog, o.Tag)
		}
	}
	return itog
}
