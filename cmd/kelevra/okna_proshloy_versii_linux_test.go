//go:build linux

package main

import "testing"

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
