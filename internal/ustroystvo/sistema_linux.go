//go:build linux

package ustroystvo

import (
	"os"
	"strings"
)

// modelSistemy — производитель и модель из DMI («HP HP Laptop 15s»); нет DMI
// (виртуалка без него, ARM) — имя компьютера. Пустой ответ на Linux дал бы
// ZapasnayaModel «Windows PC» — неправду в панели.
func modelSistemy() string {
	chitat := func(put string) string {
		b, err := os.ReadFile(put)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	proizv := chitat("/sys/class/dmi/id/sys_vendor")
	model := chitat("/sys/class/dmi/id/product_name")
	if model != "" && !strings.HasPrefix(model, proizv) && proizv != "" {
		model = proizv + " " + model
	}
	if model != "" {
		return model
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return "Linux " + h
	}
	return "Linux PC"
}

// platformaSistemy — PRETTY_NAME из /etc/os-release («Debian GNU/Linux 13 (trixie)»).
func platformaSistemy() string {
	b, err := os.ReadFile("/etc/os-release")
	if err == nil {
		for _, s := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(s, "PRETTY_NAME="); ok {
				if v = strings.Trim(strings.TrimSpace(v), `"`); v != "" {
					return v
				}
			}
		}
	}
	return "Linux"
}
