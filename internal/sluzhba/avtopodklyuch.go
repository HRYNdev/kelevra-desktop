package sluzhba

// Автоподключение при старте службы (hranenie.Nastroyki.Avtopodklyuch).
//
// Зачем. Поле лежало в настройках с первых версий, но его не писал и не читал
// никто: служба поднимала связь на старте только через авторежим. Человек в
// ручном режиме («Вручную») или с автоматом, выключенным пультом, после
// перезагрузки работал напрямую мимо туннеля, пока сам не нажмёт
// «Подключиться» (карта переходов 07.11, дыра Д2).
//
// Что значит поле теперь: человек хочет связь. Пишется словом человека:
//   - «Подключиться» — да (zapomnitPodklyuchenie);
//   - «Отключить» — нет (otklyuchit);
//   - выбор «Вручную» — как связь стоит в эту минуту (avtorezhimRuchka);
//   - старт службы в ручном режиме при принятом живом ядре — да: связь
//     держали, а поле ещё не знало (первый запуск версии с этим полем).
//
// Читается на старте службы (PodklyuchitPriStarte): авторежим включён и
// разрешён — решает он, как и раньше; иначе поле «да» и связи нет — подъём тем
// же путём, что кнопка (PodnyatZashchitu, под общим замком переходов).

import (
	"context"
	"log"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// zapomnitPodklyuchenie — «Подключиться»: слово человека «хочу связь».
func (s *Sluzhba) zapomnitPodklyuchenie() {
	s.avtorezhimZamok.Lock()
	s.podklyucheniy++
	bylo := s.Nastroyki.Avtopodklyuch
	s.Nastroyki.Avtopodklyuch = true
	nastroyki := s.Nastroyki
	s.avtorezhimZamok.Unlock()
	if bylo {
		return
	}
	if err := hranenie.Sohranit(nastroyki); err != nil {
		log.Printf("«Подключиться»: не сохранил автоподключение: %v", err)
	}
}

// PodklyuchitPriStarte — старт службы (cmd/kelevra), после приёма живого
// ядра и до авторежима: поднять связь, если человек был подключён.
func (s *Sluzhba) PodklyuchitPriStarte(ctx context.Context) {
	s.avtorezhimZamok.Lock()
	avto := s.Nastroyki.Avtorezhim
	nado := s.Nastroyki.Avtopodklyuch
	kodEst := s.Nastroyki.Kod != "" || s.Nastroyki.Podpiska != ""
	s.avtorezhimZamok.Unlock()
	if avto && s.avtomatRazreshen() {
		return // связью распоряжается авторежим (main.go: ZapustitAvtorezhim)
	}
	if s.zashchitaPodnyata() {
		// Живое ядро прошлой копии принято: человек подключён. Поле
		// запоминаем, если оно ещё не знало (первый запуск версии с ним).
		if !nado {
			s.avtorezhimZamok.Lock()
			s.Nastroyki.Avtopodklyuch = true
			nastroyki := s.Nastroyki
			s.avtorezhimZamok.Unlock()
			if err := hranenie.Sohranit(nastroyki); err != nil {
				log.Printf("автоподключение: не сохранил: %v", err)
			}
		}
		return
	}
	if !nado || !kodEst {
		return
	}
	log.Printf("автоподключение: до перезапуска связь была поднята (авторежим выключен) — поднимаю")
	if err := s.PodnyatZashchitu(ctx); err != nil {
		log.Printf("автоподключение: связь не поднялась: %v", err)
	}
}
