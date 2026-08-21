package yadro

// Выбор узла. Профиль отдаёт не один выход, а группу-переключатель
// (`selector`): у хозяина это «Соединение» с вариантами «Нидерланды» и «Комната».
// Пока приложение молчало про группы, человек получал ровно тот вариант, что
// стоял на сервере, и сменить его из окна не мог никак.
//
// Переключение живёт в самом ядре: Clash API отдаёт группы (`GET /proxies`) и
// принимает выбор (`PUT /proxies/<группа>`). Мы только показываем это человеку.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Uzel — один вариант внутри группы.
type Uzel struct {
	Imya      string `json:"imya"`
	Zaderzhka int    `json:"zaderzhka,omitempty"` // мс по последнему замеру, 0 = не мерили
	Gruppa    bool   `json:"gruppa,omitempty"`    // сам является группой (вложенный selector/urltest)
}

// Gruppa — переключатель выходов.
type Gruppa struct {
	Imya    string `json:"imya"`
	Seychas string `json:"seychas"`       // что выбрано прямо сейчас
	Sam     bool   `json:"sam,omitempty"` // выбирает автоматика (urltest), руками не переключить
	Uzly    []Uzel `json:"uzly"`
}

// otvetProxies — ответ Clash API как он есть.
type uzelApi struct {
	Type    string   `json:"type"`
	Now     string   `json:"now"`
	All     []string `json:"all"`
	History []struct {
		Delay int `json:"delay"`
	} `json:"history"`
}

// Gruppy — какие переключатели есть у работающего ядра и что в них выбрано.
func (y *Yadro) Gruppy() ([]Gruppa, error) {
	otvet, err := y.zapros("/proxies")
	if err != nil {
		return nil, err
	}
	defer otvet.Body.Close()
	if otvet.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ядро ответило %d", otvet.StatusCode)
	}
	var v struct {
		Proxies map[string]uzelApi `json:"proxies"`
	}
	if err := json.NewDecoder(otvet.Body).Decode(&v); err != nil {
		return nil, err
	}
	var out []Gruppa
	for imya, u := range v.Proxies {
		sam := strings.EqualFold(u.Type, "urltest")
		if !strings.EqualFold(u.Type, "selector") && !sam {
			continue
		}
		if imya == "GLOBAL" { // служебная группа самого Clash API, человеку она не нужна
			continue
		}
		// Группа, которой правит автоматика и в которой один-единственный
		// вариант, человеку не говорит ничего: переключить нечего, выбор
		// предрешён. В окне она была лишней строкой (проверено на профиле хозяина:
		// «Нидерланды» → «Нидерланды · прямой»).
		if sam && len(u.All) < 2 {
			continue
		}
		g := Gruppa{Imya: imya, Seychas: u.Now, Sam: sam}
		for _, n := range u.All {
			d := v.Proxies[n]
			z := poslednyaya(d.History)
			// Вариант сам может быть группой: своей истории у него может не
			// быть, а человеку нужна задержка того выхода, который в нём
			// сейчас выбран, — иначе строка молчит без причины.
			if z == 0 && d.Now != "" {
				z = poslednyaya(v.Proxies[d.Now].History)
			}
			g.Uzly = append(g.Uzly, Uzel{
				Imya:      n,
				Zaderzhka: z,
				Gruppa:    strings.EqualFold(d.Type, "selector") || strings.EqualFold(d.Type, "urltest"),
			})
		}
		out = append(out, g)
	}
	// Порядок карты в Go случайный, а окно перерисовывается каждые две секунды:
	// без сортировки список прыгал бы под курсором.
	sort.Slice(out, func(i, j int) bool { return out[i].Imya < out[j].Imya })
	return out, nil
}

// Vybrat переключает группу на узел. Ядро откажет, если узел не входит в группу
// или если группой правит автоматика.
func (y *Yadro) Vybrat(gruppa, uzel string) error {
	if gruppa == "" || uzel == "" {
		return fmt.Errorf("не сказано, что и на что переключить")
	}
	telo, _ := json.Marshal(map[string]string{"name": uzel})
	req, err := http.NewRequest(http.MethodPut,
		"http://"+y.api()+"/proxies/"+url.PathEscape(gruppa), bytes.NewReader(telo))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if y.Sekret != "" {
		req.Header.Set("Authorization", "Bearer "+y.Sekret)
	}
	otvet, err := y.klient().Do(req)
	if err != nil {
		return err
	}
	defer otvet.Body.Close()
	if otvet.StatusCode >= 400 {
		return fmt.Errorf("ядро не приняло выбор «%s» (%d)", uzel, otvet.StatusCode)
	}
	return nil
}

