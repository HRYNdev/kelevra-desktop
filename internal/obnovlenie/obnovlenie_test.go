package obnovlenie

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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

// Порядок релизов в ответе GitHub по версиям НЕ упорядочен — замерено живьём
// 24.08.2026 на самом kelevra-desktop: app-v0.6.9 стоял в списке ВЫШЕ, чем
// app-v0.6.15. Клиент 0.6.10 упирался в первый же app-релиз (0.6.9), слышал
// «ты и так свежий» и застревал навсегда.
func TestSpisokNeUporyadochenPoVersii(t *testing.T) {
	telo := `[
	  {"tag_name":"app-v0.6.9","draft":false,"prerelease":false,
	   "assets":[{"name":"Kelevra.exe","size":10,"url":"u9"}]},
	  {"tag_name":"app-v0.6.15","draft":false,"prerelease":false,
	   "assets":[{"name":"Kelevra.exe","size":20,"url":"u15"}]}
	]`
	s := server(t, telo)
	n, err := Proverit(context.Background(), s.Client(), s.URL, "0.6.10")
	if err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	if n == nil {
		t.Fatal("клиент 0.6.10 застрял: обновлятор упёрся в первый app-релиз списка (0.6.9)")
	}
	if n.Versiya != "0.6.15" {
		t.Fatalf("ждал 0.6.15 (максимум по версии), получил %s", n.Versiya)
	}
}

// TestPostavitHelperProcess — не самостоятельный тест, а тело для СУБПРОЦЕССА,
// который запускает TestPostavitMezhprocessnayaGonkaNePortitFayl (стандартный
// go-приём: перезапуск os.Args[0] с -test.run=этот_тест). Без переменной
// окружения он ничего не делает и завершается как обычный зелёный тест.
func TestPostavitHelperProcess(t *testing.T) {
	if os.Getenv("KD_POSTAVIT_HELPER") != "1" {
		return
	}
	put := os.Getenv("KD_POSTAVIT_PUT")
	adres := os.Getenv("KD_POSTAVIT_URL")
	razmer, _ := strconv.ParseInt(os.Getenv("KD_POSTAVIT_RAZMER"), 10, 64)
	err := Postavit(context.Background(), &http.Client{Timeout: 30 * time.Second},
		Novaya{Versiya: "0.9.9", Ssylka: adres, Razmer: razmer}, put)
	if err != nil {
		fmt.Fprintln(os.Stderr, "postavit:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// TestPostavitMezhprocessnayaGonkaNePortitFayl — настоящая гонка: несколько
// ЗАПУЩЕННЫХ .exe (реальные ОС-процессы, не горутины) одновременно тянут
// обновление на один и тот же putExe. cmd/kelevra/obnovlenie.go зовёт
// Postavit ДО одиночного замка приложения, поэтому в жизни это ровно так и
// бывает: два экземпляра стартовали почти вместе.
//
// Опасное окно ýже, чем «записи наложились»: оно между тем, как процесс
// ЗАКРЫЛ временный файл и проверил размер (значит, поверил, что всё
// записано верно), и тем, как он его ПЕРЕИМЕНОВАЛ на место putExe. Если в
// этот самый момент другой процесс открывает ОБЩЕЕ имя ".new" с O_TRUNC, он
// обнуляет уже проверенные байты первого — и первый переименует в
// putExe чужую, ещё не дописанную порцию, хотя сам вернёт nil (успех). Ровно
// поэтому попытка — не одна: узкое окно ловится не с первого раза, но у
// сегодняшнего кода — почти всегда за разумное число попыток, а у
// исправленного — никогда (у каждого процесса свой файл).
func TestPostavitMezhprocessnayaGonkaNePortitFayl(t *testing.T) {
	// Эталонное тело: большое и не однородное, чтобы порчу от наложения
	// записей нельзя было случайно не заметить.
	etalon := make([]byte, 64*1024)
	for i := range etalon {
		etalon[i] = byte(i % 251)
	}

	// Сервер отдаёт тело МЕДЛЕННО, кусками с задержкой: так у нескольких
	// процессов гарантированно есть окно, где их запись друг друга накладывается.
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		const kusok = 256
		for i := 0; i < len(etalon); i += kusok {
			konec := i + kusok
			if konec > len(etalon) {
				konec = len(etalon)
			}
			w.Write(etalon[i:konec])
			if fl != nil {
				fl.Flush()
			}
			time.Sleep(300 * time.Microsecond)
		}
	}))
	defer s.Close()

	const N = 8        // процессов в одной попытке
	const Popytok = 25 // попыток: узкое окно ловится не всегда, но накопительно — надёжно
	kolvoUspehovVsego := 0
	for popytka := 0; popytka < Popytok; popytka++ {
		papka := t.TempDir()
		put := filepath.Join(papka, "Kelevra.exe")
		if err := os.WriteFile(put, []byte("STARYY"), 0o755); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		uspeh := make([]bool, N)
		vyvod := make([]string, N)
		for i := 0; i < N; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				cmd := exec.Command(os.Args[0], "-test.run=^TestPostavitHelperProcess$")
				cmd.Env = append(os.Environ(),
					"KD_POSTAVIT_HELPER=1",
					"KD_POSTAVIT_PUT="+put,
					"KD_POSTAVIT_URL="+s.URL,
					"KD_POSTAVIT_RAZMER="+strconv.Itoa(len(etalon)),
				)
				out, err := cmd.CombinedOutput()
				vyvod[idx] = string(out)
				uspeh[idx] = err == nil
			}(i)
		}
		wg.Wait()

		kolvoUspehov := 0
		for _, u := range uspeh {
			if u {
				kolvoUspehov++
			}
		}
		kolvoUspehovVsego += kolvoUspehov
		if kolvoUspehov == 0 {
			continue // в этой попытке никто не поставился — судить не о чем
		}

		// Главная проверка: раз хоть один процесс отчитался успехом (err==nil),
		// файл на месте putExe обязан побайтно совпасть с эталоном. Несовпадение
		// значит, что проверка размера прошла по СВОИМ записанным байтам, а не
		// по факту на диске, и гонка при записи временного файла её обманула.
		posle, err := os.ReadFile(put)
		if err != nil {
			t.Fatalf("попытка %d: после обновления файл не читается: %v", popytka, err)
		}
		if !bytes.Equal(posle, etalon) {
			t.Fatalf("попытка %d: файл на месте приложения испорчен гонкой: длина %d (ждали %d), успешных процессов %d из %d, вывод: %v",
				popytka, len(posle), len(etalon), kolvoUspehov, N, vyvod)
		}
	}
	if kolvoUspehovVsego == 0 {
		t.Fatalf("ни один процесс ни в одной из %d попыток не поставил обновление", Popytok)
	}
}

