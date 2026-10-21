package yadro

// Замена ядра у стоящих установок.
//
// До 21.10.2026 ядро качалось только если его файла не было вовсе: новая
// сборка core-* доходила лишь до новых установок, а у стоящих ядро не менялось
// никогда. Теперь замена идёт в два шага.
//
//  1. Приготовить (Prigotovit, фоном, ядро может работать): найти самую свежую
//     подписанную сборку, сверить её тег с версией стоящего ядра, скачать
//     архив, проверить подпись ed25519 и отложить архив рядом с ядром.
//  2. Сменить (Smenit, только когда ядро не запущено — при следующем
//     подключении): ещё раз проверить подпись отложенного архива, отодвинуть
//     прежний файл ядра и поставить новый. Ядро, которое не поднялось, служба
//     возвращает на прежнее (Smena.Otkatit) и больше эту сборку не берёт.
//
// Подпись проверяется на обоих шагах и не зависит от obnovlenie.TrebovatPodpis:
// замена ядра без подписи не делается ни в каком режиме.
//
// Версия стоящего ядра — тег сборки в versiya.json рядом с ядром вместе с
// sha256 файла. Файл ядра, чей хэш с запиской не сходится (записки нет —
// установка старше замены), считается ядром неизвестной версии: при первой
// подготовке служба сверяет его с файлом из подписанного архива и, если
// совпало, просто записывает версию, ничего не меняя.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpis"
)

// popytokSmeny — сколько раз подряд пробовать поставить одну и ту же сборку,
// если не поднимается ни новое ядро, ни прежнее (дело не в ядре, а, например,
// в профиле). После этого сборка считается негодной: каждое подключение не
// должно стоить человеку двух подъёмов ядра.
const popytokSmeny = 3

// versiyaYadra — записка о стоящем ядре (versiya.json).
type versiyaYadra struct {
	Teg    string `json:"teg"`
	Sha256 string `json:"sha256"`
	// Otvergnuty — сборки, которые на этой машине не поднялись. Их служба
	// больше не качает; следующая новая сборка пробуется как обычно.
	Otvergnuty []string `json:"otvergnuty,omitempty"`
}

// otlozhennoe — записка об отложенном архиве (novoe.json).
type otlozhennoe struct {
	Teg string `json:"teg"`
	// Podpis — текст файла .sig из релиза: подпись проверяется ещё раз перед
	// самой заменой, без сети.
	Podpis string `json:"podpis"`
	// Prezhniy — тег ядра, которое стояло до замены (для отката).
	Prezhniy string `json:"prezhniy,omitempty"`
	// Popytok — сколько раз эту сборку уже ставили (и при замене, и при
	// продолжении пробы после обрыва службы).
	Popytok int `json:"popytok,omitempty"`
	// Faza — где замена (журнал для восстановления после обрыва службы):
	// "" — архив отложен, файл ядра не тронут; fazaMenyayu — намерение
	// записано, переименования идут; fazaProba — новое ядро на месте, прежнее
	// лежит рядом (.prezhnee) до подтверждения.
	Faza string `json:"faza,omitempty"`
	// ShaNovogo — sha256 нового файла ядра: по нему после обрыва видно,
	// успело ли новое ядро встать на место.
	ShaNovogo string `json:"sha_novogo,omitempty"`
}

const (
	fazaMenyayu = "menyayu"
	fazaProba   = "proba"
)

func (y *Yadro) chitatOtlozhennoe() (otlozhennoe, bool) {
	var o otlozhennoe
	ok := chitatJSON(y.putOtlozhennogo(), &o) && o.Teg != ""
	return o, ok
}

func (y *Yadro) sohranitOtlozhennoe(o otlozhennoe) error {
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	return zapisatZakryto(y.putOtlozhennogo(), b)
}

func (y *Yadro) putVersii() string       { return filepath.Join(y.Papka, "versiya.json") }
func (y *Yadro) putOtlozhennogo() string { return filepath.Join(y.Papka, "novoe.json") }
func (y *Yadro) putArhivaNovogo() string { return filepath.Join(y.Papka, "novoe.zip") }
func (y *Yadro) putPrezhnego() string    { return y.Bin + ".prezhnee" }

