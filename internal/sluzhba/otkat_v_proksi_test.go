package sluzhba

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/tunnel"
)

// Последняя ступень лестницы деградации режимов: полный режим не поднялся.
//
// Дыра, которую эти проверки закрывают (прогон 31.08). Права у приложения
// есть — значит конфиг собран под туннель; ядро на нём не поднялось (адаптер
// не создался, драйвера нет, система не дала). Приложение уходило в
// «сломалось», человек оставался с надписью «нет связи» И БЕЗ ВСЯКОЙ защиты —
// хотя половинная поднимается тут же рядом и прав не требует вовсе. Правильно
// — честно спуститься ступенькой ниже, а не упасть с лестницы.
//
// Проверки написаны на СЦЕНАРИЙ, а не на устройство отката: что человек
// получил в итоге (связь, честное «частично» и правдивую причину) и что
// осталось на диске от неудавшейся попытки.

// oshibkaTunnelya — то же, что печатает настоящее ядро, когда сетевой адаптер
// создать не вышло (эту строку показывает и стенд облика, сцена 5_slomalos).
const oshibkaTunnelya = `FATAL[0000] start service: initialize inbound/tun[0]: configure tun interface: permission denied`

// stendSPravami — стенд лестницы, которому подменены права администратора.
// Без подмены полный режим на проверяющей машине не воспроизвести вовсе:
// prava.Est() отвечает про настоящий процесс, а гонять проверки от
// администратора значит поднимать настоящий сетевой адаптер (см. поле
// Sluzhba.pravaDlyaStenda и запрет трогать сеть на машине человека).
func stendSPravami(t *testing.T) *Sluzhba {
	t.Helper()
	s := gotovStendLestnicy(t)
	s.pravaDlyaStenda = func() bool { return true }
	return s
}

// Туннель не создался — человек обязан остаться СО СВЯЗЬЮ: приложение
// поднимается в режиме браузеров, честно называет себя «частично» и говорит
// правдивую причину. Плюс за неудачной попыткой не должно остаться следа на
// диске: он соврал бы следующему запуску, что туннель поднимали мы.
func TestTunnelNePodnyalsyaOtkatVRezhimBrauzerov(t *testing.T) {
	s := stendSPravami(t)
	// След прошлого сеанса — то, что уборка отката обязана снять за собой.
	tunnel.Otmetit("tun125", 4242)

	popytok := 0
	var vhodyPeredOtkatom []string
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		switch popytok {
		case 1:
			return fmt.Errorf("%s", oshibkaTunnelya)
		case 2:
			vhodyPeredOtkatom = tipyVhodov(t, s)
			return nil
		default:
			t.Fatalf("ядро позвали в %d-й раз — откату хватает двух попыток", popytok)
			return nil
		}
	}

	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("туннель не поднялся, но и откат не случился — человек остался без связи: %v", err)
	}

	s.zamok.Lock()
	k := s.kartina
	s.zamok.Unlock()
	if k.Rezhim != konfig.Proksi {
		t.Fatalf("после отката режим %q, ждали режим браузеров", k.Rezhim)
	}
	if !k.Chastichnaya {
		t.Fatal("защита половинная, а приложение об этом молчит: круг покажет обычное «подключено»")
	}
	if k.PochemuChastichnaya != konfig.PrichinaTunnelNePodnyalsya {
		t.Fatalf("причина половинчатости %q — про права тут врать нельзя, они есть", k.PochemuChastichnaya)
	}
	// Заметка не имеет права оказаться ни одной из тех, что говорят «так и
	// задумано»: ZametkaBezPrav шлёт человека на кнопку «Включить для всех
	// программ», которой при наличии прав на экране нет вовсе, а
	// ZametkaBezTunnelya врёт, что полного режима нет в ключе доступа. Ровно
	// это и получалось, пока подъём прокси затирал заметку отката своей.
	//
	// Точное равенство ZametkaTunnelNePodnyalsya тут не проверить: на линуксе
	// (где и гоняются проверки) proksi.Postavit — заглушка, всегда «не
	// поставил», и заметка честно становится «впишите прокси руками». Сам
	// текст отката проверяется там, где он рождается, — internal/konfig,
	// TestOtkatBezTunnelyaGovoritPravdu.
	if k.Zametka == konfig.ZametkaBezPrav || k.Zametka == konfig.ZametkaBezTunnelya {
		t.Fatalf("заметка %q — она говорит, что так и задумано, хотя полный режим только что не вышел", k.Zametka)
	}
	if _, _, est := tunnel.ProchestMetku(); est {
		t.Fatal("след неудачной попытки остался на диске — следующий запуск будет искать несуществующий адаптер")
	}
	// Конфиг второй попытки — уже без входа-туннеля: иначе ядро упало бы на
	// том же самом месте, и «откат» был бы повтором той же попытки.
	for _, tip := range vhodyPeredOtkatom {
		if tip == "tun" {
			t.Fatalf("во второй попытке снова вход-туннель (%v) — это не откат, а тот же заход", vhodyPeredOtkatom)
		}
	}
	if len(vhodyPeredOtkatom) == 0 {
		t.Fatal("во второй попытке в конфиге не осталось ни одного входа — подключаться нечем")
	}
}

