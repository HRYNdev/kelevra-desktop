//go:build linux

package vinsluzhba

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

// Служба на Linux — системный юнит systemd от root. Делает то же, что служба
// Windows: держит туннель и запрет мимо туннеля одним хозяином на машину,
// поднимается при загрузке и после падения. Окно и значок живут в сеансе
// человека и ходят к ней по той же метке копии в общей папке.
const (
	ImyaYunita       = "kelevra.service"
	PutYunita        = "/etc/systemd/system/kelevra.service"
	KatalogUstanovki = "/opt/kelevra"
	PutUstanovki     = "/opt/kelevra/kelevra"
	// PeremennayaSluzhby ставит юнит: по ней копия узнаёт, что её поднял
	// systemd, а не человек тем же аргументом руками (тот же смысл, что
	// svc.IsWindowsService на Windows).
	PeremennayaSluzhby = "KELEVRA_SYSTEMD"
)

// tekstYunita — юнит службы.
//
// KillMode=process: при остановке и перезапуске systemd гасит только саму
// службу, а не её ядро. Ядро передаётся от старой копии к новой при
// обновлении (yadro/peredacha.go) — с убийством всей группы туннель рвался бы
// на каждом обновлении. Штатную остановку ядро получает от самой службы.
//
// Restart=on-failure: после обновления служба уходит с отказом (как служба
// Windows) — systemd поднимает её уже новой версией. Штатный выход (stop) —
// не повод поднимать.
func tekstYunita(putExe string) string {
	return fmt.Sprintf(`[Unit]
Description=Kelevra
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
ExecStart=%s %s
Environment=%s=1
Restart=on-failure
RestartSec=2
KillMode=process

[Install]
WantedBy=multi-user.target
`, putExe, Argument, PeremennayaSluzhby)
}

// PodSluzhboy — нас поднял systemd по нашему юниту.
func PodSluzhboy() bool {
	return os.Getenv(PeremennayaSluzhby) == "1" && os.Getenv("INVOCATION_ID") != ""
}

// Ustanovlena — лежит ли юнит службы.
func Ustanovlena() (bool, error) {
	_, err := os.Stat(PutYunita)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// Rabotaet — активна ли служба (is-active не требует прав).
func Rabotaet() (bool, error) {
	out, err := exec.Command("systemctl", "is-active", ImyaYunita).Output()
	s := strings.TrimSpace(string(out))
	switch s {
	case "active", "activating", "reloading":
		return true, nil
	case "":
		return false, fmt.Errorf("systemctl is-active: %w", err)
	}
	return false, nil
}

func systemctl(args ...string) error {
	if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Ustanovit пишет юнит на putExe, включает его и запускает.
func Ustanovit(putExe string) error {
	if err := os.WriteFile(PutYunita, []byte(tekstYunita(putExe)), 0o644); err != nil {
		return fmt.Errorf("юнит службы не записан: %w", err)
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	return systemctl("enable", "--now", ImyaYunita)
}

// Udalit останавливает и снимает службу; данные не трогает.
func Udalit() error {
	_ = systemctl("disable", "--now", ImyaYunita)
	if err := os.Remove(PutYunita); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return systemctl("daemon-reload")
}

func Zapustit() error  { return systemctl("start", ImyaYunita) }
func Ostanovit() error { return systemctl("stop", ImyaYunita) }

// Krutit — работа службы до сигнала остановки от systemd (SIGTERM).
func Krutit(rabota func(ctx context.Context)) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	rabota(ctx)
	return nil
}

// PopravitSamopodyom — самоподъём задан в юните (Restart=on-failure).
func PopravitSamopodyom() {}

// PerenestiVProgramFiles — юнит с первой установки смотрит в /opt/kelevra,
// переносить нечего.
func PerenestiVProgramFiles() (bool, error) { return false, nil }

// PutUstanovlennoy — путь установленной копии, если она есть.
func PutUstanovlennoy() string {
	if st, err := os.Stat(PutUstanovki); err == nil && !st.IsDir() {
		return PutUstanovki
	}
	return ""
}
