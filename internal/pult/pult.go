// Пакет pult: настройки и команды с сервера (договор KelevraServer
// docs/PULT.md, протокол v1).
//
// Мозг своей сети меняет поведение клиента без выпуска приложения: настройки
// (желаемое состояние) и команды (разовые, со сроком и квитанцией). Едет это
// в сводке /info полем pult, подписанным ключом мозга. Клиент принимает только
// ключи и команды из своего списка (Reestr, Umeyu) и кода по сети не исполняет.
//
// Слои значений: код (всегда рабочий) ← последний принятый пульт с диска ←
// свежий пульт, прошедший подпись, адресата и версию. Без сервера клиент живёт
// на диске, без диска — на коде. Ключа нет — значение из кода.
package pult

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpis"
)

// Umeet — capver Desktop: 1 = все ключи и команды v1 столбца Desktop.
const Umeet = 1

// metka — с чего начинается подписываемая строка (договор, «Подпись»).
const metka = "kelevra-pult-v1\n"

// KlyuchiHex — открытые ключи пульта (основной мозга и запасной хоста),
// сырые 32 байта ed25519 в hex. Отдельные от ключа выпусков (internal/podpis).
var KlyuchiHex = []string{
	"89f9504b7dfa4bad83b8f781e8fb81225e5883739ac502804669429b1a6ab552",
	"56b5e97341b4aa0828a5660faee970aecbb25291ec1e9e57505bd462f5230e90",
}

// ProverochnyyKlyuchHex — третий, проверочный открытый ключ стенда (hex, 32
// байта). В обычной сборке пуст: принимаются только KlyuchiHex. Вшивается
// только сборкой стенда, по образцу podpis.KlyuchHex:
//
//	go build -ldflags "-X github.com/HRYNdev/kelevra-desktop/internal/pult.ProverochnyyKlyuchHex=<hex>"
//
// Закрытая половина живёт только у стенда и в выпуск не попадает.
var ProverochnyyKlyuchHex = ""

// SrokIspytaniya — сколько новые настройки «на испытании».
var SrokIspytaniya = 10 * time.Minute

// PadeniyDoOtkata — столько перезапусков процесса за испытание дают откат.
const PadeniyDoOtkata = 3

// PamyatKomand — сколько помнить выполненные id и держать квитанции.
const PamyatKomand = 7 * 24 * time.Hour

// PredelKvitanciy — сколько квитанций максимум в одном заголовке.
const PredelKvitanciy = 20

// Blok — поле pult в /info.
type Blok struct {
	Dannye string `json:"dannye"`
	Podpis string `json:"podpis"`
}

// Komanda — одна команда пульта.
type Komanda struct {
	ID    string          `json:"id"`
	Chto  string          `json:"chto"`
	Do    string          `json:"do"`
	Param json.RawMessage `json:"param,omitempty"`
}

// ParamStroka — строковый параметр команды ("" — нет такого).
func (k Komanda) ParamStroka(imya string) string {
	var m map[string]any
	if json.Unmarshal(k.Param, &m) != nil {
		return ""
	}
	s, _ := m[imya].(string)
	return strings.TrimSpace(s)
}

// Obnovlenie — блок выкатки версии (договор, «Главная цель», п. 6; «Провод»).
// Desktop сравнивает VersiyaImya («0.6.85») со своей версией; VersiyaKod —
// versionCode Android, у сборок Windows 0 или нет вовсе.
type Obnovlenie struct {
	VersiyaImya string `json:"versiya_imya"`
	VersiyaKod  int64  `json:"versiya_kod,omitempty"`
	URL         string `json:"url"`
	Sha256      string `json:"sha256"`
	Razmer      int64  `json:"razmer,omitempty"`
	Zametka     string `json:"zametka,omitempty"`
}

// Nomer — номер версии сборки из блока ("" — не назван).
func (o *Obnovlenie) Nomer() string {
	if o == nil {
		return ""
	}
	return strings.TrimSpace(o.VersiyaImya)
}

