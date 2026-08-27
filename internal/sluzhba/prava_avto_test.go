package sluzhba

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// TestPervoePodklyuchenieSamoSprashivaetPrava — сердце задачи: первое
// успешное подключение с профилем, которому нужен туннель (EstTunnel), на
// СВЕЖИХ настройках (файла не было, hranenie.Zagruzit уже разрешила вопрос)
// само один раз спрашивает права администратора — тем же путём (poprositPrava),
// каким это делает кнопка «Включить для всех программ». Второй коннект после
// этого просить права заново не должен: они уже спрошены (или отказаны) и
// отмечены в Nastroyki.
func TestPervoePodklyuchenieSamoSprashivaetPrava(t *testing.T) {
	s := gotovStendLestnicy(t)
	s.zapustitYadro = func(ctx context.Context) error { return nil }

	sprosheno := make(chan int, 8)
	s.poprositPrava = func(smenaPID int) error {
		sprosheno <- smenaPID
		return errors.New("права не выданы") // отказ — тест мерит только сам факт вопроса
	}
	s.vyhod = func() {} // на случай согласия (тут его не будет) — не гасить тестовый процесс

	// Сигнал из продукта, а не опрос: zaprositPravaAvtomaticheskiEsliNado
	// зовёт этот хук РОВНО когда отметка уже дописана на диск (см. sluzhba.go).
	// Опрос s.Nastroyki.UzheSprosiliPrava() из этой же горутины без канала —
	// это и есть та гонка, которую ловил go test -race: 27.08.
	gotovo := make(chan struct{}, 1)
	s.posleAvtozaprosaPrav = func() { gotovo <- struct{}{} }

	m := s.Obsluzhit()
	postPodklyuchit := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/podklyuchit", nil)
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		return w
	}

	if w := postPodklyuchit(); w.Code != 200 {
		t.Fatalf("первый коннект вернул код %d: %s", w.Code, w.Body.String())
	}

	select {
	case <-sprosheno:
	case <-time.After(3 * time.Second):
		t.Fatal("после первого успешного подключения с EstTunnel=true на свежих настройках права не были запрошены сами")
	}

	// Отказ пишется на диск асинхронно (та же горутина) — дождаться сигнала
	// от продукта, что запись завершена, прежде чем звать второй коннект.
	select {
	case <-gotovo:
	case <-time.After(3 * time.Second):
		t.Fatal("после отказа в правах Nastroyki.PravaZaprosheny не проставился в разумный срок")
	}

	if w := postPodklyuchit(); w.Code != 200 {
		t.Fatalf("второй коннект вернул код %d: %s", w.Code, w.Body.String())
	}

	select {
	case pid := <-sprosheno:
		t.Fatalf("права запрошены повторно (pid %d) — после первого раза (отказ или согласие) второй коннект не должен спрашивать снова", pid)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestAvtozaprosSohranyaetDoUhoda — правка 27.08: при согласии на права
// отметка обязана лечь на диск РАНЬШЕ, чем эта копия начнёт уходить, иначе
// не доехавшая до диска запись заставит следующий (уже обычный, НЕ
// повышенный) запуск спросить права ЕЩЁ РАЗ. Разницу временем не поймать —
// у настоящего ухода есть запас 300 мс (uydiPosleSoglasiyaNaPrava), в который
// быстрый Sohranit на t.TempDir() укладывается что до, что после спавна той
// горутины. Порядок ловим прямой записью двух синхронных меток.
func TestAvtozaprosSohranyaetDoUhoda(t *testing.T) {
	s := gotovStendLestnicy(t)
	s.zapustitYadro = func(ctx context.Context) error { return nil }
	s.poprositPrava = func(smenaPID int) error { return nil } // согласие
	s.vyhod = func() {}                                       // не гасить тестовый процесс

	var zamok sync.Mutex
	var poryadok []string
	metka := func(sobytie string) {
		zamok.Lock()
		poryadok = append(poryadok, sobytie)
		zamok.Unlock()
	}
	s.sohranitNastroyki = func() error {
		metka("sohranil")
		return hranenie.Sohranit(s.Nastroyki)
	}
	s.priUhode = func() { metka("uydi") }

	gotovo := make(chan struct{}, 1)
	s.posleAvtozaprosaPrav = func() { gotovo <- struct{}{} }

	m := s.Obsluzhit()
	r := httptest.NewRequest("POST", "/"+s.klyuch+"/api/podklyuchit", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("коннект вернул код %d: %s", w.Code, w.Body.String())
	}

	select {
	case <-gotovo:
	case <-time.After(3 * time.Second):
		t.Fatal("автозапрос прав при согласии не завершился в разумный срок")
	}

	zamok.Lock()
	defer zamok.Unlock()
	if len(poryadok) != 2 || poryadok[0] != "sohranil" || poryadok[1] != "uydi" {
		t.Fatalf("сохранение отметки должно случиться ДО ухода, а порядок вышел %v", poryadok)
	}
}
