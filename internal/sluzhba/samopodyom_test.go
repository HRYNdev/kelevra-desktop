package sluzhba

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/fizset"
)

// stendSamo — ступень самоподъёма на подменах: часы, туннель, физическая
// сеть, ядро (pid) и перезапуск.
type stendSamo struct {
	t       *testing.T
	s       *Sluzhba
	mu      sync.Mutex
	seychas time.Time
	// tunnel — что видят замеры; svezhiy — что покажет свежий замер перед
	// перезапуском (nil — то же, что tunnel).
	tunnel  bool
	svezhiy *bool
	fiz     fizset.Itog
	gotov   bool
	pid     int
	// perezapuski — когда и с какой причиной перезапускали ядро.
	perezapuski []perezapuskSamo
}

type perezapuskSamo struct {
	kogda    time.Time
	prichina string
}

func novyyStendSamo(t *testing.T, nachalo time.Time) *stendSamo {
	t.Helper()
	st := &stendSamo{t: t, s: &Sluzhba{}, seychas: nachalo, tunnel: true, fiz: fizset.SetEst, gotov: true, pid: 100}
	h := &st.s.samo
	h.seychas = func() time.Time { st.mu.Lock(); defer st.mu.Unlock(); return st.seychas }
	h.gotov = func() bool { st.mu.Lock(); defer st.mu.Unlock(); return st.gotov }
	h.pidYadra = func() string { st.mu.Lock(); defer st.mu.Unlock(); return fmt.Sprint(st.pid) }
	h.tunnel = func(_ context.Context, svezhiy bool) (bool, bool) {
		st.mu.Lock()
		defer st.mu.Unlock()
		if svezhiy && st.svezhiy != nil {
			return *st.svezhiy, true
		}
		return st.tunnel, true
	}
	h.fiz = func(context.Context) fizset.Rezultat {
		st.mu.Lock()
		defer st.mu.Unlock()
		return fizset.Rezultat{Itog: st.fiz, Opisanie: "подмена: " + st.fiz.String()}
	}
	h.perezapustit = func(_ context.Context, prichina string) bool {
		st.mu.Lock()
		defer st.mu.Unlock()
		st.perezapuski = append(st.perezapuski, perezapuskSamo{st.seychas, prichina})
		st.pid++ // поднято новое ядро
		return true
	}
	return st
}

// shag — прошло d, шаг ступени.
func (st *stendSamo) shag(d time.Duration) {
	st.mu.Lock()
	st.seychas = st.seychas.Add(d)
	st.mu.Unlock()
	st.s.shagSamopodyoma(context.Background())
}

// idti — шаги по shagSamopodyoma, пока не пройдёт skolko.
func (st *stendSamo) idti(skolko time.Duration) {
	for proshlo := time.Duration(0); proshlo < skolko; proshlo += shagSamopodyoma {
		st.shag(shagSamopodyoma)
	}
}

func (st *stendSamo) postavit(f func()) {
	st.mu.Lock()
	f()
	st.mu.Unlock()
}

func (st *stendSamo) skolkoPerezapuskov() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.perezapuski)
}