// Dannye — содержимое dannye после base64.
type Dannye struct {
	V          int                        `json:"v"`
	Versiya    int64                      `json:"versiya"`
	Vydano     string                     `json:"vydano"`
	Komu       string                     `json:"komu"`
	Nastroyki  map[string]json.RawMessage `json:"nastroyki"`
	Komandy    []Komanda                  `json:"komandy"`
	Obnovlenie *Obnovlenie                `json:"obnovlenie,omitempty"`
}

// Otkaz — пульт целиком не принят; Prichina — слово для X-Pult (pult:<причина>).
type Otkaz struct{ Prichina string }

func (o *Otkaz) Error() string { return "пульт отвергнут: " + o.Prichina }

// Itog — что принёс принятый пульт.
type Itog struct {
	Versiya int64
	// IzmenilisNastroyki — действующие настройки сменились (новые на испытании).
	IzmenilisNastroyki bool
	// Komandy — команды, которые надо выполнить сейчас (каждая — один раз).
	Komandy []Komanda
	// NovoeObnovlenie — блок obnovlenie появился или сменился.
	NovoeObnovlenie bool
}

type ispytanie struct {
	Versiya    int64     `json:"versiya"`
	Nachalo    time.Time `json:"nachalo"`
	Padeniy    int       `json:"padeniy"`
	BylaSet    bool      `json:"byla_set"`
	Nuzhen     bool      `json:"nuzhen_tunnel"`
	Podnyalsya bool      `json:"podnyalsya"`
}

type vypolnennaya struct {
	Itog  string    `json:"itog"`
	Kogda time.Time `json:"kogda"`
	// Kvit — сервер ещё присылает эту команду, квитанцию держим в заголовке.
	Kvit bool `json:"kvit"`
	// idet — команда выполняется в этом процессе: квитанции ещё нет, в X-Pult
	// её не шлём (на диске стоит oshibka-prervano — на случай смерти посреди).
	idet bool
}

// zapis — что лежит на диске (pult.json).
type zapis struct {
	Versiya          int64                      `json:"versiya"`
	Nastroyki        map[string]json.RawMessage `json:"nastroyki,omitempty"`
	NastroykiVersiya int64                      `json:"nastroyki_versiya,omitempty"`
	Proshlye         map[string]json.RawMessage `json:"proshlye,omitempty"`
	Ispytanie        *ispytanie                 `json:"ispytanie,omitempty"`
	Plohie           []int64                    `json:"plohie,omitempty"`
	OshibkiKlyuchey  []string                   `json:"oshibki_klyuchey,omitempty"`
	Otkat            bool                       `json:"otkat,omitempty"`
	Obnovlenie       *Obnovlenie                `json:"obnovlenie,omitempty"`
	Vypolneno        map[string]*vypolnennaya   `json:"vypolneno,omitempty"`
	// ChistyyVyhod — прошлый процесс ушёл штатно (Vyhod): его перезапуск
	// падением не считается.
	ChistyyVyhod bool `json:"chistyy_vyhod,omitempty"`
}

// Pult — принятый пульт этой машины.
type Pult struct {
	// Put — файл на диске; "" — не хранить (проверки).
	Put string
	// Reestr — какие ключи понимаем; Umeyu — какие команды.
	Reestr Reestr
	Umeyu  map[string]bool
	// Klyuchi — чем проверять подпись; nil — вшитые KlyuchiHex.
	Klyuchi []ed25519.PublicKey
	// Seychas — часы; nil — time.Now.
	Seychas func() time.Time

	mu sync.Mutex
	z  zapis
	// otkaz — почему последний пришедший пульт не принят (в память, не на диск).
	otkaz string
	// oshibkaObnovleniya — чем кончилась установка из блока obnovlenie.
	oshibkaObnovleniya string
}

// Zagruzit поднимает пульт с диска (слой 2). Нет файла или битый — слой 1 (код).
func Zagruzit(put string, r Reestr, umeyu map[string]bool) *Pult {
	p := &Pult{Put: put, Reestr: r, Umeyu: umeyu}
	if put == "" {
		return p
	}
	b, err := os.ReadFile(put)
	if err != nil {
		return p
	}
	var z zapis
	if err := json.Unmarshal(b, &z); err != nil {
		log.Printf("пульт: файл %s нечитаем (%v) — работаю на значениях из кода", filepath.Base(put), err)
		return p
	}
	// Реестр мог сузиться в новой версии: незнакомое с диска не применяем.
	z.Nastroyki, _ = r.Proverit(z.Nastroyki)
	z.Proshlye, _ = r.Proverit(z.Proshlye)
	p.z = z
	return p
}

