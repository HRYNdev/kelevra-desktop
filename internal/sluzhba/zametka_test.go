package sluzhba

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
)

// TestSostoyanieNeNesetZametkuKogdaZashchitaOpushchena — 22.08: s.kartina
// заполняется при сборке конфига и переживает OpustitZashchitu молча — ни
// ручной тумблер, ни авторежим (avtorezhimKolbek) её не чистят. Из-за этого
// /api/sostoyanie продолжало отдавать Zametka про ОБЪЁМ защиты («защищены
// только браузеры» и соседи, internal/konfig/konfig.go), пока сама защита
// стояла опущенной, — то есть не защищено было НИЧЕГО. Заметка обязана
// доехать до окна только тогда, когда ядро реально поднято (Sost==Rabotaet):
// иначе неважно, сколько появится новых путей опускания защиты — заметка
// будет врать на каждом из них.
func TestSostoyanieNeNesetZametkuKogdaZashchitaOpushchena(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()

	// Как будто конфиг только что собрали в режиме «только браузеры», а
	// затем защиту опустили — Yadro при этом стоит (Sost()==Stoit), ядро не
	// запускали вовсе.
	s.zamok.Lock()
	s.kartina = konfig.Kartina{Rezhim: konfig.Proksi, Zametka: konfig.ZametkaBezTunnelya}
	s.zamok.Unlock()

	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("код ответа %d: %s", w.Code, w.Body.String())
	}
	var o otvetSostoyaniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("не разобрал /api/sostoyanie: %v", err)
	}
	if o.Sost != "stoit" {
		t.Fatalf("стенд без запущенного ядра обязан отвечать sost=stoit, получил %q", o.Sost)
	}
	if o.Zametka != "" {
		t.Fatalf("защита опущена (sost=%q), а /api/sostoyanie всё равно несёт заметку "+
			"про объём защиты: %q — окно скажет человеку неправду", o.Sost, o.Zametka)
	}
}
