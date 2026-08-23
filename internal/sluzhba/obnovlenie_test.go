package sluzhba

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// Пункт «Проверить обновление» в настройках (index.html) появился 23.08:
// обновление и так ставится само и молча (cmd/kelevra/obnovlenie.go), но
// человеку негде спросить прямо сейчас, свежая у него версия или нет
// (эталон телефона — SimpleSettingsScreen.kt). Тесты ниже держат саму
// ручку /api/obnovlenie (internal/sluzhba/sluzhba.go).

func relizObnovleniya(teg, fayl string, razmer int) string {
	return fmt.Sprintf(`[{"tag_name":%q,"draft":false,"prerelease":false,"assets":[{"name":%q,"browser_download_url":"http://primer/%s","size":%d}]}]`,
		teg, fayl, teg, razmer)
}

func podmenitVersiyu(t *testing.T, novaya string) {
	t.Helper()
	stariy := podpiska.Versiya
	podpiska.Versiya = novaya
	t.Cleanup(func() { podpiska.Versiya = stariy })
}

func TestObnovlenieRuchkaVidyatNovuyuVersiyu(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()
	podmenitVersiyu(t, "0.4.0")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, relizObnovleniya("app-v0.9.0", obnovlenie.ImyaFayla, 42))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KELEVRA_RELIZY", srv.URL)

	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/obnovlenie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("код ответа %d: %s", w.Code, w.Body.String())
	}
	var o otvetObnovleniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	if o.Novaya != "0.9.0" {
		t.Fatalf("не увидел новую версию: %+v", o)
	}
	if o.Beda != "" {
		t.Fatalf("не ждали беды: %+v", o)
	}
	if o.Tekushchaya != "0.4.0" {
		t.Fatalf("не сказал текущую версию: %+v", o)
	}
}

func TestObnovlenieRuchkaSvezhayaVersiyaBezNovoy(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()
	podmenitVersiyu(t, "0.9.0")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, relizObnovleniya("app-v0.9.0", obnovlenie.ImyaFayla, 42))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KELEVRA_RELIZY", srv.URL)

	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/obnovlenie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	var o otvetObnovleniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	if o.Novaya != "" || o.Beda != "" {
		t.Fatalf("на своей же версии не ждали ни обновления, ни беды: %+v", o)
	}
}

// Сеть на стенде окна (stend/oblik_snimok.py) намеренно недоступна — ручка
// обязана ответить понятной бедой, а не повесить запрос: окно не должно
// зависнуть на «Проверяем…» навсегда (хозяин 22.08, эталон — телефон).
func TestObnovlenieRuchkaChestnoOtvechaetBezSeti(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()
	t.Setenv("KELEVRA_RELIZY", "http://127.0.0.1:1/nikogo-tam-net")

	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/obnovlenie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("ошибка сети не должна ронять ручку: код %d, %s", w.Code, w.Body.String())
	}
	var o otvetObnovleniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал ответ: %v", err)
	}
	if o.Beda == "" {
		t.Fatalf("ждали понятную беду при недоступной сети: %+v", o)
	}
	if strings.Contains(strings.ToLower(o.Beda), "http") || strings.Contains(o.Beda, "dial") {
		t.Fatalf("беда несёт сырой сетевой текст, а не понятную подпись: %q", o.Beda)
	}
}
