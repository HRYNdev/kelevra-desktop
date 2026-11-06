package yadro

import (
	"os"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/lzheyadro"
	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
)

// Старые проверки идут на заглушках без подписи; подпись ядра проверяет
// podpis_yadra_test.go (там требование включается обратно).
func TestMain(m *testing.M) {
	obnovlenie.TrebovatPodpis = false
	kod := m.Run()
	lzheyadro.Ubrat() // лже-ядро проверок слежки (storozh*_test.go)
	os.Exit(kod)
}