func (p *Pult) seychas() time.Time {
	if p.Seychas != nil {
		return p.Seychas()
	}
	return time.Now()
}

func (p *Pult) klyuchi() []ed25519.PublicKey {
	if p.Klyuchi != nil {
		return p.Klyuchi
	}
	var k []ed25519.PublicKey
	for _, h := range VshityeKlyuchi() {
		if kl, err := podpis.Klyuch(h); err == nil {
			k = append(k, kl)
		}
	}
	return k
}

// VshityeKlyuchi — чем проверяется подпись: боевые и, только в сборке стенда,
// проверочный (ProverochnyyKlyuchHex).
func VshityeKlyuchi() []string {
	k := append([]string(nil), KlyuchiHex...)
	if h := strings.TrimSpace(ProverochnyyKlyuchHex); h != "" {
		k = append(k, h)
	}
	return k
}

// sohranit — под p.mu.
func (p *Pult) sohranit() {
	if p.Put == "" {
		return
	}
	b, err := json.MarshalIndent(p.z, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p.Put), 0o755); err != nil {
		log.Printf("пульт: не сохранён: %v", err)
		return
	}
	if err := hranenie.ZapisatZakryto(p.Put, b); err != nil {
		log.Printf("пульт: не сохранён: %v", err)
	}
}

// Razobrat проверяет блок и отдаёт его данные; ошибка — *Otkaz с причиной.
// komu — X-Device-Id, ровно как его шлёт клиент.
func (p *Pult) Razobrat(syroe json.RawMessage, komu string) (*Dannye, error) {
	var blok Blok
	if err := json.Unmarshal(syroe, &blok); err != nil {
		return nil, &Otkaz{"podpis"}
	}
	dannye, err1 := base64.StdEncoding.DecodeString(strings.TrimSpace(blok.Dannye))
	sig, err2 := base64.StdEncoding.DecodeString(strings.TrimSpace(blok.Podpis))
	if err1 != nil || err2 != nil || len(sig) != ed25519.SignatureSize || len(dannye) == 0 {
		return nil, &Otkaz{"podpis"}
	}
	soobshchenie := append([]byte(metka), dannye...)
	verna := false
	for _, k := range p.klyuchi() {
		if ed25519.Verify(k, soobshchenie, sig) {
			verna = true
			break
		}
	}
	if !verna {
		return nil, &Otkaz{"podpis"}
	}
	var d Dannye
	if err := json.Unmarshal(dannye, &d); err != nil {
		return nil, &Otkaz{"v"}
	}
	if d.V != 1 {
		return nil, &Otkaz{"v"}
	}
	if d.Komu == "" || d.Komu != komu {
		return nil, &Otkaz{"komu"}
	}
	p.mu.Lock()
	sohranennaya := p.z.Versiya
	p.mu.Unlock()
	if d.Versiya < 0 || d.Versiya < sohranennaya {
		return nil, &Otkaz{"versiya"}
	}
	return &d, nil
}

