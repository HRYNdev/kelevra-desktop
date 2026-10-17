package sluzhba

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
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
// «Подключаюсь» перед этим — их человек смотрит впустую. С 0.6.77 частичного
// режима нет вовсе: туннель на другом имени или честный отказ с причиной.
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
		t.Fatal("повтор пошёл без входа-туннеля")
	}
	s.zamok.Lock()
	k := s.kartina
	s.zamok.Unlock()
	if k.Rezhim != konfig.Tunnel {
		t.Fatalf("после удачного повтора режим %q — человек потерял туннель зря", k.Rezhim)
	}
}

// Свободного имени нет вовсе (заняты и имя из профиля, и все соседние).
// Ступени ниже туннеля нет: связь не поднимается, ядро не запускают вовсе
// (заведомо падающий запуск стоит человеку пятнадцати секунд), а человеку
// названа причина и то единственное, что её снимает, — перезагрузка.
func TestSvobodnogoImeniNetOtkazSPrichinoy(t *testing.T) {
	var vse []string
	for i := 100; i <= 200; i++ {
		vse = append(vse, fmt.Sprintf("tun%d", i))
	}
	s := stendSAdapterami(t, vse...)

	zapuskov := 0
	s.zapustitYadro = func(ctx context.Context) error {
		zapuskov++
		return nil
	}

	err := s.PodnyatZashchitu(context.Background())
	if !errors.Is(err, OshibkaAdapterZanyat) {
		t.Fatalf("ждали OshibkaAdapterZanyat, получили %v", err)
	}
	if err.Error() != "Сетевое устройство занято прошлым подключением. Требуется перезагрузка компьютера." {
		t.Fatalf("человеку сказано %q", err.Error())
	}
	if zapuskov != 0 {
		t.Fatalf("ядро запускали %d раз — свободного имени нет, запуск заведомо упадёт", zapuskov)
	}
	s.zamok.Lock()
	otkaz := s.otkaz
	s.zamok.Unlock()
	if otkaz != OshibkaAdapterZanyat.Error() {
		t.Fatalf("окну отдан отказ %q — причина обязана дойти до экрана", otkaz)
	}
}

// Ядро падает на занятом имени и на каждом повторе. Повторов — разумный
// предел (povtorovAdaptera), дальше честный отказ: дело уже не в остатке
// прошлой попытки, а в системе. Прокси вместо туннеля не поднимается.
func TestPovtoryNeVyshliOtkazBezProksi(t *testing.T) {
	s := stendSAdapterami(t)

	var imena []string
	s.zapustitYadro = func(ctx context.Context) error {
		imena = append(imena, imyaAdaptera(t, s))
		return fmt.Errorf("%s", oshibkaZanyatogoAdaptera)
	}

	err := s.PodnyatZashchitu(context.Background())
	if !errors.Is(err, OshibkaAdapterZanyat) {
		t.Fatalf("ждали OshibkaAdapterZanyat, получили %v", err)
	}
	if len(imena) != 1+povtorovAdaptera {
		t.Fatalf("заходов %d (%v) — ждали исходный и %d повтора", len(imena), imena, povtorovAdaptera)
	}
	vidnye := map[string]bool{}
	for _, imya := range imena {
		if imya == "" {
			t.Fatalf("один из заходов шёл без туннеля (%v) — это откат в прокси, которого быть не должно", imena)
		}
		if vidnye[imya] {
			t.Fatalf("повтор пошёл на уже упавшем имени: %v", imena)
		}
		vidnye[imya] = true
	}
	if _, _, est := tunnel.ProchestMetku(); est {
		t.Fatal("след неудачной попытки остался на диске — следующий запуск будет искать несуществующий адаптер")
	}
	for _, tip := range tipyVhodov(t, s) {
		if tip != "tun" {
			t.Fatalf("в конфиге ядра появился вход %q", tip)
		}
	}
}

// Второй повтор удался: туннель поднят, адрес сдвинут тем же шагом, что имя.
func TestVtoroyPovtorUdalsyaAdresSdvinut(t *testing.T) {
	s := stendSAdapterami(t)
	var imena []string
	s.zapustitYadro = func(ctx context.Context) error {
		imena = append(imena, imyaAdaptera(t, s))
		if len(imena) < 3 {
			return fmt.Errorf("%s", oshibkaZanyatogoAdaptera)
		}
		return nil
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("второй повтор удался, а связь не поднята: %v", err)
	}
	if len(imena) != 3 || imena[2] != "tun127" {
		t.Fatalf("имена заходов %v — ждали tun125, tun126, tun127", imena)
	}
	if !strings.Contains(string(konfigSDiska(t, s)), "172.19.0.9/30") {
		t.Fatal("адрес туннеля не сдвинут вместе с именем — ядро упадёт на «object already exists»")
	}
	s.zamok.Lock()
	otkaz := s.otkaz
	s.zamok.Unlock()
	if otkaz != "" {
		t.Fatalf("связь поднята, а окну отдан отказ %q", otkaz)
	}
}

// konfigSDiska — конфиг, приготовленный для ядра прямо сейчас.
func konfigSDiska(t *testing.T, s *Sluzhba) []byte {
	t.Helper()
	b, err := os.ReadFile(s.Yadro.PutKonfiga())
	if err != nil {
		t.Fatalf("не прочитал конфиг с диска: %v", err)
	}
	return b
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
