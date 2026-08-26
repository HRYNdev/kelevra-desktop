package main

import (
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
)

// ustanovitObnovlenie — хук, которым тычок в пузырь трея (trey_windows.go:
// tychokVPuzyr) зовёт установку найденного обновления. Живёт без build-тега
// (не в trey_windows.go/trey_other.go): подключение из main.go должно
// собираться на любой платформе, а не только на Windows, где живёт сам трей.
// nil — служба ещё не поднялась (или это стенд-сборка без main) — тогда
// tychokVPuzyr тихо пишет в журнал и ничего не делает.
var ustanovitObnovlenie func() (string, error)

// zapustitSmenuPosleObnovleniya поднимает новую копию (уже свежую с диска)
// после того, как sluzhba.PostavitNaydennoe заменила .exe на месте: ровно та
// же передача смены, что уже работает у prava.Poprosit после согласия на UAC
// (см. main.go: --smena, zhdatSmenu) — новая копия сама дожидается смерти
// этого pid, а не гонка на фиксированной паузе. pid здесь — эта, ещё старая
// копия, которая вот-вот уйдёт (см. sluzhba.PostavitNaydennoe).
//
// put приходит от вызывающего кода, а не от собственного obnovlenie.PutSebya()
// здесь: этот хук зовётся из ЕЩЁ ЖИВОЙ старой копии уже ПОСЛЕ Postavit(), а на
// Linux os.Executable() у такого процесса возвращает путь к переименованному
// .old-файлу (readlink /proc/self/exe следует за inode при rename) — новая
// копия стартовала бы от старого файла и сама себя перекачивала бы заново. См.
// комментарий поля PerezapuskPosleObnovleniya в internal/sluzhba/sluzhba.go.
func zapustitSmenuPosleObnovleniya(put string, pid int) error {
	cmd := exec.Command(put, argTiho, argSmena, strconv.Itoa(pid))
	cmd.Env = os.Environ()
	return cmd.Start()
}

// periodFonovoyProverki — обычно obnovlenie.PeriodFonovoyProverki (часы), но
// KELEVRA_PERIOD_OBNOVLENIYA (формат time.ParseDuration, например "50ms")
// позволяет стенду разогнать тикер SleditZaObnovleniem, не дожидаясь
// настоящих часов, — по тому же образцу, что KELEVRA_RELIZY подменяет адрес
// GitHub. У человека эта переменная не задана.
func periodFonovoyProverki() time.Duration {
	if svoy := os.Getenv("KELEVRA_PERIOD_OBNOVLENIYA"); svoy != "" {
		if d, err := time.ParseDuration(svoy); err == nil {
			return d
		}
	}
	return obnovlenie.PeriodFonovoyProverki
}

// srokTolchkaFonovoyProverki — сколько ждём ответа уже работающей копии на
// толчок проверить обновление. Локальный HTTP по 127.0.0.1, поэтому коротко:
// не ответила за это время — сама разберётся по своему расписанию
// (internal/sluzhba.SleditZaObnovleniem), ждать тут нечего и незачем.
const srokTolchkaFonovoyProverki = 3 * time.Second

// podtolknutFonovuyuProverku просит уже работающую копию (адрес взят из
// adresKopii, main.go) проверить обновление прямо сейчас — момент, когда
// человек только что кликнул значок в трее, чтобы поднять её окно. Копия
// могла висеть в фоне неделями, и её собственный тик по расписанию ещё
// нескоро — раньше в этот момент проверки не было вовсе.
//
// Фоном, без ожидания ответа в main(): открытие окна не имеет права
// задержаться на сетевой (пусть и локальный) запрос. Отказ (адрес мёртв,
// служба не успела поднять ручку) тоже не беда — тихо в журнал, есть же
// собственное расписание проверки внутри самой копии.
func podtolknutFonovuyuProverku(adres string) {
	go func() {
		klient := &http.Client{Timeout: srokTolchkaFonovoyProverki}
		req, err := http.NewRequest(http.MethodPost, adres+"api/obnovlenie_proverit", nil)
		if err != nil {
			return
		}
		otvet, err := klient.Do(req)
		if err != nil {
			log.Printf("толчок фоновой проверке обновления у чужой копии не дошёл (%v), не беда — сработает по своему расписанию", err)
			return
		}
		otvet.Body.Close()
	}()
}
