package yadro

import (
	"context"
	"errors"
	"net/http"
	"os"
	"runtime"
	"slices"
	"testing"
)

// gotovoKSmene — стоит v1 (core-v1-1), отложена подписанная core-v2-1.
func gotovoKSmene(t *testing.T) *Yadro {
	t.Helper()
	y := stendSmeny(t, "v1", sborkaStenda{teg: "core-v2-1", yadro: "v2", podpisano: "v2"})
	y.zapisatVersiyu("core-v1-1")
	if itog, err := y.Prigotovit(context.Background(), http.DefaultClient); err != nil || !itog.Otlozheno {
		t.Fatalf("подготовка: %+v, %v", itog, err)
	}
	return y
}

// novayaKopiya — та же папка глазами копии службы, поднятой после обрыва.
func novayaKopiya(y *Yadro) *Yadro {
	return &Yadro{Papka: y.Papka, Bin: y.Bin, Spisok: y.Spisok}
}

func (y *Yadro) faza(t *testing.T) otlozhennoe {
	t.Helper()
	o, _ := y.chitatOtlozhennoe()
	return o
}

// Обрыв после переименований, но до записи версии и фазы пробы: следующий
// подъём продолжает пробу, прежнее ядро (.prezhnee) не удаляется, откат
// возвращает именно его.
func TestObryvPosleZamenyDoZapisiVersii(t *testing.T) {
	y := gotovoKSmene(t)
	if s, err := y.Smenit(); err != nil || s == nil {
		t.Fatalf("замена: %v", err)
	}
	// Откатываем журнал и записку о версии к состоянию «только что
	// переименовали»: фаза — намерение, версия — прежняя (с хэшем v1).
	o := y.faza(t)
	o.Faza = fazaMenyayu
	if err := y.sohranitOtlozhennoe(o); err != nil {
		t.Fatal(err)
	}
	v := y.chitatVersiyu()
	v.Teg = "core-v1-1"
	_ = y.sohranitVersiyu(v)

	y2 := novayaKopiya(y)
	s, err := y2.Smenit()
	if err != nil || s == nil {
		t.Fatalf("проба не продолжилась: %v", err)
	}
	if got := soderzhimoe(t, y.putPrezhnego()); got != "v1" {
		t.Fatalf("прежнее ядро потеряно: .prezhnee = %q", got)
	}
	if got := soderzhimoe(t, y.Bin); got != "v2" {
		t.Fatalf("на месте %q", got)
	}
	if err := s.Otkatit(); err != nil || soderzhimoe(t, y.Bin) != "v1" {
		t.Fatalf("откат после обрыва: %v", err)
	}
}

// Обрыв после записи намерения, до переименования: файл ядра — прежний,
// замена начинается заново и доходит до конца.
func TestObryvDoPereimenovaniya(t *testing.T) {
	y := gotovoKSmene(t)
	o := y.faza(t)
	o.Faza, o.ShaNovogo, o.Prezhniy = fazaMenyayu, "nevazhno", "core-v1-1"
	if err := y.sohranitOtlozhennoe(o); err != nil {
		t.Fatal(err)
	}
	if err := y.otlozhitPrezhnee(); err != nil { // жёсткая ссылка успела появиться
		t.Fatal(err)
	}
	s, err := novayaKopiya(y).Smenit()
	if err != nil || s == nil {
		t.Fatalf("замена после обрыва: %v", err)
	}
	if soderzhimoe(t, y.Bin) != "v2" || soderzhimoe(t, y.putPrezhnego()) != "v1" {
		t.Fatal("после повторной замены файлы не на своих местах")
	}
	if f := y.faza(t); f.Faza != fazaProba {
		t.Fatalf("фаза %q", f.Faza)
	}
}

// Новое ядро роняет службу на каждом подъёме: продолжение пробы считается
// попыткой; после предела возвращается прежнее и сборка отвергается.
func TestProbaSObryvamiOgranichena(t *testing.T) {
	y := gotovoKSmene(t)
	if s, err := y.Smenit(); err != nil || s == nil {
		t.Fatal(err)
	}
	var err error
	for i := 0; i < popytokSmeny+1; i++ {
		var s *Smena
		s, err = novayaKopiya(y).Smenit() // служба упала, не подтвердив и не откатив
		if s == nil {
			break
		}
	}
	if err == nil {
		t.Fatal("проба с обрывами крутится без конца")
	}
	if got := soderzhimoe(t, y.Bin); got != "v1" {
		t.Fatalf("после предела на месте %q, ждал прежнее", got)
	}
	if estFayl(y.putArhivaNovogo()) {
		t.Fatal("отвергнутая сборка осталась отложенной")
	}
	if itog, _ := y.Prigotovit(context.Background(), http.DefaultClient); !itog.Otvergnuto {
		t.Fatalf("сборка не отвергнута: %+v", itog)
	}
}

