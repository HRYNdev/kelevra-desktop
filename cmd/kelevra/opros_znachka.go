package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// Подсказка значка по состоянию службы (правка 06.11).
//
// Процесс --trey не поднимает связь и хука sluzhba.MetkaZashchity у него нет,
// поэтому подсказка всегда оставалась «отключено». Теперь значок сам спрашивает
// службу раз в shagOprosaZnachka и зовёт pometitZashchitu — только при смене.

// shagOprosaZnachka — как часто значок спрашивает состояние службы.
const shagOprosaZnachka = 4 * time.Second

// sostoyanieSluzhby — поле sost из /api/sostoyanie. Пусто — служба не
// ответила или ответ не разобрать.
func sostoyanieSluzhby(adres string) string {
	klient := &http.Client{Timeout: srokVoprosaVersii}
	otvet, err := klient.Get(adres + "api/sostoyanie")
	if err != nil {
		return ""
	}
	defer otvet.Body.Close()
	if otvet.StatusCode != http.StatusOK {
		return ""
	}
	var telo struct {
		Sost string `json:"sost"`
	}
	if err := json.NewDecoder(otvet.Body).Decode(&telo); err != nil {
		return ""
	}
	return telo.Sost
}

// svyazPodnyata — sost "rabotaet" (yadro.Rabotaet: API ядра отвечает) — то же
// условие, по которому окно рисует «весь трафик» (oblik/index.html). "podnimaem"
// ещё не связь, всё остальное и пустая строка — тем более.
func svyazPodnyata(sost string) bool { return sost == "rabotaet" }

// oprashivatSostoyanie держит подсказку значка в согласии со службой, пока не
// закрыт stop. Первый опрос — сразу, и значок получает вызов, только когда
// значение отличается от последнего сообщённого (изначально — «не поднята»,
// так значок и стартует). Службы нет или не ответила — «не поднята».
func oprashivatSostoyanie(shag time.Duration, adresSluzhby func() (string, bool),
	sost func(string) string, pometit func(bool), stop <-chan struct{}) {
	izvestno := false
	for {
		podnyata := false
		if adres, est := adresSluzhby(); est {
			podnyata = svyazPodnyata(sost(adres))
		}
		if podnyata != izvestno {
			izvestno = podnyata
			if podnyata {
				log.Printf("значок: связь поднята")
			} else {
				log.Printf("значок: связь не поднята")
			}
			pometit(podnyata)
		}
		select {
		case <-stop:
			return
		case <-time.After(shag):
		}
	}
}
