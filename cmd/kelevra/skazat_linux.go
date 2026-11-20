//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
)

// skazat — сообщение человеку на Linux: окно zenity (есть в GNOME и XFCE),
// иначе уведомление, иначе stderr (запуск из терминала).
func skazat(zagolovok, tekst string) {
	if p, err := exec.LookPath("zenity"); err == nil {
		if exec.Command(p, "--error", "--title="+zagolovok, "--text="+tekst, "--no-markup").Run() == nil {
			return
		}
	}
	if uvedomit(zagolovok, tekst) {
		return
	}
	fmt.Fprintf(os.Stderr, "\n=== %s ===\n%s\n", zagolovok, tekst)
}

// uvedomit — всплывающее уведомление рабочего стола (notify-send, libnotify).
func uvedomit(zagolovok, tekst string) bool {
	p, err := exec.LookPath("notify-send")
	if err != nil {
		return false
	}
	return exec.Command(p, "--app-name=Kelevra", "--icon=kelevra", zagolovok, tekst).Run() == nil
}
