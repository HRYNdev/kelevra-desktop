package ochered

import (
	"context"
	"testing"
	"time"
)

// Пороги очереди из пульта (uzel.porog_otkazov, uzel.vozvrat_na_pervyy_min):
// заданы — действуют они; 0 — константы, поведение прежнее.
func TestPorogIzPulta(t *testing.T) {
	sey := time.Unix(1000, 0)
	p := &podstava{vybrano: "Нидерланды", mertvye: map[string]bool{"Нидерланды": true}}
	o := ochered(p, &sey)
	o.Porog = func() int { return 3 }
	o.Shag(context.Background())
	if o.Shag(context.Background()) {
		t.Fatal("при пороге 3 переключились со второго провала")
	}
	if !o.Shag(context.Background()) || p.vybrano != "Финляндия" {
		t.Fatalf("с третьего провала ждал Финляндию, стоим на %q", p.vybrano)
	}

	p2 := &podstava{vybrano: "Финляндия", mertvye: map[string]bool{}}
	o2 := ochered(p2, &sey)
	o2.Vozvrat = func() time.Duration { return time.Minute }
	o2.Porog = func() int { return 0 } // 0 — константа
	o2.Shag(context.Background())
	sey = sey.Add(time.Minute)
	if !o2.Shag(context.Background()) || p2.vybrano != "Нидерланды" {
		t.Fatalf("возврат через минуту из пульта не случился, стоим на %q", p2.vybrano)
	}
}
