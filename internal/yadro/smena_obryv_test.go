package yadro

import (
	"context"
	"net/http"
	"os"
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
