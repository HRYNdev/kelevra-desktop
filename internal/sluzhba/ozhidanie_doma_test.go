package sluzhba

import (
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// TestOzhidanieDoma — Вова, 27.08: «если я дома то он тупо переходит в режим
// ожидания». Круг (index.html) знает только rabotaet/podnimaem/slomalos из
// ядра, поэтому источник истины для «ожидания» обязан жить в Go
// (ozhidanieDoma, sluzhba.go) и быть покрыт таблицей случаев, а не только
// сборкой окна.
func TestOzhidanieDoma(t *testing.T) {
	sluchai := []struct {
		imya           string
		avtorezhim     bool
		obstanovka     string
		sost           string
		hochuOzhidanie bool
	}{
		{
			imya:           "дома, авторежим включён, ядро не работает",
			avtorezhim:     true,
			obstanovka:     avtorezhim.Doma.String(),
			sost:           string(yadro.Stoit),
			hochuOzhidanie: true,
		},
		{
			imya:           "дома, авторежим включён, ядро работает",
			avtorezhim:     true,
			obstanovka:     avtorezhim.Doma.String(),
			sost:           string(yadro.Rabotaet),
			hochuOzhidanie: false,
		},
		{
			imya:           "вне дома, авторежим включён",
			avtorezhim:     true,
			obstanovka:     avtorezhim.VneDoma.String(),
			sost:           string(yadro.Stoit),
			hochuOzhidanie: false,
		},
		{
			imya:           "дома, ручной режим (авторежим выключен)",
			avtorezhim:     false,
			obstanovka:     avtorezhim.Doma.String(),
			sost:           string(yadro.Stoit),
			hochuOzhidanie: false,
		},
		{
			imya:           "обстановка неизвестна, авторежим включён",
			avtorezhim:     true,
			obstanovka:     avtorezhim.Neizvestno.String(),
			sost:           string(yadro.Stoit),
			hochuOzhidanie: false,
		},
	}
	for _, sl := range sluchai {
		t.Run(sl.imya, func(t *testing.T) {
			poluchil := ozhidanieDoma(sl.avtorezhim, sl.obstanovka, sl.sost)
			if poluchil != sl.hochuOzhidanie {
				t.Fatalf("%s: ozhidanieDoma(%v, %q, %q) = %v, хочу %v", sl.imya, sl.avtorezhim, sl.obstanovka, sl.sost, poluchil, sl.hochuOzhidanie)
			}
		})
	}
}
