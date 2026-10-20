package sluzhba

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/zhurnaly"
)

// KELEVRA_BEZ_OTPRAVKI_ZHURNALOV=1 гасит не только расписание, но и срочную
// посылку после неудачи связи: копия на стенде не шлёт журнал никуда.
func TestBezOtpravkiZhurnalovGasitSrochnuyu(t *testing.T) {
	s := stend(t)

	var prishlo atomic.Int32
	priyomnik := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prishlo.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer priyomnik.Close()

	putZhurnala := filepath.Join(t.TempDir(), "kelevra.log")
	if err := os.WriteFile(putZhurnala, []byte("связь не поднялась"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.otpravshchikZhurnalovDlyaStenda = &zhurnaly.Otpravshchik{
		Adres:    priyomnik.URL + "/logs",
		DeviceID: "stend",
		Versiya:  "stend",
		Puti:     []string{putZhurnala},
		PutMetok: filepath.Join(t.TempDir(), "otmetki.json"),
	}

	t.Setenv("KELEVRA_BEZ_OTPRAVKI_ZHURNALOV", "1")
	s.OtpravitZhurnalySrochno(context.Background())
	if n := prishlo.Load(); n != 0 {
		t.Fatalf("при KELEVRA_BEZ_OTPRAVKI_ZHURNALOV=1 срочный журнал ушёл (%d запросов)", n)
	}

	// Контроль: без выключателя тот же отправщик доходит до приёмника.
	t.Setenv("KELEVRA_BEZ_OTPRAVKI_ZHURNALOV", "")
	s.OtpravitZhurnalySrochno(context.Background())
	if prishlo.Load() == 0 {
		t.Fatal("без выключателя срочный журнал не ушёл — проверка выше ничего не доказывает")
	}
}
