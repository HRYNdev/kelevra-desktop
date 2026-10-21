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
	// Popytok — сколько раз эту сборку уже ставили и откатывали.
	Popytok int `json:"popytok,omitempty"`
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
// может работать: файл ядра не трогается.
func (y *Yadro) Prigotovit(ctx context.Context, klient *http.Client) (ItogPodgotovki, error) {
	if !y.EstBinar() {
		return ItogPodgotovki{}, ErrNetYadra
	}
	sborka, err := y.vybratSborku(ctx, klient, true)
	if err != nil {
		return ItogPodgotovki{}, err
	}
	itog := ItogPodgotovki{Svezhaya: sborka.Teg, Stoit: y.Versiya()}
	v := y.chitatVersiyu()
	if slices.Contains(v.Otvergnuty, sborka.Teg) {
		itog.Otvergnuto = true
		return itog, nil
	}
	if itog.Stoit == sborka.Teg {
		y.ubratOtlozhennoeEsliStoit(sborka.Teg)
		return itog, nil
	}
	// Сборка с версией не выше стоящей на замену не идёт никогда.
	if itog.Stoit != "" && !NoveeTega(sborka.Teg, itog.Stoit) {
		return itog, nil
	}
	var uzhe otlozhennoe
	if chitatJSON(y.putOtlozhennogo(), &uzhe) && uzhe.Teg == sborka.Teg {
		if _, err := os.Stat(y.putArhivaNovogo()); err == nil {
			itog.Otlozheno = true
			return itog, nil
		}
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
	if err := hranenie.ZakrytOtPolzovateley(vremenny); err != nil {
		_ = os.Remove(vremenny)
		return itog, err
	}
	if err := skachat(ctx, klient, sborka.Ssylka, vremenny); err != nil {
		_ = os.Remove(vremenny)
		return itog, err
	}
	sig, err := skachatPodpis(ctx, klient, sborka.SsylkaPodpisi)
	if err != nil {
		_ = os.Remove(vremenny)
		return itog, err
	}
	if err := podpis.Proverit(vremenny, ImyaArhiva(), sig); err != nil {
		_ = os.Remove(vremenny)
		return itog, fmt.Errorf("%w: сборка ядра %s не отложена (%v)", obnovlenie.ErrPodpis, sborka.Teg, err)
	}

	// Ядро неизвестной версии: если это та же сборка — только записать версию.
	if itog.Stoit == "" {
		odinakovo, err := y.toZheYadro(vremenny)
		if err != nil {
			_ = os.Remove(vremenny)
			return itog, err
		}
		if odinakovo {
			_ = os.Remove(vremenny)
			y.zapisatVersiyu(sborka.Teg)
			y.ubratOtlozhennoeEsliStoit(sborka.Teg)
			itog.Stoit, itog.Priznano = sborka.Teg, true
			return itog, nil
		}
	}

	if err := os.Rename(vremenny, y.putArhivaNovogo()); err != nil {
		_ = os.Remove(vremenny)
		return itog, err
	}
	b, _ := json.Marshal(otlozhennoe{Teg: sborka.Teg, Podpis: string(sig)})
	if err := zapisatZakryto(y.putOtlozhennogo(), b); err != nil {
		_ = os.Remove(y.putArhivaNovogo())
		return itog, err
	}
	itog.Otlozheno = true
	return itog, nil
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
// стоит и отката не ждёт (прежнего файла рядом нет).
func (y *Yadro) ubratOtlozhennoeEsliStoit(teg string) {
	var o otlozhennoe
	if !chitatJSON(y.putOtlozhennogo(), &o) {
		return
	}
	if _, err := os.Stat(y.putPrezhnego()); err == nil && o.Teg == teg {
		return // идёт проба новой сборки — откат ещё может понадобиться
	}
	if o.Teg == teg || o.Teg == "" {
		y.ubratOtlozhennoe()
	}
}

func (y *Yadro) ubratOtlozhennoe() {
	_ = os.Remove(y.putArhivaNovogo())
	_ = os.Remove(y.putOtlozhennogo())
}

// Smena — поставленная, но ещё не подтверждённая новая сборка ядра.
type Smena struct {
	y        *Yadro
	Teg      string
	Prezhniy string
}

// Smenit ставит отложенную сборку ядра на место (шаг 2, см. шапку файла).
// Звать только когда ядро этой копии не запущено. nil без ошибки — менять
// нечего. Прежний файл ядра остаётся рядом до Zakrepit или Otkatit.
func (y *Yadro) Smenit() (*Smena, error) {
	y.zamok.Lock()
	zanyato := y.derzh != nil
	y.zamok.Unlock()
	if zanyato {
		return nil, nil
	}
	var o otlozhennoe
	if !chitatJSON(y.putOtlozhennogo(), &o) || o.Teg == "" {
		return nil, nil
	}
	if !y.EstBinar() {
		return nil, nil // ядра нет вовсе — его поставит Zagruzit
	}
	// Живое ядро прошлой копии работает из того же файла (его меняют рядом,
	// smenitYadroPlavno): отодвинутый файл сбил бы опознание своих ядер по
	// образу (NashiYadra). Замена — в следующий подъём, когда ядра нет.
	if len(NashiYadra(y.Bin)) > 0 {
		return nil, nil
	}
	// Проба этой сборки уже идёт (служба оборвалась посреди прошлой попытки):
	// новое ядро стоит, прежнее лежит рядом — продолжаем ту же пробу.
	if _, err := os.Stat(y.putPrezhnego()); err == nil && y.Versiya() == o.Teg {
		return &Smena{y: y, Teg: o.Teg, Prezhniy: o.Prezhniy}, nil
	}
	// Отложенная сборка не новее стоящей (стоящее ядро успело обновиться
	// иначе) — ставить её нельзя.
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
	if err := podpis.Proverit(arhiv, ImyaArhiva(), []byte(o.Podpis)); err != nil {
		y.ubratOtlozhennoe()
		return nil, fmt.Errorf("%w: отложенная сборка ядра %s (%v), убрана", obnovlenie.ErrPodpis, o.Teg, err)
	}
	novyy := y.Bin + ".novoe"
	_ = os.Remove(novyy)
	if err := raspakovat(arhiv, novyy); err != nil {
		y.ubratOtlozhennoe()
		return nil, fmt.Errorf("отложенная сборка ядра %s не распаковалась: %w", o.Teg, err)
	}
	prezhniyTeg := y.Versiya()
	_ = os.Remove(y.putPrezhnego())
	if err := os.Rename(y.Bin, y.putPrezhnego()); err != nil {
		_ = os.Remove(novyy)
		return nil, fmt.Errorf("прежнее ядро не отодвинуть: %w", err)
	}
	if err := os.Rename(novyy, y.Bin); err != nil {
		_ = os.Rename(y.putPrezhnego(), y.Bin)
		_ = os.Remove(novyy)
		return nil, fmt.Errorf("новое ядро не встало на место: %w", err)
	}
	o.Prezhniy = prezhniyTeg
	o.Popytok++
	if b, err := json.Marshal(o); err == nil {
		_ = zapisatZakryto(y.putOtlozhennogo(), b)
	}
	y.zapisatVersiyu(o.Teg)
	return &Smena{y: y, Teg: o.Teg, Prezhniy: prezhniyTeg}, nil
}

// Zakrepit — новое ядро поднялось: прежний файл и отложенный архив больше
// не нужны.
func (s *Smena) Zakrepit() {
	_ = os.Remove(s.y.putPrezhnego())
	s.y.ubratOtlozhennoe()
}

// Otkatit возвращает прежнее ядро на место. Отложенный архив остаётся: будет
// ли сборка отвергнута, решает вызывающий (Otvergnut), когда прежнее ядро
// поднимется.
func (s *Smena) Otkatit() error {
	y := s.y
	if _, err := os.Stat(y.putPrezhnego()); err != nil {
		return fmt.Errorf("прежнего ядра рядом нет: %w", err)
	}
	vremenny := y.Bin + ".otkat"
	_ = os.Remove(vremenny)
	if err := os.Rename(y.Bin, vremenny); err != nil {
		return fmt.Errorf("новое ядро не отодвинуть: %w", err)
	}
	if err := os.Rename(y.putPrezhnego(), y.Bin); err != nil {
		_ = os.Rename(vremenny, y.Bin)
		return fmt.Errorf("прежнее ядро не вернуть: %w", err)
	}
	_ = os.Remove(vremenny)
	y.zapisatVersiyu(s.Prezhniy) // сбрасывает и кеш хэша
	return nil
}

// Otvergnut — сборка на этой машине не поднялась, а прежнее ядро поднялось:
// больше её не берём.
func (s *Smena) Otvergnut() { s.y.otvergnut(s.Teg) }

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
