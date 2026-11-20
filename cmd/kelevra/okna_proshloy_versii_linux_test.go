//go:build linux

package main

import (
	"syscall"
	"testing"
	"time"
)

func TestOknoProshloyVersii(t *testing.T) {
	sluchai := []struct {
		cmd     []string
		exe     string
		proshl  bool
		nyneshn bool
	}{
		{[]string{"/opt/kelevra/kelevra"}, "/opt/kelevra/kelevra (deleted)", true, false},
		{[]string{"/opt/kelevra/kelevra"}, "/opt/kelevra/kelevra.old (deleted)", true, false},
		{[]string{"/opt/kelevra/kelevra"}, "/opt/kelevra/kelevra", false, true},
		{[]string{"/opt/kelevra/kelevra", "--trey"}, "/opt/kelevra/kelevra (deleted)", false, false},
		{[]string{"/opt/kelevra/kelevra", "--vinsluzhba"}, "/opt/kelevra/kelevra (deleted)", false, false},
		{[]string{"/usr/bin/bash"}, "/usr/bin/bash (deleted)", false, false},
	}
	for _, s := range sluchai {
		if got := oknoProshloyVersii(s.cmd, s.exe, "kelevra"); got != s.proshl {
			t.Errorf("oknoProshloyVersii(%v, %q) = %v", s.cmd, s.exe, got)
		}
		if got := oknoNyneshneyVersii(s.cmd, s.exe, "kelevra"); got != s.nyneshn {
			t.Errorf("oknoNyneshneyVersii(%v, %q) = %v", s.cmd, s.exe, got)
		}
	}
}

func TestProcessZnachka(t *testing.T) {
	if !processZnachka([]string{"/opt/kelevra/kelevra", "--trey"}) || !processZnachka([]string{"/opt/kelevra/kelevra", "--trey", "--smena", "5"}) {
		t.Fatal("значок не узнан")
	}
	if processZnachka([]string{"/opt/kelevra/kelevra"}) || processZnachka([]string{"/usr/bin/foo", "--trey"}) {
		t.Fatal("не значок принят за значок")
	}
}

func TestZamokZnachkaLinux(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if zamokZnachkaZanyat() {
		t.Fatal("замок занят до взятия")
	}
	if !zanyatMestoZnachka(0) {
		t.Fatal("замок не взят")
	}
	defer func() { syscall.Close(fdZamkaZnachka); fdZamkaZnachka = -1 }()
	if !zamokZnachkaZanyat() {
		t.Fatal("взятый замок не виден как занятый")
	}
	if zanyatMestoZnachka(200 * time.Millisecond) {
		t.Fatal("второй значок взял тот же замок")
	}
}