// Живой случай 07.11 (ноут, сеть отца): Wi-Fi выключен 02:27:19 — туннель
// молчит, физической сети нет: ядро не трогаем. Wi-Fi включён 02:28:27, в
// 02:28:30 событие «сеть вернулась», сеть доказана мимо ядра, а туннель так
// и не ожил — перезапуск через ~30 с после возврата (человек нажал
// «Отключить → Подключить» только в 02:30:01). Строка журнала с диагнозом.
func TestSamopodyomZhivoySluchay0711(t *testing.T) {
	z := zhurnalDlyaProverki(t)
	msk := time.FixedZone("MSK", 3*3600)
	st := novyyStendSamo(t, time.Date(2026, 11, 7, 2, 26, 45, 0, msk))
	st.idti(30 * time.Second) // 02:27:15 — туннель жив
	if d := st.s.Diagnoz(); d != DiagnozOk {
		t.Fatalf("туннель жив — диагноз %q", d)
	}
	// 02:27:19 Wi-Fi выключен.
	st.postavit(func() { st.tunnel, st.fiz = false, fizset.NetSeti })
	for st.seychas.Before(time.Date(2026, 11, 7, 2, 28, 27, 0, msk)) {
		st.shag(shagSamopodyoma)
	}
	if n := st.skolkoPerezapuskov(); n != 0 {
		t.Fatalf("нет сети — ядро перезапущено %d раз", n)
	}
	if d := st.s.Diagnoz(); d != DiagnozNetSeti {
		t.Fatalf("нет сети — диагноз %q", d)
	}
	// 02:28:27 Wi-Fi включён; 02:28:30 событие сети; туннель мёртв дальше.
	st.postavit(func() { st.fiz = fizset.SetEst; st.seychas = time.Date(2026, 11, 7, 2, 28, 30, 0, msk) })
	st.s.otmetitSmenuSeti()
	st.s.shagSamopodyoma(context.Background())
	for st.skolkoPerezapuskov() == 0 && st.seychas.Before(time.Date(2026, 11, 7, 2, 31, 0, 0, msk)) {
		st.shag(shagSamopodyoma)
	}
	if st.skolkoPerezapuskov() != 1 {
		t.Fatalf("сеть вернулась, туннель мёртв — ждали один перезапуск, было %d", st.skolkoPerezapuskov())
	}
	p := st.perezapuski[0]
	ot := p.kogda.Sub(time.Date(2026, 11, 7, 2, 28, 30, 0, msk))
	if ot != porogPosleSmenySeti {
		t.Fatalf("перезапуск через %s после возврата сети, ждали %s (не обычные %s)", ot, porogPosleSmenySeti, lestnicaSamopodyoma[0])
	}
	if !p.kogda.Before(time.Date(2026, 11, 7, 2, 30, 1, 0, msk)) {
		t.Fatalf("перезапуск в %s — не раньше человека", p.kogda.Format("15:04:05"))
	}
	if !strings.Contains(p.prichina, "сеть есть, туннель мёртв 30 с — перезапускаю ядро") {
		t.Fatalf("причина: %q", p.prichina)
	}
	if d := st.s.Diagnoz(); d != DiagnozTunnelMertv {
		t.Fatalf("диагноз перед перезапуском %q", d)
	}
	zh := z.String()
	if strings.Count(zh, "физической сети нет") != 1 {
		t.Fatalf("строка «нет сети» — ровно одна:\n%s", zh)
	}
	// Новое ядро ответило — туннель жив.
	st.postavit(func() { st.tunnel = true })
	st.shag(shagSamopodyoma)
	if d := st.s.Diagnoz(); d != DiagnozOk {
		t.Fatalf("после перезапуска диагноз %q", d)
	}
	if !strings.Contains(z.String(), "самоподъём: туннель ожил") {
		t.Fatalf("нет строки об оживлении:\n%s", z.String())
	}
}

