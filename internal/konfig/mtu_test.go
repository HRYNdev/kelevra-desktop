package konfig

import (
	"encoding/json"
	"testing"
)

// mtuTunVhoda — размер пакета у входа-туннеля готового конфига.
func mtuTunVhoda(t *testing.T, gotovyy []byte) float64 {
	t.Helper()
	for _, vh := range vhody(razobrat(t, gotovyy)) {
		if vh["type"] != "tun" {
			continue
		}
		m, _ := vh["mtu"].(float64)
		return m
	}
	t.Fatal("в конфиге нет входа-туннеля — проверять нечего")
	return 0
}

// На компьютере размер пакета обязан быть пригодным для интернета.
//
// Профиль присылает 9000 — заводское значение sing-box, и на телефоне оно
// работает. На Windows те же 9000 стоят человеку секунд: замер 08.09 показал
// TLS-рукопожатие в 3.6-6.5 секунды при мгновенном соединении, потому что
// каждый пакет дробился по дороге. После 1420 те же сайты открывались за
// 1.1-1.4 секунды.
//
// Правка живёт в клиенте, а НЕ на сервере: конфиг общий для всей семьи, и
// менять его ради одной платформы значит чинить одно, ломая другое.
func TestMtuSbivaetsyaPodInternet(t *testing.T) {
	gotovyy, _, err := Prigotovit(profil(t), Vybor{Prava: true})
	if err != nil {
		t.Fatalf("конфиг не собрался: %v", err)
	}
	if m := mtuTunVhoda(t, gotovyy); m > MtuDlyaOkna {
		t.Fatalf("mtu туннеля %.0f — пакеты будут дробиться по дороге, и каждое TLS-рукопожатие займёт секунды", m)
	}
}

// Меньший размер, если он вдруг придёт с сервера, не увеличиваем: там могли
// знать про сеть человека больше нашего.
func TestMenshiyMtuNeTrogaem(t *testing.T) {
	syroy := podmenitMtu(t, profil(t), 1280)
	gotovyy, _, err := Prigotovit(syroy, Vybor{Prava: true})
	if err != nil {
		t.Fatalf("конфиг не собрался: %v", err)
	}
	if m := mtuTunVhoda(t, gotovyy); m != 1280 {
		t.Fatalf("mtu стал %.0f, а профиль просил 1280 — чужое решение переписано без причины", m)
	}
}

// podmenitMtu — профиль с другим размером пакета у входа-туннеля.
func podmenitMtu(t *testing.T, syroy []byte, mtu float64) []byte {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal(syroy, &d); err != nil {
		t.Fatal(err)
	}
	spisok, _ := d["inbounds"].([]any)
	for _, v := range spisok {
		vh, ok := v.(map[string]any)
		if !ok || vh["type"] != "tun" {
			continue
		}
		vh["mtu"] = mtu
	}
	novyy, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return novyy
}
