package podpis

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func para(t *testing.T) (ed25519.PublicKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, base64.StdEncoding.EncodeToString(priv.Seed())
}

func fayl(t *testing.T, soderzhimoe string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Kelevra.exe")
	if err := os.WriteFile(p, []byte(soderzhimoe), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestVernayaPodpis(t *testing.T) {
	pub, seed := para(t)
	p := fayl(t, "MZ nastoyashchiy")
	sig, err := Podpisat(seed, "Kelevra.exe", p)
	if err != nil {
		t.Fatal(err)
	}
	if err := ProveritKlyuchom(pub, p, "Kelevra.exe", []byte(sig)); err != nil {
		t.Fatalf("верная подпись отвергнута: %v", err)
	}
}

func TestPodmenennyyFaylOtvergaetsya(t *testing.T) {
	pub, seed := para(t)
	p := fayl(t, "MZ nastoyashchiy")
	sig, _ := Podpisat(seed, "Kelevra.exe", p)
	chuzhoy := fayl(t, "MZ podmenennyy")
	if err := ProveritKlyuchom(pub, chuzhoy, "Kelevra.exe", []byte(sig)); !errors.Is(err, ErrNeVerna) {
		t.Fatalf("подменённый файл прошёл или не тот отказ: %v", err)
	}
}

func TestNetPodpisi(t *testing.T) {
	pub, _ := para(t)
	p := fayl(t, "MZ")
	for _, sig := range []string{"", "  \n"} {
		if err := ProveritKlyuchom(pub, p, "Kelevra.exe", []byte(sig)); !errors.Is(err, ErrNetPodpisi) {
			t.Fatalf("пустая подпись %q: %v", sig, err)
		}
	}
}

func TestChuzhoyKlyuchOtvergaetsya(t *testing.T) {
	_, seed := para(t)
	drugoy, _ := para(t)
	p := fayl(t, "MZ")
	sig, _ := Podpisat(seed, "Kelevra.exe", p)
	if err := ProveritKlyuchom(drugoy, p, "Kelevra.exe", []byte(sig)); !errors.Is(err, ErrNeVerna) {
		t.Fatalf("подпись чужого ключа прошла: %v", err)
	}
}

// Подпись ядра нельзя выдать за подпись приложения: имя входит в подписанное.
func TestPodpisPrivyazanaKImeni(t *testing.T) {
	pub, seed := para(t)
	p := fayl(t, "MZ")
	sig, _ := Podpisat(seed, "sing-box-windows-amd64.zip", p)
	if err := ProveritKlyuchom(pub, p, "Kelevra.exe", []byte(sig)); !errors.Is(err, ErrNeVerna) {
		t.Fatalf("подпись под другим именем прошла: %v", err)
	}
}

func TestIspochennayaPodpis(t *testing.T) {
	pub, _ := para(t)
	p := fayl(t, "MZ")
	if err := ProveritKlyuchom(pub, p, "Kelevra.exe", []byte("не base64 !!!")); !errors.Is(err, ErrNeVerna) {
		t.Fatalf("мусор вместо подписи: %v", err)
	}
}

// Вшитый ключ обязан разбираться: иначе обновление не поставится никогда.
func TestVshityyKlyuchRazbiraetsya(t *testing.T) {
	if _, err := Klyuch(KlyuchHex); err != nil {
		t.Fatal(err)
	}
}

func TestZakrytyyKlyuchNeRazbiraetsya(t *testing.T) {
	if _, err := Podpisat("abc", "x", "y"); err == nil {
		t.Fatal("мусорный закрытый ключ принят")
	}
}
