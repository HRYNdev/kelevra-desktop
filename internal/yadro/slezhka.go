package yadro

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Слежка за РАБОТАЮЩИМ ядром: упало ли оно само и не зависло ли.
//
// Зачем. До 07.11 смерть своего ядра замечала только горутина Wait — и ставила
// «сломалось», на которое в службе не реагировал никто, кроме авторежима; без
// авторежима человек оставался без туннеля до ручного «Подключиться». Зависшее
// ядро (процесс жив, Clash API молчит) не замечал никто, если оно своё, а
// принятое объявлялось умершим, но процесс не гасился: авторежим поднимал
// второе ядро рядом с зомби, державшим tun125 и маршруты, — ровно механизм
// утечки #168 («два TUN, новый молчит»).
//
// Теперь: ядро, которое работало и ушло само, — Padenie через PriPadenii
// (служба перезапускает его, sluzhba/storozh_yadra.go). Зависшее — гасится и
// дожидается выхода процесса, и только потом объявляется Padenie: новое ядро
// не поднимется рядом с живым старым.

// Padenie — ядро работало и ушло не по нашей остановке.
type Padenie struct {
	PID int
	// Svoyo — ядро запускала эта копия (иначе принятое у прошлой копии).
	Svoyo bool
	// Kod — код выхода процесса; -1 — неизвестен (принятое ядро не на Windows,
	// зависшее, погашенное нами).
	Kod int
	// Zavislo — процесс был жив, а Clash API молчал дольше порога: погашен
	// слежкой (Molchal — сколько молчал).
	Zavislo bool
	Molchal time.Duration
	// Hvost — причина из журнала ядра (hvostLoga).
	Hvost string
	Kogda time.Time
}

// Opisanie — одной строкой для журнала службы.
func (p Padenie) Opisanie() string {
	rod := "принятое"
	if p.Svoyo {
		rod = "своё"
	}
	kod := "код выхода неизвестен"
	if p.Kod >= 0 {
		kod = fmt.Sprintf("код выхода %d", p.Kod)
	}
	if p.Zavislo {
		return fmt.Sprintf("ядро зависло (pid %d, %s): процесс жив, Clash API молчал %s — погашено", p.PID, rod, p.Molchal.Round(time.Second))
	}
	return fmt.Sprintf("ядро упало само (pid %d, %s, %s)", p.PID, rod, kod)
}

// PorogZavisaniyaPoUmolchaniyu — сколько Clash API живого процесса может
// молчать, прежде чем ядро считается зависшим. С запасом: под нагрузкой и
// после сна ответ бывает медленным, а гасить работающее ядро — рвать связь.
const PorogZavisaniyaPoUmolchaniyu = 20 * time.Second

// ShagSlezhkiPoUmolchaniyu — как часто слежка спрашивает Clash API.
const ShagSlezhkiPoUmolchaniyu = 2 * time.Second

// srokVyhoda — сколько ждать выхода процесса после сигнала.
const srokVyhoda = 5 * time.Second

func (y *Yadro) porogZavisaniya() time.Duration {
	if y.PorogZavisaniya > 0 {
		return y.PorogZavisaniya
	}
	return PorogZavisaniyaPoUmolchaniyu
}

func (y *Yadro) shagSlezhki() time.Duration {
	if y.ShagSlezhki > 0 {
		return y.ShagSlezhki
	}
	return ShagSlezhkiPoUmolchaniyu
}

// soobshchitPadenie — зовёт PriPadenii в своей горутине: слушатель поднимает
// ядро заново, а это Zapustit, которому нужен y.zamok.
func (y *Yadro) soobshchitPadenie(p Padenie) {
	if y.PriPadenii == nil {
		return
	}
	go y.PriPadenii(p)
}

