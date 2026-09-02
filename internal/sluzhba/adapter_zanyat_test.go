package sluzhba

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/tunnel"
)

// Занятое имя сетевого адаптера туннеля.
//
// Беда с живой машины человека 01.09, из его журнала:
//
//	22:07:42 поднят туннель (адаптер "tun125")
//	22:07:45 человек нажал «Отключить» / останавливаю ядро
//	22:07:58 ядро запущено: pid 29604
//	22:08:14 ядро упало само: FATAL start service: start inbound/tun[tun-in]:
//	         configure tun interface: (create adapter: Cannot create a file when
//	         that file already exists. | open existing adapter: Element not found.)
//	22:08:14 полный режим не поднялся — работаю частично
//
// Повторилось в тот вечер дважды. Человек выдал права, туннель ему положен, а
// приложение показало «Частично — мимо идут игры». Плюс пятнадцать секунд
// «Подключаюсь» перед этим — их человек смотрит впустую.
//
// Проверки написаны на СЦЕНАРИЙ: что человек получил в итоге и что уехало
// ядру, а не как устроен подбор имени.

// oshibkaZanyatogoAdaptera — дословно то, что ядро напечатало на машине
// человека. Подставляется в подменный запуск ядра, чтобы приложение чинило
// именно эту беду, а не её пересказ.
const oshibkaZanyatogoAdaptera = `ядро упало при старте: FATAL[0015] start service: start inbound/tun[tun-in]: ` +
	`configure tun interface: (create adapter: Cannot create a file when that file already exists. ` +
	`| open existing adapter: Element not found.)`

// imyaAdaptera — interface_name входа-туннеля в конфиге, который приготовлен
// для ядра прямо сейчас. Пусто — входа-туннеля в конфиге нет вовсе.
func imyaAdaptera(t *testing.T, s *Sluzhba) string {
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
		if tip, _ := m["type"].(string); tip != "tun" {
			continue
		}
		imya, _ := m["interface_name"].(string)
		return imya
	}
	return ""
}

// stendSAdapterami — стенд лестницы с правами администратора и подменённой
// проверкой сетевых адаптеров. Настоящих адаптеров в проверках быть не должно
// вовсе: воспроизвести занятое имя живьём значит поднять на машине
// проверяющего настоящий туннель (запрет трогать сеть).
//
// Срок ожидания — наносекунда: проверки судят ВЫБОР приложения, а не его
// терпение, и три боевых секунды сна в каждой воровали бы время прогона.
func stendSAdapterami(t *testing.T, zanyatye ...string) *Sluzhba {
	t.Helper()
	s := stendSPravami(t)
	nabor := map[string]bool{}
	for _, i := range zanyatye {
		nabor[i] = true
	}
	s.adapterZhivDlyaStenda = func(imya string) bool { return nabor[imya] }
	s.srokOsvobozhdeniyaDlyaStenda = time.Nanosecond
	return s
}

// Главный сценарий починки: имя из профиля занято остатком прошлой попытки.
// Приложение обязано узнать это ДО запуска ядра и поднять туннель на
// свободном имени — а не потратить пятнадцать секунд человека и откатиться.
func TestZanyatoeImyaAdapteraNeStoitChelovekuPolnogoRezhima(t *testing.T) {
	s := stendSAdapterami(t, "tun125")

	popytok := 0
	var imyaVYadre string
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		imyaVYadre = imyaAdaptera(t, s)
		return nil
	}

	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("имя адаптера было занято, и защита не поднялась вовсе: %v", err)
	}
	if popytok != 1 {
		t.Fatalf("ядро запускали %d раза — занятость имени видна заранее, лишних попыток быть не должно", popytok)
	}
	if imyaVYadre == "tun125" {
		t.Fatal("ядру уехало занятое имя tun125 — оно упадёт на нём через 15 секунд, как и падало")
	}
	if imyaVYadre == "" {
		t.Fatal("вход-туннель выброшен из конфига: приложение отдало полный режим там, где хватило сменить имя")
	}

	s.zamok.Lock()
	k := s.kartina
	s.zamok.Unlock()
	if k.Rezhim != konfig.Tunnel {
		t.Fatalf("режим %q — человек выдал права и остался без полного режима из-за имени адаптера", k.Rezhim)
	}
	if k.Chastichnaya {
		t.Fatal("защита объявлена половинной, хотя туннель поднят")
	}
	// След туннеля на диске обязан нести НАСТОЯЩЕЕ имя адаптера: по нему
	// следующий запуск ищет остаток после жёсткой смерти копии.
	sled, _, est := tunnel.ProchestMetku()
	if !est || sled != imyaVYadre {
		t.Fatalf("в следе имя %q (есть=%v), а туннель поднят на %q — следующий запуск будет искать не тот адаптер",
			sled, est, imyaVYadre)
	}
}

