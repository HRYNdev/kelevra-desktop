package konfig

import "testing"

// imyaTunVhoda — interface_name у входа-туннеля готового конфига.
func imyaTunVhoda(t *testing.T, gotovyy []byte) string {
	t.Helper()
	for _, vh := range vhody(razobrat(t, gotovyy)) {
		if vh["type"] != "tun" {
			continue
		}
		imya, _ := vh["interface_name"].(string)
		return imya
	}
	t.Fatal("в конфиге нет входа-туннеля — проверять нечего")
	return ""
}

// Vybor.TunImya — ответ на беду с машины человека 01.09: имя адаптера из
// профиля («tun125») осталось занято остатком прошлой попытки, и ядро на нём
// падало через 15 секунд. Занятость видит служба (internal/tunnel), а сборка
// конфига обязана уметь принять от неё другое имя.
func TestSvobodnoeImyaAdapteraDoezzhaetDoYadra(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{TunImya: "tun126"})
	if err != nil {
		t.Fatalf("конфиг со своим именем адаптера не собрался: %v", err)
	}
	if k.Rezhim != Tunnel {
		t.Fatalf("режим %q — подмена имени не имеет права ронять полный режим", k.Rezhim)
	}
	if imya := imyaTunVhoda(t, gotovyy); imya != "tun126" {
		t.Fatalf("ядру уехало имя адаптера %q, а служба выбрала tun126 — ядро упадёт на занятом", imya)
	}
	// Kartina.TunImya обязана поехать вместе с конфигом: по ней след туннеля
	// на диске (internal/tunnel) ищет адаптер после жёсткой смерти копии, и
	// разъехаться этим двум именам нельзя — иначе следующий запуск будет
	// искать не тот адаптер.
	if k.TunImya != "tun126" {
		t.Fatalf("в картине имя адаптера %q, а в конфиге tun126 — след туннеля будет искать не то", k.TunImya)
	}
}

// Пустой TunImya — обычный путь: имя берётся из профиля, как было всегда.
func TestBezPodmenyImyaBeretsyaIzProfilya(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	if imya := imyaTunVhoda(t, gotovyy); imya != "tun125" {
		t.Fatalf("имя адаптера %q, в профиле tun125 — сборка переписала чужую строку без спросу", imya)
	}
	if k.TunImya != "tun125" {
		t.Fatalf("в картине имя адаптера %q, в профиле tun125", k.TunImya)
	}
}
