package sluzhba

import (
	"encoding/json"
	"log"
	"sort"
	"strings"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/ochered"
)

// Выбор пункта в окне (Nastroyki.Uzly) хранит ИМЯ пункта, а имена задаёт сервер. Когда
// сервер называет пункты иначе (узел «Финляндия» распался на «Финляндия · XHTTP» и
// «Финляндия · Vision», или признак откатили и имена вернулись), на диске остаётся имя
// пункта, которого в конфиге больше нет. Такой выбор ничего не выбирает (ядро его не
// примет), но очередь узлов считала его ручным и не вмешивалась никогда, а окно
// показывало несуществующий выбор. Правило: сохранённый выбор, которого нет среди
// пунктов нового конфига, считается отсутствующим и забывается в момент, когда новый
// конфиг ложится на диск (perestroit).
//
// Переносить выбор «по смыслу» («Финляндия» → «Финляндия · XHTTP») не стали: имя пункта —
// подпись сервера, разбирать её значит угадывать; прежний пункт был группой из всех
// способов узла, и ни один новый пункт ему не равен; а перенос превратил бы выбор в
// ручной, который держит очередь. Забытый выбор возвращает автоматику — безопасная сторона.

// punktyGrupp — пункты каждой группы выбора (selector и urltest) конфига ядра: группа →
// множество имён пунктов. Конфиг не разобран — nil (и тогда ничего не забываем).
func punktyGrupp(syroy []byte) map[string]map[string]bool {
	var d struct {
		Outbounds []struct {
			Type      string   `json:"type"`
			Tag       string   `json:"tag"`
			Outbounds []string `json:"outbounds"`
		} `json:"outbounds"`
	}
	if json.Unmarshal(syroy, &d) != nil || len(d.Outbounds) == 0 {
		return nil
	}
	gruppy := map[string]map[string]bool{}
	for _, o := range d.Outbounds {
		if !strings.EqualFold(o.Type, "selector") && !strings.EqualFold(o.Type, "urltest") {
			continue
		}
		p := make(map[string]bool, len(o.Outbounds))
		for _, n := range o.Outbounds {
			p[n] = true
		}
		gruppy[o.Tag] = p
	}
	return gruppy
}

// ustarevshiyVybor — группы, где сохранённый выбор указывает на пункт, которого в конфиге
// нет (нет самой группы или пункта в ней). Конфиг не разобран — пусто: по мусору выбор
// человека не забываем. Порядок по имени группы — для журнала и проверок.
func ustarevshiyVybor(syroy []byte, uzly map[string]string) []string {
	if len(uzly) == 0 {
		return nil
	}
	gruppy := punktyGrupp(syroy)
	if gruppy == nil {
		return nil
	}
	var ubrat []string
	for g, u := range uzly {
		if !gruppy[g][u] {
			ubrat = append(ubrat, g)
		}
	}
	sort.Strings(ubrat)
	return ubrat
}

// pervyyVOcheredi — пункт uzel группы gruppa первый в очереди узлов этого конфига. Выбор
// первого пункта и есть автоматика: очередь сама стоит на нём, пока он жив.
func pervyyVOcheredi(syroy []byte, gruppa, uzel string) bool {
	g, uzly, ok := ochered.IzKonfiga(syroy)
	return ok && g == gruppa && len(uzly) > 0 && uzly[0] == uzel
}

// zabytUstarevshiyVybor забывает сохранённый выбор, которого нет среди пунктов нового
// конфига ядра. Зовётся там, где конфиг ложится на диск (perestroit), — это и обновление
// профиля, и пересборка под права, и старт службы.
//
// Карта не правится на месте, а заменяется новой: её читают без замка и окно
// (/api/sostoyanie), и очередь узлов.
func (s *Sluzhba) zabytUstarevshiyVybor(syroy []byte) {
	bylo := s.Nastroyki.Uzly
	ubrat := ustarevshiyVybor(syroy, bylo)
	if len(ubrat) == 0 {
		return
	}
	stalo := make(map[string]string, len(bylo))
	for g, u := range bylo {
		stalo[g] = u
	}
	for _, g := range ubrat {
		log.Printf("выбор «%s» в группе %q забыт: такого пункта в новом конфиге нет — дальше выбирает очередь узлов", bylo[g], g)
		delete(stalo, g)
	}
	if len(stalo) == 0 {
		stalo = nil
	}
	s.Nastroyki.Uzly = stalo
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		log.Printf("не сохранил забытый выбор узла: %v", err)
	}
}
