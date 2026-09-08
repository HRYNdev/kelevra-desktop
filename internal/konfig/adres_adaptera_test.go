package konfig

import "testing"

// adresaTunVhoda — поле address у входа-туннеля готового конфига.
func adresaTunVhoda(t *testing.T, gotovyy []byte) []string {
	t.Helper()
	for _, vh := range vhody(razobrat(t, gotovyy)) {
		if vh["type"] != "tun" {
			continue
		}
		return spisokStrok(vh["address"])
	}
	t.Fatal("в конфиге нет входа-туннеля — проверять нечего")
	return nil
}

// Vybor.TunSdvigAdresa — ответ на беду с машины человека 06.09: подбор имени
// сработал, а ядро упало второй раз подряд, теперь на занятом адресе
// («set ipv4 address: The object already exists»). Остаток прошлой попытки
// держит имя и адрес вместе, поэтому адрес обязан ехать за именем.
func TestSdvigAdresaDoezzhaetDoYadra(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{Prava: true, TunImya: "tun126", TunSdvigAdresa: 1})
	if err != nil {
		t.Fatalf("конфиг со сдвинутым адресом не собрался: %v", err)
	}
	if k.Rezhim != Tunnel {
		t.Fatalf("режим %q — сдвиг адреса не имеет права ронять полный режим", k.Rezhim)
	}
	adresa := adresaTunVhoda(t, gotovyy)
	if len(adresa) == 0 {
		t.Fatal("у входа-туннеля не осталось ни одного адреса — ядро не поднимется")
	}
	for _, a := range adresa {
		if a == "172.19.0.1/30" {
			t.Fatalf("ядру уехал прежний адрес %q — он занят остатком, ядро упадёт на нём второй раз", a)
		}
	}
	if adresa[0] != "172.19.0.5/30" {
		t.Fatalf("первый адрес %q, ждём соседний блок 172.19.0.5/30", adresa[0])
	}
}

// Обычный путь: сдвига нет — адреса профиля не трогаем вовсе.
func TestBezSdvigaAdresaBerutsyaIzProfilya(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatal(err)
	}
	adresa := adresaTunVhoda(t, gotovyy)
	if len(adresa) == 0 || adresa[0] != "172.19.0.1/30" {
		t.Fatalf("адреса %v, в профиле 172.19.0.1/30 — сборка переписала чужую строку без спросу", adresa)
	}
}
