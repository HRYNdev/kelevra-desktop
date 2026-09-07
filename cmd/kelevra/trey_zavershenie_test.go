package main

import (
	"os"
	"regexp"
	"testing"
)

// TestZnachokSnimaetsyaPriZavershenii — стенд для беды «значок-призрак»:
// путь завершения по сигналу (zhdatSignal в zapustitSluzhbu) обязан снять
// значок трея ДО того, как гаснет ядро (s.Yadro.Ostanovit()), а не только
// по пункту меню «Выход» (тот путь снимает его сам в trey_windows.go).
//
// Живого Windows-стенда тут нет (см. наряд), поэтому проверка структурная:
// читает исходник main.go и требует, чтобы между zhdatSignal(...) и
// Ostanovit() стоял вызов снятия значка. На тексте main.go ДО правки этот
// тест обязан падать — так и было проверено (git stash / исходный main.go):
// красный ДО, зелёный ПОСЛЕ.
func TestZnachokSnimaetsyaPriZavershenii(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("не прочитал main.go: %v", err)
	}

	re := regexp.MustCompile(`(?s)zhdatSignal\(vyhodIzTreya\)(.*?)_ = s\.Yadro\.Ostanovit\(\)`)
	m := re.FindSubmatch(src)
	if m == nil {
		t.Fatal("не нашёл в main.go участок между zhdatSignal(vyhodIzTreya) и s.Yadro.Ostanovit() — структура main.go изменилась сильнее, чем ждал этот тест")
	}

	mezhdu := string(m[1])
	if !regexp.MustCompile(`ubratZnachokPriZavershenii\(\)`).MatchString(mezhdu) {
		t.Fatal("между zhdatSignal(vyhodIzTreya) и s.Yadro.Ostanovit() нет вызова снятия значка трея (ubratZnachokPriZavershenii) — при остановке службы Windows/обновлении/диспетчере задач значок останется висеть с подсказкой «...работает» над мёртвой защитой")
	}
}
