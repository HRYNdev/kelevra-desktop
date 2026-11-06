package yadro

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/lzheyadro"
)

// Слежка за работающим ядром (slezhka.go) на лже-ядре (cmd/lzhe_yadro):
// упало само — Padenie с кодом выхода и причиной из журнала; зависло (процесс
// жив, Clash API молчит) — погашено, и Padenie объявлено только ПОСЛЕ выхода
// процесса (иначе новое ядро встало бы рядом с живым старым — #168).

// yadroNaLzhe — ядро в своей папке на лже-ядре с режимом rezhim.
func yadroNaLzhe(t *testing.T, rezhim string, env map[string]string) (*Yadro, chan Padenie) {
	t.Helper()
	papka := t.TempDir()
	bin := lzheyadro.V(t, papka)
	adres := lzheyadro.SvobodnyyAdres(t)
	if err := os.WriteFile(filepath.Join(papka, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LZHE_REZHIM", rezhim)
	t.Setenv("LZHE_ADRES", adres)
	for k, v := range env {
		t.Setenv(k, v)
	}
	padeniya := make(chan Padenie, 4)
	y := &Yadro{Bin: bin, Papka: papka, Api: adres,
		ShagSlezhki: 100 * time.Millisecond, PorogZavisaniya: 30 * time.Second,
		PriPadenii: func(p Padenie) { padeniya <- p }}
	t.Cleanup(func() { _ = y.Ostanovit() })
	return y, padeniya
}

func zhdatPadeniya(t *testing.T, padeniya chan Padenie, srok time.Duration) Padenie {
	t.Helper()
	select {
	case p := <-padeniya:
		return p
	case <-time.After(srok):
		t.Fatalf("за %s падения не было", srok)
	}
	return Padenie{}
}

func TestSlezhkaSvoyoUpalo(t *testing.T) {
	y, padeniya := yadroNaLzhe(t, "padenie", map[string]string{
		"LZHE_POSLE_MS": "600", "LZHE_KOD": "7", "LZHE_STROKA": "лже-ядро: старт"})
	if err := y.Zapustit(context.Background()); err != nil {
		t.Fatalf("не запустилось: %v", err)
	}
	p := zhdatPadeniya(t, padeniya, 10*time.Second)
	if !p.Svoyo || p.Zavislo || p.Kod != 7 {
		t.Fatalf("не то падение: %+v", p)
	}
	if !strings.Contains(p.Hvost, "падаю с кодом 7") {
		t.Fatalf("причина не из журнала ядра: %q", p.Hvost)
	}
	if y.Sost() != Slomalos {
		t.Fatalf("состояние после падения: %s", y.Sost())
	}
	// Ядро пишет журнал само: строки ядра и строки службы о запуске и выходе.
	b, err := os.ReadFile(y.PutZhurnala())
	if err != nil {
		t.Fatal(err)
	}
	for _, nuzhno := range []string{"лже-ядро: старт", "падаю с кодом 7", "kelevra: ядро запущено, pid", "вышел, код 7"} {
		if !strings.Contains(string(b), nuzhno) {
			t.Errorf("в журнале ядра нет «%s»:\n%s", nuzhno, b)
		}
	}
}

// Падение на старте — дело подъёма (ошибка Zapustit), а не сторожа.
func TestSlezhkaPadenieNaStarteNeSobytie(t *testing.T) {
	y, padeniya := yadroNaLzhe(t, "padenie", map[string]string{"LZHE_POSLE_MS": "0", "LZHE_KOD": "2"})
	y.Api = lzheyadro.SvobodnyyAdres(t) // API там, где ядро его не поднимет
	err := y.Zapustit(context.Background())
	if err == nil || !strings.Contains(err.Error(), "упало при старте") {
		t.Fatalf("ожидал отказ старта, получил %v", err)
	}
	select {
	case p := <-padeniya:
		t.Fatalf("падение на старте объявлено сторожу: %+v", p)
	case <-time.After(500 * time.Millisecond):
	}
}

// Остановка нами — не падение.
func TestSlezhkaOstanovkaNePadenie(t *testing.T) {
	y, padeniya := yadroNaLzhe(t, "rabota", nil)
	if err := y.Zapustit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := y.Ostanovit(); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-padeniya:
		t.Fatalf("остановка объявлена падением: %+v", p)
	case <-time.After(700 * time.Millisecond):
	}
}

func TestSlezhkaSvoyoZavisloGasitsyaDoSobytiya(t *testing.T) {
	y, padeniya := yadroNaLzhe(t, "zavis", map[string]string{"LZHE_POSLE_MS": "300"})
	y.PorogZavisaniya = 700 * time.Millisecond
	if err := y.Zapustit(context.Background()); err != nil {
		t.Fatal(err)
	}
	pid := y.PID()
	p := zhdatPadeniya(t, padeniya, 15*time.Second)
	if !p.Zavislo || !p.Svoyo {
		t.Fatalf("не то падение: %+v", p)
	}
	if pid == "" || p.PID == 0 {
		t.Fatal("нет номера процесса")
	}
	if zhiv, _, znaem := processZhiv(p.PID); znaem && zhiv {
		t.Fatalf("Padenie объявлено, а зависший процесс %d ещё жив", p.PID)
	}
	if y.Sost() != Slomalos {
		t.Fatalf("состояние: %s", y.Sost())
	}
}

// Короткое молчание API (меньше порога) — не зависание.
func TestSlezhkaKorotkoeMolchanieNeZavisanie(t *testing.T) {
	y, padeniya := yadroNaLzhe(t, "rabota", nil)
	y.PorogZavisaniya = 5 * time.Second
	if err := y.Zapustit(context.Background()); err != nil {
		t.Fatal(err)
	}
	// API молчит секунду: ядро будто занято, но живо.
	y.Api = lzheyadro.SvobodnyyAdres(t)
	time.Sleep(time.Second)
	select {
	case p := <-padeniya:
		t.Fatalf("секунда молчания объявлена зависанием: %+v", p)
	default:
	}
	if y.Sost() != Rabotaet {
		t.Fatalf("состояние: %s", y.Sost())
	}
}

// prinyatoeLzhe — лже-ядро, запущенное «прошлой копией» (этим тестом), и
// записка о нём. Тест сам дожидается процесса: на unix иначе убитый ребёнок
// остался бы зомби и считался живым.
func prinyatoeLzhe(t *testing.T, y *Yadro) Peredacha {
	t.Helper()
	cmd := exec.Command(y.Bin)
	cmd.Dir = y.Papka
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	srok := time.Now().Add(10 * time.Second)
	for !y.Zhivo() {
		if time.Now().After(srok) {
			t.Fatal("лже-ядро не подняло API")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return Peredacha{PID: cmd.Process.Pid, Bin: y.Bin, Api: y.Api}
}

func TestSlezhkaPrinyatoeZavisloGasitsya(t *testing.T) {
	y, padeniya := yadroNaLzhe(t, "zavis", map[string]string{"LZHE_POSLE_MS": "400"})
	y.PorogZavisaniya = 700 * time.Millisecond
	p := prinyatoeLzhe(t, y)
	if itog := y.Prinyat(p, ""); !itog.Prinyato {
		t.Fatalf("не принято: %s", itog.Pochemu)
	}
	pd := zhdatPadeniya(t, padeniya, 20*time.Second)
	if !pd.Zavislo || pd.Svoyo || pd.PID != p.PID {
		t.Fatalf("не то падение: %+v", pd)
	}
	if zhiv, _, znaem := processZhiv(p.PID); znaem && zhiv {
		t.Fatalf("Padenie объявлено, а зависшее принятое ядро %d живо", p.PID)
	}
	if _, est := ProchestPeredachu(y.Papka); est {
		t.Error("записка о погашенном ядре осталась")
	}
}

// Принятое ядро вышло само — замечено сразу по ОС, не дожидаясь порога
// зависания.
func TestSlezhkaPrinyatoeUpalo(t *testing.T) {
	y, padeniya := yadroNaLzhe(t, "padenie", map[string]string{"LZHE_POSLE_MS": "500", "LZHE_KOD": "5"})
	y.PorogZavisaniya = time.Minute
	p := prinyatoeLzhe(t, y)
	if itog := y.Prinyat(p, ""); !itog.Prinyato {
		t.Fatalf("не принято: %s", itog.Pochemu)
	}
	pd := zhdatPadeniya(t, padeniya, 10*time.Second)
	if pd.Zavislo || pd.Svoyo {
		t.Fatalf("не то падение: %+v", pd)
	}
	if runtime.GOOS == "windows" && pd.Kod != 5 {
		t.Errorf("код выхода принятого ядра: %d", pd.Kod)
	}
}

func TestZapretitZapusk(t *testing.T) {
	y := &Yadro{}
	y.ZapretitZapusk()
	if err := y.Zapustit(context.Background()); !errors.Is(err, ErrOstanovleno) {
		t.Fatalf("после запрета Zapustit: %v", err)
	}
}

// Ротация хранит несколько прошлых запусков: падения подряд (сторож) и
// лечение «Дома» не затирают журнал той беды за минуты.
func TestZhurnalYadraHranitNeskolkoProshlyh(t *testing.T) {
	papka := t.TempDir()
	for i := 1; i <= skolkoProshlyh+2; i++ {
		f, err := sozdatZhurnalYadra(papka)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString("запуск " + string(rune('0'+i)) + "\n")
		f.Close()
	}
	put := filepath.Join(papka, imyaZhurnala)
	posledniy := skolkoProshlyh + 2
	for n := 1; n <= skolkoProshlyh; n++ {
		b, err := os.ReadFile(putProshlogo(put, n))
		if err != nil {
			t.Fatalf("нет %d-го прошлого журнала: %v", n, err)
		}
		if want := "запуск " + string(rune('0'+posledniy-n)); !strings.Contains(string(b), want) {
			t.Fatalf("%d-й прошлый: ждал «%s», там %q", n, want, b)
		}
	}
	if _, err := os.Stat(putProshlogo(put, skolkoProshlyh+1)); err == nil {
		t.Fatal("прошлых журналов больше предела")
	}
}

// Предел размера: живое ядро держит журнал открытым, служба уводит его
// содержимое в .proshlyy и усекает файл, а ядро продолжает писать с начала —
// без дыры из нулей (дозапись).
func TestObrezatZhurnalPodZhivymYadrom(t *testing.T) {
	y, _ := yadroNaLzhe(t, "zavis", map[string]string{"LZHE_POSLE_MS": "1500", "LZHE_STROKA": "лже-ядро: до обрезки"})
	if err := y.Zapustit(context.Background()); err != nil {
		t.Fatal(err)
	}
	srok := time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(y.PutZhurnala())
		if strings.Contains(string(b), "до обрезки") {
			break
		}
		if time.Now().After(srok) {
			t.Fatalf("ядро не написало первую строку: %q", b)
		}
		time.Sleep(50 * time.Millisecond)
	}
	obrezan, err := ObrezatZhurnal(y.Papka, 10)
	if err != nil || !obrezan {
		t.Fatalf("обрезка: %v %v", obrezan, err)
	}
	b, err := os.ReadFile(y.PutProshlogoZhurnala())
	if err != nil || !strings.Contains(string(b), "до обрезки") {
		t.Fatalf("содержимое не уехало в .proshlyy: %q %v", b, err)
	}
	srok = time.Now().Add(5 * time.Second)
	for {
		b, _ = os.ReadFile(y.PutZhurnala())
		if strings.Contains(string(b), "зависаю") {
			break
		}
		if time.Now().After(srok) {
			t.Fatalf("после обрезки ядро в журнал не пишет: %q", b)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if strings.Contains(string(b), "до обрезки") || strings.ContainsRune(string(b), 0) {
		t.Fatalf("после обрезки в журнале старое или дыра из нулей: %q", b)
	}
	if ok, _ := ObrezatZhurnal(y.Papka, 1<<20); ok {
		t.Fatal("маленький журнал обрезан")
	}
}

// Шум http2 в причину падения не попадает.
func TestHvostBezShuma(t *testing.T) {
	put := filepath.Join(t.TempDir(), "yadro.log")
	tekst := "ERROR [1 2ms] connection: http2: response body closed\n" +
		"ERROR [2 1ms] inbound/tun[tun-in]: настоящая беда\n" +
		"ERROR [3 3ms] connection: http2: response body closed\n"
	if err := os.WriteFile(put, []byte(tekst), 0o600); err != nil {
		t.Fatal(err)
	}
	h := hvostLoga(put)
	if strings.Contains(h, "response body closed") || !strings.Contains(h, "настоящая беда") {
		t.Fatalf("хвост: %q", h)
	}
}
