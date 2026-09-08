package hranenie

import (
	"os"
	"path/filepath"
	"testing"
)

// Перенос обязан взять то, ради чего он затеян: код доступа, настройки и
// профиль. Потеря любого из них означает, что человек после обновления
// оказался с пустым приложением и заново вводит код.
func TestPerenositGlavnoe(t *testing.T) {
	iz, v := t.TempDir(), filepath.Join(t.TempDir(), "novaya")
	zapisat(t, filepath.Join(iz, "nastroyki.json"), `{"avtorezhim":true}`)
	zapisat(t, filepath.Join(iz, "profil.json"), `{"kod":"secret"}`)

	if err := Perenesti(iz, v); err != nil {
		t.Fatalf("перенос отказал: %v", err)
	}
	for _, imya := range []string{"nastroyki.json", "profil.json"} {
		if _, err := os.Stat(filepath.Join(v, imya)); err != nil {
			t.Fatalf("%s не переехал: %v", imya, err)
		}
	}
}

// Старая папка остаётся нетронутой: откат на прежнюю версию не должен
// оставлять человека без кода доступа.
func TestStaroeOstayotsyaNaMeste(t *testing.T) {
	iz, v := t.TempDir(), filepath.Join(t.TempDir(), "novaya")
	staryy := filepath.Join(iz, "profil.json")
	zapisat(t, staryy, `{"kod":"secret"}`)

	if err := Perenesti(iz, v); err != nil {
		t.Fatalf("перенос отказал: %v", err)
	}
	if _, err := os.Stat(staryy); err != nil {
		t.Fatalf("старый профиль исчез после переноса: %v", err)
	}
}

// Следы прошлых процессов и журнал не переносятся: метка запуска описывает
// процесс, которого уже нет, и новая копия приняла бы её за живую.
func TestSledyNePerenosyatsya(t *testing.T) {
	iz, v := t.TempDir(), filepath.Join(t.TempDir(), "novaya")
	for _, imya := range []string{"zapushcheno.json", "proksi.json", "tunnel.json", "kelevra.log"} {
		zapisat(t, filepath.Join(iz, imya), "старое")
	}
	zapisat(t, filepath.Join(iz, "nastroyki.json"), "{}")

	if err := Perenesti(iz, v); err != nil {
		t.Fatalf("перенос отказал: %v", err)
	}
	for _, imya := range []string{"zapushcheno.json", "proksi.json", "tunnel.json", "kelevra.log"} {
		if _, err := os.Stat(filepath.Join(v, imya)); err == nil {
			t.Fatalf("%s переехал, хотя описывает уже мёртвый процесс", imya)
		}
	}
}

// Своё новее принесённого: повторный перенос не затирает то, что служба уже
// написала сама.
func TestNeZatiraetSvoyo(t *testing.T) {
	iz, v := t.TempDir(), t.TempDir()
	zapisat(t, filepath.Join(iz, "nastroyki.json"), "старое")
	zapisat(t, filepath.Join(v, "nastroyki.json"), "новое")

	if err := Perenesti(iz, v); err != nil {
		t.Fatalf("перенос отказал: %v", err)
	}
	dano, err := os.ReadFile(filepath.Join(v, "nastroyki.json"))
	if err != nil {
		t.Fatalf("не прочитать: %v", err)
	}
	if string(dano) != "новое" {
		t.Fatalf("перенос затёр своё: %q", string(dano))
	}
}

// Чистая машина: старой папки нет вовсе. Это не отказ, это обычная установка.
func TestPustoyPerenosNeOshibka(t *testing.T) {
	if err := Perenesti(filepath.Join(t.TempDir(), "net-takoy"), t.TempDir()); err != nil {
		t.Fatalf("перенос с несуществующей папкой должен молчать, а вернул: %v", err)
	}
}

func zapisat(t *testing.T, put, tekst string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
		t.Fatalf("не создать папку: %v", err)
	}
	if err := os.WriteFile(put, []byte(tekst), 0o600); err != nil {
		t.Fatalf("не записать %s: %v", put, err)
	}
}
