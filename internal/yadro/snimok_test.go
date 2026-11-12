package yadro

import (
	"os"
	"os/exec"
	"testing"
)

// Снимок конфига живого ядра отдаётся, только если он от того же конфига,
// что в записке (отпечаток), и только пока ядро держится этой копией.
func TestSnimokKonfigaRaboty(t *testing.T) {
	papka := t.TempDir()
	telo := []byte(`{"inbounds":[{"type":"tun","interface_name":"tun126"}]}`)
	otp := OtpechatokKonfigaBayt(telo)

	if k := SnimokDlyaOtpechatka(papka, otp); k.Telo != nil {
		t.Fatal("снимка нет, а отдан")
	}
	zapisatSnimok(papka, telo)
	if k := SnimokDlyaOtpechatka(papka, otp); string(k.Telo) != string(telo) || k.Otpechatok != otp {
		t.Fatalf("снимок не отдан: %+v", k)
	}
	if k := SnimokDlyaOtpechatka(papka, "0000000000000000"); k.Telo != nil {
		t.Fatal("снимок чужого конфига отдан")
	}
	if imyaAdapteraIzBayt(telo) != "tun126" {
		t.Fatal("имя адаптера из байтов")
	}

	y := &Yadro{Papka: papka}
	if _, rabotaet := y.KonfigVRabote(); rabotaet {
		t.Fatal("ядро не держим, а «работает»")
	}
	y.derzh, y.otpechatokRaboty = &derzhatel{pid: 4242, cmd: &exec.Cmd{}}, otp
	k, rabotaet := y.KonfigVRabote()
	if !rabotaet || string(k.Telo) != string(telo) {
		t.Fatalf("конфиг живого ядра: %v %+v", rabotaet, k)
	}
	y.derzh = nil

	ubratSnimok(papka)
	if _, err := os.Stat(putSnimka(papka)); !os.IsNotExist(err) {
		t.Fatal("снимок не убран")
	}
}
