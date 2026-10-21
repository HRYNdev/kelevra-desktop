package yadro

import (
	"context"
	"net/http"
	"testing"
)

// Порядок версий сборок: semver sing-box (пробные ниже релиза), затем номер сборки.
func TestPoryadokVersiySborok(t *testing.T) {
	poryadok := []string{
		"core-v1.13.9-1",
		"core-v1.14.0-alpha.2-5",
		"core-v1.14.0-beta.4-1",
		"core-v1.14.0-beta.4-2",
		"core-v1.14.0-beta.10-1",
		"core-v1.14.0-rc.1-1",
		"core-v1.14.0-1",
		"core-v1.14.1-1",
		"core-v1.14.1-2",
		"core-v1.14.1-10",
		"core-v2.0.0-1",
	}
	for i := 0; i < len(poryadok); i++ {
		for j := 0; j < len(poryadok); j++ {
			if got, hochu := NoveeTega(poryadok[i], poryadok[j]), i > j; got != hochu {
				t.Errorf("NoveeTega(%s, %s) = %v, ждал %v", poryadok[i], poryadok[j], got, hochu)
			}
		}
	}
}

func TestNerazbiraemyeTegi(t *testing.T) {
	for _, teg := range []string{"core-nightly", "core-v1.14.1", "core-vx.y.z-1", "core-v1.14.1-", "core-v1.14.0--1", "core-v1.2.3.4-1", "app-v0.6.84"} {
		if _, err := razobratTeg(teg); err == nil {
			t.Errorf("тег %q разобран, ждал отказ", teg)
		}
		if NoveeTega(teg, "core-v0.0.1-1") {
			t.Errorf("неразбираемый %q объявлен новее", teg)
		}
	}
}

// Сборка со старым ядром выложена ПОЗЖЕ новой (первой в списке GitHub) — не берётся.
func TestStarayaVylozhennayaPozzheNeBeretsya(t *testing.T) {
	y := stendSmeny(t, "",
		sborkaStenda{teg: "core-v1.14.0-beta.4-3", yadro: "staroe", podpisano: "staroe"},
		sborkaStenda{teg: "core-v1.14.1-1", yadro: "novoe", podpisano: "novoe"},
	)
	s, err := y.vybratSborku(context.Background(), http.DefaultClient, true)
	if err != nil || s.Teg != "core-v1.14.1-1" {
		t.Fatalf("выбрана %q (%v), ждал core-v1.14.1-1", s.Teg, err)
	}
}

// Неразбираемый тег пропускается; если годных нет вовсе — ошибка, а не он.
func TestNerazbiraemyyTegPropuskaetsya(t *testing.T) {
	y := stendSmeny(t, "",
		sborkaStenda{teg: "core-nightly", yadro: "x", podpisano: "x"},
		sborkaStenda{teg: "core-v1.14.0-beta.4-1", yadro: "v", podpisano: "v"},
	)
	s, err := y.vybratSborku(context.Background(), http.DefaultClient, true)
	if err != nil || s.Teg != "core-v1.14.0-beta.4-1" {
		t.Fatalf("выбрана %q (%v)", s.Teg, err)
	}
	y = stendSmeny(t, "", sborkaStenda{teg: "core-nightly", yadro: "x", podpisano: "x"})
	if s, err := y.vybratSborku(context.Background(), http.DefaultClient, true); err == nil {
		t.Fatalf("взята неразбираемая сборка %q", s.Teg)
	}
}

// Стоит сборка новее или равная той, что в релизах, — на замену ничего не идёт.
func TestNeNoveeStoyashcheyNeOtkladyvaetsya(t *testing.T) {
	for _, sluchay := range []struct{ stoit, vRelizah string }{
		{"core-v1.14.1-2", "core-v1.14.1-1"},        // в релизах старее
		{"core-v1.14.1-1", "core-v1.14.0-beta.4-1"}, // в релизах старее (пробная sing-box)
		{"core-v1.14.1-1", "core-v1.14.1-1"},        // равная
	} {
		y := stendSmeny(t, "stoit", sborkaStenda{teg: sluchay.vRelizah, yadro: "drugoe", podpisano: "drugoe"})
		y.zapisatVersiyu(sluchay.stoit)
		itog, err := y.Prigotovit(context.Background(), http.DefaultClient)
		if err != nil || itog.Otlozheno {
			t.Fatalf("стоит %s, в релизах %s: отложено %v (%v)", sluchay.stoit, sluchay.vRelizah, itog.Otlozheno, err)
		}
		if estFayl(y.putArhivaNovogo()) {
			t.Fatalf("стоит %s: архив %s отложен", sluchay.stoit, sluchay.vRelizah)
		}
		if s, _ := y.Smenit(); s != nil || soderzhimoe(t, y.Bin) != "stoit" {
			t.Fatalf("стоит %s: ядро сменилось на %s", sluchay.stoit, sluchay.vRelizah)
		}
	}
}