// Файла ядра нет, прежнее лежит рядом — Zagruzit возвращает его, не качая.
func TestZagruzitVozvrashchaetPrezhnee(t *testing.T) {
	y := gotovoKSmene(t)
	if s, err := y.Smenit(); err != nil || s == nil {
		t.Fatal(err)
	}
	if err := os.Remove(y.Bin); err != nil {
		t.Fatal(err)
	}
	y.Spisok = "http://127.0.0.1:1/net" // сеть не нужна
	if err := y.Zagruzit(context.Background()); err != nil {
		t.Fatalf("прежнее не вернулось: %v", err)
	}
	if got := soderzhimoe(t, y.Bin); got != "v1" {
		t.Fatalf("на месте %q", got)
	}
}

// Пока идёт проба, сверка не трогает отложенный архив и журнал.
func TestPodgotovkaNeLomaetProbu(t *testing.T) {
	y := stendSmeny(t, "v1",
		sborkaStenda{teg: "core-v3-1", yadro: "v3", podpisano: "v3"},
	)
	y.zapisatVersiyu("core-v1-1")
	if _, err := y.Prigotovit(context.Background(), http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	if s, err := y.Smenit(); err != nil || s == nil {
		t.Fatal(err)
	}
	do := y.faza(t)
	if _, err := y.Prigotovit(context.Background(), http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	if posle := y.faza(t); posle != do || !estFayl(y.putPrezhnego()) {
		t.Fatalf("сверка во время пробы тронула журнал: было %+v, стало %+v", do, posle)
	}
}

// Обрезанный .prezhnee (обрыв посреди копии) не встаёт поверх ядра: откат
// отказывает, испорченный файл убирается.
func TestObrezannoePrezhneeNeVozvrashchaetsya(t *testing.T) {
	y := gotovoKSmene(t)
	s, err := y.Smenit()
	if err != nil || s == nil {
		t.Fatal(err)
	}
	os.Remove(y.putPrezhnego()) // жёсткую ссылку рвём, пишем «обрезок»
	if err := os.WriteFile(y.putPrezhnego(), []byte("v"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Otkatit(); err == nil {
		t.Fatal("обрезанное прежнее ядро встало на место")
	}
	if estFayl(y.Bin) {
		t.Fatalf("на месте ядра осталось %q", soderzhimoe(t, y.Bin))
	}
	if estFayl(y.putPrezhnego()) {
		t.Fatal("испорченный .prezhnee остался")
	}
}

// Файла ядра нет, а .prezhnee не сходится с журналом — Zagruzit его не
// ставит (качает заново).
func TestZagruzitNeStavitChuzhoePrezhnee(t *testing.T) {
	y := gotovoKSmene(t)
	if s, err := y.Smenit(); err != nil || s == nil {
		t.Fatal(err)
	}
	os.Remove(y.putPrezhnego())
	os.WriteFile(y.putPrezhnego(), []byte("chuzhoe"), 0o755)
	os.Remove(y.Bin)
	if err := y.Zagruzit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := soderzhimoe(t, y.Bin); got == "chuzhoe" {
		t.Fatal("несверенный .prezhnee встал на место ядра")
	}
}

// Висящий .prezhnee без пробы (Zakrepit не смог удалить) не мешает
// следующей замене.
func TestVisyashcheePrezhneeNeMeshaet(t *testing.T) {
	y := gotovoKSmene(t)
	os.WriteFile(y.putPrezhnego(), []byte("musor"), 0o755)
	s, err := y.Smenit()
	if err != nil || s == nil {
		t.Fatalf("замена при висящем .prezhnee: %v", err)
	}
	if got := soderzhimoe(t, y.putPrezhnego()); got != "v1" {
		t.Fatalf(".prezhnee = %q, ждал прежнее ядро", got)
	}
}

// Новое ядро убрали с диска (антивирус), на месте прежнее: подъём удался,
// но подтверждения нет — сборка отвергается с записью, без цикла.
func TestNovoeYadroPropaloNeZakreplyaetsya(t *testing.T) {
	y := gotovoKSmene(t)
	s, err := y.Smenit()
	if err != nil || s == nil {
		t.Fatal(err)
	}
	os.Remove(y.Bin)
	if !y.prezhneeIzZhurnala() {
		t.Fatal("прежнее не вернулось")
	}
	if err := s.Zakrepit(); !errors.Is(err, ErrNovoeYadroPropalo) {
		t.Fatalf("закреплено при чужом файле ядра: %v", err)
	}
	if itog, _ := y.Prigotovit(context.Background(), http.DefaultClient); !itog.Otvergnuto {
		t.Fatalf("сборка не отвергнута: %+v", itog)
	}
}

// Проба выдаётся одному вызывающему; отпущенная (чужая ошибка) попытку не
// тратит и продолжается следующим подъёмом.
func TestProbaOdnomuIOtpustit(t *testing.T) {
	y := gotovoKSmene(t)
	s, err := y.Smenit()
	if err != nil || s == nil {
		t.Fatal(err)
	}
	if s2, _ := y.Smenit(); s2 != nil {
		t.Fatal("проба выдана второму вызывающему")
	}
	for i := 0; i < popytokSmeny+2; i++ {
		s.Otpustit()
		s, err = y.Smenit()
		if err != nil || s == nil {
			t.Fatalf("отпущенная проба %d не продолжилась: %v", i+1, err)
		}
	}
	if o := y.faza(t); o.Popytok != 0 {
		t.Fatalf("чужие ошибки потратили %d попыток", o.Popytok)
	}
	if err := s.Zakrepit(); err != nil {
		t.Fatal(err)
	}
}

// Ревью, круг 3: откат не удался (прежнего рядом нет). Упавшая сборка не
// становится стоящим ядром: она отвергнута, её файл отодвинут, следующая
// установка ставит последнюю подписанную НЕ отвергнутую сборку.
func TestRev3OtkatNeUdalsyaNovoeStanovitsyaPostoyannym(t *testing.T) {
	y := stendSmeny(t, "v1",
		sborkaStenda{teg: "core-v2-1", yadro: "v2", podpisano: "v2"},
		sborkaStenda{teg: "core-v1-1", yadro: "v1", podpisano: "v1"},
	)
	y.zapisatVersiyu("core-v1-1")
	if itog, err := y.Prigotovit(context.Background(), http.DefaultClient); err != nil || !itog.Otlozheno {
		t.Fatalf("подготовка: %+v, %v", itog, err)
	}
	s, err := y.Smenit()
	if err != nil || s == nil {
		t.Fatalf("замена: %v", err)
	}
	os.Remove(y.putPrezhnego()) // .prezhnee пропал (антивирус/очистка)
	if err := s.Otkatit(); err == nil {
		t.Fatal("откат без прежнего прошёл")
	}
	if s2, _ := y.Smenit(); s2 != nil {
		t.Fatal("упавшая сборка снова выдана на пробу")
	}
	if _, est := y.chitatOtlozhennoe(); est {
		t.Fatal("журнал упавшей сборки остался")
	}
	if v := y.chitatVersiyu(); len(v.Otvergnuty) != 1 || v.Otvergnuty[0] != "core-v2-1" {
		t.Fatalf("отметки об отказе нет: %v", v.Otvergnuty)
	}
	if estFayl(y.Bin) || y.Versiya() == "core-v2-1" {
		t.Fatal("упавшая сборка осталась стоящим ядром")
	}
	if err := y.Zagruzit(context.Background()); err != nil {
		t.Fatalf("установка после отказа: %v", err)
	}
	if got := soderzhimoe(t, y.Bin); got != "v1" || y.Versiya() != "core-v1-1" {
		t.Fatalf("после отказа поставлено %q (%q), ждал последнюю не отвергнутую core-v1-1", got, y.Versiya())
	}
}

// Откат не удался, и файл упавшей сборки не отодвинуть (занят): журнал
// остаётся, сборка отмечена отвергнутой, следующая попытка отодвигает.
func TestOtodvinutOtvergnutoePovtor(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("занятый файл не переименовать только на Windows")
	}
	y := gotovoKSmene(t)
	s, err := y.Smenit()
	if err != nil || s == nil {
		t.Fatal(err)
	}
	os.Remove(y.putPrezhnego())
	f, err := os.Open(y.Bin) // держим файл ядра — переименование не пройдёт
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Otkatit(); err == nil {
		t.Fatal("откат без прежнего прошёл")
	}
	f.Close()
	if o, est := y.chitatOtlozhennoe(); !est || o.Faza != fazaOtkaz {
		t.Skipf("файл переименовался и открытым (фаза %q) — сценарий на этой ФС не воспроизвести", o.Faza)
	}
	if v := y.chitatVersiyu(); !slices.Contains(v.Otvergnuty, "core-v2-1") {
		t.Fatal("отказ не записан")
	}
	if s2, err := y.Smenit(); s2 != nil || err != nil {
		t.Fatalf("повтор: %v, %v", s2, err)
	}
	if estFayl(y.Bin) || !estFayl(y.Bin+".otvergnutoe") {
		t.Fatal("упавшая сборка не отодвинута при повторе")
	}
	if _, est := y.chitatOtlozhennoe(); est {
		t.Fatal("журнал не убран после повтора")
	}
}
