// Пакет dom: вход домой своей сети (договор «Своя сеть Kelevra: вход домой»,
// часть Desktop).
//
// Дома стоит узел с хабом WireGuard. Компьютер поднимает к нему свой
// эндпоинт «Дом», и трафик к адресам дома (100.77.0.0/16) идёт по нему поверх
// обычного туннеля «Соединение». Здесь три вещи:
//
//   - ключ WireGuard этого устройства (klyuch.go): рождается здесь один раз,
//     закрытая половина не покидает машину;
//   - блок dom из сводки /info: что мозг разрешил этому устройству (адрес,
//     ключ хаба, мост, сети), подписан ключом пульта и проверяется тем же
//     алгоритмом, что пульт (internal/pult);
//   - кеш последнего проверенного блока на диске: без мозга вход домой живёт
//     по последней раскладке.
//
// Блок без верной подписи не применяется никогда: он решает, куда уходит
// трафик к дому, и подменённый блок увёл бы его чужому хабу.
package dom

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpis"
	"github.com/HRYNdev/kelevra-desktop/internal/pult"
)

// Metka — с чего начинается подписываемая строка блока dom. Своя, не пультовая:
// подпись пульта не должна подходить к блоку дома и наоборот (ключ один).
// Узел дома подписывает свою раскладку меткой "kelevra-dom-uzel-v1\n".
const Metka = "kelevra-dom-v1\n"

// SvoyaSet — своя подсеть (договор, «Адреса и имена»). Сети блока обязаны
// лежать внутри неё: подписанный блок с 0.0.0.0/0 увёл бы весь трафик машины
// в дом, и такой блок отвергается как испорченный.
var SvoyaSet = netip.MustParsePrefix("100.77.0.0/16")

// MtuPoUmolchaniyu — MTU внутри WireGuard, если блок его не назвал.
const MtuPoUmolchaniyu = 1280

// Blok — что мозг разрешил этому устройству (поле dom сводки /info).
type Blok struct {
	Adres   string   `json:"adres"`    // адрес устройства, «100.77.0.N/32»
	UzelPub string   `json:"uzel_pub"` // открытый ключ хаба дома, base64
	Most    string   `json:"most"`     // куда слать WireGuard, «100.76.0.1:51820»
	Seti    []string `json:"seti"`     // что вести в дом
	DNS     string   `json:"dns"`      // DNS дома
	Zona    string   `json:"zona"`     // зона имён, «dom»
	MTU     int      `json:"mtu,omitempty"`
	Versiya int64    `json:"versiya"`
	// Komu — слот адресата («01-2»). Блок применяется, только если это свой
	// слот: блок чужого устройства (тот же человек, другая машина) не наш.
	Komu string `json:"komu,omitempty"`
	// Pub — открытый ключ устройства, на который выдан блок (договор,
	// «Безопасность» п.3): не свой — блок не применяется («чужой ключ»).
	Pub string `json:"pub"`
	// Otozvan — подписанный отзыв ключа Pub: устройство рождает новый ключ.
	// Голый ответ двери «otozvan» ключ не перерождает — его может подделать VPS.
	Otozvan bool `json:"otozvan,omitempty"`
	// Portaly — входы VPS, где стоит портал моста домой: только через них
	// UDP на мост доходит до дома. Клиент ведёт «Дом» через свои выходы на
	// эти входы, а не через выбор человека для интернета. Нет поля (старый
	// мозг) — мост идёт через «Соединение», как раньше.
	Portaly []Portal `json:"portaly,omitempty"`
}

// Portal — вход VPS с порталом моста: узел (для журнала), адрес и порт
// входа, как они стоят у выхода VLESS в профиле (server, server_port).
type Portal struct {
	Uzel   string `json:"uzel"`
	Server string `json:"server"`
	Port   int    `json:"port"`
}

// MaksPortalov — больше входов с порталом в блоке не бывает.
const MaksPortalov = 16

var serverPortala = regexp.MustCompile(`^[A-Za-z0-9.:\[\]-]{1,253}$`)

// MostHostPort — мост раздельно.
func (b *Blok) MostHostPort() (string, int) {
	host, p, err := net.SplitHostPort(b.Most)
	if err != nil {
		return "", 0
	}
	port, _ := strconv.Atoi(p)
	return host, port
}

// Mtu — MTU блока или MtuPoUmolchaniyu.
func (b *Blok) Mtu() int {
	if b.MTU <= 0 {
		return MtuPoUmolchaniyu
	}
	return b.MTU
}

