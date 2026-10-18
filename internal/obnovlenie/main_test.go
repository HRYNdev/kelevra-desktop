package obnovlenie

import (
	"os"
	"testing"
)

// Старые проверки установки идут на заглушках без подписи; подпись проверяют
// podpis_test.go (там требование включается обратно) и пакет internal/podpis.
func TestMain(m *testing.M) {
	TrebovatPodpis = false
	os.Exit(m.Run())
}