// podmenitPovtory подменяет pereimenovat/spat на время теста и возвращает
// функцию восстановления исходных (реальных) значений.
func podmenitPovtory(t *testing.T, pereimen func(ot, kuda string) error) {
	t.Helper()
	staryyPereimenovat, staryySpat := pereimenovat, spat
	pereimenovat = pereimen
	spat = func(time.Duration) {} // тест не обязан ждать настоящие паузы антивируса
	t.Cleanup(func() {
		pereimenovat = staryyPereimenovat
		spat = staryySpat
	})
}

// TestPostavitVtoroePereimenovanieProhoditSoVtoroyPopytki — антивирус держит
// свежескачанный .exe первые два переименования, отпускает на третьем: живой
// симптом 28.08 не должен возникать из-за КАЖДОЙ мимолётной занятости файла.
func TestPostavitVtoroePereimenovanieProhoditSoVtoroyPopytki(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	if err := os.WriteFile(put, []byte("STARYY"), 0o755); err != nil {
		t.Fatal(err)
	}
	novoe := "NOVYY-KELEVRA"
	s := server(t, novoe)

	popytok := 0
	podmenitPovtory(t, func(ot, kuda string) error {
		popytok++
		if kuda == put && popytok <= 2 {
			return fmt.Errorf("занято антивирусом (попытка %d)", popytok)
		}
		return os.Rename(ot, kuda)
	})

	err := Postavit(context.Background(), s.Client(),
		Novaya{Versiya: "0.5.0", Ssylka: s.URL, Razmer: int64(len(novoe))}, put)
	if err != nil {
		t.Fatalf("с третьей попытки переименование должно было пройти, а Postavit вернул: %v", err)
	}
	if b, _ := os.ReadFile(put); string(b) != novoe {
		t.Fatalf("на месте putExe не новое содержимое: %q", b)
	}
	if b, _ := os.ReadFile(put + ".old"); string(b) != "STARYY" {
		t.Fatalf("хвост .old не сохранил старое содержимое: %q", b)
	}
}

