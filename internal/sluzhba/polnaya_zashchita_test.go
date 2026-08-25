package sluzhba

import (
	"errors"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/kopiya"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Беда 25.08 («2 нахуй открыто»): метка снималась ДО окна UAC, поэтому
// повышенная копия, ничего не зная о старой, стартовала как первая. Починка —
// метка живёт у этой копии до её смерти, а новая копия узнаёт о старой по
// pid, переданному аргументом --smena (см. cmd/kelevra/main.go: zhdatSmenu).
// Этот тест проверяет обе половины: метка не трогается ДО и ВО ВРЕМЯ окна
// UAC, а свой pid действительно уходит в poprositPrava.
func TestMetkaZhivetVoVremyaUACIPidPeredan(t *testing.T) {
	papka := t.TempDir()
	t.Setenv("KELEVRA_DIR", papka)
	t.Setenv("KELEVRA_PRAVA", "net") // штатный рычаг стенда: под root права «уже есть»
	if err := kopiya.Zanyat(papka, "http://127.0.0.1:1/klyuch/", time.Now()); err != nil {
		t.Fatal(err)
	}

	var metkaVoVremyaUAC, sprosili bool
	var poluchennyyPID int
	s := &Sluzhba{}
	// Отказом обрываем путь до os.Exit: здесь мерится только момент вызова.
	s.poprositPrava = func(smenaPID int) error {
		_, err := os.Stat(kopiya.Metka(papka))
		metkaVoVremyaUAC, sprosili = err == nil, true
		poluchennyyPID = smenaPID
		return errors.New("права не выданы")
	}

	zapis := httptest.NewRecorder()
	s.polnayaZashchita(zapis, httptest.NewRequest("POST", "/api/polnaya_zashchita", nil))

	if !sprosili {
		t.Fatal("обработчик не дошёл до запроса прав — тест ничего не измерил")
	}
	if !metkaVoVremyaUAC {
		t.Fatal("метка исчезла до окна UAC — новая копия увидит «приложения нет» и стартует как первая (беда 25.08)")
	}
	if poluchennyyPID != os.Getpid() {
		t.Fatalf("poprositPrava получил pid %d, а должен был получить свой же %d — новой копии нечем будет ждать смерть старой", poluchennyyPID, os.Getpid())
	}
}

// Отказ в правах не должен стоить приложению метки: копия продолжает
// работать, а метку никто не трогал — она просто остаётся на месте.
func TestOtkazVPravahOstavlyaetMetkuNaMeste(t *testing.T) {
	papka := t.TempDir()
	t.Setenv("KELEVRA_DIR", papka)
	t.Setenv("KELEVRA_PRAVA", "net") // штатный рычаг стенда: под root права «уже есть»
	adres := "http://127.0.0.1:1/klyuch/"
	if err := kopiya.Zanyat(papka, adres, time.Now()); err != nil {
		t.Fatal(err)
	}

	s := &Sluzhba{}
	s.poprositPrava = func(smenaPID int) error { return errors.New("права не выданы") }

	zapis := httptest.NewRecorder()
	s.polnayaZashchita(zapis, httptest.NewRequest("POST", "/api/polnaya_zashchita", nil))

	if zapis.Code != 400 {
		t.Fatalf("отказ в правах должен уехать в окно ошибкой, а код %d", zapis.Code)
	}
	if _, err := os.Stat(kopiya.Metka(papka)); err != nil {
		t.Fatal("метка пропала после отказа в правах: второй запуск поднимет второе ядро")
	}
}

// Согласие: копия уходит, а метку она с собой НЕ забирает — снять её обязана
// новая копия, дождавшись смерти старой (cmd/kelevra/main.go: zhdatSmenu).
// Если бы эта, уходящая копия сама убирала метку, вернулась бы прежняя
// гонка: os.Exit ниже пропускает defer'ы zapustitSluzhbu, и метка либо
// пропадала бы раньше времени, либо не пропадала вовсе.
func TestSoglasieNeTrogaetMetkuSama(t *testing.T) {
	papka := t.TempDir()
	t.Setenv("KELEVRA_DIR", papka)
	t.Setenv("KELEVRA_PRAVA", "net") // штатный рычаг стенда: под root права «уже есть»
	adres := "http://127.0.0.1:1/klyuch/"
	if err := kopiya.Zanyat(papka, adres, time.Now()); err != nil {
		t.Fatal(err)
	}

	ushel := make(chan struct{})
	s := &Sluzhba{Yadro: &yadro.Yadro{}}
	s.poprositPrava = func(smenaPID int) error { return nil }
	s.vyhod = func() { close(ushel) }

	zapis := httptest.NewRecorder()
	s.polnayaZashchita(zapis, httptest.NewRequest("POST", "/api/polnaya_zashchita", nil))
	if zapis.Code != 200 {
		t.Fatalf("согласие должно вернуться в окно успехом, а код %d", zapis.Code)
	}
	select {
	case <-ushel:
	case <-time.After(3 * time.Second):
		t.Fatal("копия не ушла после согласия на права — на машине останутся две")
	}
	if _, err := os.Stat(kopiya.Metka(papka)); err != nil {
		t.Fatal("метка пропала сама, хотя убирать её должна новая копия, а не эта, уходящая")
	}
}