// keshHesha — sha256 ядра по размеру и времени изменения: хэшировать файл
// в десятки мегабайт на каждый вопрос о версии незачем.
type keshHesha struct {
	zamok  sync.Mutex
	put    string
	razmer int64
	vremya int64
	hesh   string
}

var heshYadra keshHesha

// sbrositKeshHesha — файл ядра сменили сами: время изменения у файлов одного
// размера может совпасть до тика часов, и кеш ответил бы прежним хэшем.
func sbrositKeshHesha() {
	heshYadra.zamok.Lock()
	heshYadra.put, heshYadra.hesh = "", ""
	heshYadra.zamok.Unlock()
}

func heshFaylaYadra(put string) (string, error) {
	st, err := os.Stat(put)
	if err != nil {
		return "", err
	}
	heshYadra.zamok.Lock()
	if heshYadra.put == put && heshYadra.razmer == st.Size() && heshYadra.vremya == st.ModTime().UnixNano() {
		h := heshYadra.hesh
		heshYadra.zamok.Unlock()
		return h, nil
	}
	heshYadra.zamok.Unlock()
	f, err := os.Open(put)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	itog := hex.EncodeToString(h.Sum(nil))
	heshYadra.zamok.Lock()
	heshYadra.put, heshYadra.razmer, heshYadra.vremya, heshYadra.hesh = put, st.Size(), st.ModTime().UnixNano(), itog
	heshYadra.zamok.Unlock()
	return itog, nil
}

