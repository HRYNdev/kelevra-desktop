package tunnel

import (
	"reflect"
	"testing"
)

// Замер 06.09 с машины человека: подбор имени сработал, а ядро упало второй
// раз подряд на занятом адресе. Эти проверки держат правку, которая двигает
// адрес вместе с именем.

func TestSdvigSchitaetsyaPoImenam(t *testing.T) {
	sluchai := []struct {
		ishodnoe, vzyatoe string
		zhdyom            int
	}{
		{"tun125", "tun126", 1},
		{"tun125", "tun133", 8},
		{"tun125", "tun125", 0},  // имя не менялось
		{"tun125", "", 0},        // свободного не нашлось
		{"", "tun126", 0},        // профиль имени не назвал
		{"tun125", "wg0", 0},     // другая основа — сравнивать нечего
		{"kelevra", "kelevra1", 0}, // имя без номера: основы разные, не гадаем
		{"tun130", "tun125", 0},  // назад не двигаем
	}
	for _, s := range sluchai {
		if bylo := Sdvig(s.ishodnoe, s.vzyatoe); bylo != s.zhdyom {
			t.Errorf("Sdvig(%q, %q) = %d, ждём %d", s.ishodnoe, s.vzyatoe, bylo, s.zhdyom)
		}
	}
}

func TestSdvinutAdresaBeryotSosedniyBlok(t *testing.T) {
	// Боевой профиль: адрес /30 (блок в четыре адреса) и IPv6 /126.
	byli := []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"}
	stali := SdvinutAdresa(byli, 1)
	zhdyom := []string{"172.19.0.5/30", "fdfe:dcba:9876::5/126"}
	if !reflect.DeepEqual(stali, zhdyom) {
		t.Fatalf("сдвиг на один блок дал %v, ждём %v", stali, zhdyom)
	}
	stali2 := SdvinutAdresa(byli, 2)
	zhdyom2 := []string{"172.19.0.9/30", "fdfe:dcba:9876::9/126"}
	if !reflect.DeepEqual(stali2, zhdyom2) {
		t.Fatalf("сдвиг на два блока дал %v, ждём %v", stali2, zhdyom2)
	}
}

func TestSdvinutAdresaNichegoNeTrogaetBezSdviga(t *testing.T) {
	byli := []string{"172.19.0.1/30"}
	if stali := SdvinutAdresa(byli, 0); !reflect.DeepEqual(stali, byli) {
		t.Fatalf("без сдвига адреса изменились: %v", stali)
	}
}

// Неразобранную строку возвращаем как есть: пусть ядро попробует прежний
// адрес, чем мы отменим человеку полный режим из-за своей же неудачи разбора.
func TestSdvinutAdresaNerazobrannoeOstavlyaetKakEst(t *testing.T) {
	byli := []string{"не адрес вовсе", "172.19.0.1", "", "172.19.0.1/32"}
	stali := SdvinutAdresa(byli, 1)
	if !reflect.DeepEqual(stali, byli) {
		t.Fatalf("неразобранное изменилось: %v", stali)
	}
}

// Край диапазона: двигать некуда — оставляем прежний адрес, а не портим его.
func TestSdvinutAdresaKrayDiapazona(t *testing.T) {
	byli := []string{"255.255.255.253/30"}
	if stali := SdvinutAdresa(byli, 1); !reflect.DeepEqual(stali, byli) {
		t.Fatalf("на краю диапазона адрес изменился: %v", stali)
	}
}
