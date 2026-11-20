//go:build linux

package main

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Окна прошлой версии после обновления службы (Linux). Окно 0.6.111 и
// старше сторожа не имеет и после обновления висит на мёртвом адресе
// («Kelevra перезапускается…» навсегда, стенд 313, 20.11.2026). Новый
// значок (--trey --smena) поднимается сразу после обновления — он и
// закрывает такие окна и открывает окно новой версии взамен.
//
// Окно прошлой версии — процесс этого же человека, запущенный без
// аргументов (так запускают окно значок, ярлык и автозапуск), чей файл
// убран обновлением: /proc/<pid>/exe кончается на « (deleted)».

// oknoProshloyVersii — по строке запуска и цели /proc/<pid>/exe (чистая
// функция для проверки).
func oknoProshloyVersii(cmdline []string, exe, imya string) bool {
	if len(cmdline) != 1 || !strings.HasSuffix(exe, " (deleted)") || filepath.Base(cmdline[0]) != imya {
		return false
	}
	return hvostObnovleniya.ReplaceAllString(filepath.Base(strings.TrimSuffix(exe, " (deleted)")), "") == imya
}

// oknoNyneshneyVersii — окно, чей файл на месте (уже новая версия).
func oknoNyneshneyVersii(cmdline []string, exe, imya string) bool {
	return len(cmdline) == 1 && filepath.Base(cmdline[0]) == imya &&
		!strings.HasSuffix(exe, " (deleted)") && filepath.Base(exe) == imya
}

// oknaKelevra — pid окон этого человека: прошлой версии и нынешней.
func oknaKelevra() (proshlye, nyneshnie []int) {
	svoy := os.Getpid()
	uid := os.Getuid()
	imya := "kelevra"
	if sebya, err := os.Executable(); err == nil {
		imya = hvostObnovleniya.ReplaceAllString(filepath.Base(sebya), "")
	}
	zapisi, _ := os.ReadDir("/proc")
	for _, z := range zapisi {
		pid, err := strconv.Atoi(z.Name())
		if err != nil || pid == svoy {
			continue
		}
		var st syscall.Stat_t
		if syscall.Stat("/proc/"+z.Name(), &st) != nil || int(st.Uid) != uid {
			continue
		}
		b, err := os.ReadFile("/proc/" + z.Name() + "/cmdline")
		if err != nil || len(b) == 0 {
			continue
		}
		cmdline := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
		exe, _ := os.Readlink("/proc/" + z.Name() + "/exe")
		switch {
		case oknoProshloyVersii(cmdline, exe, imya):
			proshlye = append(proshlye, pid)
		case oknoNyneshneyVersii(cmdline, exe, imya):
			nyneshnie = append(nyneshnie, pid)
		}
	}
	return proshlye, nyneshnie
}

// zamenitOknaProshloyVersii — закрыть окна прошлой версии и, если они были,
// а окна нынешней ещё нет, открыть его.
func zamenitOknaProshloyVersii() {
	proshlye, _ := oknaKelevra()
	if len(proshlye) == 0 {
		return
	}
	log.Printf("значок: окна прошлой версии после обновления (pid %v) — закрываю и открываю окно новой", proshlye)
	for _, pid := range proshlye {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	for i := 0; i < 30; i++ {
		zhivye := 0
		for _, pid := range proshlye {
			if syscall.Kill(pid, 0) == nil {
				zhivye++
			}
		}
		if zhivye == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, pid := range proshlye {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	if _, nyneshnie := oknaKelevra(); len(nyneshnie) > 0 {
		log.Printf("значок: окно новой версии уже открыто (pid %v)", nyneshnie)
		return
	}
	otkrytOknoIzTreya()
}