func chitatJSON(put string, v any) bool {
	b, err := os.ReadFile(put)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

// zapisatZakryto пишет файл через временный, закрыв права до записи (в общей
// папке — только система и администраторы, см. hranenie.ZakrytOtPolzovateley).
func zapisatZakryto(put string, telo []byte) error {
	vremenny := put + ".tmp"
	_ = os.Remove(vremenny)
	f, err := os.OpenFile(vremenny, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if err := hranenie.ZakrytOtPolzovateley(vremenny); err != nil {
		f.Close()
		_ = os.Remove(vremenny)
		return err
	}
	_, zerr := f.Write(telo)
	if cerr := f.Close(); zerr == nil {
		zerr = cerr
	}
	if zerr != nil {
		_ = os.Remove(vremenny)
		return zerr
	}
	return os.Rename(vremenny, put)
}

func (y *Yadro) chitatVersiyu() versiyaYadra {
	var v versiyaYadra
	chitatJSON(y.putVersii(), &v)
	return v
}

func (y *Yadro) sohranitVersiyu(v versiyaYadra) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return zapisatZakryto(y.putVersii(), b)
}

// zapisatVersiyu запоминает, что на месте стоит ядро сборки teg.
// Ошибка записи не мешает работе: без записки ядро будет признано при
// следующей подготовке.
func (y *Yadro) zapisatVersiyu(teg string) {
	sbrositKeshHesha()
	hesh, err := heshFaylaYadra(y.Bin)
	if err != nil {
		return
	}
	v := y.chitatVersiyu()
	v.Teg, v.Sha256 = teg, hesh
	_ = y.sohranitVersiyu(v)
}

// Versiya — тег сборки стоящего ядра; пусто — версия неизвестна (записки
// нет или файл ядра с ней не сходится).
func (y *Yadro) Versiya() string {
	v := y.chitatVersiyu()
	if v.Teg == "" {
		return ""
	}
	hesh, err := heshFaylaYadra(y.Bin)
	if err != nil || hesh != v.Sha256 {
		return ""
	}
	return v.Teg
}

// ItogPodgotovki — чем кончилась подготовка замены (для журнала).
type ItogPodgotovki struct {
	Svezhaya string // тег самой свежей годной сборки
	Stoit    string // тег стоящего ядра (после признания — уже известный)
	// Otlozheno — архив новой сборки проверен и ждёт подключения.
	Otlozheno bool
	// Priznano — стоящее ядро неизвестной версии оказалось этой сборкой.
	Priznano bool
	// Otvergnuto — эта сборка на машине уже не поднималась, не берём.
	Otvergnuto bool
}

// ErrNetYadra — ядра на машине нет: менять нечего, его поставит Zagruzit.
var ErrNetYadra = errors.New("ядра на машине нет")

// Prigotovit готовит замену ядра (шаг 1, см. шапку файла). Ядро при этом
// может работать: файл ядра не трогается. Скачивание идёт без замка, решения
// и запись отложенного — под замком замены (zamokSmeny).
func (y *Yadro) Prigotovit(ctx context.Context, klient *http.Client) (ItogPodgotovki, error) {
	if !y.EstBinar() {
		return ItogPodgotovki{}, ErrNetYadra
	}
	sborka, err := y.vybratSborku(ctx, klient, true)
	if err != nil {
		return ItogPodgotovki{}, err
	}
	y.zamokSmeny.Lock()
	itog, nuzhno := y.reshitPodgotovku(sborka)
	y.zamokSmeny.Unlock()
	if !nuzhno {
		return itog, nil
	}

	// Скачиваем во временный файл с закрытыми правами и проверяем подпись.
	vremenny := y.putArhivaNovogo() + ".tmp"
	_ = os.Remove(vremenny)
	if err := os.MkdirAll(y.Papka, 0o755); err != nil {
		return itog, err
	}
	f, err := os.OpenFile(vremenny, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return itog, err
	}
	f.Close()
	defer os.Remove(vremenny)
	if err := hranenie.ZakrytOtPolzovateley(vremenny); err != nil {
		return itog, err
	}
	if err := skachat(ctx, klient, sborka.Ssylka, vremenny); err != nil {
		return itog, err
	}
	sig, err := skachatPodpis(ctx, klient, sborka.SsylkaPodpisi)
	if err != nil {
		return itog, err
	}
	if err := podpis.ProveritSTegom(vremenny, ImyaArhiva(), sborka.Teg, sig); err != nil {
		return itog, fmt.Errorf("%w: сборка ядра %s не отложена — нет подписи с этим тегом (%v)", obnovlenie.ErrPodpis, sborka.Teg, err)
	}

	y.zamokSmeny.Lock()
	defer y.zamokSmeny.Unlock()
	// Пока качали, могла начаться замена — тогда отложенное не трогаем.
	if o, ok := y.chitatOtlozhennoe(); ok && o.Faza != "" {
		return itog, nil
	}
	// Ядро неизвестной версии: если это та же сборка — только записать версию.
	if itog.Stoit == "" {
		odinakovo, err := y.toZheYadro(vremenny)
		if err != nil {
			return itog, err
		}
		if odinakovo {
			y.zapisatVersiyu(sborka.Teg)
			y.ubratOtlozhennoeEsliStoit(sborka.Teg)
			itog.Stoit, itog.Priznano = sborka.Teg, true
			return itog, nil
		}
	}
	if err := os.Rename(vremenny, y.putArhivaNovogo()); err != nil {
		return itog, err
	}
	if err := y.sohranitOtlozhennoe(otlozhennoe{Teg: sborka.Teg, Podpis: string(sig)}); err != nil {
		_ = os.Remove(y.putArhivaNovogo())
		return itog, err
	}
	itog.Otlozheno = true
	return itog, nil
}

// reshitPodgotovku — нужно ли качать сборку. Под zamokSmeny.
func (y *Yadro) reshitPodgotovku(sborka Sborka) (ItogPodgotovki, bool) {
	itog := ItogPodgotovki{Svezhaya: sborka.Teg, Stoit: y.Versiya()}
	if o, ok := y.chitatOtlozhennoe(); ok && o.Faza != "" {
		// Идёт замена или проба: отложенное трогать нельзя.
		itog.Otlozheno = true
		return itog, false
	}
	v := y.chitatVersiyu()
	if slices.Contains(v.Otvergnuty, sborka.Teg) {
		itog.Otvergnuto = true
		return itog, false
	}
	if itog.Stoit == sborka.Teg {
		y.ubratOtlozhennoeEsliStoit(sborka.Teg)
		return itog, false
	}
	// Сборка с версией не выше стоящей на замену не идёт никогда.
	if itog.Stoit != "" && !NoveeTega(sborka.Teg, itog.Stoit) {
		return itog, false
	}
	if o, ok := y.chitatOtlozhennoe(); ok && o.Teg == sborka.Teg {
		if _, err := os.Stat(y.putArhivaNovogo()); err == nil {
			itog.Otlozheno = true
			return itog, false
		}
	}
	return itog, true
}

// skachatPodpis — текст файла .sig по ссылке (не больше podpis.PredelRazmera).
func skachatPodpis(ctx context.Context, klient *http.Client, ssylka string) ([]byte, error) {
	if ssylka == "" {
		return nil, fmt.Errorf("%w: %v", obnovlenie.ErrPodpis, podpis.ErrNetPodpisi)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ssylka, nil)
	if err != nil {
		return nil, err
	}
	otvet, err := klient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("подпись ядра не скачалась: %w", err)
	}
	defer otvet.Body.Close()
	if otvet.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("подпись ядра не скачалась: %w", &obnovlenie.OshibkaStatusa{Kod: otvet.StatusCode})
	}
	return io.ReadAll(io.LimitReader(otvet.Body, podpis.PredelRazmera))
}

