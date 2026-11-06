package sluzhba

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// pultStend — подставной Clash API для щелчка и сверки моста: что выбрано в
// селекторах и все принятые переключения по порядку.
type pultStend struct {
	mu     sync.Mutex
	vybor  map[string]string
	spisok []string
	// oshibkaPut — любое переключение отвергнуто; molchit — API не отвечает
	// вовсе; zalip — с заглушки селектор не уходит, хотя ядро отвечает «да».
	oshibkaPut error
	molchit    bool
	zalip      bool
	// naZaglushku — зовётся на каждое принятое переключение на заглушку.
	naZaglushku func()
}

// novyyPultStend — селектор щелчка стоит на выходе моста профиля телефона
// без порталов («Соединение»), как у ядра, только что поднятого с default.
func novyyPultStend() *pultStend {
	return &pultStend{vybor: map[string]string{konfig.TegShchelchkaDoma: "Соединение"}}
}

func (p *pultStend) Vybrano(g string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.molchit {
		return "", errors.New("ядро молчит")
	}
	return p.vybor[g], nil
}

func (p *pultStend) VybratZa(g, u string, _ time.Duration) error {
	p.mu.Lock()
	if p.molchit || p.oshibkaPut != nil {
		err := p.oshibkaPut
		if p.molchit {
			err = errors.New("ядро молчит")
		}
		p.mu.Unlock()
		return err
	}
	p.spisok = append(p.spisok, g+" → "+u)
	if !(p.zalip && p.vybor[g] == konfig.TegZaglushkiDoma) {
		p.vybor[g] = u
	}
	hook := p.naZaglushku
	p.mu.Unlock()
	if u == konfig.TegZaglushkiDoma && hook != nil {
		hook()
	}
	return nil
}

func (p *pultStend) zapisi() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.spisok...)
}

func (p *pultStend) seychas() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.vybor[konfig.TegShchelchkaDoma]
}

func (p *pultStend) postavit(u string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vybor[konfig.TegShchelchkaDoma] = u
}

func zhurnalDlyaProverki(t *testing.T) *syncBuffer {
	t.Helper()
	z := &syncBuffer{}
	log.SetOutput(z)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return z
}

// Щелчок помог: на заглушку и обратно на выход моста (проверено ответом
// ядра), ядро не тронуто; следующая проба удачна — в журнале, за сколько
// секунд ожил дом, лестница сброшена.
func TestDomShchelchokOzhil(t *testing.T) {
	st := novyyStendLecheniya(t)
	z := zhurnalDlyaProverki(t)
	for i := 0; i < 3; i++ {
		st.proba()
	}
	st.zhdat(t, 1, 0, "три неудачи")
	if got := st.pult.zapisi(); len(got) != 2 || got[0] != konfig.TegShchelchkaDoma+" → "+konfig.TegZaglushkiDoma ||
		got[1] != konfig.TegShchelchkaDoma+" → Соединение" {
		t.Fatalf("переключения %v", got)
	}
	if st.pult.seychas() != "Соединение" {
		t.Fatalf("после щелчка селектор на «%s»", st.pult.seychas())
	}
	st.sejchas = st.sejchas.Add(ShagSlezhkiZaDomom)
	st.otvet = nil
	st.proba()
	st.zhdat(t, 1, 0, "дом ожил")
	if !strings.Contains(z.String(), "дом: на связи после щелчка моста за 30 с") {
		t.Fatalf("оживления после щелчка нет в журнале:\n%s", z.String())
	}
	if st.s.dom.pauzaLecheniya != 0 || !st.s.dom.shchelknuliV.IsZero() {
		t.Fatalf("лестница не сброшена: пауза %v, щелчок %v", st.s.dom.pauzaLecheniya, st.s.dom.shchelknuliV)
	}
	// Снова молчит — снова щелчок сразу по порогу, без паузы.
	st.otvet = errors.New("молчит")
	for i := 0; i < 3; i++ {
		st.proba()
	}
	st.zhdat(t, 2, 0, "новый сбой после оживления")
}

// Ядро не приняло переключение на заглушку — щелчка не было, перезапуск
// ядра сразу, в той же пробе.
func TestDomShchelchokNeVyshel(t *testing.T) {
	st := novyyStendLecheniya(t)
	st.pult.oshibkaPut = errors.New("ядро не приняло выбор (400)")
	for i := 0; i < 3; i++ {
		st.proba()
	}
	st.zhdat(t, 0, 1, "щелчок отвергнут")
	if p := st.perezapuski[0]; !strings.Contains(p, "мост не щёлкнулся") || !strings.Contains(p, "перезапускаю ядро") {
		t.Fatalf("причина: %q", p)
	}
}

// Селектор не вернулся с заглушки (ядро говорит «да», а now — заглушка):
// перезапуск ядра сразу; на следующем шаге слежки сверка возвращает мост.
func TestDomShchelchokNeVernulsya(t *testing.T) {
	st := novyyStendLecheniya(t)
	z := zhurnalDlyaProverki(t)
	st.pult.zalip = true
	for i := 0; i < 3; i++ {
		st.proba()
	}
	st.zhdat(t, 1, 1, "селектор застрял на заглушке")
	if p := st.perezapuski[0]; !strings.Contains(p, "мост не щёлкнулся") || !strings.Contains(p, "обратно на «Соединение» не вернулся") {
		t.Fatalf("причина: %q", p)
	}
	st.pult.mu.Lock()
	st.pult.zalip = false
	st.pult.mu.Unlock()
	st.proba()
	if st.pult.seychas() != "Соединение" {
		t.Fatalf("сверка не вернула мост: %q", st.pult.seychas())
	}
	if !strings.Contains(z.String(), "стоял на «"+konfig.TegZaglushkiDoma+"» (на шаге слежки) — вернул на «Соединение»") {
		t.Fatalf("возврата моста нет в журнале:\n%s", z.String())
	}
}

