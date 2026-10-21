package podpiska

import (
	"context"
	"strings"
	"testing"
)

func TestSkrytKodVStroke(t *testing.T) {
	const kod = "ABCD1234efgh"
	for _, c := range []struct{ vhod, ozhid string }{
		{"Post \"https://x.example/k/" + kod + "/ustroystvo\": dial tcp: refused", "Post \"https://x.example/k/AB…/ustroystvo\": dial tcp: refused"},
		{"https://x.example/u/" + kod, "https://x.example/u/AB…"},
		{"https://x.example/s/" + kod + "/info?device=1", "https://x.example/s/AB…/info?device=1"},
		{"https://x.example/i/" + kod, "https://x.example/i/AB…"},
		{"https://x.example/panel/vhod/" + kod + "?x=1", "https://x.example/panel/vhod/AB…?x=1"},
		// короткий код начала не оставляет вовсе — иначе «маска» показала бы его целиком
		{"https://x.example/k/AB12CD", "https://x.example/k/…"},
		// проверка живости сервера — не код
		{"https://x.example/k/health", "https://x.example/k/health"},
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
	if !strings.Contains(err.Error(), "/SE…") {
		t.Fatalf("маски нет: %v", err)
	}
}
