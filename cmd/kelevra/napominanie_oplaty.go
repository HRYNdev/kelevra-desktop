package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

// Напоминание об оплате пузырём в трее (решение 12.11: за сроком человек не
// следит сам). Значок раз в shagNapominaniya спрашивает службу
// /api/napominanie; служба решает, что пора сказать (podpiska.NapominanieOplaty:
// за 3 суток до срока и после окончания), и помнит сказанное на диске
// (hranenie.Nastroyki.NapomnenoOplaty). Пузырь показан — значок отмечает это
// POST'ом; не показан — спросит снова на следующем заходе. Новых процессов
// нет: это горутина уже живущего значка.
//
// Файл без build-тега: опрос проверяется на сервере без Windows
// (napominanie_oplaty_test.go), пузырь — trey_windows.go / trey_other.go.

const (
	// pervyyZahodNapominaniya — первый заход после старта значка: служба
	// успевает прочитать подписку.
	pervyyZahodNapominaniya = time.Minute
	shagNapominaniya        = 30 * time.Minute
	// srokVoprosaNapominaniya — служба может сходить к серверу (до 25 с).
	srokVoprosaNapominaniya = 30 * time.Second
)

// puzyrOplatyPoslednim — последний пузырь значка был напоминанием об оплате:
// тычок в него открывает окно, а не ставит обновление (tychokVPuzyr).
var puzyrOplatyPoslednim atomic.Bool

// napominanieSluzhby — GET <адрес>api/napominanie: ключ и текст; пусто —
// говорить нечего или служба не ответила.
func napominanieSluzhby(adres string) (klyuch, tekst string) {
	klient := &http.Client{Timeout: srokVoprosaNapominaniya}
	otvet, err := klient.Get(adres + "api/napominanie")
	if err != nil {
		return "", ""
	}
	defer otvet.Body.Close()
	if otvet.StatusCode != http.StatusOK {
		return "", ""
	}
	var telo struct {
		Klyuch string `json:"klyuch"`
		Tekst  string `json:"tekst"`
	}
	if err := json.NewDecoder(otvet.Body).Decode(&telo); err != nil {
		return "", ""
	}
	return telo.Klyuch, telo.Tekst
}

// otmetitNapominanieSluzhby — POST <адрес>api/napominanie?klyuch=…: показано.
func otmetitNapominanieSluzhby(adres, klyuch string) {
	klient := &http.Client{Timeout: srokVoprosaVersii}
	otvet, err := klient.Post(adres+"api/napominanie?klyuch="+url.QueryEscape(klyuch), "application/json", nil)
	if err != nil {
		log.Printf("значок: отметка о напоминании не дошла до службы: %v", err)
		return
	}
	otvet.Body.Close()
}

// oprashivatNapominaniya — пока не закрыт stop (nil — всю жизнь процесса):
// первый заход через pervyy, дальше раз в shag.
func oprashivatNapominaniya(pervyy, shag time.Duration, adresSluzhby func() (string, bool),
	sprosit func(string) (string, string), pokazat func(string) bool, otmetit func(string, string),
	stop <-chan struct{}) {
	zhdat := pervyy
	for {
		select {
		case <-stop:
			return
		case <-time.After(zhdat):
		}
		zhdat = shag
		adres, est := adresSluzhby()
		if !est {
			continue
		}
		klyuch, tekst := sprosit(adres)
		if klyuch == "" || tekst == "" {
			continue
		}
		if !pokazat(tekst) {
			log.Printf("значок: напоминание об оплате не показано — спрошу снова")
			continue
		}
		log.Printf("значок: напоминание об оплате показано (%s)", klyuch)
		otmetit(adres, klyuch)
	}
}
