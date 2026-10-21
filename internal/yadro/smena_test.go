package yadro

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpis"
)

// sborkaStenda — один релиз ядра на подменном «GitHub».
type sborkaStenda struct {
	teg        string
	yadro      string // что лежит в архиве (содержимое sing-box)
	podpisano  string // что подписано; "" — подписи в релизе нет
	prerelease bool
	draft      bool
	// bezTega — подпись прежнего формата (без тега), как у живой сборки
	// core-v1.14.0-beta.4-1.
	bezTega bool
	// tegPodpisi — под каким тегом подписан архив; пусто — под своим.
	tegPodpisi string
}

// stendSmeny поднимает «GitHub» со списком сборок (от свежих к старым) и
// ядро, у которого на месте уже стоит файл s soderzhimym stoit.
//
// Требование подписи в пакете выключено (main_test.go): замена ядра обязана
// требовать подпись сама, и эти проверки это подтверждают.
func stendSmeny(t *testing.T, stoit string, sborki ...sborkaStenda) *Yadro {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	staryKlyuch := podpis.KlyuchHex
	podpis.KlyuchHex = hex.EncodeToString(pub)
	t.Cleanup(func() { podpis.KlyuchHex = staryKlyuch })

	arhivy := map[string][]byte{}
	podpisi := map[string]string{}
	for _, s := range sborki {
		arhivy[s.teg] = zipYadra(t, s.yadro)
		if s.podpisano != "" {
			tmp := filepath.Join(t.TempDir(), "p.zip")
			os.WriteFile(tmp, zipYadra(t, s.podpisano), 0o644)
			seed := base64.StdEncoding.EncodeToString(priv.Seed())
			teg := s.teg
			if s.tegPodpisi != "" {
				teg = s.tegPodpisi
			}
			var sig string
			var err error
			if s.bezTega {
				sig, err = podpis.Podpisat(seed, ImyaArhiva(), tmp)
			} else {
				sig, err = podpis.PodpisatSTegom(seed, ImyaArhiva(), teg, tmp)
			}
			if err != nil {
				t.Fatal(err)
			}
			podpisi[s.teg] = sig
		}
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/spisok":
			var relizy []map[string]any
			for _, s := range sborki {
				assets := []map[string]string{{"name": ImyaArhiva(), "browser_download_url": srv.URL + "/a/" + s.teg}}
				if s.podpisano != "" {
					assets = append(assets, map[string]string{"name": ImyaArhiva() + ".sig", "browser_download_url": srv.URL + "/s/" + s.teg})
				}
				relizy = append(relizy, map[string]any{"tag_name": s.teg, "prerelease": s.prerelease, "draft": s.draft, "assets": assets})
			}
			json.NewEncoder(w).Encode(relizy)
		case strings.HasPrefix(r.URL.Path, "/a/"):
			w.Write(arhivy[strings.TrimPrefix(r.URL.Path, "/a/")])
		case strings.HasPrefix(r.URL.Path, "/s/"):
			fmt.Fprint(w, podpisi[strings.TrimPrefix(r.URL.Path, "/s/")])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	papka := t.TempDir()
	y := &Yadro{Papka: papka, Bin: filepath.Join(papka, imyaYadra()), Spisok: srv.URL + "/spisok"}
	if stoit != "" {
		if err := os.WriteFile(y.Bin, []byte(stoit), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return y
}

func soderzhimoe(t *testing.T, put string) string {
	t.Helper()
	b, err := os.ReadFile(put)
	if err != nil {
		t.Fatalf("нет файла %s: %v", put, err)
	}
	return string(b)
}

func estFayl(put string) bool { _, err := os.Stat(put); return err == nil }

// Пробная сборка (prerelease) и черновик не берутся: свежей считается
// следующая обычная сборка. Стенд берёт пробные явно.
func TestVybratSborkuPropuskaetProbnye(t *testing.T) {
	y := stendSmeny(t, "",
		sborkaStenda{teg: "core-v3-1", yadro: "v3", podpisano: "v3", draft: true},
		sborkaStenda{teg: "core-v2-1", yadro: "v2", podpisano: "v2", prerelease: true},
		sborkaStenda{teg: "core-v1-1", yadro: "v1", podpisano: "v1"},
	)
	s, err := y.vybratSborku(context.Background(), http.DefaultClient, true)
	if err != nil || s.Teg != "core-v1-1" {
		t.Fatalf("выбрана %q (%v), ждал core-v1-1: пробная сборка ушла бы людям", s.Teg, err)
	}
	y.BratProbnye = true
	s, err = y.vybratSborku(context.Background(), http.DefaultClient, true)
	if err != nil || s.Teg != "core-v2-1" {
		t.Fatalf("стенд с пробными получил %q (%v), ждал core-v2-1", s.Teg, err)
	}
}

// Первая установка тоже не берёт пробную сборку и запоминает версию.
func TestZagruzitNeBeretProbnuyuIZapominaetVersiyu(t *testing.T) {
	y := stendSmeny(t, "",
		sborkaStenda{teg: "core-v2-1", yadro: "v2", podpisano: "v2", prerelease: true},
		sborkaStenda{teg: "core-v1-1", yadro: "v1", podpisano: "v1"},
	)
	if err := y.Zagruzit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := soderzhimoe(t, y.Bin); got != "v1" {
		t.Fatalf("поставлено %q, ждал v1", got)
	}
	if v := y.Versiya(); v != "core-v1-1" {
		t.Fatalf("версия стоящего ядра %q, ждал core-v1-1", v)
	}
}

// Главный путь: стоит v1, вышла v2 — архив откладывается, ядро не трогается;
// при подключении v2 встаёт на место, после подтверждения хвостов нет.
func TestZamenaYadraPolnyyPut(t *testing.T) {
	y := stendSmeny(t, "v1", sborkaStenda{teg: "core-v2-1", yadro: "v2", podpisano: "v2"})
	y.zapisatVersiyu("core-v1-1")

	itog, err := y.Prigotovit(context.Background(), http.DefaultClient)
	if err != nil || !itog.Otlozheno || itog.Stoit != "core-v1-1" || itog.Svezhaya != "core-v2-1" {
		t.Fatalf("подготовка: %+v, %v", itog, err)
	}
	if got := soderzhimoe(t, y.Bin); got != "v1" {
		t.Fatalf("подготовка тронула стоящее ядро: %q", got)
	}

	smena, err := y.Smenit()
	if err != nil || smena == nil {
		t.Fatalf("замена не случилась: %v", err)
	}
	if got := soderzhimoe(t, y.Bin); got != "v2" {
		t.Fatalf("после замены на месте %q", got)
	}
	if y.Versiya() != "core-v2-1" || smena.Prezhniy != "core-v1-1" {
		t.Fatalf("версия %q, прежняя %q", y.Versiya(), smena.Prezhniy)
	}
	if got := soderzhimoe(t, y.putPrezhnego()); got != "v1" {
		t.Fatalf("прежнее ядро для отката не сохранено: %q", got)
	}
	smena.Zakrepit()
	for _, put := range []string{y.putPrezhnego(), y.putArhivaNovogo(), y.putOtlozhennogo()} {
		if estFayl(put) {
			t.Fatalf("после подтверждения остался %s", filepath.Base(put))
		}
	}
	// Повторная сверка: стоит свежая, ничего не откладывается.
	itog, err = y.Prigotovit(context.Background(), http.DefaultClient)
	if err != nil || itog.Otlozheno || itog.Stoit != "core-v2-1" {
		t.Fatalf("повторная сверка: %+v, %v", itog, err)
	}
	if s, _ := y.Smenit(); s != nil {
		t.Fatal("замена без отложенной сборки")
	}
}

// Архив, не сходящийся с подписью, не откладывается, ядро не трогается.
func TestZamenaPodmenennoeOtvergaetsya(t *testing.T) {
	y := stendSmeny(t, "v1", sborkaStenda{teg: "core-v2-1", yadro: "vredonosnoe", podpisano: "v2"})
	y.zapisatVersiyu("core-v1-1")
	_, err := y.Prigotovit(context.Background(), http.DefaultClient)
	if !errors.Is(err, obnovlenie.ErrPodpis) {
		t.Fatalf("подменённый архив: ошибка %v, ждал ErrPodpis", err)
	}
	if estFayl(y.putArhivaNovogo()) || estFayl(y.putOtlozhennogo()) {
		t.Fatal("подменённый архив отложен")
	}
	if s, _ := y.Smenit(); s != nil || soderzhimoe(t, y.Bin) != "v1" {
		t.Fatal("подменённое ядро встало")
	}
}

// Сборка без подписи на замену не берётся, даже когда требование подписи
// выключено (как в этом пакете): замена ядра без подписи не делается никогда.
func TestZamenaBezPodpisiNeBeretsya(t *testing.T) {
	if obnovlenie.TrebovatPodpis {
		t.Fatal("ждал выключенное требование подписи в этом пакете (main_test.go)")
	}
	y := stendSmeny(t, "v1", sborkaStenda{teg: "core-v2-1", yadro: "v2"})
	y.zapisatVersiyu("core-v1-1")
	if _, err := y.Prigotovit(context.Background(), http.DefaultClient); err == nil {
		t.Fatal("сборка без подписи принята на замену")
	}
	if estFayl(y.putArhivaNovogo()) {
		t.Fatal("архив без подписи отложен")
	}
}

// Отложенный архив подменили на диске — замена не делается, архив убран.
func TestZamenaPodmenaNaDiske(t *testing.T) {
	y := stendSmeny(t, "v1", sborkaStenda{teg: "core-v2-1", yadro: "v2", podpisano: "v2"})
	y.zapisatVersiyu("core-v1-1")
	if _, err := y.Prigotovit(context.Background(), http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(y.putArhivaNovogo(), zipYadra(t, "vredonosnoe"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := y.Smenit()
	if s != nil || !errors.Is(err, obnovlenie.ErrPodpis) {
		t.Fatalf("подменённый на диске архив: замена %v, ошибка %v", s, err)
	}
	if got := soderzhimoe(t, y.Bin); got != "v1" {
		t.Fatalf("на месте ядра %q", got)
	}
	if estFayl(y.putArhivaNovogo()) {
		t.Fatal("подменённый архив остался лежать")
	}
}

// Установка старше замены (записки о версии нет): если стоящее ядро и есть
// свежая сборка — только записать версию, ничего не меняя.
func TestNeizvestnoeYadroPriznaetsya(t *testing.T) {
	y := stendSmeny(t, "v1", sborkaStenda{teg: "core-v1-1", yadro: "v1", podpisano: "v1"})
	if y.Versiya() != "" {
		t.Fatal("версия известна без записки")
	}
	itog, err := y.Prigotovit(context.Background(), http.DefaultClient)
	if err != nil || !itog.Priznano || itog.Otlozheno {
		t.Fatalf("признание: %+v, %v", itog, err)
	}
	if y.Versiya() != "core-v1-1" {
		t.Fatalf("версия после признания %q", y.Versiya())
	}
	if estFayl(y.putArhivaNovogo()) {
		t.Fatal("отложен архив той же сборки")
	}
}

// Неизвестное ядро, которое отличается от свежей сборки, меняется.
func TestNeizvestnoeDrugoeYadroMenyaetsya(t *testing.T) {
	y := stendSmeny(t, "staroe", sborkaStenda{teg: "core-v2-1", yadro: "v2", podpisano: "v2"})
	itog, err := y.Prigotovit(context.Background(), http.DefaultClient)
	if err != nil || !itog.Otlozheno {
		t.Fatalf("подготовка: %+v, %v", itog, err)
	}
	// Файл ядра подменили мимо приложения — записка о версии ему не верит.
	y.zapisatVersiyu("core-v2-1")
	os.WriteFile(y.Bin, []byte("drugoe"), 0o755)
	if y.Versiya() != "" {
		t.Fatal("записка признала чужой файл ядра")
	}
}

// Новое ядро не поднялось: откат возвращает прежнее, сборка отвергается и
// больше не качается.
func TestOtkatIOtvergnutaya(t *testing.T) {
	y := stendSmeny(t, "v1", sborkaStenda{teg: "core-v2-1", yadro: "v2", podpisano: "v2"})
	y.zapisatVersiyu("core-v1-1")
	if _, err := y.Prigotovit(context.Background(), http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	smena, err := y.Smenit()
	if err != nil || smena == nil {
		t.Fatalf("замена: %v", err)
	}
	if err := smena.Otkatit(); err != nil {
		t.Fatalf("откат: %v", err)
	}
	if got := soderzhimoe(t, y.Bin); got != "v1" || y.Versiya() != "core-v1-1" {
		t.Fatalf("после отката на месте %q, версия %q", got, y.Versiya())
	}
	smena.Otvergnut()
	itog, err := y.Prigotovit(context.Background(), http.DefaultClient)
	if err != nil || !itog.Otvergnuto || itog.Otlozheno {
		t.Fatalf("отвергнутая сборка снова в деле: %+v, %v", itog, err)
	}
	if s, _ := y.Smenit(); s != nil {
		t.Fatal("отвергнутая сборка встала снова")
	}
}

// Не поднимается ни новое, ни прежнее ядро — сборка пробуется ограниченное
// число раз, потом отвергается.
func TestPredelPopytokSmeny(t *testing.T) {
	y := stendSmeny(t, "v1", sborkaStenda{teg: "core-v2-1", yadro: "v2", podpisano: "v2"})
	y.zapisatVersiyu("core-v1-1")
	if _, err := y.Prigotovit(context.Background(), http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < popytokSmeny; i++ {
		smena, err := y.Smenit()
		if err != nil || smena == nil {
			t.Fatalf("попытка %d: %v", i+1, err)
		}
		if err := smena.Otkatit(); err != nil {
			t.Fatal(err)
		}
	}
	if s, err := y.Smenit(); s != nil || err == nil {
		t.Fatalf("после %d попыток сборка всё ещё ставится", popytokSmeny)
	}
	if got := soderzhimoe(t, y.Bin); got != "v1" {
		t.Fatalf("на месте %q", got)
	}
}

// Служба оборвалась посреди пробы новой сборки: следующий подъём продолжает
// ту же пробу (откат ещё возможен), а не теряет прежнее ядро.
func TestProbaPerezhivaetPerezapusk(t *testing.T) {
	y := stendSmeny(t, "v1", sborkaStenda{teg: "core-v2-1", yadro: "v2", podpisano: "v2"})
	y.zapisatVersiyu("core-v1-1")
	if _, err := y.Prigotovit(context.Background(), http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	if s, err := y.Smenit(); err != nil || s == nil {
		t.Fatal(err)
	}
	// «Перезапуск»: новая копия, те же файлы.
	y2 := &Yadro{Papka: y.Papka, Bin: y.Bin, Spisok: y.Spisok}
	if itog, err := y2.Prigotovit(context.Background(), http.DefaultClient); err != nil || itog.Stoit != "core-v2-1" {
		t.Fatalf("сверка посреди пробы: %+v, %v", itog, err)
	}
	smena, err := y2.Smenit()
	if err != nil || smena == nil || smena.Prezhniy != "core-v1-1" {
		t.Fatalf("проба не продолжилась: %+v, %v", smena, err)
	}
	if err := smena.Otkatit(); err != nil || soderzhimoe(t, y.Bin) != "v1" {
		t.Fatalf("откат после перезапуска: %v", err)
	}
}

// Ядра на машине нет — менять нечего (его ставит Zagruzit).
func TestPrigotovitBezYadra(t *testing.T) {
	y := stendSmeny(t, "", sborkaStenda{teg: "core-v2-1", yadro: "v2", podpisano: "v2"})
	if _, err := y.Prigotovit(context.Background(), http.DefaultClient); !errors.Is(err, ErrNetYadra) {
		t.Fatalf("ошибка %v, ждал ErrNetYadra", err)
	}
}

// Подпись прежнего формата (без тега) на замену не годится: такой подписью
// нельзя доказать, под каким тегом выпускалась сборка.
func TestZamenaPoPodpisiBezTegaNeBeretsya(t *testing.T) {
	y := stendSmeny(t, "v1", sborkaStenda{teg: "core-v2-1", yadro: "v2", podpisano: "v2", bezTega: true})
	y.zapisatVersiyu("core-v1-1")
	if _, err := y.Prigotovit(context.Background(), http.DefaultClient); !errors.Is(err, obnovlenie.ErrPodpis) {
		t.Fatalf("подпись без тега: ошибка %v, ждал ErrPodpis", err)
	}
	if estFayl(y.putArhivaNovogo()) {
		t.Fatal("архив с подписью без тега отложен")
	}
}

// Атака откатом: старый подписанный архив (core-v1-1) выложен под тегом
// core-v9.9.9-1. Подпись сходится с файлом, но не с тегом — не берётся.
func TestStaryyArhivPodChuzhimTegomNeBeretsya(t *testing.T) {
	y := stendSmeny(t, "v2", sborkaStenda{teg: "core-v9.9.9-1", yadro: "v1", podpisano: "v1", tegPodpisi: "core-v1-1"})
	y.zapisatVersiyu("core-v2-1")
	if _, err := y.Prigotovit(context.Background(), http.DefaultClient); !errors.Is(err, obnovlenie.ErrPodpis) {
		t.Fatalf("архив под чужим тегом: ошибка %v, ждал ErrPodpis", err)
	}
	if s, _ := y.Smenit(); s != nil || soderzhimoe(t, y.Bin) != "v2" {
		t.Fatal("ядро откатилось на старую сборку под чужим тегом")
	}
}

// Первая установка принимает и прежний формат (живая сборка подписана им),
// но версию не записывает: тег без подписи ничего не доказывает.
func TestPervayaUstanovkaPrinimaetPrezhniyFormat(t *testing.T) {
	obnovlenie.TrebovatPodpis = true
	t.Cleanup(func() { obnovlenie.TrebovatPodpis = false })
	y := stendSmeny(t, "", sborkaStenda{teg: "core-v1.14.0-beta.4-1", yadro: "v1", podpisano: "v1", bezTega: true})
	if err := y.Zagruzit(context.Background()); err != nil {
		t.Fatalf("первая установка по прежней подписи: %v", err)
	}
	if soderzhimoe(t, y.Bin) != "v1" {
		t.Fatal("ядро не поставлено")
	}
	if v := y.Versiya(); v != "" {
		t.Fatalf("по подписи без тега записана версия %q", v)
	}
	// С подписью с тегом версия записывается.
	y2 := stendSmeny(t, "", sborkaStenda{teg: "core-v1.14.1-1", yadro: "v2", podpisano: "v2"})
	if err := y2.Zagruzit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v := y2.Versiya(); v != "core-v1.14.1-1" {
		t.Fatalf("версия %q", v)
	}
	// Под чужим тегом первая установка тоже не проходит.
	y3 := stendSmeny(t, "", sborkaStenda{teg: "core-v9.9.9-1", yadro: "v1", podpisano: "v1", tegPodpisi: "core-v1-1"})
	if err := y3.Zagruzit(context.Background()); err == nil {
		t.Fatal("архив под чужим тегом поставлен")
	}
}
