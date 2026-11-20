//go:build linux

package avtozapusk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Автозапуск на Linux — ярлык в ~/.config/autostart (стандарт freedesktop,
// его читают GNOME, KDE, XFCE). Как и на Windows, при входе поднимается
// значок (--trey) — туннель держит служба systemd; без службы — служебный
// режим без окна (--tiho).

func putYarlyka() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "autostart", "kelevra.desktop"), nil
}

func sluzhbaEst() bool {
	_, err := os.Stat("/etc/systemd/system/kelevra.service")
	return err == nil
}

func tekstNa(put string) string {
	return fmt.Sprintf("[Desktop Entry]\nType=Application\nName=%s\nExec=%s %s\nIcon=kelevra\nTerminal=false\nX-GNOME-Autostart-enabled=true\n",
		Imya, put, Nuzhnyy(sluzhbaEst()))
}

// strokaZapisi — строка Exec= из ярлыка (пусто — ярлыка нет).
func strokaZapisi() (string, error) {
	put, err := putYarlyka()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(put)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for _, s := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(s), "Exec="); ok {
			return v, nil
		}
	}
	return "", nil
}

func Vklyuchen() (bool, error) {
	s, err := strokaZapisi()
	return s != "", err
}

func Ustarela() (bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	return UstarelaDlya(exe)
}

func UstarelaDlya(put string) (bool, error) {
	s, err := strokaZapisi()
	if err != nil || s == "" {
		return false, err
	}
	return s != put+" "+Nuzhnyy(sluzhbaEst()), nil
}

func Vklyuchit() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return VklyuchitNa(exe)
}

func VklyuchitNa(put string) error {
	p, err := putYarlyka()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(tekstNa(put)), 0o644)
}

func Vyklyuchit() error {
	p, err := putYarlyka()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// PochinitNaProgramFiles — на Linux: ярлык смотрит не на установленную копию
// (/opt/kelevra), а копия есть — перенацелить.
func PochinitNaProgramFiles(putPF string) (string, string, bool, error) {
	s, err := strokaZapisi()
	if err != nil || s == "" || putPF == "" {
		return s, s, false, err
	}
	nuzhna := putPF + " " + Nuzhnyy(sluzhbaEst())
	if s == nuzhna {
		return s, s, false, nil
	}
	if err := VklyuchitNa(putPF); err != nil {
		return s, s, false, err
	}
	return s, nuzhna, true, nil
}

func VseZapisi() ([]Zapis, error) {
	s, err := strokaZapisi()
	if err != nil || s == "" {
		return nil, err
	}
	put := strings.Fields(s)[0]
	_, e := os.Stat(put)
	return []Zapis{{Stroka: s, Put: put, FaylEst: e == nil}}, nil
}
