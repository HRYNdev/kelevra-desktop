package nrpt

import (
	"context"
	"strings"
	"testing"
)

// Подставная система: настоящий реестр машины проверки не трогают.
type stend struct {
	pravila map[string]Pravilo
	vyzovov int
}

func (s *stend) Svoi() ([]string, error) {
	var imena []string
	for i := range s.pravila {
		if strings.HasPrefix(i, Imya) { // настоящая система возвращает только своё
			imena = append(imena, i)
		}
	}
	return imena, nil
}
func (s *stend) Postavit(p Pravilo) error { s.vyzovov++; s.pravila[Imya] = p; return nil }
func (s *stend) Ubrat(imya string) error  { s.vyzovov++; delete(s.pravila, imya); return nil }

func TestSoglasovat(t *testing.T) {
	sys := &stend{pravila: map[string]Pravilo{"ChuzhoeVPN": {Zona: ".corp"}, Imya + "Staroe": {}}}
	u := &Upravlenie{Sys: sys}
	u.UbratOstatki()
	if _, est := sys.pravila["ChuzhoeVPN"]; !est || len(sys.pravila) != 1 {
		t.Fatalf("остатки: %v", sys.pravila)
	}
	p := Pravilo{Zona: ".dom", Server: "100.77.0.1"}
	_ = u.Soglasovat(true, p)
	_ = u.Soglasovat(true, p)
	if sys.pravila[Imya] != p || sys.vyzovov != 2 {
		t.Fatalf("постановка: %v, вызовов %d", sys.pravila, sys.vyzovov)
	}
	_ = u.Soglasovat(false, p)
	_ = u.Soglasovat(false, p)
	if _, est := sys.pravila[Imya]; est || sys.vyzovov != 3 {
		t.Fatalf("снятие: %v, вызовов %d", sys.pravila, sys.vyzovov)
	}
	if err := (&Upravlenie{}).Soglasovat(true, p); err != nil {
		t.Fatal("без системы — ничего не делать")
	}
}

// Подмена PowerShell: записывает сценарии, настоящий не запускается.
type zapisPS struct {
	scenarii []string
	otvet    string
}

func (z *zapisPS) zapusk(_ context.Context, sc string) (string, error) {
	z.scenarii = append(z.scenarii, sc)
	return z.otvet, nil
}

const guid = "{0A1B2C3D-0000-4111-8222-333344445555}"

func TestPowerShellPostavit(t *testing.T) {
	z := &zapisPS{otvet: strings.Join([]string{guid, "KelevraDom", "{bad}"}, "\r\n")}
	ps := &PowerShell{Zapusk: z.zapusk}
	if err := ps.Postavit(Pravilo{Zona: ".dom", Server: "100.77.0.1"}); err != nil {
		t.Fatal(err)
	}
	vse := strings.Join(z.scenarii, " | ")
	for _, ozh := range []string{
		"Add-DnsClientNrptRule -Namespace '.dom' -NameServers '100.77.0.1' -Comment 'Kelevra: вход домой'",
		"Remove-DnsClientNrptRule -Name '" + guid + "'",
		`DnsPolicyConfig\KelevraDom'`,
	} {
		if !strings.Contains(vse, ozh) {
			t.Errorf("нет %q в сценариях: %s", ozh, vse)
		}
	}
	if strings.Contains(vse, "{bad}") {
		t.Error("не-GUID имя попало в сценарий")
	}
	if err := ps.Postavit(Pravilo{Zona: ".dom'; calc", Server: "100.77.0.1"}); err == nil {
		t.Error("зона с кавычкой принята")
	}
	if err := ps.Postavit(Pravilo{Zona: ".dom", Server: "x'; calc"}); err == nil {
		t.Error("адрес не IP принят")
	}
}

func TestPowerShellChuzhoeNeTrogaet(t *testing.T) {
	z := &zapisPS{}
	ps := &PowerShell{Zapusk: z.zapusk}
	if err := ps.Ubrat("ChuzhoeVPN"); err == nil || len(z.scenarii) != 0 {
		t.Fatal("чужое имя должно быть отвергнуто без вызова PowerShell")
	}
	if err := ps.Ubrat(guid); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(z.scenarii[0], "Comment -eq 'Kelevra: вход домой'") {
		t.Error("снятие без сверки комментария")
	}
}
