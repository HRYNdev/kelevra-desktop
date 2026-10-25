package konfig

import (
	"encoding/json"
	"path/filepath"
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

// По частям (0.6.93): тег без пути в карте остаётся remote — его ядро качает
// само, — а остальные переходят на local. Раньше одного недостающего тега
// хватало, чтобы Prigotovit отказал целиком и человек остался без правил;
// со своими наборами (svoi-*), которых во встроенном комплекте нет, так
// выходило бы на каждом подъёме.
func TestPravilaIzKomplektaPoChastyam(t *testing.T) {
	komplekt, err := pravila.Razlozhit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	delete(komplekt, "ads")
	gotovyy, _, err := Prigotovit(profil(t), Vybor{PravilaIzKomplekta: komplekt})
	if err != nil {
		t.Fatalf("неполная карта наборов должна применяться по частям, а не отказом: %v", err)
	}
	r := razobrat(t, gotovyy)["route"].(map[string]any)
	for _, rs := range r["rule_set"].([]any) {
		m := rs.(map[string]any)
		switch m["tag"] {
		case "ads":
			if m["type"] != "remote" || m["url"] == nil {
				t.Fatalf("набор ads без пути в карте должен остаться remote со своим адресом, а он %v", m)
			}
		default:
			if m["type"] != "local" {
				t.Fatalf("набор %v с путём в карте остался %v", m["tag"], m["type"])
			}
		}
	}
}

// Свой набор (svoi-*) лежит JSON-файлом: local-запись обязана сказать ядру
// format:"source", иначе оно попробует читать JSON как двоичный набор и не
// поднимется.
func TestPravilaIzKeshaJsonEtoSource(t *testing.T) {
	d := razobrat(t, profil(t))
	r := d["route"].(map[string]any)
	r["rule_set"] = append(r["rule_set"].([]any), map[string]any{
		"tag": "svoi-tunnel", "type": "remote", "format": "source",
		"url": "https://dver.example/u/klyuch/svoi-tunnel", "update_interval": "2m",
	})
	syroy, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	put := filepath.Join(t.TempDir(), "svoi-tunnel.json")
	gotovyy, _, err := Prigotovit(syroy, Vybor{PravilaIzKomplekta: map[string]string{"svoi-tunnel": put}})
	if err != nil {
		t.Fatal(err)
	}
	for _, rs := range razobrat(t, gotovyy)["route"].(map[string]any)["rule_set"].([]any) {
		m := rs.(map[string]any)
		if m["tag"] != "svoi-tunnel" {
			continue
		}
		if m["type"] != "local" || m["format"] != "source" || m["path"] != put {
			t.Fatalf("свой набор из кеша описан неверно: %v", m)
		}
		return
	}
	t.Fatal("свой набор пропал из route.rule_set")
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

// Правила из КЕША — не встроенный комплект, и заметки про комплект быть не должно.
//
// Беда, замеченная владельцем 08.09: в окне висело «Свежие правила не
// скачались — работают встроенные (от )» — с пустой скобкой вместо даты и,
// главное, неправдой по сути. Правила у него были не встроенные, а свежий
// кеш, скачанный этой же машиной. Пустая дата и была признаком кеша:
// служба ставит PravilaKomplektData = "" именно в этой ветке.
func TestPravilaIzKeshaNeVydayutsyaZaVstroennyyKomplekt(t *testing.T) {
	komplekt, err := pravila.Razlozhit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, k, err := Prigotovit(profil(t), Vybor{
		PravilaIzKomplekta: komplekt,
		// Даты нет: так служба помечает наборы, взятые из кеша на диске.
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(k.Zametka, "встроенные") {
		t.Fatalf("заметка про встроенный комплект при правилах из кеша: %q", k.Zametka)
	}
	if k.PravilaOtkuda != "кеш" {
		t.Fatalf("источник правил %q, а наборы взяты из кеша", k.PravilaOtkuda)
	}
	if k.PravilNaborov != len(komplekt) {
		t.Fatalf("наборов в картине %d, а подставлено %d", k.PravilNaborov, len(komplekt))
	}
}

// Размер пакета туннеля виден в картине, а значит и в журнале.
//
// От него прямо зависит скорость: профиль присылает 9000, клиент сбивает до
// 1420, и разница на живой машине была 7.7 с против 1.1 с на открытие ya.ru.
// При этом слова «mtu» не было ни в одной строке журналов десктопа за всю их
// историю, и жалобу «сайты по 10 секунд» проверить было нечем.
func TestRazmerPaketaVidenVKartine(t *testing.T) {
	_, k, err := Prigotovit(profil(t), Vybor{})
	if err != nil {
		t.Fatal(err)
	}
	if k.MtuTun != MtuDlyaOkna {
		t.Fatalf("в картине размер пакета %d, а туннель собран с %d", k.MtuTun, MtuDlyaOkna)
	}
}
