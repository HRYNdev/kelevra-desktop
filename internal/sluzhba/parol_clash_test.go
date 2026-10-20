package sluzhba

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// Пароль Clash API, лежавший до 0.6.80 открытым, меняется при запуске ядра с
// нуля, и запускается ядро уже с новым: в конфиге и у службы один и тот же
// пароль, иначе каждый её запрос к ядру получал бы 401.
func TestStaryyParolMenyaetsyaPriZapuskeYadra(t *testing.T) {
	s := gotovStendLestnicy(t)
	staryy := strings.Repeat("ab", 32) // как писали версии до 0.6.80
	if err := os.WriteFile(hranenie.PutSekretaClash(), []byte(staryy), 0o600); err != nil {
		t.Fatal(err)
	}
	// Пересборка конфига без запуска ядра (обновление профиля и т.п.) пароль
	// не меняет: ядро, возможно, уже живёт с ним.
	if err := s.PerestroitKonfig(); err != nil {
		t.Fatal(err)
	}
	if s.Yadro.Sekret != staryy {
		t.Fatalf("пересборка без запуска ядра сменила пароль: %q", s.Yadro.Sekret)
	}

	zapushcheno := 0
	var vKonfige string
	s.zapustitYadro = func(ctx context.Context) error {
		zapushcheno++
		b, err := os.ReadFile(s.Yadro.PutKonfiga())
		if err != nil {
			return err
		}
		vKonfige = string(b)
		return nil
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("PodnyatZashchitu: %v", err)
	}
	if zapushcheno != 1 {
		t.Fatalf("ядро запущено %d раз, ждал 1", zapushcheno)
	}
	novyy := hranenie.SekretClash()
	if novyy == staryy || !strings.HasPrefix(novyy, "k2-") {
		t.Fatalf("пароль не сменён перед запуском: %q", novyy)
	}
	if s.Yadro.Sekret != novyy {
		t.Fatalf("служба ходит к ядру с %q, а в файле %q", s.Yadro.Sekret, novyy)
	}
	if !strings.Contains(vKonfige, novyy) || strings.Contains(vKonfige, staryy) {
		t.Fatal("ядро запущено с конфигом без нового пароля")
	}
}
