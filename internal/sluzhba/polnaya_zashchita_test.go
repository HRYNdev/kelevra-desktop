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

// Гонка 23.08: копия, которую поднимает окно UAC, первым делом смотрит метку.
// Если метка ещё цела, копия решает «приложение уже работает» и открывает окно
// на адрес умирающей службы. Поэтому метка обязана исчезнуть ДО запроса прав.
func TestMetkaSnimaetsyaDoOknaUAC(t *testing.T) {
	papka := t.TempDir()
	t.Setenv("KELEVRA_DIR", papka)
	t.Setenv("KELEVRA_PRAVA", "net") // штатный рычаг стенда: под root права «уже есть»
	if err := kopiya.Zanyat(papka, "http://127.0.0.1:1/klyuch/", time.Now()); err != nil {
		t.Fatal(err)
	}

	var metkaVoVremyaUAC, sprosili bool
	s := &Sluzhba{Adres: "http://127.0.0.1:1/klyuch/"}
	// Отказом обрываем путь до os.Exit: здесь мерится только момент снятия метки.
	s.poprositPrava = func() error {
		_, err := os.Stat(kopiya.Metka(papka))
		metkaVoVremyaUAC, sprosili = err == nil, true
		return errors.New("права не выданы")
	}

	zapis := httptest.NewRecorder()
	s.polnayaZashchita(zapis, httptest.NewRequest("POST", "/api/polnaya_zashchita", nil))

	if !sprosili {
		t.Fatal("обработчик не дошёл до запроса прав — тест ничего не измерил")
	}
	if metkaVoVremyaUAC {
		t.Fatal("метка была жива в момент окна UAC — поднятая копия увидит труп службы и откроет окно в никуда")
	}
}

// Отказ в правах не должен стоить приложению метки: копия продолжает работать,
// а без метки следующий двойной щелчок поднимет второе ядро на занятые порты.
func TestOtkazVPravahVozvrashchaetMetku(t *testing.T) {
	papka := t.TempDir()
	t.Setenv("KELEVRA_DIR", papka)
	t.Setenv("KELEVRA_PRAVA", "net") // штатный рычаг стенда: под root права «уже есть»
	adres := "http://127.0.0.1:1/klyuch/"
	if err := kopiya.Zanyat(papka, adres, time.Now()); err != nil {
		t.Fatal(err)
	}

	s := &Sluzhba{Adres: adres}
	s.poprositPrava = func() error { return errors.New("права не выданы") }

	zapis := httptest.NewRecorder()
	s.polnayaZashchita(zapis, httptest.NewRequest("POST", "/api/polnaya_zashchita", nil))

	if zapis.Code != 400 {
		t.Fatalf("отказ в правах должен уехать в окно ошибкой, а код %d", zapis.Code)
	}
	if _, err := os.Stat(kopiya.Metka(papka)); err != nil {
		t.Fatal("метка не вернулась после отказа в правах: второй запуск поднимет второе ядро")
	}
}

// Согласие: копия уходит, и метка после неё НЕ остаётся — иначе следующий
// запуск открыл бы окно на адрес уже мёртвой службы.
func TestSoglasieUbiraetMetkuNasovsem(t *testing.T) {
	papka := t.TempDir()
	t.Setenv("KELEVRA_DIR", papka)
	t.Setenv("KELEVRA_PRAVA", "net") // штатный рычаг стенда: под root права «уже есть»
	adres := "http://127.0.0.1:1/klyuch/"
	if err := kopiya.Zanyat(papka, adres, time.Now()); err != nil {
		t.Fatal(err)
	}

	ushel := make(chan struct{})
	s := &Sluzhba{Adres: adres, Yadro: &yadro.Yadro{}}
	s.poprositPrava = func() error { return nil }
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
	if _, err := os.Stat(kopiya.Metka(papka)); err == nil {
		t.Fatal("метка пережила ушедшую копию: следующий запуск откроет окно в никуда")
	}
}