// Otkaz — блок не принят; Prichina — одно слово для журнала.
type Otkaz struct{ Prichina string }

func (o *Otkaz) Error() string { return "блок дома отвергнут: " + o.Prichina }

// konvert — блок в конверте, как пульт: {"dannye": base64, "podpis": base64}.
type konvert struct {
	Dannye string `json:"dannye"`
	Podpis string `json:"podpis"`
}

// VshityeKlyuchi — ключи пульта (боевые и проверочный стенда): блок дома
// подписывает тот же мозг тем же ключом.
func VshityeKlyuchi() []ed25519.PublicKey {
	var k []ed25519.PublicKey
	for _, h := range pult.VshityeKlyuchi() {
		if kl, err := podpis.Klyuch(h); err == nil {
			k = append(k, kl)
		}
	}
	return k
}

// Razobrat проверяет подпись блока и его содержимое. Договор закрепил
// плоскую форму (2, KelevraServer docs/VHOD-DOMOY-API.md); конверт (1) тоже
// принимается:
//
//  1. конверт, как у пульта: {"dannye": base64(JSON блока), "podpis": base64};
//     подписано Metka + байты dannye до base64;
//  2. плоский блок договора: поля блока и рядом "podpis"; подписано Metka +
//     канонический JSON блока без поля podpis (ключи по алфавиту, без
//     пробелов, UTF-8 как есть, числа как пришли — kanonicheskiy).
//
// komu — слот этого устройства: блок с другим komu (или без него) не
// принимается. Пустой komu (слот неизвестен) — не принимается никакой.
func Razobrat(syroe []byte, klyuchi []ed25519.PublicKey, komu string) (*Blok, error) {
	syroe = bytes.TrimSpace(syroe)
	if len(syroe) == 0 || string(syroe) == "null" {
		return nil, &Otkaz{"pusto"}
	}
	var pervyy map[string]json.RawMessage
	if err := json.Unmarshal(syroe, &pervyy); err != nil {
		return nil, &Otkaz{"format"}
	}
	var dannye, sig []byte
	if _, est := pervyy["dannye"]; est {
		var k konvert
		if err := json.Unmarshal(syroe, &k); err != nil {
			return nil, &Otkaz{"format"}
		}
		var err1, err2 error
		dannye, err1 = base64.StdEncoding.DecodeString(strings.TrimSpace(k.Dannye))
		sig, err2 = base64.StdEncoding.DecodeString(strings.TrimSpace(k.Podpis))
		if err1 != nil || err2 != nil || len(dannye) == 0 {
			return nil, &Otkaz{"format"}
		}
	} else {
		var s string
		if err := json.Unmarshal(pervyy["podpis"], &s); err != nil || s == "" {
			return nil, &Otkaz{"podpis"}
		}
		var err error
		if sig, err = base64.StdEncoding.DecodeString(strings.TrimSpace(s)); err != nil {
			return nil, &Otkaz{"podpis"}
		}
		if dannye, err = kanonicheskiy(syroe); err != nil {
			return nil, &Otkaz{"format"}
		}
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, &Otkaz{"podpis"}
	}
	soobshchenie := append([]byte(Metka), dannye...)
	verna := false
	for _, k := range klyuchi {
		if ed25519.Verify(k, soobshchenie, sig) {
			verna = true
			break
		}
	}
	if !verna {
		return nil, &Otkaz{"podpis"}
	}
	var b Blok
	if err := json.Unmarshal(dannye, &b); err != nil {
		return nil, &Otkaz{"format"}
	}
	if komu == "" || strings.TrimSpace(b.Komu) != komu {
		return nil, &Otkaz{"komu"}
	}
	if err := b.proverit(); err != nil {
		return nil, err
	}
	return &b, nil
}

// kanonicheskiy — плоский блок без поля podpis в каноническом виде.
// Повторяет json.dumps(..., sort_keys=True, separators=(",", ":"),
// ensure_ascii=False) мозга: Go сортирует ключи карты сам, числа держим
// дословно (UseNumber), HTML-экранирование выключено.
func kanonicheskiy(syroe []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(syroe))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	delete(m, "podpis")
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

var zonaDopustima = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)

