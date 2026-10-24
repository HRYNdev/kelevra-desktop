//go:build windows

package main

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// Настоящий замок ОС: свободен → значок берёт его → окно видит «жив» →
// второй значок не берёт. Имя своё на прогон: взятый замок держится до
// конца процесса теста и не должен задеть настоящий значок на машине.
func TestZamokZnachkaOdinNaSeans(t *testing.T) {
	staroe := imyaZamkaZnachka
	imyaZamkaZnachka = fmt.Sprintf(`Local\KelevraZnachokTest-%d-%d`, os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { imyaZamkaZnachka = staroe })

	if zamokZnachkaZanyat() {
		t.Fatal("свежий замок считается занятым")
	}
	// Проверка окна не должна оставить замок за собой.
	if zamokZnachkaZanyat() {
		t.Fatal("после проверки окном замок остался занятым")
	}
	if !zanyatMestoZnachka(0) {
		t.Fatal("первый значок не взял свободный замок")
	}
	if !zamokZnachkaZanyat() {
		t.Fatal("значок держит замок, а окно видит его свободным")
	}
	if zanyatMestoZnachka(100 * time.Millisecond) {
		t.Fatal("второй значок взял занятый замок")
	}
}