// toZheYadro — совпадает ли ядро из архива с файлом стоящего ядра.
func (y *Yadro) toZheYadro(arhiv string) (bool, error) {
	vremenny := y.Bin + ".sverka"
	defer os.Remove(vremenny)
	if err := raspakovat(arhiv, vremenny); err != nil {
		return false, err
	}
	sbrositKeshHesha()
	a, err := heshFaylaYadra(vremenny)
	if err != nil {
		return false, err
	}
	b, err := heshFaylaYadra(y.Bin)
	if err != nil {
		return false, err
	}
	return a == b, nil
}

// ubratOtlozhennoeEsliStoit убирает отложенный архив, если эта сборка уже
// стоит и замена не идёт.
func (y *Yadro) ubratOtlozhennoeEsliStoit(teg string) {
	o, ok := y.chitatOtlozhennoe()
	if !ok || o.Faza != "" {
		return
	}
	if o.Teg == teg {
		y.ubratOtlozhennoe()
	}
}

func (y *Yadro) ubratOtlozhennoe() {
	_ = os.Remove(y.putArhivaNovogo())
	_ = os.Remove(y.putOtlozhennogo())
}

// vernutPrezhnee ставит .prezhnee (последнее подтверждённое ядро) на место
// файла ядра. Переименование с заменой: файла ядра нет ни мгновения.
func (y *Yadro) vernutPrezhnee() error {
	if err := os.Rename(y.putPrezhnego(), y.Bin); err != nil {
		return err
	}
	sbrositKeshHesha()
	return nil
}