// proverit — содержимое блока годно для конфига ядра.
func (b *Blok) proverit() error {
	if k, err := base64.StdEncoding.DecodeString(b.Pub); err != nil || len(k) != 32 {
		return &Otkaz{"pub"}
	}
	if b.Versiya < 0 {
		return &Otkaz{"versiya"}
	}
	if b.Otozvan {
		return nil // отзыв несёт только ключ и версию
	}
	p, err := netip.ParsePrefix(strings.TrimSpace(b.Adres))
	if err != nil || !p.Addr().Is4() || p.Bits() != 32 || !SvoyaSet.Contains(p.Addr()) {
		return &Otkaz{"adres"}
	}
	b.Adres = p.String()
	if k, err := base64.StdEncoding.DecodeString(b.UzelPub); err != nil || len(k) != 32 {
		return &Otkaz{"uzel_pub"}
	}
	host, port := b.MostHostPort()
	if host == "" || port <= 0 || port > 65535 {
		return &Otkaz{"most"}
	}
	if len(b.Seti) == 0 || len(b.Seti) > 32 {
		return &Otkaz{"seti"}
	}
	for i, s := range b.Seti {
		ps, err := netip.ParsePrefix(strings.TrimSpace(s))
		if err != nil || !ps.Addr().Is4() || ps.Bits() < SvoyaSet.Bits() || !SvoyaSet.Contains(ps.Addr()) {
			return &Otkaz{"seti"}
		}
		b.Seti[i] = ps.Masked().String()
	}
	dns, err := netip.ParseAddr(strings.TrimSpace(b.DNS))
	if err != nil || !b.vSetyah(dns) {
		return &Otkaz{"dns"}
	}
	b.DNS = dns.String()
	b.Zona = strings.ToLower(strings.TrimSpace(b.Zona))
	if !zonaDopustima.MatchString(b.Zona) {
		return &Otkaz{"zona"}
	}
	if b.MTU != 0 && (b.MTU < 576 || b.MTU > 1420) {
		return &Otkaz{"mtu"}
	}
	if b.Versiya < 0 {
		return &Otkaz{"versiya"}
	}
	if len(b.Portaly) > MaksPortalov {
		return &Otkaz{"portaly"}
	}
	for i, p := range b.Portaly {
		p.Server = strings.TrimSpace(p.Server)
		if !serverPortala.MatchString(p.Server) || p.Port <= 0 || p.Port > 65535 || len(p.Uzel) > 64 {
			return &Otkaz{"portaly"}
		}
		b.Portaly[i] = p
	}
	return nil
}

func (b *Blok) vSetyah(a netip.Addr) bool {
	for _, s := range b.Seti {
		if p, err := netip.ParsePrefix(s); err == nil && p.Contains(a) {
			return true
		}
	}
	return false
}

// Kesh — последний проверенный блок на диске (работа без мозга).
//
// На диске лежит блок как пришёл, с подписью, и при чтении проверяется
// заново: файл, подменённый на диске, применён не будет. Рядом — память
// (PutPamyati): наибольшая принятая версия и «вход домой включался». Она не
// сбрасывается при снятии блока: снятие приходит неподписанным (сводка без
// поля dom), и стёртая версия позволила бы потом вернуть старый подписанный
// блок (договор, «Безопасность» п.3).
type Kesh struct {
	Put        string              // "" — только в памяти (проверки)
	PutPamyati string              // "" — память только в памяти (проверки)
	Klyuchi    []ed25519.PublicKey // nil — VshityeKlyuchi
	Komu       string              // свой слот; пусто — блок не принимается (слот неизвестен)
	// SvoyPub — открытый ключ этого устройства; блок на другой ключ не наш.
	SvoyPub func() string

	mu     sync.Mutex
	blok   *Blok
	syroe  []byte
	zagruz bool
	pam    pamyat
	// bezPamyati — ни одного файла памяти не прочитано; poteryana — и при
	// этом на диске был блок (PamyatPoteryana).
	bezPamyati, poteryana bool
}

// pamyat — dom_pamyat.json.
type pamyat struct {
	MaxVersiya   int64 `json:"max_versiya"`
	Vklyuchalsya bool  `json:"vklyuchalsya,omitempty"`
}

// Itog — что принёс блок из сводки.
type Itog struct {
	// Izmenilsya — действующий блок сменился: конфиг ядра пересобрать.
	Izmenilsya bool
	// Otozvan — подписанный отзыв своего ключа: родить новый.
	Otozvan bool
	// ChuzhoyKlyuch — подписанный блок выдан на другой ключ: не применён.
	ChuzhoyKlyuch bool
}

// UstanovitKomu — свой слот стал известен (или сменился): блок с диска
// перечитывается и сверяется с ним заново.
func (k *Kesh) UstanovitKomu(komu string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Komu == komu {
		return
	}
	k.Komu = komu
	k.blok, k.syroe, k.zagruz = nil, nil, false
}

