package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// Новая копия занимает место старой.
//
// Беда 08.09 с живой машины. Человек обновился: файл на диске стал новым,
// новая копия запустилась — и замолчала, потому что место было занято живой
// старой (замок ОС, internal/kopiya). Связью продолжала рулить прежняя
// версия, а человек был уверен, что работает свежая. Все правки того дня до
// него просто не доезжали: он ставил 0.6.57, а работала 0.6.55. Со стороны
// это выглядело как «ничего не чинится».
//
// Само занятое место — правильная защита: две копии дерутся за один сетевой
// адаптер и рвут человеку связь (разбор там же). Не хватало ровно одного —
// способа сказать старой копии, что пришла новее. Обычное обновление уходит
// само (PostavitNaydennoe гасит себя и поднимает смену), но запуск нового
// файла РУКАМИ этого пути не знает вовсе, а именно так человек и обновляется,
// когда качает установщик сам.
//
// Решение здесь только просит. Проверяет версии и уступает — сама старая
// копия (ручка /api/ustupit_mesto): доверять чужому «я новее» нельзя, иначе
// случайный запуск прошлогоднего файла из Загрузок откатил бы человека назад.

// srokUstupki — сколько ждём, пока старая копия уйдёт. Локальный HTTP плюс
// остановка ядра: если за это время не ушла, значит и не уйдёт.
const srokUstupki = 12 * time.Second

// zanyatMestoEsliMyNovee просит живую чужую копию уступить место, когда наша
// версия новее. true — место освободилось, можно поднимать своё.
//
// Молчаливое false во всех остальных случаях: чужая копия той же версии (или
// новее) — это норма, человек просто запустил приложение второй раз.
func zanyatMestoEsliMyNovee(adres string) bool {
	chuzhaya := versiyaChuzhoyKopii(adres)
	if chuzhaya == "" {
		return false
	}
	if obnovlenie.Sravnit(podpiska.Versiya, chuzhaya) <= 0 {
		// Мы не новее — ничего не трогаем: у чужой копии больше прав на место.
		return false
	}
	log.Printf("на машине работает версия %s, а мы %s — прошу её уступить место", chuzhaya, podpiska.Versiya)

	klient := &http.Client{Timeout: srokUstupki}
	req, err := http.NewRequest(http.MethodPost, adres+"api/ustupit_mesto?versiya="+podpiska.Versiya, nil)
	if err != nil {
		return false
	}
	otvet, err := klient.Do(req)
	if err != nil {
		log.Printf("старая копия не ответила на просьбу уступить место (%v) — работаю как есть", err)
		return false
	}
	otvet.Body.Close()
	if otvet.StatusCode != http.StatusOK {
		log.Printf("старая копия место не уступила (код %d) — работаю как есть", otvet.StatusCode)
		return false
	}
	return zhdatOsvobozhdeniya(adres)
}

// versiyaChuzhoyKopii спрашивает работающую копию, какая она версии.
func versiyaChuzhoyKopii(adres string) string {
	klient := &http.Client{Timeout: 3 * time.Second}
	otvet, err := klient.Get(adres + "api/obnovlenie")
	if err != nil {
		return ""
	}
	defer otvet.Body.Close()
	var telo struct {
		Tekushchaya string `json:"tekushchaya"`
	}
	if err := json.NewDecoder(otvet.Body).Decode(&telo); err != nil {
		return ""
	}
	return telo.Tekushchaya
}

// zhdatOsvobozhdeniya ждёт, пока старая копия перестанет отвечать. Ждём по
// ФАКТУ молчания, а не на фиксированной паузе: та же причина, по какой
// zhdatSmenu ждёт смерть pid, а не спит наугад.
func zhdatOsvobozhdeniya(adres string) bool {
	klient := &http.Client{Timeout: time.Second}
	predel := time.Now().Add(srokUstupki)
	for time.Now().Before(predel) {
		otvet, err := klient.Get(adres + "api/sostoyanie")
		if err != nil {
			log.Printf("старая копия ушла, занимаю место")
			return true
		}
		otvet.Body.Close()
		time.Sleep(300 * time.Millisecond)
	}
	log.Printf("старая копия за %s так и не ушла — работаю как есть", srokUstupki)
	return false
}
