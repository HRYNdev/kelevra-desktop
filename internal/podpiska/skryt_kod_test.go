package podpiska

import (
	"context"
	"strings"
	"testing"
)

func TestSkrytKodVStroke(t *testing.T) {
	const kod = "ABCD1234efgh"
	for _, c := range []struct{ vhod, ozhid string }{
		{"Post \"https://x.example/k/" + kod + "/ustroystvo\": dial tcp: refused", "Post \"https://x.example/k/***gh/ustroystvo\": dial tcp: refused"},
		{"https://x.example/u/" + kod, "https://x.example/u/***gh"},
		{"https://x.example/s/" + kod + "/info?device=1", "https://x.example/s/***gh/info?device=1"},
		{"без кода", "без кода"},
	} {
		if got := SkrytKod(c.vhod); got != c.ozhid {
			t.Errorf("SkrytKod(%q) = %q, ждали %q", c.vhod, got, c.ozhid)
		}
	}
}

// Сетевой отказ Voyti не должен нести код целиком (журнал уезжает на сервер).
func TestVoytiOshibkaSetiBezKoda(t *testing.T) {
	const kod = "SEKRETNYYkod99"
	k := &Klient{Shema: "http"}
	_, err := k.Voyti(context.Background(), kod, []string{"127.0.0.1:1"})
	if err == nil {
		t.Fatal("ждали ошибку сети")
	}
	if strings.Contains(err.Error(), kod) {
		t.Fatalf("код в тексте ошибки: %v", err)
	}
	if !strings.Contains(SkrytKod(err.Error()), "***") && !strings.Contains(err.Error(), "***") {
		t.Fatalf("маски нет: %v", err)
	}
}
