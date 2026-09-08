package sluzhba

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// Разъяснение после обновления показывается человеку РОВНО ОДИН раз.
//
// Место опасное: окно опрашивает состояние раз в две секунды, и признак,
// который не гаснет, превращает разовую карточку в прилипшую к экрану
// помеху. А признак, который гаснет слишком рано (до того, как окно вообще
// спросило), не показывает её вовсе — и человек снова не узнает, что
// изменилось. Обе беды тихие: приложение работает, просто говорит не то.
//
// Поэтому тест держит обе границы, а не одну.

func sluzhbaDlyaProverki(t *testing.T) *Sluzhba {
	t.Helper()
	t.Setenv("KELEVRA_DIR", t.TempDir())
	s, err := Novaya()
	if err != nil {
		t.Fatalf("не поднял службу: %v", err)
	}
	return s
}

// sostoyanieOkna — один опрос состояния, как его делает окно.
// (имя sprositSostoyanie в пакете уже занято соседним тестом обновления)
func sostoyanieOkna(t *testing.T, s *Sluzhba) otvetSostoyaniya {
	t.Helper()
	zapis := httptest.NewRecorder()
	s.sostoyanie(zapis, httptest.NewRequest(http.MethodGet, "/api/sostoyanie?okno=1", nil))
	if zapis.Code != http.StatusOK {
		t.Fatalf("состояние ответило кодом %d", zapis.Code)
	}
	var o otvetSostoyaniya
	if err := json.Unmarshal(zapis.Body.Bytes(), &o); err != nil {
		t.Fatalf("состояние не разобралось: %v", err)
	}
	return o
}

func TestRazyasneniePosleObnovleniyaPokazyvaetsyaOdinRaz(t *testing.T) {
	s := sluzhbaDlyaProverki(t)

	// Обычный запуск: рассказывать нечего.
	if v := sostoyanieOkna(t, s).PosleObnovleniya; v != "" {
		t.Fatalf("без обновления окно получило разъяснение %q — карточка вылезет на ровном месте", v)
	}

	s.SkazatChtoPosleObnovleniya("0.6.52")

	// Первый опрос забирает разъяснение — ради него всё и затевалось.
	pervyy := sostoyanieOkna(t, s).PosleObnovleniya
	if pervyy != "0.6.52" {
		t.Fatalf("первый опрос получил %q, а обновились на 0.6.52 — человек не узнает, что изменилось", pervyy)
	}

	// Второй опрос — уже нет: иначе карточка висит на экране до конца сеанса.
	if vtoroy := sostoyanieOkna(t, s).PosleObnovleniya; vtoroy != "" {
		t.Fatalf("второй опрос снова получил %q — карточка прилипнет к экрану", vtoroy)
	}
}

// Ручка /api/posle_obnovleniya — то, чем окно (отдельный процесс) сообщает
// службе о смене после обновления. Версию служба берёт свою: чужую строку из
// запроса в окно человека пускать нельзя.
func TestRuchkaPosleObnovleniyaBeryotSvoyuVersiyu(t *testing.T) {
	s := sluzhbaDlyaProverki(t)

	zapis := httptest.NewRecorder()
	s.posleObnovleniyaRuchka(zapis, httptest.NewRequest(http.MethodPost, "/api/posle_obnovleniya", nil))
	if zapis.Code != http.StatusOK {
		t.Fatalf("ручка ответила кодом %d", zapis.Code)
	}
	if v := sostoyanieOkna(t, s).PosleObnovleniya; v != podpiska.Versiya {
		t.Fatalf("окну уехала версия %q, а работает %q", v, podpiska.Versiya)
	}
}

// GET эту ручку не трогает: состояние приложения меняет только POST.
func TestRuchkaPosleObnovleniyaNeSluchaynymGET(t *testing.T) {
	s := sluzhbaDlyaProverki(t)

	zapis := httptest.NewRecorder()
	s.posleObnovleniyaRuchka(zapis, httptest.NewRequest(http.MethodGet, "/api/posle_obnovleniya", nil))
	if zapis.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET получил код %d, а менять состояние должен только POST", zapis.Code)
	}
	if v := sostoyanieOkna(t, s).PosleObnovleniya; v != "" {
		t.Fatalf("после GET окно получило разъяснение %q — карточка вылезет от чужого запроса", v)
	}
}
