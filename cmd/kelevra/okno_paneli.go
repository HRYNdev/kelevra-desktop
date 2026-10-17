package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

// argPanel — эта копия запущена ради одного окна: панели своей сети (кнопка «Панель сети» в окне
// приложения). Служба, трей и замок копии ей не нужны — окно открывается и живёт само.
const argPanel = "--panel"

// ssylkaPaneliIzArgov — адрес после --panel, если он похож на одноразовую ссылку панели.
func ssylkaPaneliIzArgov(args []string) (string, bool) {
	for i, a := range args {
		if a == argPanel && i+1 < len(args) {
			return args[i+1], proveritSsylkuPaneli(args[i+1]) == nil
		}
	}
	return "", false
}

// proveritSsylkuPaneli — открываем только https и только одноразовую ссылку панели: процесс
// окна не должен уметь открыть что угодно по чужой подсказке.
func proveritSsylkuPaneli(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || !strings.HasPrefix(u.Path, "/panel/vhod/") {
		return fmt.Errorf("не ссылка панели: %q", s)
	}
	return nil
}

// zapustitOknoPaneli — отдельный процесс этого же exe с --panel <ссылка>. Зовётся из окна приложения.
func zapustitOknoPaneli(ssylka string) error {
	if err := proveritSsylkuPaneli(ssylka); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, argPanel, ssylka)
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Printf("окно панели сети запущено (pid %d)", cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	return nil
}
