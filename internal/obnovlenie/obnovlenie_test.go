package obnovlenie

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spisok собирает ответ GitHub из кусков «тег → файл нужного размера».
func spisok(relizy ...string) string {
	return "[" + strings.Join(relizy, ",") + "]"
}

func reliz(teg, fayl string, razmer int, chernovik, pred bool) string {
	return fmt.Sprintf(`{"tag_name":%q,"draft":%t,"prerelease":%t,"assets":[{"name":%q,"browser_download_url":"http://primer/%s","size":%d}]}`,
		teg, chernovik, pred, fayl, teg, razmer)
}

func server(t *testing.T, telo string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, telo)
	}))
	t.Cleanup(s.Close)
	return s
}

// Главная ловушка: в этом же репозитории лежат релизы ЯДРА, и самый свежий
// релиз запросто оказывается ядром. Приложение не должно принять его за себя.
func TestRelizYadraNeSchitaetsyaObnovleniem(t *testing.T) {
	s := server(t, spisok(
		reliz("core-v1.14.0-beta.4-1", "sing-box-windows-amd64.zip", 999, false, false),
		reliz("app-v0.4.2", ImyaFayla, 100, false, false),
	))
	n, err := Proverit(context.Background(), s.Client(), s.URL, "0.4.2")
	if err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	if n != nil {
		t.Fatalf("посчитал обновлением %+v, а мы и так свежие", n)
	}
}

func TestNahoditSvezheePropuskaetChernovik(t *testing.T) {
	s := server(t, spisok(
		reliz("app-v0.9.9", ImyaFayla, 7, true, false),   // черновик
		reliz("app-v0.9.8", ImyaFayla, 7, false, true),   // предрелиз
		reliz("app-v0.5.0", ImyaFayla, 42, false, false), // вот это настоящее
	))
	n, err := Proverit(context.Background(), s.Client(), s.URL, "0.4.2")
	if err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	if n == nil || n.Versiya != "0.5.0" || n.Razmer != 42 {
		t.Fatalf("нашёл %+v, ждал 0.5.0 размером 42", n)
	}
}

func TestStaryyRelizNeStavitsya(t *testing.T) {
	s := server(t, spisok(reliz("app-v0.3.0", ImyaFayla, 42, false, false)))
	n, err := Proverit(context.Background(), s.Client(), s.URL, "0.4.2")
	if err != nil || n != nil {
		t.Fatalf("получил %+v, %v — откат на старую версию недопустим", n, err)
	}
}

func TestSravnit(t *testing.T) {
	sluchai := []struct {
		a, b string
		zhdu int
	}{
		{"0.5.0", "0.4.2", 1},
		{"0.4.2", "0.5.0", -1},
		{"0.4.2", "0.4.2", 0},
		{"0.10.0", "0.9.0", 1}, // не по алфавиту
		{"1.0.0", "0.99.99", 1},
		{"0.5.0", "0.5.0-rabota", 0}, // хвост сборки не считается
		{"0.1.0", "0.1.0-rabota", 0},
	}
	for _, s := range sluchai {
		if got := Sravnit(s.a, s.b); got != s.zhdu {
			t.Errorf("Sravnit(%q,%q)=%d, ждал %d", s.a, s.b, got, s.zhdu)
		}
	}
}

// Замена файла на своём месте: старый уезжает в .old, новый встаёт вместо него.
func TestPostavitZamenyaetFayl(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	if err := os.WriteFile(put, []byte("STARYY"), 0o755); err != nil {
		t.Fatal(err)
	}
	novoe := "NOVYY-KELEVRA"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, novoe)
	}))
	defer s.Close()

	err := Postavit(context.Background(), s.Client(),
		Novaya{Versiya: "0.5.0", Ssylka: s.URL, Razmer: int64(len(novoe))}, put)
	if err != nil {
		t.Fatalf("не поставилось: %v", err)
	}
	if b, _ := os.ReadFile(put); string(b) != novoe {
		t.Fatalf("на месте приложения %q", b)
	}
	if b, _ := os.ReadFile(put + ".old"); string(b) != "STARYY" {
		t.Fatalf("старое приложение не сохранено: %q", b)
	}
	if _, err := os.Stat(put + ".new"); !os.IsNotExist(err) {
		t.Fatal("временный файл остался мусором рядом с приложением")
	}
	UbratHvost(put)
	if _, err := os.Stat(put + ".old"); !os.IsNotExist(err) {
		t.Fatal("хвост .old не убран")
	}
}

// Оборванная закачка не должна встать на место рабочего приложения.
func TestOborvannayaZakachkaNeStavitsya(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	os.WriteFile(put, []byte("STARYY"), 0o755)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "куск")
	}))
	defer s.Close()

	err := Postavit(context.Background(), s.Client(),
		Novaya{Versiya: "0.5.0", Ssylka: s.URL, Razmer: 100500}, put)
	if err == nil {
		t.Fatal("обрыв закачки прошёл как успех")
	}
	if b, _ := os.ReadFile(put); string(b) != "STARYY" {
		t.Fatalf("рабочее приложение испорчено: %q", b)
	}
	if _, err := os.Stat(put + ".new"); !os.IsNotExist(err) {
		t.Fatal("огрызок закачки остался на диске")
	}
}
