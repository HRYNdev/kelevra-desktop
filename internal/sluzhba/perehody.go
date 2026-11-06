package sluzhba

// Переходы ядра под одним замком (zamokPodyoma).
//
// Переход — всё, что гасит, поднимает или меняет ядро: подъём связи
// (PodnyatZashchitu), перезапуск ради входа домой (perezapustit), перезапуск
// сторожем после падения (storozh_yadra.go), смена ядра при обновлении
// (smenitYadroPlavno), опускание авторежимом (avtorezhimKolbek), запись
// config.json (PerestroitKonfig), уход процесса после установки обновления
// (PostavitNaydennoe) и остановка службы (OstanovitYadroPriVyhode).
//
// Зачем один замок (карта переходов 07.11, гонки Г1–Г5):
//   - обновление exe уходило через os.Exit посреди перезапуска: ядро погашено,
//     запрет WFP живёт в сессии уходящего процесса — человек без туннеля и без
//     запрета до подъёма службы диспетчером;
//   - «Отключить» посреди перезапуска гасило пустое место, а перезапуск
//     поднимал ядро против слова человека;
//   - остановка службы посреди перезапуска гасила пустое место, а перезапуск
//     поднимал ядро уже после ухода службы — сирота с туннелем без хозяина;
//   - ежечасный профиль перетирал config.json, только что собранный подъёмом.
//
// «Отключить» замок не ждёт (кнопка отвечает сразу и обрывает идущий старт
// ядра), но доводит своё после конца идущего перехода (dovestiOtklyuchenie).

import (
	"log"
	"time"
)

// srokPerehodaPeredVyhodom — сколько уход после установки обновления ждёт
// конца идущего перехода. Переход ограничен сам (подъём до 70 с, повторы на
// занятом имени адаптера), этот срок — от вечного ожидания при поломке.
var srokPerehodaPeredVyhodom = 3 * time.Minute

// srokZavershitPerehody — сколько остановка службы ждёт, пока оборванный
// переход дойдёт до конца: диспетчер служб ждёт остановку недолго.
var srokZavershitPerehody = 5 * time.Second

// zhdatZamokPerehoda — взять zamokPodyoma, ожидая не дольше srok. vzyal —
// замок у вызывающего; zhdali — пришлось ждать.
func (s *Sluzhba) zhdatZamokPerehoda(srok time.Duration) (vzyal, zhdali bool) {
	if s.zamokPodyoma.TryLock() {
		return true, false
	}
	konec := time.NewTimer(srok)
	defer konec.Stop()
	shag := time.NewTicker(50 * time.Millisecond)
	defer shag.Stop()
	for {
		select {
		case <-konec.C:
			return false, true
		case <-shag.C:
			if s.zamokPodyoma.TryLock() {
				return true, true
			}
		}
	}
}

// zanyatPerehodyPeredVyhodom — уход после установки обновления: дождаться
// конца идущего перехода и держать замок до выхода процесса (новый переход не
// начнётся). Ядро НЕ трогается: новая копия примет его по записке. false —
// переход не кончился за srok, уходим всё равно (как до замка): вечно висеть
// со вставшей на диск новой версией хуже.
func (s *Sluzhba) zanyatPerehodyPeredVyhodom(srok time.Duration) bool {
	nachalo := time.Now()
	vzyal, zhdali := s.zhdatZamokPerehoda(srok)
	switch {
	case !vzyal:
		log.Printf("установка обновления: переход ядра не кончился за %s — ухожу всё равно", srok)
	case zhdali:
		log.Printf("установка обновления: дождался конца перехода ядра (%s) — ухожу между переходами", time.Since(nachalo).Round(100*time.Millisecond))
	}
	return vzyal
}

// zavershitPerehody — служба уходит (остановка, уступка места): с этой минуты
// ядро не запускается вовсе (yadro.ZapretitZapusk), идущий старт обрывается,
// идущий переход дожидается своего конца не дольше srok. Замок остаётся у
// уходящего: процесс кончается.
func (s *Sluzhba) zavershitPerehody(srok time.Duration) {
	s.uhodit.Store(true)
	if s.Yadro == nil {
		return
	}
	s.Yadro.ZapretitZapusk()
	_ = s.Yadro.Ostanovit()
	if vzyal, _ := s.zhdatZamokPerehoda(srok); !vzyal {
		log.Printf("служба уходит: переход ядра не закончился за %s — ядро погашено и больше не запустится", srok)
	}
}

// OstanovitYadroPriVyhode — последний шаг остановки службы (cmd/kelevra):
// погасить ядро так, чтобы его не поднял ни идущий перезапуск, ни сторож.
func (s *Sluzhba) OstanovitYadroPriVyhode() {
	s.zavershitPerehody(srokZavershitPerehody)
	if s.Yadro != nil {
		_ = s.Yadro.Ostanovit()
	}
}

// chisloPodklyucheniy — сколько раз человек нажал «Подключиться».
func (s *Sluzhba) chisloPodklyucheniy() uint64 {
	s.avtorezhimZamok.Lock()
	defer s.avtorezhimZamok.Unlock()
	return s.podklyucheniy
}

// dovestiOtklyuchenie — «Отключить» пришлось на идущий переход ядра (подъём,
// перезапуск «Дома», перезапуск сторожем). Сам переход «Отключить» видит
// (поколение, число отключений) и ядро не поднимает, но между последней его
// проверкой и стартом ядра есть щель. Поэтому, когда переход кончится, связь
// опускается ещё раз — если человек с тех пор не сказал ничего нового
// (pokolenie, podkl сняты сразу после «Отключить»).
func (s *Sluzhba) dovestiOtklyuchenie(pokolenie, podkl uint64) {
	if s.zamokPodyoma.TryLock() {
		// Перехода нет: «Отключить» уже сделало всё.
		s.zamokPodyoma.Unlock()
		return
	}
	log.Printf("«Отключить» пришлось на переход ядра — после его конца проверю, что связь опущена")
	go func() {
		vzyal, _ := s.zhdatZamokPerehoda(srokPerehodaPeredVyhodom)
		if !vzyal {
			log.Printf("«Отключить»: переход ядра не кончился за %s — не довожу", srokPerehodaPeredVyhodom)
			return
		}
		defer s.zamokPodyoma.Unlock()
		if s.pokolenieAvtorezhima() != pokolenie || s.chisloPodklyucheniy() != podkl {
			return // человек с тех пор сказал своё
		}
		if !s.zashchitaPodnyata() {
			return
		}
		log.Printf("«Отключить»: переход ядра успел поднять связь — опускаю")
		if err := s.OpustitZashchitu(); err != nil {
			log.Printf("«Отключить»: не опустил связь после перехода: %v", err)
		}
	}()
}
