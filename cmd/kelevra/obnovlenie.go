package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// srokProverki — сколько ждём ответа GitHub. Это время человек стоит перед
// закрытым окном, поэтому оно короткое: не ответили — работаем как есть.
const srokProverki = 6 * time.Second

// srokZagruzki — сколько даём на скачивание самой сборки (около 8 МБ).
const srokZagruzki = 3 * time.Minute

// obnovitsya ставит свежую сборку и перезапускает приложение.
// true — этот процесс своё отработал, дальше живёт уже новый.
//
// Обновление идёт ДО поднятия службы: так не приходится освобождать порт и
// метку запуска, а человек в худшем случае видит, что приложение открывается
// на несколько секунд дольше обычного.
func obnovitsya() bool {
	if os.Getenv("KELEVRA_BEZ_OBNOVLENIYA") == "1" {
		return false
	}
	put, err := obnovlenie.PutSebya()
	if err != nil {
		log.Printf("обновление: не знаю, где лежу: %v", err)
		return false
	}
	// Хвост прошлого обновления: пока старый файл был запущен, удалить его
	// было нельзя.
	obnovlenie.UbratHvost(put)

	adres := obnovlenie.SpisokReliza
	if svoy := os.Getenv("KELEVRA_RELIZY"); svoy != "" {
		adres = svoy // стенд
	}
	ctx, otmena := context.WithTimeout(context.Background(), srokProverki)
	defer otmena()
	n, err := obnovlenie.Proverit(ctx, &http.Client{Timeout: srokProverki}, adres, podpiska.Versiya)
	if err != nil {
		// Нет сети — это обычное дело для приложения, которое чинит сеть.
		log.Printf("обновление: проверить не вышло (%v), работаю как есть", err)
		return false
	}
	if n == nil {
		return false
	}

	log.Printf("обновление: вышла версия %s, ставлю", n.Versiya)
	ctxZ, otmenaZ := context.WithTimeout(context.Background(), srokZagruzki)
	defer otmenaZ()
	if err := obnovlenie.Postavit(ctxZ, &http.Client{Timeout: srokZagruzki}, *n, put); err != nil {
		// Частый случай — приложение лежит там, куда нет права записи.
		// Это не повод не запуститься: работаем старой версией.
		log.Printf("обновление: не поставилось (%v), работаю старой версией", err)
		return false
	}
	log.Printf("обновление: версия %s встала, перезапускаюсь", n.Versiya)

	cmd := exec.Command(put, os.Args[1:]...)
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		// Новый файл на месте, но запустить его не вышло: сказать человеку
		// честнее, чем молча закрыться.
		log.Printf("ОТКАЗ: новая версия не запустилась: %v", err)
		skazat("Kelevra обновилась", "Kelevra обновилась до версии "+n.Versiya+
			", но запустить её сама не смогла.\n\nЗапустите приложение ещё раз.")
		return true
	}
	return true
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