// TestPostavitOtkatVozvrashchaetStaroe — второе переименование не встаёт
// НИКОГДА, но откат (staryy -> putExe) проходит: человек обязан остаться с
// работающей (пусть и старой) программой, а Postavit — вернуть ошибку.
func TestPostavitOtkatVozvrashchaetStaroe(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	if err := os.WriteFile(put, []byte("STARYY"), 0o755); err != nil {
		t.Fatal(err)
	}
	novoe := "NOVYY-KELEVRA"
	s := server(t, novoe)

	// Откат (ot==staryy, kuda==put) должен пройти, а вот именно ПОСТАНОВКА
	// нового (ot==vremennyy, kuda==put) — падать всегда. Различаем по имени
	// ot: новый временный файл — это .kelevra-*.new.
	podmenitPovtory(t, func(ot, kuda string) error {
		if kuda == put && strings.Contains(filepath.Base(ot), ".kelevra-") {
			return fmt.Errorf("занято антивирусом навсегда")
		}
		return os.Rename(ot, kuda)
	})

	err := Postavit(context.Background(), s.Client(),
		Novaya{Versiya: "0.5.0", Ssylka: s.URL, Razmer: int64(len(novoe))}, put)
	if err == nil {
		t.Fatal("постоянный отказ второго переименования должен вернуть ошибку")
	}
	if b, _ := os.ReadFile(put); string(b) != "STARYY" {
		t.Fatalf("человек остался без рабочей программы: putExe=%q, ждал откат к STARYY", b)
	}
}

// TestPostavitKopiyaKogdaOtkatNeVstal — самый тяжёлый случай 28.08: и
// постановка нового, и откат старого через os.Rename проваливаются всегда.
// Последний рубеж — копия содержимого staryy в putExe: человек всё равно
// обязан остаться с рабочей программой, а текст ошибки — подсказать про .old.
func TestPostavitKopiyaKogdaOtkatNeVstal(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	if err := os.WriteFile(put, []byte("STARYY"), 0o755); err != nil {
		t.Fatal(err)
	}
	novoe := "NOVYY-KELEVRA"
	s := server(t, novoe)

	podmenitPovtory(t, func(ot, kuda string) error {
		if kuda == put {
			// И постановка нового, и откат старого — оба переименования в
			// putExe — не встают НИКОГДА.
			return fmt.Errorf("putExe занято навсегда")
		}
		return os.Rename(ot, kuda)
	})

	err := Postavit(context.Background(), s.Client(),
		Novaya{Versiya: "0.5.0", Ssylka: s.URL, Razmer: int64(len(novoe))}, put)
	if err == nil {
		t.Fatal("постоянный отказ и постановки, и отката должен вернуть ошибку")
	}
	if b, _ := os.ReadFile(put); string(b) != "STARYY" {
		t.Fatalf("человек остался без рабочей программы: putExe=%q, ждал STARYY через копию", b)
	}
	if !strings.Contains(err.Error(), ".old") {
		t.Fatalf("ошибка обязана подсказать про файл .old, получил: %v", err)
	}
}

// TestUbratHvostChistitOboiHvostaIMusor — UbratHvost сносит и штатный .old, и
// след уже исправленного бага (имя без .exe), и забытые временные файлы, но
// не трогает сам putExe и посторонний файл рядом.
func TestUbratHvostChistitOboiHvostaIMusor(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	staryyOld := put + ".old"                                                 // Kelevra.exe.old
	bezRasshireniyaOld := strings.TrimSuffix(put, filepath.Ext(put)) + ".old" // Kelevra.old
	musor := filepath.Join(papka, ".kelevra-abc123.new")
	postoronniy := filepath.Join(papka, "chuzhoy-fayl.txt")

	for _, p := range []string{put, staryyOld, bezRasshireniyaOld, musor, postoronniy} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	UbratHvost(put)

	for _, p := range []string{staryyOld, bezRasshireniyaOld, musor} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("хвост %s не убран", p)
		}
	}
	for _, p := range []string{put, postoronniy} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("UbratHvost тронул чужое: %s пропал (%v)", p, err)
		}
	}
}

