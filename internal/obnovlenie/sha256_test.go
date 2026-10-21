package obnovlenie

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Сборка из пульта (блок obnovlenie): сумма из подписанного пульта сверяется
// до подписи и до того, как тронут рабочий exe. Не сошлась — файл удалён,
// рабочее приложение на месте.
func TestSha256IzPultaNeSoshlas(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	_ = os.WriteFile(put, []byte("STARYY"), 0o755)
	novoe := "NOVYY-KELEVRA"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, novoe) }))
	defer s.Close()

	err := Postavit(context.Background(), s.Client(),
		Novaya{Versiya: "0.6.90", Ssylka: s.URL, Razmer: int64(len(novoe)), Sha256: strings.Repeat("0", 64)}, put)
	if !errors.Is(err, ErrSha256) {
		t.Fatalf("ждали ErrSha256, получили %v", err)
	}
	if b, _ := os.ReadFile(put); string(b) != "STARYY" {
		t.Fatalf("рабочее приложение тронуто: %q", b)
	}
	ostalos, _ := filepath.Glob(filepath.Join(papka, ".kelevra-*.new"))
	if len(ostalos) != 0 {
		t.Fatalf("скачанный файл не удалён: %v", ostalos)
	}
}

func TestSha256IzPultaSoshlas(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "Kelevra.exe")
	_ = os.WriteFile(put, []byte("STARYY"), 0o755)
	novoe := "NOVYY-KELEVRA"
	summa := sha256.Sum256([]byte(novoe))
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, novoe) }))
	defer s.Close()

	err := Postavit(context.Background(), s.Client(),
		Novaya{Versiya: "0.6.90", Ssylka: s.URL, Razmer: int64(len(novoe)), Sha256: strings.ToUpper(hex.EncodeToString(summa[:]))}, put)
	if err != nil {
		t.Fatalf("сумма сошлась, а не поставилось: %v", err)
	}
	if b, _ := os.ReadFile(put); string(b) != novoe {
		t.Fatalf("на месте приложения %q", b)
	}
	UbratHvost(put)
}