func (k *Kesh) klyuchi() []ed25519.PublicKey {
	if k.Klyuchi != nil {
		return k.Klyuchi
	}
	return VshityeKlyuchi()
}

func (k *Kesh) svoyPub() string {
	if k.SvoyPub == nil {
		return ""
	}
	return k.SvoyPub()
}

// Blok — действующий блок (nil — дома нет). Первый вызов читает диск.
func (k *Kesh) Blok() *Blok {
	if k == nil {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.zagruzit()
	if k.blok == nil {
		return nil
	}
	b := *k.blok
	b.Seti = append([]string(nil), k.blok.Seti...)
	return &b
}

// Vklyuchalsya — вход домой на этом устройстве хоть раз был (принят блок или
// мозг сказал, что «Дом» включён). Тогда без действующего блока сети и имена
// дома закрыты (konfig, «закрыто по умолчанию»), а не уходят в обычный туннель.
func (k *Kesh) Vklyuchalsya() bool {
	if k == nil {
		return false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.zagruzit()
	return k.pam.Vklyuchalsya
}

// OtmetitVklyuchenie — мозг сказал, что «Дом» этому устройству включён.
func (k *Kesh) OtmetitVklyuchenie() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.zagruzit()
	if !k.pam.Vklyuchalsya {
		k.pam.Vklyuchalsya = true
		k.sohranitPamyat()
	}
}

// MaxVersiya — наибольшая принятая версия блока.
func (k *Kesh) MaxVersiya() int64 {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.zagruzit()
	return k.pam.MaxVersiya
}

// zagruzit — под k.mu.
func (k *Kesh) zagruzit() {
	if k.zagruz {
		return
	}
	k.zagruz = true
	if k.PutPamyati != "" {
		// Память в двух файлах (договор, «Безопасность» п.14): берётся
		// наибольшее из обоих; один потерян или испорчен — восстанавливается
		// из другого; оба потеряны — bezPamyati.
		prochitano, polno := 0, true
		for _, put := range k.putiPamyati() {
			var p pamyat
			b, err := os.ReadFile(put)
			if err != nil || json.Unmarshal(b, &p) != nil {
				polno = false
				continue
			}
			prochitano++
			if p.MaxVersiya > k.pam.MaxVersiya {
				k.pam.MaxVersiya = p.MaxVersiya
			}
			k.pam.Vklyuchalsya = k.pam.Vklyuchalsya || p.Vklyuchalsya
		}
		k.bezPamyati = prochitano == 0
		if prochitano > 0 && !polno {
			log.Printf("дом: один из файлов памяти блока потерян — восстанавливаю из другого")
			k.sohranitPamyat()
		}
	}
	if k.Put == "" {
		return
	}
	syroe, err := os.ReadFile(k.Put)
	if err != nil {
		return
	}
	if k.bezPamyati && k.PutPamyati != "" {
		// Блок есть, а памяти о его версии нет: старый подписанный блок
		// мог быть подложен. Без нового подтверждения не принимается.
		k.poteryana = true
		log.Printf("дом: память версий блока потеряна (оба файла) — блок с диска не принимаю")
		return
	}
	b, err := Razobrat(syroe, k.klyuchi(), k.Komu)
	if err == nil && (b.Otozvan || b.Pub != k.svoyPub()) {
		err = &Otkaz{"pub"}
	}
	if err == nil && b.Versiya < k.pam.MaxVersiya {
		err = &Otkaz{"versiya"}
	}
	if err != nil {
		log.Printf("дом: блок на диске не принят (%v) — без входа домой до свежего с сервера", err)
		return
	}
	k.blok, k.syroe = b, syroe
	k.otmetitVersiyu(b.Versiya)
}

// putiPamyati — основной файл памяти и запасной рядом.
func (k *Kesh) putiPamyati() []string {
	return []string{k.PutPamyati, k.PutPamyati + ".zapas"}
}

// PamyatPoteryana — оба файла памяти потеряны, а следы входа домой (блок на
// диске) есть: принятым блокам больше нечего противопоставить при откате.
// Вызывающий решает (служба: новый ключ — новое подтверждение владельцем).
func (k *Kesh) PamyatPoteryana() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.zagruzit()
	return k.poteryana
}

// BezPamyati — ни одного файла памяти нет (свежая установка или потеря).
func (k *Kesh) BezPamyati() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.zagruzit()
	return k.bezPamyati
}

