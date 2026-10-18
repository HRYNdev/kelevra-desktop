package yadro

import (
	"os"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
)

// Старые проверки идут на заглушках без подписи; подпись ядра проверяет
// podpis_yadra_test.go (там требование включается обратно).
func TestMain(m *testing.M) {
	obnovlenie.TrebovatPodpis = false
	os.Exit(m.Run())
}