// Дальше — четыре сцены беды 28-29.08, ради которых Postavit и переписан
// («нажал обновился, у меня теперь тупо 2 kelevra.exe.old висят и нихуя»;
// «обновления все ещё как то через жопу работают так что я снёс тупо старую
// версию и качнул с гитхаба новую»). Общее требование у всех одно: ПОСЛЕ
// ЛЮБОГО ИСХОДА по пути putExe лежит работающий файл.

// podmenitProverku подменяет проверку годности нового файла на время теста —
// тем же приёмом, каким podmenitPovtory подменяет переименование.
func podmenitProverku(t *testing.T, svoya func(novyy, obrazets string) error) {
	t.Helper()
	prezhnyaya := proveritNovyy
	proveritNovyy = svoya
	t.Cleanup(func() { proveritNovyy = prezhnyaya })
}

// TestNovyyNeSkachalsyaStaryyNaMeste — сцена «новый файл не скачался».
// GitHub отдал 404 (релиз снесли, ссылка протухла): Postavit обязан не
// тронуть ни putExe, ни завести .old, ни оставить временный файл.
func TestNovyyNeSkachalsyaStaryyNaMeste(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	if err := os.WriteFile(put, []byte("STARYY-RABOCHIY"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer s.Close()

	err := Postavit(context.Background(), s.Client(),
		Novaya{Versiya: "0.5.0", Ssylka: s.URL, Razmer: 100}, put)
	if err == nil {
		t.Fatal("404 при скачивании обязан вернуть ошибку")
	}
	if b, _ := os.ReadFile(put); string(b) != "STARYY-RABOCHIY" {
		t.Fatalf("несостоявшаяся закачка тронула рабочее приложение: %q", b)
	}
	if _, err := os.Stat(put + ".old"); !os.IsNotExist(err) {
		t.Fatal("хвост .old заведён на пустом месте — старое приложение никто не отодвигал")
	}
	musor, _ := filepath.Glob(filepath.Join(papka, ".kelevra-*.new"))
	if len(musor) != 0 {
		t.Fatalf("временные файлы остались мусором: %v", musor)
	}
}

// TestSkachalasZaglushkaVmestoSborki — «скачалось, но это не приложение».
// Ровно то, что подсовывает проксёр или упавший GitHub: вместо .exe приходит
// HTML-страница нужного размера. До 31.08 она вставала на место рабочего
// файла как есть. Теперь Postavit узнаёт её ДО того, как тронет putExe.
func TestSkachalasZaglushkaVmestoSborki(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	// Настоящий Windows-.exe начинается с "MZ" — подделываем именно род файла,
	// содержимое дальше не важно ни проверке, ни тесту.
	staroe := "MZ\x90\x00STARAYA-SBORKA"
	if err := os.WriteFile(put, []byte(staroe), 0o755); err != nil {
		t.Fatal(err)
	}
	zaglushka := "<html><body>502 Bad Gateway</body></html>"
	s := server(t, zaglushka)

	err := Postavit(context.Background(), s.Client(),
		Novaya{Versiya: "0.5.0", Ssylka: s.URL, Razmer: int64(len(zaglushka))}, put)
	if err == nil {
		t.Fatal("страница-заглушка не должна вставать на место приложения")
	}
	if b, _ := os.ReadFile(put); string(b) != staroe {
		t.Fatalf("заглушка добралась до рабочего приложения: %q", b)
	}
	if _, err := os.Stat(put + ".old"); !os.IsNotExist(err) {
		t.Fatal("хвост .old заведён зря: до отодвигания старого файла дело дойти не должно")
	}
}

// TestNovyyVstalNerabochimOtkatIHvostUbran — сцена «новый скачался, но не
// запустился». Файл прошёл проверку до замены, встал на место, а на месте
// оказался негодным (антивирус обрезал или подменил свежий .exe уже после
// переименования). Требование: откат на старую сборку И убранный хвост .old
// — 28.08 у человека не сработало ни то, ни другое.
func TestNovyyVstalNerabochimOtkatIHvostUbran(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	if err := os.WriteFile(put, []byte("STARYY-RABOCHIY"), 0o755); err != nil {
		t.Fatal(err)
	}
	novoe := "NOVYY-NO-MYORTVYY"
	s := server(t, novoe)

	// Проверка ДО замены (образец — putExe) проходит, проверка ПОСЛЕ замены
	// (образец — уже отодвинутый .old) падает: различаем по имени образца.
	podmenitProverku(t, func(novyy, obrazets string) error {
		if strings.HasSuffix(obrazets, ".old") {
			return fmt.Errorf("новая сборка не запускается")
		}
		return nil
	})

	err := Postavit(context.Background(), s.Client(),
		Novaya{Versiya: "0.5.0", Ssylka: s.URL, Razmer: int64(len(novoe))}, put)
	if err == nil {
		t.Fatal("нерабочая новая сборка обязана вернуть ошибку, а не тихо остаться на месте")
	}
	if b, _ := os.ReadFile(put); string(b) != "STARYY-RABOCHIY" {
		t.Fatalf("отката не было, человек остался с нерабочим приложением: %q", b)
	}
	if _, err := os.Stat(put + ".old"); !os.IsNotExist(err) {
		t.Fatal("хвост .old остался висеть после отката — ровно та картина 28.08")
	}
	musor, _ := filepath.Glob(filepath.Join(papka, ".kelevra-*.new"))
	if len(musor) != 0 {
		t.Fatalf("временные файлы остались мусором: %v", musor)
	}
}

// TestOtkatKopieyTozheUbiraetHvost — тот же отказ проверки после замены, но
// вдобавок откат переименованием не встаёт (putExe занят навсегда) и
// работает только копия. Хвост .old обязан быть убран и в этом случае: он
// больше не нужен, а уборка при следующем запуске 28.08 не случилась —
// её код жил внутри исчезнувшего .exe.
func TestOtkatKopieyTozheUbiraetHvost(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	if err := os.WriteFile(put, []byte("STARYY-RABOCHIY"), 0o755); err != nil {
		t.Fatal(err)
	}
	novoe := "NOVYY-NO-MYORTVYY"
	s := server(t, novoe)

	podmenitProverku(t, func(novyy, obrazets string) error {
		if strings.HasSuffix(obrazets, ".old") {
			return fmt.Errorf("новая сборка не запускается")
		}
		return nil
	})
	// Отодвинуть старое (kuda == .old) можно, а вот вернуть что-либо в putExe
	// переименованием — нельзя никогда. Остаётся копия.
	podmenitPovtory(t, func(ot, kuda string) error {
		if kuda == put && strings.HasSuffix(ot, ".old") {
			return fmt.Errorf("putExe занято навсегда")
		}
		return os.Rename(ot, kuda)
	})

	err := Postavit(context.Background(), s.Client(),
		Novaya{Versiya: "0.5.0", Ssylka: s.URL, Razmer: int64(len(novoe))}, put)
	if err == nil {
		t.Fatal("нерабочая новая сборка обязана вернуть ошибку")
	}
	if b, _ := os.ReadFile(put); string(b) != "STARYY-RABOCHIY" {
		t.Fatalf("копия не вернула рабочее приложение: %q", b)
	}
	if _, err := os.Stat(put + ".old"); !os.IsNotExist(err) {
		t.Fatal("хвост .old не убран после отката копией")
	}
}

// TestDvaObnovleniyaPodryadNePlodyatHvosty — «2 kelevra.exe.old висят»
// дословно. Два удачных обновления подряд обязаны оставить рядом с
// приложением РОВНО ОДИН хвост, а UbratHvost — снести и его.
func TestDvaObnovleniyaPodryadNePlodyatHvosty(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	if err := os.WriteFile(put, []byte("SBORKA-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sborka := range []string{"SBORKA-2", "SBORKA-3"} {
		s := server(t, sborka)
		if err := Postavit(context.Background(), s.Client(),
			Novaya{Versiya: "0.5.0", Ssylka: s.URL, Razmer: int64(len(sborka))}, put); err != nil {
			t.Fatalf("обновление до %s не встало: %v", sborka, err)
		}
		if b, _ := os.ReadFile(put); string(b) != sborka {
			t.Fatalf("после обновления до %s на месте приложения %q", sborka, b)
		}
	}

	hvosty, _ := filepath.Glob(filepath.Join(papka, "*.old"))
	if len(hvosty) != 1 {
		t.Fatalf("после двух обновлений подряд хвостов %d (%v), а должен быть ровно один", len(hvosty), hvosty)
	}
	// Хвост от ВТОРОГО обновления, а не окаменевший от первого.
	if b, _ := os.ReadFile(put + ".old"); string(b) != "SBORKA-2" {
		t.Fatalf("хвост .old держит не предыдущую сборку: %q", b)
	}

	UbratHvost(put)
	hvosty, _ = filepath.Glob(filepath.Join(papka, "*.old"))
	if len(hvosty) != 0 {
		t.Fatalf("UbratHvost не убрал хвосты: %v", hvosty)
	}
	if b, _ := os.ReadFile(put); string(b) != "SBORKA-3" {
		t.Fatalf("UbratHvost задел само приложение: %q", b)
	}
}

// ХОД СКАЧИВАНИЯ (02.09). Окно показывало «Скачиваю…» с неопределённой
// полосой, потому что доли не существовало нигде: io.Copy считал байты только
// себе. На телефоне в этом же месте стоит настоящее «Скачиваю… 47%».
//
// sborkaDliny собирает сборку заведомо длиннее одного буфера io.Copy (32КБ):
// доля, которую доложили один раз в самом конце, ничем не отличается от
// отсутствия доли, и тест обязан видеть ПРОМЕЖУТОЧНЫЕ доклады, а не только
// последний.
func sborkaDliny(bayt int) string { return strings.Repeat("K", bayt) }

// dokladchik — общий сбор докладов Hod для тестов ниже. Замка нет и не нужно:
// Hod зовётся из того же хода io.Copy, что и чтение тела, — одной горутиной.
func dokladchik() (Hod, *[][2]int64) {
	var vse [][2]int64
	return func(skachano, vsego int64) {
		vse = append(vse, [2]int64{skachano, vsego})
	}, &vse
}

func TestHodSchitaetDolyuPoHoduSkachivaniya(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	if err := os.WriteFile(put, []byte("STARYY"), 0o755); err != nil {
		t.Fatal(err)
	}
	novoe := sborkaDliny(200_000)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Длину называем САМИ: тело крупнее буфера ответа, и без этого
		// заголовка Go ушёл бы в chunked — то есть в сцену следующего теста.
		w.Header().Set("Content-Length", strconv.Itoa(len(novoe)))
		fmt.Fprint(w, novoe)
	}))
	defer s.Close()

	hod, dokladi := dokladchik()
	err := PostavitSHodom(context.Background(), s.Client(),
		Novaya{Versiya: "0.5.0", Ssylka: s.URL, Razmer: int64(len(novoe))}, put, hod)
	if err != nil {
		t.Fatalf("не поставилось: %v", err)
	}
	d := *dokladi
	if len(d) < 3 {
		t.Fatalf("докладов о ходе %d — доля, доложенная разом в конце, это не ход", len(d))
	}
	var promezhutochnyy bool
	var proshlyy int64
	for i, p := range d {
		skachano, vsego := p[0], p[1]
		if vsego != int64(len(novoe)) {
			t.Fatalf("доклад %d: знаменатель %d, а длина ответа %d", i, vsego, len(novoe))
		}
		if skachano < proshlyy {
			t.Fatalf("доклад %d: скачано %d, а до того было %d — доля пошла назад", i, skachano, proshlyy)
		}
		if skachano > vsego {
			t.Fatalf("доклад %d: скачано %d из %d — доля больше единицы", i, skachano, vsego)
		}
		proshlyy = skachano
		if skachano > 0 && skachano < vsego {
			promezhutochnyy = true
		}
	}
	if !promezhutochnyy {
		t.Fatal("ни одного доклада между началом и концом — окну нечего показывать, пока идёт загрузка")
	}
	if proshlyy != int64(len(novoe)) {
		t.Fatalf("последний доклад %d байт из %d — конец скачивания не доложен", proshlyy, len(novoe))
	}
}

