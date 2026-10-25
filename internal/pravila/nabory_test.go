package pravila

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// svoyNabor — свой набор так, как его отдаёт выдача: source JSON.
func svoyNabor(domen string) []byte {
	return []byte(`{"version":2,"rules":[{"domain_suffix":["` + domen + `"]}]}`)
}

// razdacha — подставная выдача: отдаёт тело по пути, с ETag и 304; тело
// можно менять на ходу (человек добавил домен в панели).
type razdacha struct {
	mu     sync.Mutex
	tela   map[string][]byte
	otdano int32
	server *httptest.Server
}

func novayaRazdacha(t *testing.T) *razdacha {
	t.Helper()
	r := &razdacha{tela: map[string][]byte{}}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, z *http.Request) {
		r.mu.Lock()
		telo, est := r.tela[z.URL.Path]
		r.mu.Unlock()
		if !est {
			http.NotFound(w, z)
			return
		}
		etag := fmt.Sprintf(`"%x"`, sha256.Sum256(telo))
		if z.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		atomic.AddInt32(&r.otdano, 1)
		w.Header().Set("ETag", etag)
		_, _ = w.Write(telo)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *razdacha) polozhit(put string, telo []byte) {
	r.mu.Lock()
	r.tela[put] = telo
	r.mu.Unlock()
}

func (r *razdacha) adres(put string) string { return r.server.URL + put }

// Кеш принимает свой набор в формате source (JSON).
//
// До 0.6.93 кеш пускал только файлы с подписью SRS: JSON-набор с выдачи
// отвергался как «не похож на набор правил», кеш никогда не становился
// полным, и ядро на каждом старте снова шло за правилами в сеть — ровно то,
// от чего кеш лечил.
func TestKeshPrinimaetSourceJson(t *testing.T) {
	papka := t.TempDir()
	r := novayaRazdacha(t)
	r.polozhit("/u/k/svoi-tunnel", svoyNabor("primer.ru"))
	adresa := map[string]string{"svoi-tunnel": r.adres("/u/k/svoi-tunnel")}

	itog, err := Obnovit(context.Background(), nil, adresa, papka)
	if err != nil {
		t.Fatalf("сверка своего набора: %v", err)
	}
	if itog.Obnovleno != 1 || len(itog.ObnovlenyTegi) != 1 || itog.ObnovlenyTegi[0] != "svoi-tunnel" {
		t.Fatalf("итог сверки %+v, ждали обновлённый svoi-tunnel", itog)
	}
	put := PutVKeshe(papka, "svoi-tunnel")
	if filepath.Ext(put) != ".json" || FormatPoPuti(put) != FormatSource {
		t.Fatalf("свой набор лёг как %q — ждали .json (format source)", put)
	}
	if IzKesha(papka, []string{"svoi-tunnel"}) == nil {
		t.Fatal("кеш со своим набором не считается полным")
	}
	if ChitatSvedeniya(papka).Nabory["svoi-tunnel"].Format != FormatSource {
		t.Fatal("в сведениях не записан вид набора")
	}

	// Вторая сверка — 304, тело не едет.
	if _, err := Obnovit(context.Background(), nil, adresa, papka); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&r.otdano); n != 1 {
		t.Fatalf("выдача отдала тело %d раз — условный запрос для JSON-набора не сработал", n)
	}
}

// Страница ошибки чужого прокси — не набор: ни SRS, ни JSON с правилами.
func TestKeshNePrinimaetMusor(t *testing.T) {
	papka := t.TempDir()
	r := novayaRazdacha(t)
	r.polozhit("/html", []byte("<html>blocked</html>"))
	r.polozhit("/pustoy", []byte(`{"version":2,"rules":[]}`))
	for _, put := range []string{"/html", "/pustoy"} {
		itog, err := Obnovit(context.Background(), nil, map[string]string{"svoi-mimo": r.adres(put)}, papka)
		if err == nil || itog.Otkazy != 1 {
			t.Fatalf("%s принят за набор правил: итог %+v, ошибка %v", put, itog, err)
		}
	}
	if PutVKeshe(papka, "svoi-mimo") != "" {
		t.Fatal("мусор остался в кеше")
	}
}

