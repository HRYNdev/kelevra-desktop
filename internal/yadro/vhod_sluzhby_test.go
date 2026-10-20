package yadro

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
)

// Вход службы (internal/zapasnoy) — служебная деталь конфига, как имя
// адаптера: живое ядро по нему менять нельзя. Иначе первое же тихое
// обновление на версию со входом объявило бы ядро «на прежнем конфиге».

func TestOtpechatokNeVidityVhodSluzhby(t *testing.T) {
	profil, err := os.ReadFile(filepath.Join("..", "konfig", "testdata", "profil_telefona.json"))
	if err != nil {
		t.Fatal(err)
	}
	bez, _, err := konfig.Prigotovit(profil, konfig.Vybor{ClashSekret: "s"})
	if err != nil {
		t.Fatal(err)
	}
	s1, _, err := konfig.Prigotovit(profil, konfig.Vybor{ClashSekret: "s", VhodSluzhby: &konfig.VhodSluzhby{Port: 20125, Parol: "s"}})
	if err != nil {
		t.Fatal(err)
	}
	s2, _, err := konfig.Prigotovit(profil, konfig.Vybor{ClashSekret: "s", VhodSluzhby: &konfig.VhodSluzhby{Port: 20131, Parol: "s"}})
	if err != nil {
		t.Fatal(err)
	}
	o := OtpechatokKonfigaBayt(bez)
	if o == "" || OtpechatokKonfigaBayt(s1) != o || OtpechatokKonfigaBayt(s2) != o {
		t.Fatalf("отпечатки разошлись: без входа %s, вход 20125 %s, вход 20131 %s",
			o, OtpechatokKonfigaBayt(s1), OtpechatokKonfigaBayt(s2))
	}
	// Контроль: смысловая правка конфига отпечаток меняет по-прежнему.
	bezPravil, _, err := konfig.Prigotovit(profil, konfig.Vybor{ClashSekret: "s", BezSetevyhPravil: true, VhodSluzhby: &konfig.VhodSluzhby{Port: 20125, Parol: "s"}})
	if err != nil {
		t.Fatal(err)
	}
	if OtpechatokKonfigaBayt(bezPravil) == o {
		t.Fatal("отпечаток перестал видеть смысловую правку")
	}
}

func TestVhodSluzhbyTolkoURabotayushchego(t *testing.T) {
	y := &Yadro{}
	if y.VhodSluzhby() != "" {
		t.Fatal("ядра нет — входа нет")
	}
	y.derzh, y.sost, y.vhodSluzhby = &derzhatel{pid: 1}, Podnimaem, "127.0.0.1:20125"
	if y.VhodSluzhby() != "" {
		t.Fatal("ядро ещё поднимается — туннелем пользоваться рано")
	}
	y.sost = Rabotaet
	if y.VhodSluzhby() != "127.0.0.1:20125" {
		t.Fatalf("работающее ядро: %q", y.VhodSluzhby())
	}
	y.derzh, y.sost = nil, Slomalos
	if y.VhodSluzhby() != "" {
		t.Fatal("упавшее ядро — входа нет")
	}
}