// ProbaAdres — куда стучится замер задержки. Адрес лёгкий и без тела ответа.
const ProbaAdres = "http://cp.cloudflare.com/generate_204"

// Zamerit — сколько миллисекунд идёт запрос через этот узел. Замер делает само
// ядро: оно поднимает соединение именно через выбранный выход, а не через нас.
func (y *Yadro) Zamerit(ctx context.Context, uzel string) (int, error) {
	put := "/proxies/" + url.PathEscape(uzel) + "/delay?timeout=3000&url=" + url.QueryEscape(ProbaAdres)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+y.api()+put, nil)
	if err != nil {
		return 0, err
	}
	if y.Sekret != "" {
		req.Header.Set("Authorization", "Bearer "+y.Sekret)
	}
	klient := y.klient()
	if klient.Timeout < 6*time.Second { // замер сам ждёт до 3 с, короткий таймаут срубил бы его
		k := *klient
		k.Timeout = 6 * time.Second
		klient = &k
	}
	otvet, err := klient.Do(req)
	if err != nil {
		return 0, err
	}
	defer otvet.Body.Close()
	var v struct {
		Delay   int    `json:"delay"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(otvet.Body).Decode(&v)
	if otvet.StatusCode >= 400 || v.Delay == 0 {
		if v.Message == "" {
			v.Message = "узел не ответил"
		}
		return 0, fmt.Errorf("%s", v.Message)
	}
	return v.Delay, nil
}

// GruppyStatik читает группы прямо из конфига на диске, а не у живого ядра:
// нужна, пока ядро стоит и спросить Clash API некого (окно раньше в этом
// состоянии показывало пустой список — хозяин, снимок 21.08, «300px пустого
// места»). Задержки взять неоткуда: замер — это запрос через само ядро, а оно
// не поднято. Zaderzhka остаётся 0 (omitempty), окно печатает такой узел как
// «—», а не как «0 мс» — ноль тут был бы неправдой (быстрее всех остальных).
//
// vybor — выбор человека, сделанный ДО подключения (Sluzhba хранит его в
// Nastroyki.Uzly, ключ — имя группы). Пуст или не содержит группу — берём
// «default» из конфига, а нет и его — первый вариант по порядку.
func GruppyStatik(syroyKonfig []byte, vybor map[string]string) ([]Gruppa, error) {
	var d struct {
		Outbounds []struct {
			Type      string   `json:"type"`
			Tag       string   `json:"tag"`
			Outbounds []string `json:"outbounds"`
			Default   string   `json:"default"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(syroyKonfig, &d); err != nil {
		return nil, err
	}
	tipy := make(map[string]string, len(d.Outbounds))
	for _, o := range d.Outbounds {
		tipy[o.Tag] = o.Type
	}
	var out []Gruppa
	for _, o := range d.Outbounds {
		sam := strings.EqualFold(o.Type, "urltest")
		if !strings.EqualFold(o.Type, "selector") && !sam {
			continue
		}
		// Та же оговорка, что и в Gruppy(): группа-автоматика с единственным
		// вариантом нечего переключать, показывать её нечего.
		if sam && len(o.Outbounds) < 2 {
			continue
		}
		seychas := vybor[o.Tag]
		if seychas == "" {
			seychas = o.Default
		}
		if seychas == "" && len(o.Outbounds) > 0 {
			seychas = o.Outbounds[0]
		}
		g := Gruppa{Imya: o.Tag, Seychas: seychas, Sam: sam}
		for _, n := range o.Outbounds {
			g.Uzly = append(g.Uzly, Uzel{
				Imya:   n,
				Gruppa: strings.EqualFold(tipy[n], "selector") || strings.EqualFold(tipy[n], "urltest"),
			})
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Imya < out[j].Imya })
	return out, nil
}

func poslednyaya(h []struct {
	Delay int `json:"delay"`
}) int {
	if len(h) == 0 {
		return 0
	}
	return h[len(h)-1].Delay
}
