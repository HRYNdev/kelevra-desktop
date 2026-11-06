package sluzhba

import (
	"context"
	"os"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
)

// Лечить ли «Дом» перезапуском ядра — только когда обычный туннель жив.
//
// Поле 31.10–07.11.2026, ноут 01-3: самолечение «Дома» перезапускало ядро
// 23 раза, а в 21 из 33 серий «Дом: нет связи» в то же время был плох и
// обычный туннель (сеть, узлы) — перезапуск ничего не лечил, только гасил
// TUN. Мёртвый «Дом» при живом туннеле — это молча умерший мост под
// WireGuard, его и лечит перезапуск; мёртвый «Дом» при мёртвом туннеле —
// сеть или узел, их перезапуск не вернёт. Поэтому перед перезапуском два
// признака, оба должны сказать «туннель жив»:
//
//   - очередь узлов (VestiOcheredUzlov) на последнем шаге не позже
//     svezhestOcheredi назад мерила текущий узел, и он не ответил — плох;
//   - замер выхода моста (обычно селектор «Соединение») ядром — два раза
//     подряд (mostOtvechaet; подмена — dom.most).
//
// Плох — ядро не трогаем и лестницу не тратим (lechitDom): ждём, пока
// туннель оживёт, тогда лечим на ближайшей неудачной пробе «Дома». Здесь
// только условие решения; очередь и замок перезапуска — прежние.

// svezhestOcheredi — насколько свежим должен быть итог очереди узлов, чтобы
// ему верить (шаг очереди — 30 с).
var svezhestOcheredi = 90 * time.Second

// otmetitTunnelPoOcheredi — итог замера текущего узла очередью узлов.
func (s *Sluzhba) otmetitTunnelPoOcheredi(zhiv bool) {
	t := s.chasyDoma()
	s.zamok.Lock()
	s.ocheredZhiv, s.ocheredKogda = zhiv, t
	s.zamok.Unlock()
}

// tunnelPlohPoOcheredi — очередь узлов недавно мерила текущий узел, и он не
// ответил.
func (s *Sluzhba) tunnelPlohPoOcheredi() bool {
	s.zamok.Lock()
	zhiv, kogda := s.ocheredZhiv, s.ocheredKogda
	s.zamok.Unlock()
	return !kogda.IsZero() && !zhiv && s.chasyDoma().Sub(kogda) <= svezhestOcheredi
}

// tunnelDlyaDoma — жив ли обычный туннель (можно ли лечить «Дом»
// перезапуском ядра) и тег выхода моста для журнала. most — подмена замера
// (dom.most); nil — mostOtvechaet.
func (s *Sluzhba) tunnelDlyaDoma(ctx context.Context, most func(context.Context) (bool, string)) (bool, string) {
	if most == nil {
		most = s.mostOtvechaet
	}
	if s.tunnelPlohPoOcheredi() {
		teg := "очередь узлов"
		if s.Yadro != nil {
			if syroy, err := os.ReadFile(s.Yadro.PutKonfiga()); err == nil {
				if t := konfig.MostDomaIzKonfiga(syroy); t != "" {
					teg = t
				}
			}
		}
		return false, teg
	}
	return most(ctx)
}