// Без смены сети: туннель умер при живой сети — перезапуск через 45 с.
func TestSamopodyomPorog45(t *testing.T) {
	st := novyyStendSamo(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	st.shag(shagSamopodyoma)
	st.postavit(func() { st.tunnel = false })
	nachalo := st.seychas.Add(shagSamopodyoma)
	for st.skolkoPerezapuskov() == 0 && st.seychas.Sub(nachalo) < 5*time.Minute {
		st.shag(shagSamopodyoma)
	}
	if st.skolkoPerezapuskov() != 1 {
		t.Fatal("туннель мёртв при живой сети 5 мин — перезапуска нет")
	}
	if ot := st.perezapuski[0].kogda.Sub(nachalo); ot != lestnicaSamopodyoma[0] {
		t.Fatalf("перезапуск через %s, ждали %s", ot, lestnicaSamopodyoma[0])
	}
}

// Нет сети — ядро не трогаем, сколько бы туннель ни молчал. Сеть есть, а
// узлы напрямую молчат (узел лежит, оператор режет) — тоже.
func TestSamopodyomNetSetiNeTrogaet(t *testing.T) {
	for _, c := range []struct {
		fiz     fizset.Itog
		diagnoz string
	}{{fizset.NetSeti, DiagnozNetSeti}, {fizset.UzlyNedostupny, DiagnozUzlyNedostupny}} {
		st := novyyStendSamo(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
		st.postavit(func() { st.tunnel, st.fiz = false, c.fiz })
		st.idti(30 * time.Minute)
		if n := st.skolkoPerezapuskov(); n != 0 {
			t.Fatalf("%v: ядро перезапущено %d раз", c.fiz, n)
		}
		if d := st.s.Diagnoz(); d != c.diagnoz {
			t.Fatalf("%v: диагноз %q, ждали %q", c.fiz, d, c.diagnoz)
		}
	}
}

// Туннель ожил сам раньше ступени — ядро не трогаем, счёт заново.
func TestSamopodyomOzhilSam(t *testing.T) {
	st := novyyStendSamo(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	for i := 0; i < 5; i++ {
		st.postavit(func() { st.tunnel = false })
		st.idti(30 * time.Second) // 30 с мёртв (< 45)
		st.postavit(func() { st.tunnel = true })
		st.shag(shagSamopodyoma)
	}
	if n := st.skolkoPerezapuskov(); n != 0 {
		t.Fatalf("туннель оживал сам — ядро перезапущено %d раз", n)
	}
}

// Перед самым перезапуском свежий замер ответил (очередь перешла на живой
// узел) — ядро не трогаем.
func TestSamopodyomSvezhiyZamerOzhil(t *testing.T) {
	st := novyyStendSamo(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	da := true
	st.postavit(func() { st.tunnel, st.svezhiy = false, &da })
	st.idti(3 * time.Minute)
	if n := st.skolkoPerezapuskov(); n != 0 {
		t.Fatalf("свежий замер жив — ядро перезапущено %d раз", n)
	}
}

// Лестница: туннель не оживает и после перезапусков — ступени 45 с, 2, 5,
// 15, 15 мин (от первого шага на новом ядре); туннель жив 10 мин — снова
// с 45 с.
func TestSamopodyomLestnica(t *testing.T) {
	st := novyyStendSamo(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	st.postavit(func() { st.tunnel = false })
	for st.skolkoPerezapuskov() < 5 && st.seychas.Before(time.Date(2026, 11, 7, 14, 0, 0, 0, time.UTC)) {
		st.shag(shagSamopodyoma)
	}
	if st.skolkoPerezapuskov() != 5 {
		t.Fatalf("перезапусков %d, ждали 5", st.skolkoPerezapuskov())
	}
	stupeni := []time.Duration{2 * time.Minute, 5 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	for i, s := range stupeni {
		promezh := st.perezapuski[i+1].kogda.Sub(st.perezapuski[i].kogda)
		// Счёт на новом ядре — с первого его шага.
		if promezh != s+shagSamopodyoma {
			t.Fatalf("между перезапусками %d и %d прошло %s, ждали %s", i+1, i+2, promezh, s+shagSamopodyoma)
		}
	}
	if !strings.Contains(st.perezapuski[4].prichina, "ступень 5") {
		t.Fatalf("причина пятого: %q", st.perezapuski[4].prichina)
	}
	// Ожил на 9 мин — лестница не сброшена; на 10 — сброшена.
	st.postavit(func() { st.tunnel = true })
	st.idti(9 * time.Minute)
	st.postavit(func() { st.tunnel = false })
	posle := st.skolkoPerezapuskov()
	st.idti(2 * time.Minute)
	if st.skolkoPerezapuskov() != posle {
		t.Fatal("туннель жил 9 мин — лестница сброшена раньше 10 мин")
	}
	st.postavit(func() { st.tunnel = true })
	st.idti(sbrosSamopodyomaPosle + shagSamopodyoma)
	st.postavit(func() { st.tunnel = false })
	nachalo := st.seychas.Add(shagSamopodyoma)
	for st.skolkoPerezapuskov() == posle && st.seychas.Sub(nachalo) < 20*time.Minute {
		st.shag(shagSamopodyoma)
	}
	if ot := st.perezapuski[posle].kogda.Sub(nachalo); ot != lestnicaSamopodyoma[0] {
		t.Fatalf("после 10 мин живого туннеля перезапуск через %s, ждали %s", ot, lestnicaSamopodyoma[0])
	}
}

// Смена сети, после которой туннель ожил, а потом умер, — обычная ступень
// 45 с, а не 30.
func TestSamopodyomSmenaSetiNeVSchetPosleOzhivleniya(t *testing.T) {
	st := novyyStendSamo(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	st.s.otmetitSmenuSeti()
	st.shag(shagSamopodyoma) // жив
	st.postavit(func() { st.tunnel = false })
	nachalo := st.seychas.Add(shagSamopodyoma)
	for st.skolkoPerezapuskov() == 0 && st.seychas.Sub(nachalo) < 5*time.Minute {
		st.shag(shagSamopodyoma)
	}
	if ot := st.perezapuski[0].kogda.Sub(nachalo); ot != lestnicaSamopodyoma[0] {
		t.Fatalf("перезапуск через %s, ждали %s", ot, lestnicaSamopodyoma[0])
	}
}

// Туннель не поднят (Отключить, дома по автомату, идёт подъём) — ничего не
// мерим, диагноза нет; выключатель пульта — то же.
func TestSamopodyomNeGotov(t *testing.T) {
	st := novyyStendSamo(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	st.postavit(func() { st.tunnel, st.gotov = false, false })
	st.idti(10 * time.Minute)
	if st.skolkoPerezapuskov() != 0 || st.s.Diagnoz() != "" {
		t.Fatalf("туннель не поднят: перезапусков %d, диагноз %q", st.skolkoPerezapuskov(), st.s.Diagnoz())
	}
}

// Общий замок переходов занят (сторож, «Дом», кнопка) — перезапуска нет,
// ступень не потрачена.
func TestSamopodyomZamokZanyat(t *testing.T) {
	s := &Sluzhba{}
	s.zamokPodyoma.Lock()
	defer s.zamokPodyoma.Unlock()
	if s.perezapustitSamopodyomom(context.Background(), "проверка", 0) {
		t.Fatal("замок занят — перезапуск не должен считаться сделанным")
	}
}

// Главная группа: селектор очереди узлов; выбрана локальная комната — не
// судим (перезапуск ядра её не лечит).
func TestGlavnayaGruppa(t *testing.T) {
	konf := []byte(`{"outbounds":[
	 {"type":"selector","tag":"Соединение","outbounds":["Нидерланды","Финляндия","Комната"]},
	 {"type":"urltest","tag":"Нидерланды","outbounds":["a"]},
	 {"type":"urltest","tag":"Финляндия","outbounds":["b"]},
	 {"type":"urltest","tag":"Дом · мост","outbounds":["a"]},
	 {"type":"vless","tag":"a","server":"203.0.113.1"},
	 {"type":"vless","tag":"b","server":"203.0.113.2"},
	 {"type":"socks","tag":"Комната","server":"127.0.0.1","server_port":8808},
	 {"type":"direct","tag":"direct"}]}`)
	g, mozhno := glavnayaGruppa(konf)
	if g != "Соединение" {
		t.Fatalf("группа %q", g)
	}
	if !mozhno("Нидерланды") || mozhno("Комната") || mozhno("direct") {
		t.Fatal("комната и direct — не туннель для суждения")
	}
	g, _ = glavnayaGruppa([]byte(`{"outbounds":[{"type":"urltest","tag":"Дом · мост"},{"type":"urltest","tag":"Узел"}]}`))
	if g != "Узел" {
		t.Fatalf("без селектора — первая группа не «Дома»: %q", g)
	}
}

// Диагноз в /api/sostoyanie — поле для пульта и журнала; окно его не
// рисует (облик не знает такого поля).
func TestSostoyanieDiagnoz(t *testing.T) {
	s := stend(t)
	s.samo.mu.Lock()
	s.samo.diagnoz = DiagnozTunnelMertv
	s.samo.mu.Unlock()
	m := s.Obsluzhit()
	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	var o otvetSostoyaniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	if o.Diagnoz != DiagnozTunnelMertv {
		t.Fatalf("diagnoz %q", o.Diagnoz)
	}
	if !strings.Contains(s.Diagnostika(), "diagnoz="+DiagnozTunnelMertv) {
		t.Fatalf("в диагностике пульта нет диагноза: %s", s.Diagnostika())
	}
	oblik, err := os.ReadDir("oblik")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range oblik {
		if f.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join("oblik", f.Name()))
		if err == nil && strings.Contains(string(b), "diagnoz") {
			t.Fatalf("окно (%s) рисует diagnoz — поле не для окна", f.Name())
		}
	}
}

// Смену физической сети ступени сообщает слежка отложенного перезапуска.
func TestSamopodyomSmenuSetiVidit(t *testing.T) {
	st := novyyStendOtlozh(t)
	st.smenitSet("wifi-dom") // слежка завелась и запомнила сеть
	st.smenitSet("wifi-drugaya")
	srok := time.Now().Add(5 * time.Second)
	for {
		st.s.samo.mu.Lock()
		est := !st.s.samo.smenaSeti.IsZero()
		st.s.samo.mu.Unlock()
		if est {
			return
		}
		if time.Now().After(srok) {
			t.Fatal("смена сети до ступени самоподъёма не дошла")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
