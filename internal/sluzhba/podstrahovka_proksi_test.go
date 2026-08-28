package sluzhba

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// oshibkaSistemnogoProksi — то же самое, что напечатало настоящее ядро на
// стенде zhivoy_trafik.sh в приёмке выпуска 0.6.32 (28.08): «start
// inbound/mixed[mixed-in]: initialize system proxy: unsupported desktop
// environment». Важна только подстрока «system proxy», как и в самой
// PodnyatZashchitu.
const oshibkaSistemnogoProksi = `FATAL[0000] start service: start inbound/mixed[mixed-in]: initialize system proxy: unsupported desktop environment`

// setSystemProxyVhoda читает конфиг с диска и возвращает set_system_proxy у
// входа mixed/http — konfig.Prigotovit пишет BezSistemnogoProksi именно туда
// (internal/konfig/konfig.go, не в route), поэтому проверять надо inbound, а
// не route, как razobratKonfig делает для правил.
func setSystemProxyVhoda(t *testing.T, s *Sluzhba) (bool, bool) {
	t.Helper()
	b, err := os.ReadFile(s.Yadro.PutKonfiga())
	if err != nil {
		t.Fatalf("не прочитал конфиг с диска: %v", err)
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatalf("не разобрал конфиг с диска: %v", err)
	}
	vhody, _ := d["inbounds"].([]any)
	for _, v := range vhody {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if tip, _ := m["type"].(string); tip == "mixed" || tip == "http" {
			sp, ok := m["set_system_proxy"].(bool)
			return sp, ok
		}
	}
	return false, false
}

// TestSistemnyyProksiLovitsyaNaStupeniKomplekta — регрессия 28.08: приёмка
// выпуска 0.6.32 замерила живым ядром на стенде zhivoy_trafik.sh, что
// PodnyatZashchitu умирает связью целиком, если «system proxy» падает не на
// самой первой попытке, а уже ВНУТРИ лестницы правил, на ступени встроенного
// комплекта. Исходная попытка отказала по «initialize rule-set» (источник
// правил недоступен), лестница честно перешла на комплект — а комплект сам
// упал на системном прокси, и единственная проверка «system proxy» в начале
// функции этот второй, более поздний отказ уже не видела: она смотрела на
// err ДО лестницы, который был про rule-set, а не про proxy. Человек
// оставался вовсе без связи там, где обе подстраховки по отдельности рабочие.
func TestSistemnyyProksiLovitsyaNaStupeniKomplekta(t *testing.T) {
	s := gotovStendLestnicy(t)

	popytok := 0
	var konfigPeredUspekhom map[string]any
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		switch popytok {
		case 1:
			// Источник правил недоступен — лестница уходит на комплект.
			return fmt.Errorf("%s", oshibkaIstochnikaPravil)
		case 2:
			// Комплект сам не смог поднять ядро — система не даёт system proxy.
			return fmt.Errorf("%s", oshibkaSistemnogoProksi)
		case 3:
			// Подстраховка обязана была домешать BezSistemnogoProksi К
			// комплекту (а не откатиться сразу на BezSetevyhPravil) и
			// повторить запуск — вот эта попытка обязана дойти и победить.
			konfigPeredUspekhom = razobratKonfig(t, s)
			return nil
		default:
			t.Fatalf("ядро позвали в %d-й раз — подстраховка обязана хватить трёх вызовов", popytok)
			return nil
		}
	}

	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("PodnyatZashchitu вернул ошибку, хотя подстраховка обязана была срастить обе беды: %v", err)
	}
	if popytok != 3 {
		t.Fatalf("ядро звали %d раз, ждали ровно 3 (rule-set → komplekt упал на proxy → komplekt без proxy)", popytok)
	}
	if konfigPeredUspekhom == nil {
		t.Fatal("не поймали конфиг перед победным запуском")
	}
	if sp, ok := setSystemProxyVhoda(t, s); !ok || sp {
		t.Fatalf("вход mixed всё ещё просит систему настроить прокси — set_system_proxy=%v (был=%v), ждали false (BezSistemnogoProksi обязан взвестись)", sp, ok)
	}
	ruleSety, _ := konfigPeredUspekhom["rule_set"].([]any)
	if len(ruleSety) == 0 {
		t.Fatal("route.rule_set пуст — победа далась откатом на BezSetevyhPravil, а не BezSistemnogoProksi поверх комплекта: умная маршрутизация потеряна зря")
	}
}

// TestSistemnyyProksiLovitsyaNaStupeniBezPravil — тот же провал, но на самой
// нижней ступени лестницы (BezSetevyhPravil): комплект не спас (не по
// причине proxy — например, файлов комплекта на диске нет), и запасная
// ступень «вовсе без правил» тоже упала на «system proxy». Подстраховка
// обязана домешать BezSistemnogoProksi и туда — иначе человек без связи даже
// после того, как отдал умную маршрутизацию целиком.
func TestSistemnyyProksiLovitsyaNaStupeniBezPravil(t *testing.T) {
	s := gotovStendLestnicy(t)
	// На месте папки под встроенный комплект лежит ФАЙЛ — pravila.Razlozhit
	// откажет на MkdirAll, лестница уйдёт сразу на BezSetevyhPravil, минуя
	// ступень 2 (комплект).
	pravilaPapka := filepath.Join(hranenie.PapkaYadra(), "pravila")
	if err := os.MkdirAll(filepath.Dir(pravilaPapka), 0o755); err != nil {
		t.Fatalf("подготовка папки: %v", err)
	}
	if err := os.WriteFile(pravilaPapka, []byte("не папка"), 0o644); err != nil {
		t.Fatalf("подложить файл вместо папки комплекта: %v", err)
	}

	popytok := 0
	var konfigPeredUspekhom map[string]any
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		switch popytok {
		case 1:
			return fmt.Errorf("%s", oshibkaIstochnikaPravil)
		case 2:
			return fmt.Errorf("%s", oshibkaSistemnogoProksi)
		case 3:
			konfigPeredUspekhom = razobratKonfig(t, s)
			return nil
		default:
			t.Fatalf("ядро позвали в %d-й раз — подстраховка обязана хватить трёх вызовов", popytok)
			return nil
		}
	}

	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("PodnyatZashchitu вернул ошибку, хотя запасная ступень обязана была срастить обе беды: %v", err)
	}
	if popytok != 3 {
		t.Fatalf("ядро звали %d раз, ждали ровно 3 (rule-set → komplekt недоступен → без правил упал на proxy → без правил без proxy)", popytok)
	}
	if konfigPeredUspekhom == nil {
		t.Fatal("не поймали конфиг перед победным запуском")
	}
	if sp, ok := setSystemProxyVhoda(t, s); !ok || sp {
		t.Fatalf("вход mixed всё ещё просит систему настроить прокси — set_system_proxy=%v (был=%v), ждали false (BezSistemnogoProksi обязан взвестись)", sp, ok)
	}
}