// Значок в трее обязан узнать про половинчатость отката тем же движением, что
// и окно: подсказка «Kelevra: подключено» на половинной защите — это та же
// ложь, из-за которой поле Chastichnaya вообще появилось.
func TestOtkatSoobshchaetZnachkuProPolovinu(t *testing.T) {
	s := stendSPravami(t)
	var chastichnaya bool
	var pochemu string
	var podnyata bool
	s.MetkaZashchity = func(p, ch bool, pch string) { podnyata, chastichnaya, pochemu = p, ch, pch }

	popytok := 0
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		if popytok == 1 {
			return fmt.Errorf("%s", oshibkaTunnelya)
		}
		return nil
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("откат не случился: %v", err)
	}

	if !podnyata || !chastichnaya || pochemu != konfig.PrichinaTunnelNePodnyalsya {
		t.Fatalf("значку сказали подняталась=%v частично=%v причина=%q", podnyata, chastichnaya, pochemu)
	}
}

// Не поднялось ничего — тогда это честная беда, а не тихое «всё хорошо».
// Наверх обязана уйти ошибка, и в ней — обе причины: почему не вышел полный
// режим и почему не вышел даже половинный.
func TestNiTunnelNiBrauzeryChestnayaOshibka(t *testing.T) {
	s := stendSPravami(t)
	tunnel.Otmetit("tun125", 4242)

	popytok := 0
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		if popytok == 1 {
			return fmt.Errorf("%s", oshibkaTunnelya)
		}
		return fmt.Errorf("порт 2412 занят другой программой")
	}

	err := s.PodnyatZashchitu(context.Background())
	if err == nil {
		t.Fatal("не поднялось ничего, а приложение ответило успехом — человек увидит зелёный круг поверх пустоты")
	}
	if !strings.Contains(err.Error(), "configure tun interface") {
		t.Fatalf("в ошибке нет первой причины (почему не вышел полный режим): %v", err)
	}
	if !strings.Contains(err.Error(), "2412") {
		t.Fatalf("в ошибке нет второй причины (почему не вышел и половинный режим): %v", err)
	}
	if _, _, est := tunnel.ProchestMetku(); est {
		t.Fatal("подключиться не вышло вовсе, а след туннеля остался лежать")
	}
}

// Обратная сторона: когда туннель поднялся, никакого отката быть не должно —
// иначе лестница деградации срабатывала бы на ровном месте и отбирала у
// человека полную защиту, которая только что заработала.
func TestUdavshiysyaTunnelNikudaNeOtkatyvaetsya(t *testing.T) {
	s := stendSPravami(t)

	popytok := 0
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		return nil
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("туннель поднялся, а метод вернул ошибку: %v", err)
	}
	if popytok != 1 {
		t.Fatalf("ядро поднимали %d раз(а) — на удавшемся туннеле попытка одна", popytok)
	}
	s.zamok.Lock()
	k := s.kartina
	s.zamok.Unlock()
	if k.Rezhim != konfig.Tunnel || k.Chastichnaya {
		t.Fatalf("режим %q, частично=%v — полный режим не имеет права стать половинным", k.Rezhim, k.Chastichnaya)
	}
	if _, _, est := tunnel.ProchestMetku(); !est {
		t.Fatal("туннель подняли, а следа на диске нет — после жёсткой смерти проверять будет нечего")
	}
}

// tipyVhodov — какие входы (tun/mixed/...) лежат в конфиге, приготовленном
// для ядра прямо сейчас.
func tipyVhodov(t *testing.T, s *Sluzhba) []string {
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
	var tipy []string
	for _, v := range vhody {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		tip, _ := m["type"].(string)
		tipy = append(tipy, tip)
	}
	return tipy
}