// Prinyat — свежий пульт из /info. Пустой блок — ничего не меняется (слой 2).
// Отказ — причина в журнал и в X-Pult, всё прежнее остаётся.
func (p *Pult) Prinyat(syroe json.RawMessage, komu string) (Itog, error) {
	if p == nil || len(syroe) == 0 || string(syroe) == "null" {
		return Itog{}, nil
	}
	d, err := p.Razobrat(syroe, komu)
	if err != nil {
		var o *Otkaz
		if errors.As(err, &o) {
			p.mu.Lock()
			p.otkaz = o.Prichina
			p.mu.Unlock()
		}
		return Itog{}, err
	}
	seychas := p.seychas()
	p.mu.Lock()
	defer p.mu.Unlock()
	if d.Versiya < p.z.Versiya { // успел принять более свежий параллельно
		p.otkaz = "versiya"
		return Itog{}, &Otkaz{"versiya"}
	}
	p.otkaz = ""
	itog := Itog{Versiya: d.Versiya}
	if d.Versiya > p.z.Versiya {
		p.z.Otkat = false // откат относился к прошлой версии
		p.oshibkaObnovleniya = ""
	}
	zn, oshibki := p.Reestr.Proverit(d.Nastroyki)
	p.z.OshibkiKlyuchey = oshibki
	plohaya := false
	for _, v := range p.z.Plohie {
		if v == d.Versiya {
			plohaya = true
		}
	}
	if !plohaya && !ravny(zn, p.z.Nastroyki) {
		if p.z.Ispytanie == nil {
			p.z.Proshlye = p.z.Nastroyki // действующие прошли испытание — это слой 2
		}
		p.z.Nastroyki = zn
		p.z.NastroykiVersiya = d.Versiya
		if ravny(zn, p.z.Proshlye) {
			p.z.Ispytanie = nil // вернулись к проверенному — испытывать нечего
		} else {
			p.z.Ispytanie = &ispytanie{Versiya: d.Versiya, Nachalo: seychas}
		}
		itog.IzmenilisNastroyki = true
	}
	if d.Versiya > p.z.Versiya {
		p.z.Versiya = d.Versiya
	}

	// Команды. Каждая — один раз за 7 суток; срок и умение проверяются тут же.
	if p.z.Vypolneno == nil {
		p.z.Vypolneno = map[string]*vypolnennaya{}
	}
	prislany := map[string]bool{}
	for _, k := range d.Komandy {
		id := chistoeID(k.ID)
		if id == "" {
			continue
		}
		prislany[id] = true
		if v, est := p.z.Vypolneno[id]; est {
			v.Kvit = true
			continue
		}
		v := &vypolnennaya{Kogda: seychas, Kvit: true}
		p.z.Vypolneno[id] = v
		srok, err := time.Parse(time.RFC3339, strings.TrimSpace(k.Do))
		switch {
		case err != nil:
			v.Itog = "oshibka-srok"
		case !seychas.Before(srok):
			v.Itog = "istekla"
		case !p.Umeyu[k.Chto]:
			v.Itog = "ne_umeyu"
		default:
			// Отметка до выполнения: если процесс умрёт посреди, повтора не будет.
			v.Itog = "oshibka-prervano"
			v.idet = true
			k.ID = id
			itog.Komandy = append(itog.Komandy, k)
		}
		if v.Itog != "oshibka-prervano" {
			log.Printf("пульт: команда %s (%s) — %s", id, chistoe(k.Chto, 40), v.Itog)
		}
	}
	for id, v := range p.z.Vypolneno {
		if !prislany[id] {
			v.Kvit = false // сервер перестал присылать — квитанция дошла
		}
		if seychas.Sub(v.Kogda) > PamyatKomand {
			delete(p.z.Vypolneno, id)
		}
	}

	if !reflect.DeepEqual(d.Obnovlenie, p.z.Obnovlenie) {
		itog.NovoeObnovlenie = d.Obnovlenie != nil
		p.z.Obnovlenie = d.Obnovlenie
	}
	p.sohranit()
	return itog, nil
}

// Zavershit — итог выполненной команды (ok, oshibka-<кратко>).
func (p *Pult) Zavershit(id, itog string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if v, est := p.z.Vypolneno[id]; est {
		v.Itog = chistoe(itog, 48)
		v.idet = false
		p.sohranit()
	}
}

// --- испытание и откат ---

// Nablyudenie — что служба видела за отрезок испытания.
type Nablyudenie struct {
	// Set — у машины была сеть (шлюз на физическом адаптере).
	Set bool
	// Nuzhen — туннель был нужен: его поднимали или он стоял.
	Nuzhen bool
	// STrafikom — туннель работал, и через него шёл трафик.
	STrafikom bool
}

// IspytanieIdet — новые настройки ещё на испытании.
func (p *Pult) IspytanieIdet() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.z.Ispytanie != nil
}

