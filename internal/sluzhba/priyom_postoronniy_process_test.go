package sluzhba

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Записка указывает на ЖИВОЙ посторонний процесс (номер переиспользован), а
// служебный порт из записки отвечает — например, его держит настоящее ядро
// под другим номером. Yadro.Prinyat честно отвечает «под номером сейчас чужая
// программа», и default-ветка PrinyatZhivoeYadro раньше гасила процесс по
// номеру из записки, не спросив, чей он. Посторонний процесс обязан выжить.
//
// Проверено живьём на стенде 13.09.2026 (Windows 11, ping -t под номером из
// записки, порт ядра отвечает).
func TestNeOpoznannayaZapiskaNeGasitPostoronniyProcess(t *testing.T) {
	s := stendSPravami(t)
	s.zapustitYadro = func(ctx context.Context) error { return nil }
	s.Nastroyki.RabotalaVersiya = "0.6.69"
	// Своего ядра по образу нет — проверяется только то, что посторонний
	// процесс из записки не гасится.
	staryyPoisk := nashiYadra
	nashiYadra = func(bin string) []int { return nil }
	t.Cleanup(func() { nashiYadra = staryyPoisk })

	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var postoronniy *exec.Cmd
	if runtime.GOOS == "windows" {
		postoronniy = exec.Command("ping", "-n", "30", "127.0.0.1")
	} else {
		postoronniy = exec.Command("sleep", "30")
	}
	if err := postoronniy.Start(); err != nil {
		t.Skipf("не смог поднять посторонний процесс: %v", err)
	}
	t.Cleanup(func() { _ = postoronniy.Process.Kill() })
	umer := make(chan struct{})
	go func() { _ = postoronniy.Wait(); close(umer) }()

	yadro.ZapisatPeredachu(hranenie.PapkaYadra(), yadro.Peredacha{
		PID:     postoronniy.Process.Pid,
		Bin:     s.Yadro.Bin,
		Api:     strings.TrimPrefix(srv.URL, "http://"),
		Adapter: "tun125",
	})

	s.PrinyatZhivoeYadro(context.Background(), "")

	select {
	case <-umer:
		t.Fatalf("посторонний процесс (pid %d) убит: записка не опознана, а default-ветка погасила процесс по номеру, не спросив ОС, чей он",
			postoronniy.Process.Pid)
	case <-time.After(500 * time.Millisecond):
	}
}

// Своё ядро, которое принять не вышло, гасится по-прежнему, и связь после
// смены версии возвращается, даже когда следа туннеля нет: при живом ядре из
// записки уборка в main след не запоминает, и адаптер берётся из записки.
func TestPogashennoeSvoyoYadroSvyazVozvrashchaetsya(t *testing.T) {
	s := stendSPravami(t)
	podyomov := 0
	s.zapustitYadro = func(ctx context.Context) error { podyomov++; return nil }
	s.Nastroyki.RabotalaVersiya = "0.6.69"

	var pogasheno []int
	staroe := pogasitChuzhoe
	pogasitChuzhoe = func(pid int) { pogasheno = append(pogasheno, pid) }
	t.Cleanup(func() { pogasitChuzhoe = staroe })

	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// «Своё ядро» — этот же тестовый процесс: его образ ОС знает. Принять его
	// не выйдет, потому что записка называет другой бинарь (y.Bin не совпадает
	// со строкой записки), а гасить можно — образ подтверждён.
	svoy, err := os.Executable()
	if err != nil {
		t.Skipf("не нашёл свой exe: %v", err)
	}
	s.Yadro.Bin = svoy
	pid := os.Getpid()
	if !yadro.ObrazNash(pid, svoy) {
		t.Skip("ОС не подтвердила образ своего процесса на этой платформе — проверка недоказательна")
	}
	yadro.ZapisatPeredachu(hranenie.PapkaYadra(), yadro.Peredacha{
		PID:     pid,
		Bin:     svoy + ".drugoy",
		Api:     strings.TrimPrefix(srv.URL, "http://"),
		Adapter: "tun125",
	})

	s.PrinyatZhivoeYadro(context.Background(), "")

	if len(pogasheno) != 1 || pogasheno[0] != pid {
		t.Fatalf("своё непринятое живое ядро не погашено: %v", pogasheno)
	}
	if podyomov == 0 {
		t.Fatal("своё ядро погасили, а связь после смены версии не поднята: адаптер из записки не использован")
	}
}