// Свой набор подменяется на месте: тот же путь, что ядро получило при
// старте, — и ядро, следящее за файлом, перечитает его без перезапуска.
// Тот же набор без изменений файл не трогает (ядро зря не дёргается).
func TestZagotovkaPodmenyaetsyaNaMeste(t *testing.T) {
	papka := t.TempDir()
	tegi := []string{"svoi-tunnel"}
	podbor, err := Podobrat(papka, tegi, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	putYadra := podbor.Puti["svoi-tunnel"]
	if putYadra == "" || len(podbor.Zagotovki) != 1 {
		t.Fatalf("своего набора нет в кеше — ждали заготовку, получили %+v", podbor)
	}
	if b, _ := os.ReadFile(putYadra); string(b) != string(Zaglushka) {
		t.Fatalf("заготовка не пустая заглушка: %s", b)
	}

	r := novayaRazdacha(t)
	r.polozhit("/svoi-tunnel", svoyNabor("novyy.ru"))
	adresa := map[string]string{"svoi-tunnel": r.adres("/svoi-tunnel")}
	if k := KSverke(papka, adresa, nil); k["svoi-tunnel"] == "" {
		t.Fatal("заготовка не попала в сверку — настоящий набор не скачается никогда")
	}
	if _, err := Obnovit(context.Background(), nil, adresa, papka); err != nil {
		t.Fatal(err)
	}
	if put := PutVKeshe(papka, "svoi-tunnel"); put != putYadra {
		t.Fatalf("настоящий набор лёг в %q, а ядро читает %q — подмены на ходу не будет", put, putYadra)
	}
	if b, _ := os.ReadFile(putYadra); !strings.Contains(string(b), "novyy.ru") {
		t.Fatalf("на месте заготовки не настоящий набор: %s", b)
	}

	// Тот же набор, но сервер без ETag отдал его заново — файл не трогаем.
	staro := time.Now().Add(-time.Hour)
	if err := os.Chtimes(putYadra, staro, staro); err != nil {
		t.Fatal(err)
	}
	sved := ChitatSvedeniya(papka)
	n := sved.Nabory["svoi-tunnel"]
	n.ETag = ""
	sved.Nabory["svoi-tunnel"] = n
	if err := zapisatSvedeniya(papka, sved); err != nil {
		t.Fatal(err)
	}
	itog, err := Obnovit(context.Background(), nil, adresa, papka)
	if err != nil {
		t.Fatal(err)
	}
	if itog.Obnovleno != 0 {
		t.Fatalf("тот же набор посчитан обновлённым: %+v", itog)
	}
	if st, _ := os.Stat(putYadra); st.ModTime().After(staro.Add(time.Minute)) {
		t.Fatal("файл набора переписан без изменений — ядро зря перечитает его")
	}

	// Человек добавил домен: новый набор ложится на то же место.
	r.polozhit("/svoi-tunnel", svoyNabor("eshchyo-novee.ru"))
	itog, err = Obnovit(context.Background(), nil, adresa, papka)
	if err != nil || itog.Obnovleno != 1 {
		t.Fatalf("изменённый набор не обновился: %+v, %v", itog, err)
	}
	if b, _ := os.ReadFile(putYadra); !strings.Contains(string(b), "eshchyo-novee.ru") {
		t.Fatalf("на месте набора старое содержимое: %s", b)
	}
}

// Подъём по частям: что в кеше — из кеша, своего нет — заготовка, чужого
// нет — качает ядро. Раньше одного недостающего тега хватало, чтобы кеш не
// годился целиком.
func TestPodborBezKomplektaPoChastyam(t *testing.T) {
	papka := t.TempDir()
	r := novayaRazdacha(t)
	r.polozhit("/ads.srs", []byte("SRS-proba"))
	if _, err := Obnovit(context.Background(), nil, map[string]string{"ads": r.adres("/ads.srs")}, papka); err != nil {
		t.Fatal(err)
	}
	tegi := []string{"ads", "main-domains", "svoi-blok", "svoi-tunnel", "svoi-tunnel-2"}
	podbor, err := Podobrat(papka, tegi, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(podbor.IzKesha, ",") != "ads" {
		t.Fatalf("из кеша %v, ждали ads", podbor.IzKesha)
	}
	if strings.Join(podbor.Zagotovki, ",") != "svoi-blok,svoi-tunnel,svoi-tunnel-2" {
		t.Fatalf("заготовки %v, ждали все свои наборы", podbor.Zagotovki)
	}
	if strings.Join(podbor.Seti, ",") != "main-domains" {
		t.Fatalf("ядру качать самому %v, ждали main-domains", podbor.Seti)
	}
	if _, est := podbor.Puti["main-domains"]; est {
		t.Fatal("чужому набору без кеша подставлен файл — ядро должно качать его само")
	}
	for _, teg := range podbor.Zagotovki {
		if filepath.Dir(podbor.Puti[teg]) != mustAbs(t, PutKesha(papka)) {
			t.Fatalf("заготовка %s лежит не в кеше (%s) — сверка положит набор в другое место", teg, podbor.Puti[teg])
		}
	}
}

// svoi-tunnel и svoi-tunnel-2 — один набор с двух дверей: есть один —
// значит есть оба. Вторая дверь мертва — её набор берёт содержимое первой.
func TestGruppaDvereyOdinNabor(t *testing.T) {
	papka := t.TempDir()
	r := novayaRazdacha(t)
	r.polozhit("/svoi-tunnel", svoyNabor("dom.example"))
	if _, err := Obnovit(context.Background(), nil, map[string]string{"svoi-tunnel": r.adres("/svoi-tunnel")}, papka); err != nil {
		t.Fatal(err)
	}
	podbor, err := Podobrat(papka, []string{"svoi-tunnel", "svoi-tunnel-2"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(podbor.Puti["svoi-tunnel-2"])
	if err != nil || !strings.Contains(string(b), "dom.example") {
		t.Fatalf("набор второй двери не взял содержимое первой: %s (%v)", b, err)
	}
	if Gruppa("svoi-tunnel-2") != "svoi-tunnel" || Gruppa("svoi-tunnel") != "svoi-tunnel" || Gruppa("main-2") != "main-2" {
		t.Fatal("группы дверей посчитаны неверно")
	}
}

// Повтор подъёма с комплектом закрывает КАЖДЫЙ тег: кеш → комплект →
// заготовка своего → пустая заглушка чужого. Иначе ядро снова упадёт на
// «initialize rule-set», и человек останется без связи из-за одного списка.
func TestPodborSKomplektomZakryvaetVse(t *testing.T) {
	papka := t.TempDir()
	papkaKomplekta := filepath.Join(papka, "pravila")
	komplekt, err := Razlozhit(papkaKomplekta)
	if err != nil {
		t.Fatal(err)
	}
	tegi := append(Tegi(), "svoi-mimo", "novyy-chuzhoy")
	podbor, err := Podobrat(papka, tegi, komplekt, papkaKomplekta)
	if err != nil {
		t.Fatal(err)
	}
	if len(podbor.Seti) != 0 {
		t.Fatalf("с комплектом ядру всё равно оставлено качать %v", podbor.Seti)
	}
	if len(podbor.Puti) != len(tegi) {
		t.Fatalf("закрыто %d тегов из %d", len(podbor.Puti), len(tegi))
	}
	if len(podbor.IzKomplekta) != len(Tegi()) {
		t.Fatalf("из комплекта %d, ждали %d", len(podbor.IzKomplekta), len(Tegi()))
	}
	if strings.Join(podbor.Pustye, ",") != "novyy-chuzhoy" {
		t.Fatalf("пустые %v, ждали novyy-chuzhoy", podbor.Pustye)
	}
	if filepath.Dir(podbor.Puti["novyy-chuzhoy"]) != mustAbs(t, papkaKomplekta) {
		t.Fatal("заглушка чужого набора легла в кеш — следующий подъём принял бы её за скачанный набор")
	}
	if PutVKeshe(papka, "novyy-chuzhoy") != "" {
		t.Fatal("заглушка чужого набора видна как кеш")
	}
	if strings.Join(podbor.Zagotovki, ",") != "svoi-mimo" {
		t.Fatalf("заготовки %v, ждали svoi-mimo", podbor.Zagotovki)
	}
}

// Свои наборы сверяются по своему интервалу (2m), чужие — не чаще часа.
func TestKSverkePoSrokuNabora(t *testing.T) {
	papka := t.TempDir()
	r := novayaRazdacha(t)
	r.polozhit("/ads.srs", []byte("SRS-proba"))
	r.polozhit("/svoi-mimo", svoyNabor("mimo.ru"))
	adresa := map[string]string{"ads": r.adres("/ads.srs"), "svoi-mimo": r.adres("/svoi-mimo")}
	sroki := map[string]time.Duration{"ads": time.Hour, "svoi-mimo": 2 * time.Minute}
	if _, err := Obnovit(context.Background(), nil, adresa, papka); err != nil {
		t.Fatal(err)
	}
	if k := KSverke(papka, adresa, sroki); len(k) != 0 {
		t.Fatalf("сразу после сверки к сверке %v", k)
	}
	// Состариваем сведения на 5 минут.
	sved := ChitatSvedeniya(papka)
	for teg, n := range sved.Nabory {
		n.Svereno = time.Now().Add(-5 * time.Minute)
		sved.Nabory[teg] = n
	}
	if err := zapisatSvedeniya(papka, sved); err != nil {
		t.Fatal(err)
	}
	k := KSverke(papka, adresa, sroki)
	if k["svoi-mimo"] == "" || k["ads"] != "" {
		t.Fatalf("через 5 минут к сверке %v — ждали только svoi-mimo", k)
	}
	// Тик пришёл на секунду раньше двух минут — набор всё равно пора сверить.
	sved = ChitatSvedeniya(papka)
	for teg, n := range sved.Nabory {
		n.Svereno = time.Now().Add(-2*time.Minute + time.Second)
		sved.Nabory[teg] = n
	}
	if err := zapisatSvedeniya(papka, sved); err != nil {
		t.Fatal(err)
	}
	k = KSverke(papka, adresa, sroki)
	if k["svoi-mimo"] == "" || k["ads"] != "" {
		t.Fatalf("через 5 минут к сверке %v — ждали только svoi-mimo", k)
	}
	// Другой ключ устройства — другой адрес: сверять сразу.
	adresa["ads"] = r.adres("/ads.srs") + "?drugoy"
	if KSverke(papka, adresa, sroki)["ads"] == "" {
		t.Fatal("сменившийся адрес набора не попал в сверку")
	}
}

// Набор сменил вид (.srs стал .json): берётся тот, что записан в сведениях,
// а не старый файл другого вида, оставшийся рядом.
func TestSmenaVidaNabora(t *testing.T) {
	papka := t.TempDir()
	r := novayaRazdacha(t)
	r.polozhit("/n", []byte("SRS-proba"))
	adresa := map[string]string{"main-domains": r.adres("/n")}
	if _, err := Obnovit(context.Background(), nil, adresa, papka); err != nil {
		t.Fatal(err)
	}
	r.polozhit("/n", svoyNabor("x.ru"))
	if _, err := Obnovit(context.Background(), nil, adresa, papka); err != nil {
		t.Fatal(err)
	}
	if put := PutVKeshe(papka, "main-domains"); filepath.Ext(put) != ".json" {
		t.Fatalf("после смены вида набор берётся из %q", put)
	}
}

func mustAbs(t *testing.T, put string) string {
	t.Helper()
	abs, err := filepath.Abs(put)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
