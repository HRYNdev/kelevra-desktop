package sluzhba

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Честный статус: окно не имеет права показывать «защищено» в прокси-режиме.
//
// Диагноз 31.08, дословно от человека: «впн не выполняет свою основную
// функцию». Приборно с его машины: в собранном yadro\config.json ровно один
// вход — mixed на 2412, туннель не поднимался; при этом окно рисовало тот же
// зелёный круг «подключено», что и при полном туннеле. Разница между режимами
// — пропасть: системный прокси накрывает только программы, которые его
// уважают, и только TCP. Весь UDP, а значит и QUIC, а значит и YouTube, идёт
// мимо, к провайдеру.

func TestObyomZashchityTablitsa(t *testing.T) {
	polnaya := konfig.Kartina{
		Rezhim:  konfig.Tunnel,
		Zametka: konfig.ZametkaVes,
	}
	polovinnaya := konfig.Kartina{
		Rezhim:              konfig.Proksi,
		Zametka:             konfig.ZametkaBezPrav,
		Chastichnaya:        true,
		PochemuChastichnaya: konfig.PrichinaBezPrav,
	}
	sluchai := []struct {
		imya         string
		sost         string
		k            konfig.Kartina
		zametka      string
		chastichnaya bool
		pochemu      string
		zachem       string
	}{
		{
			imya: "туннель поднят", sost: string(yadro.Rabotaet), k: polnaya,
			zametka: konfig.ZametkaVes, chastichnaya: false, pochemu: "",
			zachem: "полная защита не должна рисоваться жёлтым «частично»",
		},
		{
			imya: "прокси поднят", sost: string(yadro.Rabotaet), k: polovinnaya,
			zametka: konfig.ZametkaBezPrav, chastichnaya: true, pochemu: konfig.PrichinaBezPrav,
			zachem: "ровно та беда 31.08: половинная защита выглядела полной",
		},
		{
			imya: "прокси собран, но защита опущена", sost: string(yadro.Stoit), k: polovinnaya,
			zametka: "", chastichnaya: false, pochemu: "",
			zachem: "у опущенной защиты нет ни полной, ни половинной степени — есть «нет защиты»",
		},
		{
			imya: "ядро сломалось", sost: string(yadro.Slomalos), k: polovinnaya,
			zametka: "", chastichnaya: false, pochemu: "",
			zachem: "круг и так красный: жёлтое «частично» поверх беды сказало бы, что что-то защищено",
		},
		{
			imya: "ядро поднимается", sost: string(yadro.Podnimaem), k: polovinnaya,
			zametka: "", chastichnaya: false, pochemu: "",
			zachem: "пока ядро не ответило, объём защиты неизвестен — называть его значит гадать",
		},
	}
	for _, s := range sluchai {
		t.Run(s.imya, func(t *testing.T) {
			zametka, chastichnaya, pochemu := obyomZashchity(s.sost, s.k)
			if zametka != s.zametka || chastichnaya != s.chastichnaya || pochemu != s.pochemu {
				t.Fatalf("объём защиты = (%q, %v, %q), ждали (%q, %v, %q) — %s",
					zametka, chastichnaya, pochemu, s.zametka, s.chastichnaya, s.pochemu, s.zachem)
			}
		})
	}
}

// Тот же вопрос, но целиком через ручку: поле обязано доехать до окна под тем
// именем, которое окно читает (index.html: s.chastichnaya, s.pochemu_chastichnaya).
// Проверка мимо HTTP пропустила бы опечатку в json-теге.
func TestSostoyanieNeObyavlyaetPoloviuZashchituPokaYadroStoit(t *testing.T) {
	s := stend(t)
	m := s.Obsluzhit()

	s.zamok.Lock()
	s.kartina = konfig.Kartina{
		Rezhim:              konfig.Proksi,
		Zametka:             konfig.ZametkaBezPrav,
		Chastichnaya:        true,
		PochemuChastichnaya: konfig.PrichinaBezPrav,
	}
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
	if o.Chastichnaya || o.PochemuChastichnaya != "" {
		t.Fatalf("защита опущена, а окно получило (частичная %v, причина %q) — "+
			"круг нарисует жёлтое «частично» на выключенном VPN", o.Chastichnaya, o.PochemuChastichnaya)
	}
}

// Имена полей в JSON — договор с окном, и ломается он молча: окно прочитает
// undefined, нарисует зелёный круг и никто не заметит. Бьёмся о сырой JSON, а
// не о структуру Go.
func TestImenaPoleyChastichnoyZashchityNeMenyayutsya(t *testing.T) {
	b, err := json.Marshal(otvetSostoyaniya{
		Chastichnaya:        true,
		PochemuChastichnaya: konfig.PrichinaBezPrav,
	})
	if err != nil {
		t.Fatal(err)
	}
	var syroy map[string]any
	if err := json.Unmarshal(b, &syroy); err != nil {
		t.Fatal(err)
	}
	if v, _ := syroy["chastichnaya"].(bool); !v {
		t.Fatalf("в ответе нет поля chastichnaya=true (index.html читает именно его): %s", b)
	}
	if v, _ := syroy["pochemu_chastichnaya"].(string); v != konfig.PrichinaBezPrav {
		t.Fatalf("в ответе нет поля pochemu_chastichnaya: %s", b)
	}
}