// Конфиг собран прежней версией (ядро принято работающим после обновления):
// селектора щелчка нет — лечит перезапуск, как раньше, Clash API не трогается.
func TestDomShchelchokStaryyKonfig(t *testing.T) {
	st := novyyStendLecheniya(t)
	syroy, err := os.ReadFile(st.s.Yadro.PutKonfiga())
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(syroy, &d); err != nil {
		t.Fatal(err)
	}
	for _, e := range d["endpoints"].([]any) {
		if e.(map[string]any)["tag"] == konfig.TegDoma {
			e.(map[string]any)["detour"] = "Соединение"
		}
	}
	syroy, _ = json.Marshal(d)
	if err := os.WriteFile(st.s.Yadro.PutKonfiga(), syroy, 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		st.proba()
	}
	st.zhdat(t, 0, 1, "старый конфиг")
	if p := st.perezapuski[0]; !strings.HasPrefix(p, "нет связи (неудачных проб подряд: 3) — перезапускаю ядро") {
		t.Fatalf("причина: %q", p)
	}
	if got := st.pult.zapisi(); len(got) != 0 {
		t.Fatalf("старый конфиг, а селектор щёлкали: %v", got)
	}
}

// Кеш выбора: служба умерла между двумя щелчками, ядро поднялось из
// cache_file на заглушке. Подъём ядра сам возвращает мост на выход — дом не
// мёртв навсегда.
func TestDomZaglushkaIzKeshaPoslePodyoma(t *testing.T) {
	s := gotovStendLestnicy(t)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s.dom.kesh.Klyuchi = []ed25519.PublicKey{priv.Public().(ed25519.PublicKey)}
	s.Nastroyki.Slot = "01-7"
	pult := novyyPultStend()
	s.dom.pult = pult
	s.dom.rabotaetDlyaStenda = func() bool { return true }
	s.zashchitaPodnyataDlyaStenda = func() bool { return true }
	zapuskov := 0
	s.zapustitYadro = func(ctx context.Context) error {
		zapuskov++
		pult.postavit(konfig.TegZaglushkiDoma) // ядро взяло выбор из cache_file
		return nil
	}
	z := zhurnalDlyaProverki(t)
	s.prinyatDom(&podpiska.Svedeniya{Dom: blokDoma(t, s, priv, 1, "100.76.0.1:51820")})
	s.dom.perezapuski.Wait()
	if zapuskov != 1 {
		t.Fatalf("запусков ядра %d", zapuskov)
	}
	if pult.seychas() != "Соединение" {
		t.Fatalf("после подъёма мост на «%s», ждали «Соединение»", pult.seychas())
	}
	if !strings.Contains(z.String(), "(после подъёма ядра) — вернул на «Соединение»") {
		t.Fatalf("возврата моста нет в журнале:\n%s", z.String())
	}
}

// То же, если ядро пережило службу (принято работающим) или его селектор
// переключили мимо службы: сверка на шаге слежки, раньше пробы. Clash API
// молчит — шаг идёт дальше, ничего не ломая.
func TestDomZaglushkaNaShageSlezhki(t *testing.T) {
	st := novyyStendLecheniya(t)
	st.otvet = nil
	st.pult.postavit(konfig.TegZaglushkiDoma)
	st.proba()
	if st.pult.seychas() != "Соединение" {
		t.Fatalf("мост на «%s»", st.pult.seychas())
	}
	n := len(st.pult.zapisi())
	st.proba()
	if len(st.pult.zapisi()) != n {
		t.Fatalf("мост на месте, а его переключали: %v", st.pult.zapisi())
	}
	st.pult.mu.Lock()
	st.pult.molchit = true
	st.pult.mu.Unlock()
	st.proba()
	if st.s.domStroka() != DomNaSvyazi {
		t.Fatalf("строка %q", st.s.domStroka())
	}
}

// Окно (и всё, что ходит через /api/vybrat) селектор щелчка и группу моста
// не переключает и в выбор человека не записывает.
func TestDomShchelchokNeVybiraetsyaVruchnuyu(t *testing.T) {
	s, _ := stendDoma(t)
	for _, g := range []string{konfig.TegShchelchkaDoma, konfig.TegMostaDoma} {
		telo, _ := json.Marshal(map[string]string{"gruppa": g, "uzel": konfig.TegZaglushkiDoma})
		r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/vybrat", bytes.NewReader(telo))
		w := httptest.NewRecorder()
		s.Obsluzhit().ServeHTTP(w, r)
		if w.Code == 200 {
			t.Fatalf("«%s» переключён вручную", g)
		}
		if _, est := s.Nastroyki.Uzly[g]; est {
			t.Fatalf("«%s» записан в выбор человека", g)
		}
	}
}