// otlozhitPrezhnee оставляет копию стоящего ядра рядом (.prezhnee), не
// трогая сам файл: жёсткая ссылка, а если не вышло — копия.
func (y *Yadro) otlozhitPrezhnee() error {
	if err := os.Link(y.Bin, y.putPrezhnego()); err == nil {
		return nil
	}
	vhod, err := os.Open(y.Bin)
	if err != nil {
		return err
	}
	defer vhod.Close()
	vyhod, err := os.OpenFile(y.putPrezhnego(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(vyhod, vhod); err != nil {
		vyhod.Close()
		_ = os.Remove(y.putPrezhnego())
		return err
	}
	if err := vyhod.Close(); err != nil {
		_ = os.Remove(y.putPrezhnego())
		return err
	}
	return nil
}

func estFaylMesto(put string) bool {
	_, err := os.Stat(put)
	return err == nil
}

// Smena — поставленная, но ещё не подтверждённая новая сборка ядра.
type Smena struct {
	y        *Yadro
	Teg      string
	Prezhniy string
}

// Smenit ставит отложенную сборку ядра на место (шаг 2, см. шапку файла).
// Звать только когда ядро этой копии не запущено. nil без ошибки — менять
// нечего. Прежнее ядро лежит рядом (.prezhnee) до Zakrepit или Otkatit.
//
// Замена журналируется в novoe.json (поле Faza): сперва записывается
// намерение, потом идут переименования. Оборвалась служба посреди — при
// следующем вызове состояние восстанавливается по журналу и хэшу файла:
// новое успело встать — проба продолжается (и считается попыткой), не
// успело — замена начинается заново. .prezhnee не удаляется, пока новое
// ядро не подтверждено рабочим (Zakrepit); возвращается оно переименованием
// с заменой, без мгновения, когда файла ядра нет.
func (y *Yadro) Smenit() (*Smena, error) {
	y.zamok.Lock()
	zanyato := y.derzh != nil
	y.zamok.Unlock()
	if zanyato {
		return nil, nil
	}
	y.zamokSmeny.Lock()
	defer y.zamokSmeny.Unlock()
	sbrositKeshHesha()

	o, ok := y.chitatOtlozhennoe()
	if !ok {
		return nil, nil
	}
	if o.Faza != "" {
		smena, err, dalshe := y.vosstanovitSmenu(&o)
		if !dalshe {
			return smena, err
		}
	}
	if !y.EstBinar() {
		return nil, nil // ядра нет вовсе — его поставит Zagruzit
	}
	// Живое ядро прошлой копии работает из того же файла (его меняют рядом,
	// smenitYadroPlavno): замена — в следующий подъём, когда ядра нет.
	if len(NashiYadra(y.Bin)) > 0 {
		return nil, nil
	}
	// Отложенная сборка не новее стоящей — ставить её нельзя.
	if tek := y.Versiya(); tek != "" && !NoveeTega(o.Teg, tek) {
		y.ubratOtlozhennoe()
		return nil, nil
	}
	if o.Popytok >= popytokSmeny {
		y.otvergnut(o.Teg)
		return nil, fmt.Errorf("сборка ядра %s не поднялась %d раза — больше не ставлю", o.Teg, o.Popytok)
	}
	arhiv := y.putArhivaNovogo()
	// Подпись проверяется ещё раз: архив пролежал на диске.
	if err := podpis.ProveritSTegom(arhiv, ImyaArhiva(), o.Teg, []byte(o.Podpis)); err != nil {
		y.ubratOtlozhennoe()
		return nil, fmt.Errorf("%w: отложенная сборка ядра %s (%v), убрана", obnovlenie.ErrPodpis, o.Teg, err)
	}
	novyy := y.Bin + ".novoe"
	_ = os.Remove(novyy)
	if err := raspakovat(arhiv, novyy); err != nil {
		y.ubratOtlozhennoe()
		return nil, fmt.Errorf("отложенная сборка ядра %s не распаковалась: %w", o.Teg, err)
	}
	sbrositKeshHesha()
	shaNovogo, err := heshFaylaYadra(novyy)
	if err != nil {
		_ = os.Remove(novyy)
		return nil, err
	}
	prezhniyTeg := y.Versiya()

	// 1. Намерение — в журнал, до любых переименований.
	o.Faza, o.ShaNovogo, o.Prezhniy = fazaMenyayu, shaNovogo, prezhniyTeg
	o.Popytok++
	if err := y.sohranitOtlozhennoe(o); err != nil {
		_ = os.Remove(novyy)
		return nil, fmt.Errorf("журнал замены ядра не записан: %w", err)
	}
	// 2. Прежнее ядро — рядом, сам файл ядра на месте.
	if err := y.otlozhitPrezhnee(); err != nil {
		_ = os.Remove(novyy)
		o.Faza = ""
		_ = y.sohranitOtlozhennoe(o)
		return nil, fmt.Errorf("прежнее ядро не отложить: %w", err)
	}
	// 3. Новое — на место файла ядра одним переименованием с заменой.
	if err := os.Rename(novyy, y.Bin); err != nil {
		_ = os.Remove(novyy)
		// Файл ядра не тронут, .prezhnee — его копия: убрать её можно.
		_ = os.Remove(y.putPrezhnego())
		o.Faza = ""
		_ = y.sohranitOtlozhennoe(o)
		return nil, fmt.Errorf("новое ядро не встало на место: %w", err)
	}
	sbrositKeshHesha()
	// 4. Проба.
	o.Faza = fazaProba
	_ = y.sohranitOtlozhennoe(o)
	y.zapisatVersiyu(o.Teg)
	return &Smena{y: y, Teg: o.Teg, Prezhniy: prezhniyTeg}, nil
}

// vosstanovitSmenu разбирает журнал замены, оставшийся от оборванной службы.
// dalshe — начать замену заново (новое не успело встать).
func (y *Yadro) vosstanovitSmenu(o *otlozhennoe) (*Smena, error, bool) {
	hesh, _ := heshFaylaYadra(y.Bin)
	if o.ShaNovogo != "" && hesh == o.ShaNovogo && estFaylMesto(y.putPrezhnego()) {
		// Новое ядро на месте, прежнее рядом: проба продолжается — и это
		// ещё одна попытка, иначе ядро, которое роняет службу, крутилось бы
		// бесконечно.
		if o.Popytok >= popytokSmeny {
			if err := y.vernutPrezhnee(); err != nil {
				return nil, fmt.Errorf("сборка ядра %s не подтвердилась %d раза, и прежнее ядро не вернуть: %w", o.Teg, o.Popytok, err), false
			}
			y.zapisatVersiyu(o.Prezhniy)
			y.otvergnut(o.Teg)
			return nil, fmt.Errorf("сборка ядра %s не подтвердилась %d раза — вернул прежнее, больше не ставлю", o.Teg, o.Popytok), false
		}
		o.Faza = fazaProba
		o.Popytok++
		_ = y.sohranitOtlozhennoe(*o)
		y.zapisatVersiyu(o.Teg)
		return &Smena{y: y, Teg: o.Teg, Prezhniy: o.Prezhniy}, nil, false
	}
	// Новое не встало (или уже откачено): на месте прежнее ядро, а
	// .prezhnee, если есть, — его же копия или последнее подтверждённое.
	if estFaylMesto(y.putPrezhnego()) {
		if err := y.vernutPrezhnee(); err != nil {
			return nil, fmt.Errorf("прежнее ядро не вернуть после обрыва замены: %w", err), false
		}
	}
	_ = os.Remove(y.Bin + ".novoe")
	o.Faza, o.ShaNovogo = "", ""
	_ = y.sohranitOtlozhennoe(*o)
	return nil, nil, true
}

// Zakrepit — новое ядро поднялось: прежний файл и отложенный архив больше
// не нужны.
func (s *Smena) Zakrepit() {
	s.y.zamokSmeny.Lock()
	defer s.y.zamokSmeny.Unlock()
	_ = os.Remove(s.y.putPrezhnego())
	s.y.ubratOtlozhennoe()
}

// Otkatit возвращает прежнее ядро на место. Отложенный архив остаётся: будет
// ли сборка отвергнута, решает вызывающий (Otvergnut), когда прежнее ядро
// поднимется.
func (s *Smena) Otkatit() error {
	y := s.y
	y.zamokSmeny.Lock()
	defer y.zamokSmeny.Unlock()
	if !estFaylMesto(y.putPrezhnego()) {
		return fmt.Errorf("прежнего ядра рядом нет")
	}
	if err := y.vernutPrezhnee(); err != nil {
		return fmt.Errorf("прежнее ядро не вернуть: %w", err)
	}
	y.zapisatVersiyu(s.Prezhniy)
	if o, ok := y.chitatOtlozhennoe(); ok {
		o.Faza, o.ShaNovogo = "", ""
		_ = y.sohranitOtlozhennoe(o)
	}
	return nil
}

// Otvergnut — сборка на этой машине не поднялась, а прежнее ядро поднялось:
// больше её не берём.
func (s *Smena) Otvergnut() {
	s.y.zamokSmeny.Lock()
	defer s.y.zamokSmeny.Unlock()
	s.y.otvergnut(s.Teg)
}

// otvergnut — под zamokSmeny.
func (y *Yadro) otvergnut(teg string) {
	v := y.chitatVersiyu()
	if !slices.Contains(v.Otvergnuty, teg) {
		v.Otvergnuty = append(v.Otvergnuty, teg)
	}
	if v.Sha256 == "" {
		if h, err := heshFaylaYadra(y.Bin); err == nil {
			v.Sha256 = h
		}
	}
	_ = y.sohranitVersiyu(v)
	y.ubratOtlozhennoe()
}
