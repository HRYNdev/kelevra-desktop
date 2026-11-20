//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Один значок на сеанс на Linux (znachok_odin.go): замок — flock на файле в
// XDG_RUNTIME_DIR (на /tmp, если его нет), держится всю жизнь процесса
// значка и отпускается ядром при его смерти. Значок версий без замка
// (0.6.111 и старше) виден по процессу --trey этого человека.
//
// До 0.6.112 на Linux замок всегда считался «взятым», а значок — «живым»:
// окно значок не поднимало, и после закрытия окна без автозапуска
// приложение пропадало с глаз (стенд 313, 20.11.2026).

var fdZamkaZnachka = -1

func putZamkaZnachka() string {
	d := os.Getenv("XDG_RUNTIME_DIR")
	if d == "" {
		d = os.TempDir()
	}
	return filepath.Join(d, fmt.Sprintf("kelevra-znachok-%d.lock", os.Getuid()))
}

// zanyatMestoZnachka — взять замок значка (ждать до srok). true — взят и
// держится до конца процесса.
func zanyatMestoZnachka(srok time.Duration) bool {
	fd, err := syscall.Open(putZamkaZnachka(), syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return true // замка не завести — не мешаем значку встать
	}
	konec := time.Now().Add(srok)
	for {
		if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) == nil {
			fdZamkaZnachka = fd
			return true
		}
		if time.Now().After(konec) {
			syscall.Close(fd)
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// zamokZnachkaZanyat — замок держит другой процесс.
func zamokZnachkaZanyat() bool {
	fd, err := syscall.Open(putZamkaZnachka(), syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return false
	}
	defer syscall.Close(fd)
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) == nil {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		return false
	}
	return true
}

// oknoTreyaEst — живой процесс значка (--trey) этого человека, кроме нас.
func oknoTreyaEst() bool {
	svoy, uid := os.Getpid(), os.Getuid()
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
		if err != nil {
			continue
		}
		if processZnachka(strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")) {
			return true
		}
	}
	return false
}

// processZnachka — строка запуска значка Kelevra (чистая функция для проверки).
func processZnachka(cmdline []string) bool {
	if len(cmdline) < 2 {
		return false
	}
	imya := hvostObnovleniya.ReplaceAllString(filepath.Base(cmdline[0]), "")
	if !strings.HasPrefix(imya, "kelevra") && !strings.HasPrefix(imya, "Kelevra") {
		return false
	}
	for _, a := range cmdline[1:] {
		if a == argTrey {
			return true
		}
	}
	return false
}

func znachokZhivOS() bool {
	return zamokZnachkaZanyat() || oknoTreyaEst()
}