// sleditZaApi следит за работающим ядром d: молчит ли его Clash API.
//
// Своё ядро: его смерть ловит горутина Wait (Zapustit); здесь — только
// зависание. Принятое: дожидаться чужого процесса нельзя, поэтому и смерть, и
// зависание замечаются отсюда — смерть по ОС (processZhiv), зависание по
// молчанию API дольше porogZavisaniya.
//
// Порог считается и числом молчаливых опросов подряд, и временем: после сна
// машины монотонное время на Windows сна не включает, а опросы в первый миг
// после пробуждения ещё могут не пройти.
func (y *Yadro) sleditZaApi(d *derzhatel, umer chan struct{}) {
	shag := y.shagSlezhki()
	porog := y.porogZavisaniya()
	nuzhnoOprosov := int(porog / shag)
	if nuzhnoOprosov < 1 {
		nuzhnoOprosov = 1
	}
	var molchitS time.Time
	molchaniy := 0
	for {
		select {
		case <-umer:
			return
		case <-time.After(shag):
		}
		y.zamok.Lock()
		nashe := y.derzh == d && y.sost == Rabotaet
		y.zamok.Unlock()
		if !nashe {
			return // остановили, сменили или уже умерло
		}
		if y.Zhivo() {
			molchaniy, molchitS = 0, time.Time{}
			continue
		}
		if molchaniy == 0 {
			molchitS = time.Now()
		}
		molchaniy++
		if !d.svoy() {
			if zhiv, kod, znaem := processZhiv(d.pid); znaem && !zhiv {
				y.prinyatoeUshlo(d, umer, Padenie{PID: d.pid, Kod: kod})
				return
			}
		}
		if molchaniy < nuzhnoOprosov || time.Since(molchitS) < porog {
			continue
		}
		y.pogasitZavisshee(d, umer, time.Since(molchitS))
		return
	}
}

// prinyatoeUshlo — принятое ядро больше не работает (вышло само или погашено
// как зависшее и вышло): состояние «сломалось», слушателю — Padenie.
func (y *Yadro) prinyatoeUshlo(d *derzhatel, umer chan struct{}, p Padenie) {
	put := filepath.Join(y.Papka, imyaZhurnala)
	y.zamok.Lock()
	if y.derzh != d { // ушло ровно в тот миг, когда его меняли
		y.zamok.Unlock()
		return
	}
	y.derzh, y.sost = nil, Slomalos
	y.poslLog = hvostLoga(put)
	p.Hvost, p.Kogda = y.poslLog, time.Now()
	y.zamok.Unlock()
	UbratPeredachu(y.Papka)
	log.Printf("%s: %s", p.Opisanie(), p.Hvost)
	close(umer)
	y.soobshchitPadenie(p)
}

// pogasitZavisshee гасит ядро, чей Clash API молчит дольше порога, и ДОЖИДАЕТСЯ
// выхода процесса: пока он жив, жив и его адаптер с маршрутами, и новое ядро
// рядом трафика не получит (#168). Не вышел — Padenie не объявляется вовсе:
// поднимать новое рядом с живым старым нельзя.
func (y *Yadro) pogasitZavisshee(d *derzhatel, umer chan struct{}, molchal time.Duration) {
	y.zamok.Lock()
	if y.derzh != d {
		y.zamok.Unlock()
		return
	}
	if d.pid == os.Getpid() {
		// Сама служба под видом принятого ядра (только проверки): не гасим,
		// только признаём неработающим — как до сторожа.
		y.derzh, y.sost = nil, Slomalos
		y.zamok.Unlock()
		close(umer)
		return
	}
	d.zavislo, d.molchal = true, molchal
	y.zamok.Unlock()
	log.Printf("ядро (pid %d, %s) живо, но Clash API молчит %s — считаю зависшим: гашу процесс и жду его выхода",
		d.pid, rodYadra(d), molchal.Round(time.Second))
	if err := d.ubit(); err != nil {
		log.Printf("зависшее ядро (pid %d) не погасилось: %v", d.pid, err)
	}
	if d.svoy() {
		// Остальное (состояние, Padenie) сделает горутина Wait, когда процесс
		// действительно выйдет: она видит d.zavislo.
		select {
		case <-umer:
		case <-time.After(srokVyhoda):
			log.Printf("зависшее ядро (pid %d) не вышло за %s — гашу ещё раз и жду", d.pid, srokVyhoda)
			_ = d.ubit()
		}
		return
	}
	if !zhdatVyhodaChuzhogo(d.pid, 6*srokVyhoda) {
		y.zamok.Lock()
		if y.derzh == d {
			y.derzh, y.sost = nil, Slomalos
			y.poslLog = "ядро зависло и не вышло после сигнала"
		}
		y.zamok.Unlock()
		log.Printf("зависшее принятое ядро (pid %d) не вышло за %s — новое рядом не поднимаю", d.pid, 6*srokVyhoda)
		close(umer)
		return
	}
	y.prinyatoeUshlo(d, umer, Padenie{PID: d.pid, Kod: -1, Zavislo: true, Molchal: molchal})
}

// zhdatVyhodaChuzhogo — ждёт, пока процесс pid выйдет (по ОС). true — вышел
// или ОС про него не знает уже ничего; false — жив по истечении srok или ОС
// не отвечает.
func zhdatVyhodaChuzhogo(pid int, srok time.Duration) bool {
	konec := time.Now().Add(srok)
	for {
		zhiv, _, znaem := processZhiv(pid)
		if znaem && !zhiv {
			return true
		}
		if time.Now().After(konec) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
