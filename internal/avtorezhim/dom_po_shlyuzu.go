package avtorezhim

import (
	"errors"
	"strings"
)

// slovaSvoegoAdaptera — куски описания/имени, по которым отсеивается НАШ
// собственный TUN-адаптер (или чужой VPN): профиль не всегда даёт нам
// заранее знать точное системное имя интерфейса (internal/konfig задаёт
// interface_name динамически), поэтому фильтр — по типу и по подстроке в
// описании, как и обычные VPN-клиенты друг у друга.
//
// Лежит здесь, а не рядом с чтением адаптера: список нужен и Windows-пути
// (SetevoyAdapter, шлюз), и стендовому (шлюз из /proc/net/route), а два
// одинаковых списка на две платформы разъедутся в первый же день.
var slovaSvoegoAdaptera = []string{"wireguard", "wintun", "sing-box", "tun"}

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

// EstDomashniyShlyuz — есть ли домашний хоть среди одного из прочитанных
// шлюзов.
//
// Вопрос ставится именно так, а не «какой шлюз главный». Маршрутов по
// умолчанию на машине бывает несколько (Radmin VPN, Hamachi, ZeroTier,
// корпоративный клиент), и который из них окажется первым или лучшим по
// метрике — их дело, не наше. Нам нужно знать одно: виден ли рядом домашний
// роутер. Если виден — человек дома, чем бы ни был занят остальной список.
//
// Замер, из которого это правило родилось, — в MakiShlyuzov: 09.09.2026
// человек сидел дома, Wi-Fi отдавал домашний шлюз, а вердикт вынес Radmin
// VPN, стоявший в списке первым.
func EstDomashniyShlyuz(maki []string, domashnie []string) bool {
	for _, m := range maki {
		if EtoDomashniyShlyuz(m, domashnie) {
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

// makiShlyuzov — номера шлюзов физической сети.
//
// Пустое поле значит «читать шлюзы некому», а НЕ «взять боевое чтение»: так
// же устроены TunnelPodnyat и SetevoyAdres. Боевое чтение ставит только
// [Novyy] — иначе каждый тест, собирающий Avtorezhim литералом, молча ушёл
// бы на настоящие шлюзы машины, где его гоняют, и проверял бы не своё
// правило, а сеть сборщика (так и вышло на Windows-раннере 09.09).
func (a *Avtorezhim) makiShlyuzov() ([]string, error) {
	if a.MakiShlyuzovFunc == nil {
		return nil, errShlyuzNeChitaetsya
	}
	return a.MakiShlyuzovFunc()
}

// errShlyuzNeChitaetsya — «читать шлюз некому». Отдельная ошибка, чтобы её
// было видно в журнале как настройку, а не как сбой сети.
var errShlyuzNeChitaetsya = errors.New("чтение шлюза не подключено")