// Сервер не назвал длины ответа (chunked) — доля неизвестна, и это ЗАКОННЫЙ
// случай, а не беда: обновление обязано встать, а окно — остаться на
// неопределённой полосе вместо выдуманного процента.
func TestHodBezDlinyOtvetaNeVydumyvaetDolyu(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	if err := os.WriteFile(put, []byte("STARYY"), 0o755); err != nil {
		t.Fatal(err)
	}
	novoe := sborkaDliny(200_000)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Content-Length НЕ ставим: тело длиннее буфера ответа, и Go сам
		// уходит в chunked — у клиента ContentLength == -1.
		fmt.Fprint(w, novoe)
	}))
	defer s.Close()

	hod, dokladi := dokladchik()
	err := PostavitSHodom(context.Background(), s.Client(),
		Novaya{Versiya: "0.5.0", Ssylka: s.URL, Razmer: int64(len(novoe))}, put, hod)
	if err != nil {
		t.Fatalf("без длины ответа обновление не встало: %v", err)
	}
	if b, _ := os.ReadFile(put); string(b) != novoe {
		t.Fatalf("на месте приложения не новая сборка (%d байт)", len(b))
	}
	d := *dokladi
	if len(d) == 0 {
		t.Fatal("ни одного доклада о ходе")
	}
	for i, p := range d {
		if p[1] != 0 {
			t.Fatalf("доклад %d назвал знаменатель %d, хотя длины ответа сервер не сообщал — "+
				"это выдуманная доля", i, p[1])
		}
	}
	if d[len(d)-1][0] != int64(len(novoe)) {
		t.Fatalf("байты и без знаменателя обязаны считаться: досчитали до %d из %d",
			d[len(d)-1][0], len(novoe))
	}
}

