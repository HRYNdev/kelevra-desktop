package sluzhba

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Записки нет ВООБЩЕ (снесена уборкой, не успела записаться, либо человек
// впервые обновился со старой версии), а собственное ядро прошлой копии живо.
// До правки поиск по образу (nashiYadra/prinyatSirotuPoObrazu) звался ТОЛЬКО
// из default-ветки, куда попадают, если записка есть, — ветка !est (priyom_
// yadra.go, старые строки 50-59) уходила в podnyatSvyazPosleSmenyVersii/return,
// не спросив ОС ни разу. Сирота без записки была невидима навсегда: служба
// говорила «stoit», «Отключить» не гасило ничего, «Подключиться» поднимало
// второе ядро рядом.
func TestSirotaPoObrazuBezZapiskiPrinimaetsya(t *testing.T) {
	s := stendSPravami(t)
	s.zapustitYadro = func(ctx context.Context) error {
		t.Fatal("своё живое ядро нашлось без записки — поднимать второе нельзя")
		return nil
	}
	s.Nastroyki.RabotalaVersiya = "0.6.69"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(srv.Close)
	api := strings.TrimPrefix(srv.URL, "http://")

	svoy, err := os.Executable()
	if err != nil {
		t.Skipf("не нашёл свой exe: %v", err)
	}
	if !yadro.ObrazNash(os.Getpid(), svoy) {
		t.Skip("ОС не подтвердила образ своего процесса на этой платформе — проверка недоказательна")
	}
	// «Своё ядро» — этот же тестовый процесс.
	s.Yadro.Bin, s.Yadro.Api = svoy, api

	var pogasheno []int
	staroe := pogasitChuzhoe
	pogasitChuzhoe = func(pid int) { pogasheno = append(pogasheno, pid) }
	t.Cleanup(func() { pogasitChuzhoe = staroe })
	staryyPoisk := nashiYadra
	nashiYadra = func(bin string) []int { return []int{os.Getpid()} }
	t.Cleanup(func() { nashiYadra = staryyPoisk })

	papka := hranenie.PapkaYadra()
	// Записки НЕТ — ключевое условие теста. yadro.ProchestPeredachu(papka)
	// должен отвечать est==false.
	if _, est := yadro.ProchestPeredachu(papka); est {
		t.Fatalf("записка неожиданно найдена в чистом каталоге стенда")
	}

	s.PrinyatZhivoeYadro(context.Background(), "")

	if s.Yadro.Sost() != yadro.Rabotaet {
		t.Fatalf("своё ядро без записки не принято: состояние %v", s.Yadro.Sost())
	}
	if len(pogasheno) != 0 {
		t.Fatalf("при приёме без записки что-то погашено: %v", pogasheno)
	}
	if p, est := yadro.ProchestPeredachu(papka); !est || p.PID != os.Getpid() {
		t.Fatalf("записка не создана на номер найденного ядра: %+v", p)
	}
}
