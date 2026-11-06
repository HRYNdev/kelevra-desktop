package yadro

import (
	"os"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
)

// Старые проверки идут на заглушках без подписи; подпись ядра проверяет
// podpis_yadra_test.go (там требование включается обратно).
func TestMain(m *testing.M) {
	// Лже-ядро для проверки конфига (proverka_test.go): тестовый exe,
	// запущенный как `<ядро> check -c <конфиг> -D <папка>`, отвечает по
	// KELEVRA_LZHE_CHECK, настоящее ядро на машине не запускается.
	if rezhim := os.Getenv("KELEVRA_LZHE_CHECK"); rezhim != "" && len(os.Args) > 1 && os.Args[1] == "check" {
		os.Exit(lzheCheck(rezhim, os.Args[2:]))
	}
	obnovlenie.TrebovatPodpis = false
	os.Exit(m.Run())
}