// Otmetit копит наблюдения испытания.
func (p *Pult) Otmetit(n Nablyudenie) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	i := p.z.Ispytanie
	if i == nil {
		return
	}
	bylo := *i
	i.BylaSet = i.BylaSet || n.Set
	i.Nuzhen = i.Nuzhen || n.Nuzhen
	i.Podnyalsya = i.Podnyalsya || n.STrafikom
	if *i != bylo {
		p.sohranit()
	}
}

// ProveritIspytanie — срок испытания вышел? true — настройки откатились.
func (p *Pult) ProveritIspytanie() bool {
	if p == nil {
		return false
	}
	seychas := p.seychas()
	p.mu.Lock()
	defer p.mu.Unlock()
	i := p.z.Ispytanie
	if i == nil || seychas.Sub(i.Nachalo) < SrokIspytaniya {
		return false
	}
	if i.BylaSet && i.Nuzhen && !i.Podnyalsya {
		p.otkatit("за испытание туннель ни разу не поднялся с трафиком при наличии сети")
		return true
	}
	log.Printf("пульт: настройки версии %d прошли испытание", i.Versiya)
	p.z.Ispytanie = nil
	p.z.Proshlye = p.z.Nastroyki
	p.sohranit()
	return false
}

// Zapusk — процесс поднялся. Шло испытание, а прошлый процесс не ушёл штатно
// (Vyhod не звучал: упал, убит, os.Exit) — это падение; на третьем — откат.
// true — откатились.
func (p *Pult) Zapusk() bool {
	if p == nil {
		return false
	}
	seychas := p.seychas()
	p.mu.Lock()
	defer p.mu.Unlock()
	chisto := p.z.ChistyyVyhod
	p.z.ChistyyVyhod = false
	i := p.z.Ispytanie
	if i == nil {
		if chisto {
			p.sohranit()
		}
		return false
	}
	if !chisto {
		i.Padeniy++
	}
	if i.Padeniy >= PadeniyDoOtkata {
		p.otkatit(fmt.Sprintf("процесс перезапускался %d раза за испытание", i.Padeniy))
		return true
	}
	// Наблюдения прошлой жизни процесса неполны — испытание начинается заново.
	*i = ispytanie{Versiya: i.Versiya, Nachalo: seychas, Padeniy: i.Padeniy}
	p.sohranit()
	return false
}

// Vyhod — процесс уходит штатно: следующий запуск падением не считается.
func (p *Pult) Vyhod() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.z.ChistyyVyhod = true
	p.sohranit()
}

// otkatit — под p.mu.
func (p *Pult) otkatit(pochemu string) {
	v := p.z.Ispytanie.Versiya
	log.Printf("пульт: откат настроек версии %d (%s) — возвращаю прошлые, версию помечаю плохой", v, pochemu)
	p.z.Plohie = append(p.z.Plohie, v)
	if len(p.z.Plohie) > 50 {
		p.z.Plohie = p.z.Plohie[len(p.z.Plohie)-50:]
	}
	p.z.Nastroyki = p.z.Proshlye
	p.z.Ispytanie = nil
	p.z.Otkat = true
	p.sohranit()
}

// --- значения для службы ---