// NachatZanovo — после потери памяти: блок с диска выброшен, вход домой
// отмечен как бывший (закрыто по умолчанию), память записана заново.
func (k *Kesh) NachatZanovo() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.zagruzit()
	k.snyat()
	k.pam.Vklyuchalsya = true
	k.sohranitPamyat()
	k.poteryana, k.bezPamyati = false, false
}

// otmetitVersiyu — под k.mu.
func (k *Kesh) otmetitVersiyu(v int64) {
	if v > k.pam.MaxVersiya || !k.pam.Vklyuchalsya {
		if v > k.pam.MaxVersiya {
			k.pam.MaxVersiya = v
		}
		k.pam.Vklyuchalsya = true
		k.sohranitPamyat()
	}
}

func (k *Kesh) sohranitPamyat() {
	if k.PutPamyati == "" {
		return
	}
	b, _ := json.Marshal(k.pam)
	if err := os.MkdirAll(filepath.Dir(k.PutPamyati), 0o755); err != nil {
		log.Printf("дом: память блока не сохранена: %v", err)
		return
	}
	for _, put := range k.putiPamyati() {
		if err := hranenie.ZapisatZakryto(put, b); err != nil {
			log.Printf("дом: память блока не сохранена (%s): %v", filepath.Base(put), err)
		}
	}
	k.bezPamyati = false
}

// snyat — под k.mu: действующего блока больше нет (память остаётся).
func (k *Kesh) snyat() bool {
	if k.blok == nil {
		return false
	}
	k.blok, k.syroe = nil, nil
	if k.Put != "" {
		if err := os.Remove(k.Put); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("дом: снятый блок не удалён с диска: %v", err)
		}
	}
	return true
}

// Prinyat — блок dom из свежей сводки. Пустой (поля нет) — мозг дом этому
// устройству не даёт: блок снимается, память остаётся. Ошибка — блок не
// принят (подпись, адресат, версия ниже наибольшей принятой), прежний остаётся.
// Подписанный блок на другой ключ — не применяется (Itog.ChuzhoyKlyuch);
// подписанный отзыв своего ключа — Itog.Otozvan, блок снимается.
func (k *Kesh) Prinyat(syroe []byte) (Itog, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.zagruzit()
	syroe = bytes.TrimSpace(syroe)
	if len(syroe) == 0 || string(syroe) == "null" {
		return Itog{Izmenilsya: k.snyat()}, nil
	}
	if k.syroe != nil && bytes.Equal(syroe, k.syroe) {
		return Itog{}, nil
	}
	if k.poteryana {
		return Itog{}, &Otkaz{"pamyat"}
	}
	b, err := Razobrat(syroe, k.klyuchi(), k.Komu)
	if err != nil {
		return Itog{}, err
	}
	if b.Versiya < k.pam.MaxVersiya {
		return Itog{}, &Otkaz{"versiya"}
	}
	svoy := k.svoyPub()
	if b.Pub != svoy {
		if b.Otozvan {
			// Отзыв прежнего ключа (свой уже новый): действующего блока на
			// свой ключ этот ответ не содержит.
			k.otmetitVersiyu(b.Versiya)
			return Itog{Izmenilsya: k.snyat()}, nil
		}
		return Itog{ChuzhoyKlyuch: true}, &Otkaz{"pub"}
	}
	k.otmetitVersiyu(b.Versiya)
	if b.Otozvan {
		return Itog{Izmenilsya: k.snyat(), Otozvan: true}, nil
	}
	stalo := k.blok == nil || !odinakovye(k.blok, b)
	k.blok, k.syroe = b, append([]byte(nil), syroe...)
	if k.Put != "" {
		if err := os.MkdirAll(filepath.Dir(k.Put), 0o755); err == nil {
			if err := hranenie.ZapisatZakryto(k.Put, k.syroe); err != nil {
				log.Printf("дом: блок не сохранён на диск: %v", err)
			}
		}
	}
	return Itog{Izmenilsya: stalo}, nil
}

func odinakovye(a, b *Blok) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// String — блок для журнала: адрес и версия (ключ хаба открытый, но журналу
// он ни к чему).
func (b *Blok) String() string {
	if b == nil {
		return "нет"
	}
	s := fmt.Sprintf("%s, мост %s, версия %d", b.Adres, b.Most, b.Versiya)
	if len(b.Portaly) > 0 {
		var p []string
		for _, x := range b.Portaly {
			p = append(p, x.Uzel+" "+net.JoinHostPort(x.Server, strconv.Itoa(x.Port)))
		}
		s += ", порталы " + strings.Join(p, ", ")
	}
	return s
}
