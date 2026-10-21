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
	// StarayaPodpis — сборка, подписанная прежним форматом (без тега):
	// тег и sha256 её файла .sig. Пока .sig тот же, архив заново не качается.
	StarayaPodpis *starayaPodpis `json:"staraya_podpis,omitempty"`
}

type starayaPodpis struct {
	Teg        string `json:"teg"`
	ShaPodpisi string `json:"sha_podpisi"`
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
	// ShaPrezhnego — sha256 прежнего ядра: .prezhnee возвращается на место
	// только при совпадении (обрезанная копия не встанет поверх целого ядра).
	ShaPrezhnego string `json:"sha_prezhnego,omitempty"`
	// PodyomIdet — подъём на новом ядре начат и ещё не дал исхода. Застали
	// это при восстановлении — служба оборвалась посреди подъёма: попытка.
	PodyomIdet bool `json:"podyom_idet,omitempty"`
}

const (
	fazaMenyayu = "menyayu"
	fazaProba   = "proba"
	// fazaOtkaz — сборка отвергнута, вернуть прежнее не вышло, а отодвинуть
	// её файл с места ядра тоже не вышло: повторить при следующем подъёме.
	fazaOtkaz = "otkaz"
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

func heshBayt(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
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
	// StarayaPodpis — сборка подписана прежним форматом (без тега): на
	// замену не годится, архив не качается, пока её .sig не сменится.
	StarayaPodpis bool
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

	// Сперва маленький .sig: сборка, уже опознанная как подписанная прежним
	// форматом, с тем же .sig заново не качается (архив — десятки мегабайт,
	// и так на каждой машине раз в несколько часов).
	sig, err := skachatPodpis(ctx, klient, sborka.SsylkaPodpisi)
	if err != nil {
		return itog, err
	}
	shaPodpisi := heshBayt(sig)
	if sp := y.chitatVersiyu().StarayaPodpis; sp != nil && sp.Teg == sborka.Teg && sp.ShaPodpisi == shaPodpisi {
		itog.StarayaPodpis = true
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
	if err := podpis.ProveritSTegom(vremenny, ImyaArhiva(), sborka.Teg, sig); err != nil {
		// Подпись прежнего формата сходится — сборка настоящая, просто
		// подписана без тега: запомнить и больше не качать. Не сходится
		// никакая — архив подменён: не запоминать, сверить в следующий раз.
		if podpis.Proverit(vremenny, ImyaArhiva(), sig) == nil {
			y.zamokSmeny.Lock()
			v := y.chitatVersiyu()
			v.StarayaPodpis = &starayaPodpis{Teg: sborka.Teg, ShaPodpisi: shaPodpisi}
			_ = y.sohranitVersiyu(v)
			y.zamokSmeny.Unlock()
			itog.StarayaPodpis = true
			return itog, nil
		}
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

// vernutPrezhnee ставит .prezhnee на место файла ядра — только если его
// хэш совпал с записанным в журнале (sha). Не совпал — .prezhnee испорчен
// (обрыв посреди копии) и убирается. Переименование с заменой: файла ядра
// нет ни мгновения.
func (y *Yadro) vernutPrezhnee(sha string) error {
	sbrositKeshHesha()
	hesh, err := heshFaylaYadra(y.putPrezhnego())
	if err != nil {
		return err
	}
	if sha == "" || hesh != sha {
		_ = os.Remove(y.putPrezhnego())
		return fmt.Errorf("прежнее ядро рядом не сходится с журналом — убрано")
	}
	if err := os.Rename(y.putPrezhnego(), y.Bin); err != nil {
		return err
	}
	sbrositKeshHesha()
	return nil
}

// ubratStaroePrezhnee убирает .prezhnee, оставшийся без пробы (Zakrepit не
// смог его удалить). Не удаляется — отодвигается под другим именем.
func (y *Yadro) ubratStaroePrezhnee() error {
	if !estFaylMesto(y.putPrezhnego()) {
		return nil
	}
	if err := os.Remove(y.putPrezhnego()); err == nil {
		return nil
	}
	for i := 1; i <= 9; i++ {
		kuda := fmt.Sprintf("%s.%d", y.putPrezhnego(), i)
		if estFaylMesto(kuda) {
			_ = os.Remove(kuda)
			if estFaylMesto(kuda) {
				continue
			}
		}
		if err := os.Rename(y.putPrezhnego(), kuda); err == nil {
			return nil
		}
	}
	return fmt.Errorf("старый %s не убрать", filepath.Base(y.putPrezhnego()))
}

// otlozhitPrezhnee оставляет копию стоящего ядра рядом (.prezhnee), не
// трогая сам файл: жёсткая ссылка, а если не вышло — копия через
// временный файл с fsync и переименованием (обрезанной копии под именем
// .prezhnee не бывает).
func (y *Yadro) otlozhitPrezhnee() error {
	if err := os.Link(y.Bin, y.putPrezhnego()); err == nil {
		return nil
	}
	vremenny := y.putPrezhnego() + ".tmp"
	_ = os.Remove(vremenny)
	vhod, err := os.Open(y.Bin)
	if err != nil {
		return err
	}
	defer vhod.Close()
	vyhod, err := os.OpenFile(vremenny, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(vyhod, vhod)
	if err == nil {
		err = vyhod.Sync()
	}
	if cerr := vyhod.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(vremenny, y.putPrezhnego())
	}
	if err != nil {
		_ = os.Remove(vremenny)
	}
	return err
}

func estFaylMesto(put string) bool {
	_, err := os.Stat(put)
	return err == nil
}

// Smena — поставленная, но ещё не подтверждённая новая сборка ядра.
// Выдаётся одному вызывающему за раз; он обязан закончить её одним из
// Zakrepit, Otkatit (+Otvergnut) или Otpustit.
type Smena struct {
	y        *Yadro
	Teg      string
	Prezhniy string
}

// Smenit ставит отложенную сборку ядра на место (шаг 2, см. шапку файла).
// Звать только когда ядро этой копии не запущено. nil без ошибки — менять
// нечего или проба уже у другого вызывающего. Прежнее ядро лежит рядом
// (.prezhnee) до Zakrepit или Otkatit.
//
// Замена журналируется в novoe.json (поле Faza): сперва записывается
// намерение, потом идут переименования. Оборвалась служба посреди — при
// следующем вызове состояние восстанавливается по журналу и хэшам файлов:
// новое успело встать — проба продолжается (обрыв посреди подъёма — это
// попытка), не успело — замена начинается заново. .prezhnee не удаляется,
// пока новое ядро не подтверждено рабочим, и возвращается только при
// совпадении хэша.
//
// Попыткой считается только настоящий отказ запуска нового ядра (Otkatit)
// и обрыв службы посреди подъёма на нём. Сбои файлов до старта нового ядра
// и чужие ошибки подъёма (Otpustit) попыток не тратят.
func (y *Yadro) Smenit() (*Smena, error) {
	y.zamok.Lock()
	zanyato := y.derzh != nil
	y.zamok.Unlock()
	if zanyato {
		return nil, nil
	}
	y.zamokSmeny.Lock()
	defer y.zamokSmeny.Unlock()
	if y.probaVydana {
		return nil, nil
	}
	sbrositKeshHesha()

	o, ok := y.chitatOtlozhennoe()
	if !ok {
		return nil, nil
	}
	// Живое ядро прошлой копии работает из того же файла (его меняют рядом,
	// smenitYadroPlavno): ни замены, ни восстановления — в следующий подъём,
	// когда ядра нет.
	if y.EstBinar() && len(NashiYadra(y.Bin)) > 0 {
		return nil, nil
	}
	// Прошлый раз упавшую сборку не удалось отодвинуть — ещё раз.
	if o.Faza == fazaOtkaz {
		sbrositKeshHesha()
		hesh, _ := heshFaylaYadra(y.Bin)
		if y.EstBinar() && hesh == o.ShaNovogo {
			if err := y.otodvinutOtvergnutoe(); err != nil {
				return nil, fmt.Errorf("упавшую сборку ядра %s не отодвинуть: %w", o.Teg, err)
			}
		}
		y.otvergnut(o.Teg)
		return nil, nil
	}
	if o.Faza != "" {
		smena, err, dalshe := y.vosstanovitSmenu(&o)
		if !dalshe {
			if smena != nil {
				y.probaVydana = true
			}
			return smena, err
		}
	}
	if !y.EstBinar() {
		return nil, nil // ядра нет вовсе — его поставит Zagruzit
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
	// Без пробы (Faza "") .prezhnee — мусор (Zakrepit не смог удалить):
	// иначе ссылка и копия падали бы на каждой замене.
	if err := y.ubratStaroePrezhnee(); err != nil {
		return nil, err
	}
	novyy := y.Bin + ".novoe"
	_ = os.Remove(novyy)
	if err := raspakovat(arhiv, novyy); err != nil {
		// Архив подписан и цел: ошибка — диск или права. Отложенное
		// остаётся, замена повторится при следующем подъёме.
		return nil, fmt.Errorf("отложенная сборка ядра %s не распаковалась: %w", o.Teg, err)
	}
	sbrositKeshHesha()
	shaNovogo, err := heshFaylaYadra(novyy)
	if err != nil {
		_ = os.Remove(novyy)
		return nil, err
	}
	sbrositKeshHesha()
	shaPrezhnego, err := heshFaylaYadra(y.Bin)
	if err != nil {
		_ = os.Remove(novyy)
		return nil, err
	}
	prezhniyTeg := y.Versiya()

	// 1. Намерение — в журнал, до любых переименований.
	o.Faza, o.ShaNovogo, o.ShaPrezhnego, o.Prezhniy = fazaMenyayu, shaNovogo, shaPrezhnego, prezhniyTeg
	if err := y.sohranitOtlozhennoe(o); err != nil {
		_ = os.Remove(novyy)
		return nil, fmt.Errorf("журнал замены ядра не записан: %w", err)
	}
	otmenit := func() {
		_ = os.Remove(novyy)
		o.Faza, o.ShaNovogo, o.ShaPrezhnego = "", "", ""
		_ = y.sohranitOtlozhennoe(o)
	}
	// 2. Прежнее ядро — рядом, сам файл ядра на месте.
	if err := y.otlozhitPrezhnee(); err != nil {
		otmenit()
		return nil, fmt.Errorf("прежнее ядро не отложить: %w", err)
	}
	// 3. Новое — на место файла ядра одним переименованием с заменой.
	if err := os.Rename(novyy, y.Bin); err != nil {
		// Файл ядра не тронут, .prezhnee — его копия: убрать её можно.
		_ = os.Remove(y.putPrezhnego())
		otmenit()
		return nil, fmt.Errorf("новое ядро не встало на место: %w", err)
	}
	sbrositKeshHesha()
	// 4. Проба: подъём на новом ядре начат.
	// Версия ядра не пишется до подтверждения (Zakrepit): стоящим
	// считается только ядро, которое поднялось.
	o.Faza, o.PodyomIdet = fazaProba, true
	_ = y.sohranitOtlozhennoe(o)
	y.probaVydana = true
	return &Smena{y: y, Teg: o.Teg, Prezhniy: prezhniyTeg}, nil
}

// vosstanovitSmenu разбирает журнал замены, оставшийся от прошлого подъёма.
// dalshe — начать замену заново (новое не успело встать). Под zamokSmeny.
func (y *Yadro) vosstanovitSmenu(o *otlozhennoe) (*Smena, error, bool) {
	hesh, _ := heshFaylaYadra(y.Bin)
	if o.ShaNovogo != "" && hesh == o.ShaNovogo && estFaylMesto(y.putPrezhnego()) {
		// Новое ядро на месте, прежнее рядом: проба продолжается. Если
		// прошлый подъём на нём оборвался вместе со службой — это попытка,
		// иначе ядро, которое роняет службу, крутилось бы бесконечно.
		if o.PodyomIdet {
			o.Popytok++
		}
		if o.Popytok >= popytokSmeny {
			if err := y.vernutPrezhnee(o.ShaPrezhnego); err != nil {
				return nil, fmt.Errorf("сборка ядра %s не подтвердилась %d раза, и прежнее ядро не вернуть: %w", o.Teg, o.Popytok, err), false
			}
			y.zapisatVersiyu(o.Prezhniy)
			y.otvergnut(o.Teg)
			return nil, fmt.Errorf("сборка ядра %s не подтвердилась %d раза — вернул прежнее, больше не ставлю", o.Teg, o.Popytok), false
		}
		o.Faza, o.PodyomIdet = fazaProba, true
		_ = y.sohranitOtlozhennoe(*o)
		return &Smena{y: y, Teg: o.Teg, Prezhniy: o.Prezhniy}, nil, false
	}
	// Новое не встало (или уже откачено). Если файла ядра нет или на месте
	// не прежнее — вернуть .prezhnee (только при совпадении хэша).
	if estFaylMesto(y.putPrezhnego()) {
		if hesh == o.ShaPrezhnego {
			// На месте и так прежнее, .prezhnee — его копия.
			_ = os.Remove(y.putPrezhnego())
		} else if err := y.vernutPrezhnee(o.ShaPrezhnego); err != nil && !y.EstBinar() {
			return nil, fmt.Errorf("прежнее ядро не вернуть после обрыва замены: %w", err), false
		}
	}
	_ = os.Remove(y.Bin + ".novoe")
	o.Faza, o.ShaNovogo, o.ShaPrezhnego, o.PodyomIdet = "", "", "", false
	_ = y.sohranitOtlozhennoe(*o)
	return nil, nil, true
}

// ErrNovoeYadroPropalo — на месте файла ядра не та сборка, что ставили
// (новое ядро убрали с диска, например антивирус).
var ErrNovoeYadroPropalo = errors.New("на месте ядра не новая сборка")

// Zakrepit — подъём удался. Подтверждается, только если на месте ядра
// именно новая сборка; иначе (новое убрали с диска, Zagruzit вернул
// прежнее) сборка отвергается с записью, а не объявляется рабочей.
func (s *Smena) Zakrepit() error {
	y := s.y
	y.zamokSmeny.Lock()
	defer y.zamokSmeny.Unlock()
	defer func() { y.probaVydana = false }()
	o, _ := y.chitatOtlozhennoe()
	sbrositKeshHesha()
	hesh, _ := heshFaylaYadra(y.Bin)
	if o.ShaNovogo == "" || hesh != o.ShaNovogo {
		if estFaylMesto(y.putPrezhnego()) && o.ShaPrezhnego != "" {
			_ = y.ubratStaroePrezhnee()
		}
		y.otvergnut(s.Teg)
		return fmt.Errorf("%w: сборка %s", ErrNovoeYadroPropalo, s.Teg)
	}
	y.zapisatVersiyu(s.Teg)
	_ = y.ubratStaroePrezhnee()
	y.ubratOtlozhennoe()
	return nil
}

// Otpustit — подъём кончился чужой ошибкой (не отказом запуска нового
// ядра): проба остаётся, попытка не тратится, следующий подъём её продолжит.
func (s *Smena) Otpustit() {
	y := s.y
	y.zamokSmeny.Lock()
	defer y.zamokSmeny.Unlock()
	y.probaVydana = false
	if o, ok := y.chitatOtlozhennoe(); ok && o.PodyomIdet {
		o.PodyomIdet = false
		_ = y.sohranitOtlozhennoe(o)
	}
}

// Otkatit — новое ядро не запустилось: прежнее возвращается на место, это
// попытка. Отложенный архив остаётся: будет ли сборка отвергнута, решает
// вызывающий (Otvergnut), когда прежнее ядро поднимется.
func (s *Smena) Otkatit() error {
	y := s.y
	y.zamokSmeny.Lock()
	defer y.zamokSmeny.Unlock()
	y.probaVydana = false
	o, _ := y.chitatOtlozhennoe()
	var err error
	if !estFaylMesto(y.putPrezhnego()) {
		err = fmt.Errorf("прежнего ядра рядом нет")
	} else if verr := y.vernutPrezhnee(o.ShaPrezhnego); verr != nil {
		err = fmt.Errorf("прежнее ядро не вернуть: %w", verr)
	}
	if err != nil {
		// Вернуть нечего, а новое не запускается: сборка отвергается, её
		// файл отодвигается — следующий подъём поставит последнюю
		// подписанную НЕ отвергнутую сборку (Zagruzit), а не оставит
		// упавшую стоящим ядром. Не отодвинулся — журнал остаётся (фаза
		// otkaz), повтор при следующем подъёме.
		if rerr := y.otodvinutOtvergnutoe(); rerr != nil {
			y.otmetitOtvergnutoy(s.Teg)
			o.Faza, o.PodyomIdet = fazaOtkaz, false
			_ = y.sohranitOtlozhennoe(o)
			return fmt.Errorf("%v; упавшую сборку не отодвинуть: %w", err, rerr)
		}
		y.otvergnut(s.Teg)
		return fmt.Errorf("%w; сборка %s отвергнута и отодвинута", err, s.Teg)
	}
	y.zapisatVersiyu(s.Prezhniy)
	if o.Teg != "" {
		o.Popytok++
		o.Faza, o.ShaNovogo, o.ShaPrezhnego, o.PodyomIdet = "", "", "", false
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

// otodvinutOtvergnutoe убирает файл упавшей сборки с места ядра.
func (y *Yadro) otodvinutOtvergnutoe() error {
	otodvinuto := y.Bin + ".otvergnutoe"
	_ = os.Remove(otodvinuto)
	if err := os.Rename(y.Bin, otodvinuto); err != nil {
		return err
	}
	sbrositKeshHesha()
	return nil
}

// otmetitOtvergnutoy — запись об отказе без уборки журнала. Под zamokSmeny.
func (y *Yadro) otmetitOtvergnutoy(teg string) {
	v := y.chitatVersiyu()
	if !slices.Contains(v.Otvergnuty, teg) {
		v.Otvergnuty = append(v.Otvergnuty, teg)
		_ = y.sohranitVersiyu(v)
	}
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

// prezhneeIzZhurnala — вернуть .prezhnee, если файла ядра нет (Zagruzit).
// Только по хэшу из журнала; без журнала — не возвращается.
func (y *Yadro) prezhneeIzZhurnala() bool {
	if y.EstBinar() || !estFaylMesto(y.putPrezhnego()) {
		return false
	}
	o, ok := y.chitatOtlozhennoe()
	if !ok || o.ShaPrezhnego == "" {
		return false
	}
	if err := y.vernutPrezhnee(o.ShaPrezhnego); err != nil {
		return false
	}
	if o.Prezhniy != "" {
		y.zapisatVersiyu(o.Prezhniy)
	}
	return true
}
