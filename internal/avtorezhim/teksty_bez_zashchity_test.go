package avtorezhim

import (
	"strings"
	"testing"
)

// TestDeystvieStringBezZashchity — сторож правила владельца (09.09): слово
// «защита»/«защищено» в текстах, которые видит человек, запрещено — Kelevra
// не защищает, она направляет трафик через туннель. Deystvie.String() уходит
// в журнал службы (sluzhba.avtorezhimKolbek), поэтому ни один вариант не
// смеет вернуть «защит».
func TestDeystvieStringBezZashchity(t *testing.T) {
	for _, d := range []Deystvie{NeTrogat, Opustit, Podnyat} {
		if got := d.String(); strings.Contains(got, "защит") {
			t.Fatalf("Deystvie(%d).String() = %q — содержит запрещённое слово «защит»", d, got)
		}
	}
}
