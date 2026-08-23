package sluzhba

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// oshibkaIstochnikaPravil — ровно то, что sing-box печатает, когда источник
// route.rule_set недоступен (замер живьём 23.08, см. комментарий в
// PodnyatZashchitu). Ловится по strings.Contains(err.Error(), "initialize
// rule-set"), поэтому важна только эта подстрока, а не текст целиком.
const oshibkaIstochnikaPravil = `initialize rule-set[11]: initial rule-set: cloudflare: Get "https://subkv.chickenkiller.com/rules/cloudflare.srs": connect: connection refused`

// gotovStendLestnicy поднимает Sluzhba на изолированном каталоге с боевым
// профилем (22 remote rule_set) и подложным бинарём ядра — иначе
// PodnyatZashchitu уйдёт качать настоящее ядро из сети.
func gotovStendLestnicy(t *testing.T) *Sluzhba {
	t.Helper()
	t.Setenv("KELEVRA_PRAVA", "net") // как на стенде: тут нет /dev/net/tun
	s := stend(t)
	profil, err := os.ReadFile("../konfig/testdata/profil_telefona.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SohranitProfil(profil); err != nil {
		t.Fatalf("не сохранил профиль: %v", err)
	}
	if err := os.WriteFile(s.Yadro.Bin, []byte("stub"), 0o755); err != nil {
		t.Fatalf("не подложил бинарь ядра: %v", err)
	}
	return s
}

// razobratKonfig читает конфиг, лежащий на диске рядом с ядром, и возвращает
// его route как map — тем же способом, каким это делает konfig.Prigotovit.
func razobratKonfig(t *testing.T, s *Sluzhba) map[string]any {
	t.Helper()
	b, err := os.ReadFile(s.Yadro.PutKonfiga())
	if err != nil {
		t.Fatalf("не прочитал конфиг с диска: %v", err)
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatalf("не разобрал конфиг с диска: %v", err)
	}
	route, _ := d["route"].(map[string]any)
	if route == nil {
		t.Fatalf("в конфиге нет route: %s", b)
	}
	return route
}

// Ступень 2 лестницы (24.08): источник правил недоступен на первом же
// запуске, но встроенный комплект поднимает ядро сам — до ступени 3
// (BezSetevyhPravil, теряющей умную маршрутизацию насовсем) дело дойти не
// должно. Раньше это доказывалось только компиляцией: опечатка в обвязке
// (не тот путь у Razlozhit, перепутанный vybor, проглоченная ошибка) молча
// уронила бы человека на ступень 3, а стенд pravila_nedostupny.sh этого не
// ловит — он бьёт мимо лестницы, прямо в cmd/zamer_konfig.
func TestVstroennyyKomplektDerzhitStupenDva(t *testing.T) {
	s := gotovStendLestnicy(t)

	popytok := 0
	var konfigPeredVtorymZapuskom map[string]any
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		switch popytok {
		case 1:
			return fmt.Errorf("%s", oshibkaIstochnikaPravil)
		case 2:
			// Конфиг под второй запуск обязан лечь на диск ДО этого вызова —
			// проверяем его прямо тут, пока ясно, что это именно попытка №2.
			konfigPeredVtorymZapuskom = razobratKonfig(t, s)
			return nil
		default:
			t.Fatalf("ядро позвали в %d-й раз — ступень 2 обязана хватить двух вызовов", popytok)
			return nil
		}
	}

	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("PodnyatZashchitu вернул ошибку при рабочей ступени 2: %v", err)
	}
	if popytok != 2 {
		t.Fatalf("ядро звали %d раз, ждали ровно 2 (до ступени 3 доходить не должно)", popytok)
	}
	if konfigPeredVtorymZapuskom == nil {
		t.Fatal("не поймали конфиг перед вторым запуском")
	}

	ruleSety, _ := konfigPeredVtorymZapuskom["rule_set"].([]any)
	if len(ruleSety) == 0 {
		t.Fatal("route.rule_set пуст — встроенный комплект не подставился, умная маршрутизация потеряна")
	}
	for _, rs := range ruleSety {
		m, ok := rs.(map[string]any)
		if !ok {
			continue
		}
		if m["type"] != "local" {
			t.Fatalf("rule_set %v остался %v, а не local — источником по-прежнему remote", m["tag"], m["type"])
		}
	}
	if final, _ := konfigPeredVtorymZapuskom["final"].(string); final != "direct" {
		t.Fatalf("route.final = %q, ждали \"direct\" — похоже, ступень 3 (BezSetevyhPravil) взвелась вместо ступени 2", final)
	}
	// BezSetevyhPravil не взводился: route.rule_set остался непустым и
	// перешёл на local (проверено выше), а route.final не переехал на
	// туннельный выход — это ровно то, чего BezSetevyhPravil не оставляет.
	// (Заметку в s.kartina.Zametka тут не проверяем: на этом стенде без
	// Windows следом идёт неудачная попытка проксировать реестр — она сама
	// переписывает Zametka своим текстом, и это отдельная от лестницы вещь.)
}

// Ступень 3 остаётся запасной: если и встроенный комплект не поднял ядро
// (второй отказ подряд с тем же текстом), лестница обязана докатиться до
// прежнего BezSetevyhPravil — иначе человек вовсе без связи.
func TestBezSetevyhPravilOstaetsyaZapasnoyStupenyu(t *testing.T) {
	s := gotovStendLestnicy(t)

	popytok := 0
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		if popytok <= 2 {
			return fmt.Errorf("%s", oshibkaIstochnikaPravil)
		}
		return nil
	}

	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("PodnyatZashchitu вернул ошибку, хотя ступень 3 обязана была спасти: %v", err)
	}
	if popytok != 3 {
		t.Fatalf("ядро звали %d раз, ждали ровно 3 (ступень 2 отказала, дошли до ступени 3)", popytok)
	}

	route := razobratKonfig(t, s)
	if ruleSety, _ := route["rule_set"].([]any); len(ruleSety) != 0 {
		t.Fatalf("route.rule_set не пуст (%d записей) — ступень 3 обязана выбросить их целиком", len(ruleSety))
	}
	if final, _ := route["final"].(string); final == "direct" {
		t.Fatal("route.final остался \"direct\" — на ступени 3 он обязан смотреть в туннельный выход")
	}
}
