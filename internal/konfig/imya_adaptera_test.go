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
	gotovyy, k, err := Prigotovit(profil(t), Vybor{Prava: true, TunImya: "tun126"})
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
	gotovyy, k, err := Prigotovit(profil(t), Vybor{Prava: true})
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

// Vybor.AdapterZanyat — последняя честная ступень: свободного имени нет вовсе
// (заняты и tun125, и все соседние). Общий текст отката тут вреден: он шлёт
// человека нажать «Подключиться» ещё раз, а повторное нажатие упрётся в то же
// занятое устройство — человек будет ходить по кругу.
func TestAdapterZanyatNazyvaetPrichinuChelovekuSlovami(t *testing.T) {
	_, k, err := Prigotovit(profil(t), Vybor{Prava: true, BezTunnelya: true, AdapterZanyat: true})
	if err != nil {
		t.Fatalf("конфиг половинного режима не собрался — человек остался бы вовсе без связи: %v", err)
	}
	if k.Rezhim != Proksi || !k.Chastichnaya {
		t.Fatalf("режим %q, половинность %v — ждали честную половину", k.Rezhim, k.Chastichnaya)
	}
	if k.Zametka != ZametkaAdapterZanyat {
		t.Fatalf("заметка %q — она обязана назвать причину, а не молчать общим «не вышло»", k.Zametka)
	}
	if k.PochemuChastichnaya != PrichinaAdapterZanyat {
		t.Fatalf("причина %q — ждали ту, что называет занятое устройство", k.PochemuChastichnaya)
	}
	// Совета «нажмите ещё раз» в этих строках быть не должно: он отправит
	// человека по кругу, из которого нажатием не выйти.
	for _, s := range []string{k.Zametka, k.PochemuChastichnaya} {
		if s == ZametkaTunnelNePodnyalsya || s == PrichinaTunnelNePodnyalsya {
			t.Fatalf("человеку сказали общее «сейчас не вышло, попробуйте ещё раз»: %q", s)
		}
	}
}

// AdapterZanyat без BezTunnelya ничего не меняет: пока полный режим поднят,
// говорить про занятое устройство не о чем.
func TestAdapterZanyatNeMeshaetPolnomuRezhimu(t *testing.T) {
	_, k, err := Prigotovit(profil(t), Vybor{Prava: true, AdapterZanyat: true})
	if err != nil {
		t.Fatal(err)
	}
	if k.Rezhim != Tunnel || k.Zametka != ZametkaVes {
		t.Fatalf("полный режим поднят, а человеку сказали %q (%s)", k.Zametka, k.Rezhim)
	}
}

// Отказ по правам не должен подменяться словами про занятое устройство:
// человека, которому сказали не то, чинит не то.
func TestBezPravSlovaOstalisPrezhnimi(t *testing.T) {
	_, k, err := Prigotovit(profil(t), Vybor{Prava: false})
	if err != nil {
		t.Fatal(err)
	}
	if k.Zametka != ZametkaBezPrav || k.PochemuChastichnaya != PrichinaBezPrav {
		t.Fatalf("без прав человеку сказали %q / %q", k.Zametka, k.PochemuChastichnaya)
	}
}
