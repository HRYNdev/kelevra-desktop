package yadro

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"
)

// Соответствие «адрес → имя сайта», взятое у самого ядра.
//
// Зачем. В строке отказа ядро печатает АДРЕС: «open connection to
// [2a02:6b8:a::a]:443 … network is unreachable». Имя оно знает, но в эту строку
// не кладёт. Из-за этого ось «сайт» в журнале компьютера отсутствовала вовсе:
// 11.09.2026 в журналах телефонов отказы читались как «mc.yandex.ru», а в
// журналах компьютеров — как голые адреса, и диагноз по компьютеру поставить
// было нечем. На телефоне это закрыли 10.09 (bg/ImenaSaytov.kt), здесь — тем же
// приёмом и по тому же источнику.
//
// Откуда берём. У ядра есть служебный порт (ApiAdres, `experimental.clash_api` в
// профиле), и там список живых соединений вместе с именем в metadata. Тот же
// порт приложение уже спрашивает про трафик — новой двери не заводим.
//
// Почему кэшем, а не запросом на каждый отказ. Отказов бывает по три сотни за
// семь минут (настоящий случай на телефоне Вики), и триста запросов к ядру ради
// строчки в журнале — цена несоразмерная. Соединение живёт секунды, но отказ
// случается сразу после его появления, поэтому короткого кэша хватает.
type Imena struct {
	zamok sync.Mutex

	// svezhie и proshlye — два поколения, а не LRU.
	//
	// Ядро отдаёт снимок ЖИВЫХ соединений, а спрашивают про отказ, чей
	// соединение уже могло исчезнуть — поэтому копим, а не заменяем снимок.
	// Копить без края нельзя, и вместо возни с порядком обращений держим два
	// поколения: переполнилось свежее — оно становится прошлым, новое пустое.
	// Имя, найденное в прошлом поколении, поднимается в свежее, так что
	// нужное не теряется, пока его спрашивают.
	svezhie  map[string]string
	proshlye map[string]string

	sprashivali time.Time

	// Potolok — сколько имён помним, прежде чем сменить поколение.
	Potolok int
	// NeChashche — реже этого срока ядро не тревожим.
	NeChashche time.Duration
	// Chitat — откуда берём тело ответа. Точка подмены для проверок: настоящее
	// чтение ходит в сеть к живому ядру, а проверкам нужен разбор и учёт
	// времени, а не сеть.
	Chitat func() (string, error)
}

// NovyeImena — справочник на живом ядре.
func NovyeImena(y *Yadro) *Imena {
	return NovyeImenaNaChtenii(func() (string, error) { return y.soedineniyaSyroe() })
}

// NovyeImenaNaChtenii — справочник на произвольном источнике ответа.
//
// Нужен тем, кто проверяет разбор отказов целиком (internal/sluzhba): живого
// ядра там нет и быть не должно, а подставить готовый ответ служебного порта
// иначе неоткуда — внутренности справочника закрыты пакетом.
func NovyeImenaNaChtenii(chitat func() (string, error)) *Imena {
	return &Imena{
		svezhie:    map[string]string{},
		proshlye:   map[string]string{},
		Potolok:    256,
		NeChashche: 5 * time.Second,
		Chitat:     chitat,
	}
}

// Imya — имя сайта по адресу, если ядро его называло.
//
// Пусто — значит не знаем, и врать не будем: в журнал уйдёт один адрес, как
// раньше. Пустой ответ здесь честнее догадки: диагноз по чужому имени хуже
// диагноза по адресу.
func (im *Imena) Imya(adres string) string {
	if im == nil || adres == "" {
		return ""
	}
	im.zamok.Lock()
	defer im.zamok.Unlock()
	if imya := im.naydti(adres); imya != "" {
		return imya
	}
	im.obnovit(time.Now())
	return im.naydti(adres)
}

// naydti — поиск по обоим поколениям. Зовётся под уже взятым замком.
func (im *Imena) naydti(adres string) string {
	if imya, est := im.svezhie[adres]; est {
		return imya
	}
	if imya, est := im.proshlye[adres]; est {
		// Спрашивают — значит ещё нужно: поднимаем в свежее поколение, чтобы
		// следующая смена поколений его не унесла.
		im.svezhie[adres] = imya
		return imya
	}
	return ""
}

// obnovit — забрать у ядра список соединений. Зовётся под уже взятым замком.
func (im *Imena) obnovit(seychas time.Time) {
	if !im.sprashivali.IsZero() && seychas.Sub(im.sprashivali) < im.NeChashche {
		return
	}
	im.sprashivali = seychas
	if im.Chitat == nil {
		return
	}
	telo, err := im.Chitat()
	if err != nil || telo == "" {
		// Молча: ядро могло перезапускаться, а шуметь в журнал на каждый
		// неудачный опрос — значит топить в нём настоящие беды.
		return
	}
	im.razobrat(telo)
}

// razobrat — разложить ответ ядра по адресам.
//
// Держим оба вида адреса: тот, куда пошли на самом деле (destinationIP), и
// исходный (remoteDestination) — отказ печатается по первому, но при подмене
// адресов они расходятся.
func (im *Imena) razobrat(telo string) {
	var otvet struct {
		Connections []struct {
			Metadata struct {
				Host              string `json:"host"`
				DestinationIP     string `json:"destinationIP"`
				RemoteDestination string `json:"remoteDestination"`
			} `json:"metadata"`
		} `json:"connections"`
	}
	if err := json.Unmarshal([]byte(telo), &otvet); err != nil {
		return
	}
	for _, s := range otvet.Connections {
		imya := strings.TrimSpace(s.Metadata.Host)
		if imya == "" || imya == "null" {
			continue
		}
		for _, adres := range []string{s.Metadata.DestinationIP, s.Metadata.RemoteDestination} {
			adres = strings.TrimSpace(adres)
			if adres == "" || adres == "null" {
				continue
			}
			im.polozhit(adres, imya)
		}
	}
}

// polozhit — одно соответствие, со сменой поколения при переполнении.
//
// Проверка потолка именно здесь, а не после разбора всего ответа: у ядра может
// быть разом больше живых соединений, чем весь наш потолок, и тогда проверка «в
// конце» пропускала бы внутрь карту любого размера. Зовётся под уже взятым
// замком.
func (im *Imena) polozhit(adres, imya string) {
	if im.Potolok > 0 && len(im.svezhie) >= im.Potolok {
		im.proshlye = im.svezhie
		im.svezhie = make(map[string]string, im.Potolok)
	}
	im.svezhie[adres] = imya
}

// skolkoPomnim — для проверок: сколько имён сейчас помним в обоих поколениях.
func (im *Imena) skolkoPomnim() int {
	im.zamok.Lock()
	defer im.zamok.Unlock()
	return len(im.svezhie) + len(im.proshlye)
}

// soedineniyaSyroe — тело ответа служебного порта про живые соединения.
//
// Отдельно от Trafik, который ходит туда же: тому нужны два счётчика из корня
// ответа, а тут — весь список. Разбирать один ответ на два разных смысла в
// одном месте значило бы связать счётчик трафика с разбором имён, хотя ломаются
// они порознь.
func (y *Yadro) soedineniyaSyroe() (string, error) {
	otvet, err := y.zapros("/connections")
	if err != nil {
		return "", err
	}
	defer otvet.Body.Close()
	// Потолок на ответ: список живых соединений обычно невелик, но связывать
	// размер своей памяти с чужим ответом нельзя.
	const potolok = 4 << 20
	b, err := io.ReadAll(io.LimitReader(otvet.Body, potolok))
	if err != nil {
		return "", err
	}
	return string(b), nil
}
