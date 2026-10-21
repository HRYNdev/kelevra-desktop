package sluzhba

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/zhurnaly"
)

// Журнал ядра (yadro.log и его .proshlyy) уходит суточной отправкой вместе с
// журналом приложения и под тем же потолком размера. Сторожим именно сборку
// боевого отправщика: путь журнала ядра берётся там, где его пишет ядро
// (novoeYadro), а не склеивается отдельно — разъедутся молча.
func TestZhurnalYadraVSutochnoyOtpravke(t *testing.T) {
	s := stend(t)
	y := novoeYadro()
	if err := os.MkdirAll(filepath.Dir(y.PutZhurnala()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hranenie.PutZhurnala(), []byte("строка приложения\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(y.PutZhurnala(), []byte("строка ядра\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(y.PutProshlogoZhurnala(), []byte("строка прошлого ядра\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	o := s.otpravshchikZhurnalov()
	telo, ushli, err := zhurnaly.Upakovat(zhurnaly.Razobrat(o.Puti, &zhurnaly.Metki{Fayly: map[string]zhurnaly.Metka{}}), zhurnaly.Potolok)
	if err != nil {
		t.Fatal(err)
	}
	imena := map[string]bool{}
	for _, k := range ushli {
		imena[k.Imya] = true
	}
	for _, nado := range []string{"kelevra.log", "yadro.log", "yadro.log.proshlyy"} {
		if !imena[nado] {
			t.Errorf("%s не попал в посылку: %v", nado, imena)
		}
	}
	gz, err := gzip.NewReader(bytes.NewReader(telo))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(gz)
	if !strings.Contains(string(b), "строка ядра") {
		t.Fatalf("строк ядра нет в теле посылки")
	}
}
