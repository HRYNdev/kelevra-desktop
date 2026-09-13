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

// Стенд 13.09.2026, п.3: номер в записке указывает на посторонний процесс,
// а туннель держит НАШЕ ядро под другим номером. Раньше приём уходил в
// default-ветку, посторонний не трогал (правильно), но и своё ядро не находил:
// служба отвечала «stoit», «Отключить» не гасило ни ядро, ни адаптер, а
// «Подключиться» поднимало второе ядро рядом. Своё ядро обязано найтись по
// образу и быть принятым.
func TestSirotaPoObrazuPrinimaetsya(t *testing.T) {
	s := stendSPravami(t)
	s.zapustitYadro = func(ctx context.Context) error {
		t.Fatal("своё живое ядро нашлось — поднимать второе нельзя")
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
	// «Своё ядро» — этот же тестовый процесс: образ ОС знает, а гасить его
	// приём не станет (принятое ядро гасит только Ostanovit).
	s.Yadro.Bin, s.Yadro.Api = svoy, api

	postoronniy := postoronniyProcess(t)
	umer := make(chan struct{})
	go func() { _ = postoronniy.Wait(); close(umer) }()

	var pogasheno []int
	staroe := pogasitChuzhoe
	pogasitChuzhoe = func(pid int) { pogasheno = append(pogasheno, pid) }
	t.Cleanup(func() { pogasitChuzhoe = staroe })
	staryyPoisk := nashiYadra
	nashiYadra = func(bin string) []int { return []int{os.Getpid()} }
	t.Cleanup(func() { nashiYadra = staryyPoisk })

	papka := hranenie.PapkaYadra()
	yadro.ZapisatPeredachu(papka, yadro.Peredacha{PID: postoronniy.Process.Pid, Bin: svoy, Api: api, Adapter: "tun125"})

	s.PrinyatZhivoeYadro(context.Background(), "")

	if s.Yadro.Sost() != yadro.Rabotaet {
		t.Fatalf("своё ядро не принято: состояние %v", s.Yadro.Sost())
	}
	if len(pogasheno) != 0 {
		t.Fatalf("при приёме что-то погашено: %v", pogasheno)
	}
	if p, est := yadro.ProchestPeredachu(papka); !est || p.PID != os.Getpid() {
		t.Fatalf("записка не переписана на номер найденного ядра: %+v", p)
	}
	select {
	case <-umer:
		t.Fatal("посторонний процесс из записки убит")
	case <-time.After(300 * time.Millisecond):
	}
}

// Своё ядро нашлось по образу, но принять его нельзя (здесь: записка называет
// другой бинарь) — гасим ИМЕННО его, раз ОС подтвердила образ, посторонний не
// трогаем, а связь после смены версии поднимаем заново на адаптере из записки.
func TestSirotaPoObrazuNeprinyatayaGasitsya(t *testing.T) {
	s := stendSPravami(t)
	podyomov := 0
	s.zapustitYadro = func(ctx context.Context) error { podyomov++; return nil }
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
	s.Yadro.Bin, s.Yadro.Api = svoy, api

	postoronniy := postoronniyProcess(t)
	umer := make(chan struct{})
	go func() { _ = postoronniy.Wait(); close(umer) }()

	var pogasheno []int
	staroe := pogasitChuzhoe
	pogasitChuzhoe = func(pid int) { pogasheno = append(pogasheno, pid) }
	t.Cleanup(func() { pogasitChuzhoe = staroe })
	staryyPoisk := nashiYadra
	nashiYadra = func(bin string) []int { return []int{os.Getpid()} }
	t.Cleanup(func() { nashiYadra = staryyPoisk })

	yadro.ZapisatPeredachu(hranenie.PapkaYadra(), yadro.Peredacha{
		PID: postoronniy.Process.Pid, Bin: svoy + ".drugoy", Api: api, Adapter: "tun125",
	})

	s.PrinyatZhivoeYadro(context.Background(), "")

	if len(pogasheno) != 1 || pogasheno[0] != os.Getpid() {
		t.Fatalf("гасить надо было ровно найденное своё ядро (pid %d), погашено: %v", os.Getpid(), pogasheno)
	}
	if podyomov == 0 {
		t.Fatal("своё ядро погашено, а связь после смены версии не поднята")
	}
	select {
	case <-umer:
		t.Fatal("посторонний процесс из записки убит")
	case <-time.After(300 * time.Millisecond):
	}
}

func postoronniyProcess(t *testing.T) *exec.Cmd {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("ping", "-n", "30", "127.0.0.1")
	} else {
		cmd = exec.Command("sleep", "30")
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("не смог поднять посторонний процесс: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd
}
