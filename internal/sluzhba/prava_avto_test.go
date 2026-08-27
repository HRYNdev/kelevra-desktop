package sluzhba

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"
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

	// Отказ пишется на диск асинхронно (та же горутина) — дождаться,
	// пока UzheSprosiliPrava() станет true, прежде чем звать второй коннект,
	// иначе тест ловит гонку, а не поведение.
	dedlayn := time.Now().Add(3 * time.Second)
	for !s.Nastroyki.UzheSprosiliPrava() {
		if time.Now().After(dedlayn) {
			t.Fatal("после отказа в правах Nastroyki.PravaZaprosheny не проставился в разумный срок")
		}
		time.Sleep(10 * time.Millisecond)
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