// Проверка перед запуском ловит не всё, и это видно в самой строке ядра:
// «open existing adapter: Element not found» значит, что в списке сетевых
// интерфейсов Windows адаптера уже нет — система честно ответит «свободно», а
// ядро всё равно упадёт. На повтор надо идти с ДРУГИМ именем.
func TestYadroUpaloNaZanyatomImeniPovtorNaSvobodnom(t *testing.T) {
	s := stendSAdapterami(t) // система говорит: свободно всё

	var imena []string
	s.zapustitYadro = func(ctx context.Context) error {
		imena = append(imena, imyaAdaptera(t, s))
		if len(imena) == 1 {
			return fmt.Errorf("%s", oshibkaZanyatogoAdaptera)
		}
		return nil
	}

	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("повтор на свободном имени не случился, защита не поднялась: %v", err)
	}
	if len(imena) != 2 {
		t.Fatalf("ядро запускали %d раз(а) с именами %v — ждали ровно один повтор", len(imena), imena)
	}
	if imena[1] == imena[0] {
		t.Fatalf("повтор пошёл на том же имени %q, на котором ядро только что упало", imena[0])
	}
	if imena[1] == "" {
		t.Fatal("повтор пошёл без входа-туннеля — это не повтор, а откат в половину")
	}
	s.zamok.Lock()
	k := s.kartina
	s.zamok.Unlock()
	if k.Rezhim != konfig.Tunnel || k.Chastichnaya {
		t.Fatalf("после удачного повтора режим %q (половинный=%v) — человек потерял полный режим зря",
			k.Rezhim, k.Chastichnaya)
	}
}

// Уборка не удалась: свободного имени нет вовсе (заняты и имя из профиля, и
// все соседние). Приложение не имеет права ни упасть, ни промолчать — оно
// опускается на ступень ниже и НАЗЫВАЕТ причину человеческими словами.
//
// Отдельно проверяется, что ядро в полном режиме не запускали вовсе: заведомо
// падающий запуск стоит человеку пятнадцати секунд «Подключаюсь».
func TestSvobodnogoImeniNetChestnyyOtkatSPrichinoy(t *testing.T) {
	var vse []string
	for i := 100; i <= 200; i++ {
		vse = append(vse, fmt.Sprintf("tun%d", i))
	}
	s := stendSAdapterami(t, vse...)

	var imena []string
	s.zapustitYadro = func(ctx context.Context) error {
		imena = append(imena, imyaAdaptera(t, s))
		return nil
	}

	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("имя адаптера занято, и человек остался вовсе без связи: %v", err)
	}
	for _, imya := range imena {
		if imya != "" {
			t.Fatalf("ядро всё-таки запускали с входом-туннелем (%v) — 15 секунд ожидания впустую", imena)
		}
	}

	s.zamok.Lock()
	k := s.kartina
	s.zamok.Unlock()
	if k.Rezhim != konfig.Proksi || !k.Chastichnaya {
		t.Fatalf("режим %q, половинность %v — ждали честную половину", k.Rezhim, k.Chastichnaya)
	}
	if k.PochemuChastichnaya != konfig.PrichinaAdapterZanyat {
		t.Fatalf("причина половинчатости %q — человеку не назвали, что случилось; "+
			"общее «попробуйте ещё раз» тут врёт: повторное нажатие упрётся в то же устройство",
			k.PochemuChastichnaya)
	}
	// Заметки, которые говорят «так и задумано», тут запрещены той же
	// причиной, что и в откате по любой другой беде.
	if k.Zametka == konfig.ZametkaBezPrav || k.Zametka == konfig.ZametkaBezTunnelya {
		t.Fatalf("заметка %q врёт, что полного режима человеку не положено", k.Zametka)
	}
}

// Ядро упало на занятом имени, повтор на соседнем упал так же. Дело не в
// остатке прошлой попытки, а в самой системе: приложение обязано перестать
// тратить время человека, опуститься в половинный режим и назвать причину.
func TestPovtorTozheNeVyshelOtkatNazyvaetPrichinu(t *testing.T) {
	s := stendSAdapterami(t)

	popytokSTunnelem := 0
	s.zapustitYadro = func(ctx context.Context) error {
		if imyaAdaptera(t, s) == "" {
			return nil // половинный режим поднимается всегда
		}
		popytokSTunnelem++
		return fmt.Errorf("%s", oshibkaZanyatogoAdaptera)
	}

	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("оба захода за туннелем не вышли, и откат тоже — человек без связи: %v", err)
	}
	if popytokSTunnelem != 2 {
		t.Fatalf("заходов за туннелем %d — ждали ровно два (исходный и один повтор на другом имени)", popytokSTunnelem)
	}

	s.zamok.Lock()
	k := s.kartina
	s.zamok.Unlock()
	if k.Rezhim != konfig.Proksi || !k.Chastichnaya {
		t.Fatalf("режим %q, половинность %v — ждали честную половину", k.Rezhim, k.Chastichnaya)
	}
	if k.PochemuChastichnaya != konfig.PrichinaAdapterZanyat {
		t.Fatalf("причина половинчатости %q — беда известна поимённо, молчать о ней нельзя", k.PochemuChastichnaya)
	}
	if _, _, est := tunnel.ProchestMetku(); est {
		t.Fatal("след неудачной попытки остался на диске — следующий запуск будет искать несуществующий адаптер")
	}
}

// Обычный запуск на свободном имени не имеет права ничего стоить и ничего
// менять: подбор заведён ради редкой беды, а платит за него каждое нажатие.
func TestSvobodnoeImyaObychnyyPutNeMenyaetsya(t *testing.T) {
	s := stendSAdapterami(t) // занятых имён нет

	var imyaVYadre string
	s.zapustitYadro = func(ctx context.Context) error {
		imyaVYadre = imyaAdaptera(t, s)
		return nil
	}
	nachalo := time.Now()
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatal(err)
	}
	if imyaVYadre != "tun125" {
		t.Fatalf("имя адаптера %q, в профиле tun125 — приложение переименовало адаптер на ровном месте", imyaVYadre)
	}
	if proshlo := time.Since(nachalo); proshlo > 5*time.Second {
		t.Fatalf("обычное подключение заняло %s — подбор имени встал в цену каждого нажатия", proshlo)
	}
}
