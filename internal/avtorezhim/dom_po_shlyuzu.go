package avtorezhim

import (
	"strings"
)

// DomashnieShlyuzyPoUmolchaniyu — аппаратные номера домашних шлюзов, зашитые
// в сборку.
//
// Пока список один: мост роутера Cudy WR3000E, 192.168.1.1 (замер 09.09.2026
// прямо с роутера — `ip link show br-lan`). Он же виден клиентам, подключённым
// через оба репитера: те работают мостом и своих адресов не раздают, поэтому
// шлюз на всю квартиру один и номер у него один.
//
// Почему списком, а не одним значением: смена роутера, вторая квартира и
// проводной сегмент с другим шлюзом — обычные вещи, и каждая из них не повод
// выпускать новую версию. Список переопределяется полем
// [Avtorezhim.DomashnieShlyuzy].
var DomashnieShlyuzyPoUmolchaniyu = []string{
	"80:af:ca:82:17:91",
}

// EtoDomashniyShlyuz — совпадает ли номер шлюза с одним из домашних.
//
// Сравнение нестрогое к виду записи: Windows отдаёт «80-AF-CA-82-17-91»,
// роутер пишет «80:af:ca:82:17:91», человек может вписать «80af.ca82.1791».
// Различать эти три записи значило бы ловить опечатку вместо сети.
func EtoDomashniyShlyuz(mak string, domashnie []string) bool {
	nash := golyyMak(mak)
	if nash == "" {
		return false
	}
	for _, d := range domashnie {
		if golyyMak(d) == nash {
			return true
		}
	}
	return false
}

// golyyMak — только шестнадцатеричные цифры, нижним регистром: разделители
// у разных источников разные, а значение одно.
func golyyMak(mak string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(mak) {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) != 12 {
		return "" // не MAC: пустая строка, мусор, обрезок
	}
	return s
}

// domashnieShlyuzy — список, по которому идёт сравнение в этом заходе.
func (a *Avtorezhim) domashnieShlyuzy() []string {
	if len(a.DomashnieShlyuzy) > 0 {
		return a.DomashnieShlyuzy
	}
	return DomashnieShlyuzyPoUmolchaniyu
}

// makShlyuza — номер шлюза физической сети (подменяемо для теста).
func (a *Avtorezhim) makShlyuza() (string, error) {
	if a.MakShlyuzaFunc != nil {
		return a.MakShlyuzaFunc()
	}
	return MakShlyuza()
}