func (p *Pult) znachenie(imya string) (json.RawMessage, bool) {
	if p == nil {
		return nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	v, est := p.z.Nastroyki[imya]
	return v, est
}

// Celoe — целое значение ключа; false — ключа нет (брать значение из кода).
func (p *Pult) Celoe(imya string) (int64, bool) {
	v, est := p.znachenie(imya)
	var c int64
	if !est || json.Unmarshal(v, &c) != nil {
		return 0, false
	}
	return c, true
}

// Dlitelnost — целое ключа, умноженное на edinica; poUmolchaniyu — без ключа.
func (p *Pult) Dlitelnost(imya string, edinica, poUmolchaniyu time.Duration) time.Duration {
	if c, est := p.Celoe(imya); est {
		return time.Duration(c) * edinica
	}
	return poUmolchaniyu
}

// FlagIli — значение bool-ключа или poUmolchaniyu.
func (p *Pult) FlagIli(imya string, poUmolchaniyu bool) bool {
	v, est := p.znachenie(imya)
	var b bool
	if !est || json.Unmarshal(v, &b) != nil {
		return poUmolchaniyu
	}
	return b
}

// Vremya — значение ключа-времени.
func (p *Pult) Vremya(imya string) (time.Time, bool) {
	v, est := p.znachenie(imya)
	var s string
	if !est || json.Unmarshal(v, &s) != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// StrokaIli — значение ключа-выбора или poUmolchaniyu.
func (p *Pult) StrokaIli(imya, poUmolchaniyu string) string {
	v, est := p.znachenie(imya)
	var s string
	if !est || json.Unmarshal(v, &s) != nil {
		return poUmolchaniyu
	}
	return s
}

// Spisok — значение ключа-списка.
func (p *Pult) Spisok(imya string) ([]string, bool) {
	v, est := p.znachenie(imya)
	var s []string
	if !est || json.Unmarshal(v, &s) != nil {
		return nil, false
	}
	return s, true
}

// Versiya — наибольшая принятая версия пульта (0 — не принимали).
func (p *Pult) Versiya() int64 {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.z.Versiya
}

// Obnovlenie — блок выкатки из последнего принятого пульта (nil — нет).
func (p *Pult) Obnovlenie() *Obnovlenie {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.z.Obnovlenie == nil {
		return nil
	}
	o := *p.z.Obnovlenie
	return &o
}

// OshibkaObnovleniya — установка из блока obnovlenie не удалась (для X-Pult).
func (p *Pult) OshibkaObnovleniya(prichina string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.oshibkaObnovleniya = chistoe(prichina, 32)
	p.mu.Unlock()
}

// Zagolovok — значение X-Pult.
func (p *Pult) Zagolovok() string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	chasti := []string{"v=1", "umeet=" + strconv.Itoa(Umeet), "versiya=" + strconv.FormatInt(p.z.Versiya, 10)}

	type kv struct {
		id string
		v  *vypolnennaya
	}
	var kvity []kv
	for id, v := range p.z.Vypolneno {
		if v.Kvit && v.Itog != "" && !v.idet {
			kvity = append(kvity, kv{id, v})
		}
	}
	sort.Slice(kvity, func(i, j int) bool {
		if !kvity[i].v.Kogda.Equal(kvity[j].v.Kogda) {
			return kvity[i].v.Kogda.After(kvity[j].v.Kogda)
		}
		return kvity[i].id < kvity[j].id
	})
	if len(kvity) > PredelKvitanciy {
		kvity = kvity[:PredelKvitanciy]
	}
	if len(kvity) > 0 {
		var s []string
		for _, k := range kvity {
			s = append(s, k.id+":"+k.v.Itog)
		}
		chasti = append(chasti, "kvit="+strings.Join(s, ","))
	}

	oshibki := append([]string(nil), p.z.OshibkiKlyuchey...)
	if p.otkaz != "" {
		oshibki = append(oshibki, "pult:"+p.otkaz)
	}
	if p.z.Otkat {
		oshibki = append(oshibki, "pult:otkat")
	}
	if p.oshibkaObnovleniya != "" {
		oshibki = append(oshibki, "obnovlenie:"+p.oshibkaObnovleniya)
	}
	if len(oshibki) > 0 {
		for i := range oshibki {
			oshibki[i] = chistoe(oshibki[i], 64)
		}
		chasti = append(chasti, "oshibki="+strings.Join(oshibki, ","))
	}
	return strings.Join(chasti, ";")
}

// chistoe — только [A-Za-z0-9._:-], не длиннее predel: id и слова приходят с
// сервера и уходят в заголовок, где ; , = и пробел — разделители.
func chistoe(s string, predel int) string {
	var b strings.Builder
	for i := 0; i < len(s) && b.Len() < predel; i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' || c == ':' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// chistoeID — id команды для kvit=<id>:<итог>: без двоеточия.
func chistoeID(s string) string { return strings.ReplaceAll(chistoe(s, 64), ":", "") }

func ravny(a, b map[string]json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if string(b[k]) != string(v) {
			return false
		}
	}
	return true
}