// Обрыв посреди закачки. Проверяем не только отказ (это уже делает
// TestOborvannayaZakachkaNeStavitsya), а то, ЧЕМ кончился ход: последний
// доклад обязан остаться НЕПОЛНЫМ. Доложенная единица на оборванной закачке
// — это «100%» в окне на пустом месте, и следом «не удалось поставить».
func TestHodPriObryveNeDohoditDoEdinicy(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	if err := os.WriteFile(put, []byte("STARYY"), 0o755); err != nil {
		t.Fatal(err)
	}
	polovina := sborkaDliny(100_000)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Обещаем вдвое больше, чем отдаём, и обрываем ответ — ровно то, что
		// видит клиент при потере связи посреди загрузки.
		w.Header().Set("Content-Length", strconv.Itoa(2*len(polovina)))
		fmt.Fprint(w, polovina)
	}))
	defer s.Close()

	hod, dokladi := dokladchik()
	err := PostavitSHodom(context.Background(), s.Client(),
		Novaya{Versiya: "0.5.0", Ssylka: s.URL, Razmer: int64(2 * len(polovina))}, put, hod)
	if err == nil {
		t.Fatal("обрыв закачки прошёл как успех")
	}
	if b, _ := os.ReadFile(put); string(b) != "STARYY" {
		t.Fatalf("рабочее приложение испорчено: %q", b)
	}
	d := *dokladi
	if len(d) == 0 {
		t.Fatal("ни одного доклада о ходе")
	}
	posledniy := d[len(d)-1]
	if posledniy[1] != int64(2*len(polovina)) {
		t.Fatalf("знаменатель последнего доклада %d, а сервер обещал %d", posledniy[1], 2*len(polovina))
	}
	if posledniy[0] >= posledniy[1] {
		t.Fatalf("оборванная закачка доложила %d из %d — окно показало бы 100%% там, "+
			"где ничего не встало", posledniy[0], posledniy[1])
	}
}
