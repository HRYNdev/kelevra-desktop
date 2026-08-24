package konfig

import (
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/pravila"
)

// Vybor.PravilaIzKomplekta — переписывает ВСЕ 22 route.rule_set боевого
// профиля с remote на local, не трогая route.final: смысл комплекта именно в
// том, что умная маршрутизация не выродится в «весь трафик в VPN».
func TestPravilaIzKomplektaPerepisyvaetVseRemoteVLocal(t *testing.T) {
	komplekt, err := pravila.Razlozhit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gotovyy, k, err := Prigotovit(profil(t), Vybor{
		PravilaIzKomplekta:  komplekt,
		PravilaKomplektData: pravila.Data(),
	})
	if err != nil {
		t.Fatal(err)
	}
	d := razobrat(t, gotovyy)
	r, ok := d["route"].(map[string]any)
	if !ok {
		t.Fatal("route пропал из конфига")
	}
	spisok, ok := r["rule_set"].([]any)
	if !ok {
		t.Fatal("route.rule_set пропал из конфига")
	}
	if len(spisok) != 22 {
		t.Fatalf("ожидал все 22 rule_set на месте, получил %d", len(spisok))
	}
	for _, rs := range spisok {
		m := rs.(map[string]any)
		if m["type"] != "local" {
			t.Fatalf("rule_set %v остался не local", m)
		}
		if m["format"] != "binary" {
			t.Fatalf("rule_set %v без format:binary — ядро его не откроет", m)
		}
		put, _ := m["path"].(string)
		if put == "" {
			t.Fatalf("rule_set %v без пути к файлу", m)
		}
		for _, chuzhoePole := range []string{"url", "update_interval", "http_client", "download_detour"} {
			if _, est := m[chuzhoePole]; est {
				t.Fatalf("rule_set %v: поле %q сетевого remote осталось в local-записи", m, chuzhoePole)
			}
		}
	}
	final, _ := r["final"].(string)
	if final != "direct" {
		t.Fatalf("route.final изменился на %q — комплект не должен трогать final (умная маршрутизация должна остаться)", final)
	}
	if !strings.Contains(k.Zametka, dataPoChelovecheski(pravila.Data())) {
		t.Fatalf("заметка не называет дату снимка комплекта: %q", k.Zametka)
	}
}

// Дата в окне — русская, а не машинная. Читает её человек, который не
// программист: «2026-08-23» он видит как строку из лога, «23.08.2026» — как
// дату. Тест краснеет и от потери формата, и от того, что машинная форма
// протекла в окно вместе с человеческой.
func TestZametkaKomplektaDataPoRusski(t *testing.T) {
	komplekt, err := pravila.Razlozhit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, k, err := Prigotovit(profil(t), Vybor{
		PravilaIzKomplekta:  komplekt,
		PravilaKomplektData: "2026-08-23",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(k.Zametka, "23.08.2026") {
		t.Fatalf("в окне не русская дата: %q", k.Zametka)
	}
	if strings.Contains(k.Zametka, "2026-08-23") {
		t.Fatalf("машинная дата протекла в окно: %q", k.Zametka)
	}
}

// Чужой формат даты не глотаем: снимок комплекта может быть подписан иначе
// («23 августа»). Пустая дата в окне врала бы сильнее непривычной, поэтому
// строка отдаётся как есть.
func TestZametkaKomplektaChuzhoyFormatDatyNeTeryaetsya(t *testing.T) {
	komplekt, err := pravila.Razlozhit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, k, err := Prigotovit(profil(t), Vybor{
		PravilaIzKomplekta:  komplekt,
		PravilaKomplektData: "23 августа",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(k.Zametka, "23 августа") {
		t.Fatalf("чужая дата потерялась из заметки: %q", k.Zametka)
	}
}

// Строгость: комплект применяется ТОЛЬКО целиком — если хотя бы для одного
// тега профиля пути в карте нет, Prigotovit обязан вернуть ошибку, а не
// подменить часть правил, оставив ядро падать на недостающем rule_set.
func TestPravilaIzKomplektaNepolnyyKomplektEtoOshibka(t *testing.T) {
	komplekt, err := pravila.Razlozhit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	delete(komplekt, "ads")
	if _, _, err := Prigotovit(profil(t), Vybor{PravilaIzKomplekta: komplekt}); err == nil {
		t.Fatal("неполный комплект (не хватает тега ads) должен быть ошибкой, а не частичной подменой")
	}
}

// PravilaIzKomplekta главнее BezSetevyhPravil: если взведены оба, ядро
// получает умную маршрутизацию (local rule_set), а не выпотрошенный
// упрощённый режим.
func TestPravilaIzKomplektaGlavneeBezSetevyhPravil(t *testing.T) {
	komplekt, err := pravila.Razlozhit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gotovyy, k, err := Prigotovit(profil(t), Vybor{
		PravilaIzKomplekta: komplekt,
		BezSetevyhPravil:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if k.Zametka == ZametkaBezSetevyhPravil {
		t.Fatal("применился упрощённый режим BezSetevyhPravil, а должен был главенствовать комплект")
	}
	d := razobrat(t, gotovyy)
	r, ok := d["route"].(map[string]any)
	if !ok {
		t.Fatal("route пропал из конфига")
	}
	if _, est := r["rule_set"]; !est {
		t.Fatal("route.rule_set пропал целиком — это поведение BezSetevyhPravil, а не комплекта")
	}
	final, _ := r["final"].(string)
	if final != "direct" {
		t.Fatalf("route.final = %q — BezSetevyhPravil переставил final, хотя комплект должен был главенствовать", final)
	}
}
