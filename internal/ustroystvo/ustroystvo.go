// Пакет ustroystvo: чем эта машина представляется серверу Kelevra.
//
// Сервер ведёт список устройств одного человека и показывает их ему списком —
// значит клиент обязан сказать о себе три вещи: КТО он (постоянный
// идентификатор), ЧТО он (модель компьютера) и НА ЧЁМ он (система с версией).
// Ровно это уже делает android-клиент; здесь тот же набор заголовков, чтобы
// одна и та же сторона сервера понимала оба клиента без ветвлений.
//
// Заголовки ставит Zagolovki — единственная дверь: и подписка (internal/podpiska),
// и суточная отправка журналов (internal/zhurnaly) зовут её, а не собирают
// заголовки каждая по-своему. Разъехаться им негде.
package ustroystvo

import (
	"net/http"
	"os"
	"strings"
	"sync"
)

// ZapasnayaModel — что шлём, когда модель определить не вышло (виртуалка,
// урезанный реестр, права). Пустую строку слать нельзя: на сервере она
// неотличима от «клиент старый и про модель не знает», и устройство в списке
// становится безымянным.
//
// Почему по-английски, хотя заказ был «Windows · ПК»: значение уходит в HTTP-
// заголовок, а заголовок обязан быть однострочным ASCII (см. vASCII ниже) —
// кириллица доезжает до сервера сырыми UTF-8 байтами и разбирается им как
// latin-1, то есть кракозябрами. «Windows PC» — то же самое по смыслу и
// доезжает целым.
const ZapasnayaModel = "Windows PC"

// ZapasnayaPlatforma — то же самое для системы: версию могли не отдать, но
// сама система известна всегда.
const ZapasnayaPlatforma = "Windows"

// PredelZagolovka — потолок длины значения. Модель из реестра приходит от
// производителя платы и в теории может быть любой длины; заголовок в
// килобайт — верный способ получить 431 вместо ответа.
const PredelZagolovka = 128

var (
	odinRaz   sync.Once
	model     string
	platforma string
)

// opredelit спрашивает систему ровно один раз за жизнь процесса: реестр за
// время работы не меняется, а ходить в него на каждом запросе к подписке
// незачем.
func opredelit() {
	odinRaz.Do(func() {
		// KELEVRA_MODEL_USTROYSTVA — только для стендов, у человека не задана
		// (тот же приём, что и у KELEVRA_PODPISKA в internal/sluzhba).
		model = vASCII(os.Getenv("KELEVRA_MODEL_USTROYSTVA"))
		if model == "" {
			model = vASCII(modelSistemy())
		}
		if model == "" {
			model = ZapasnayaModel
		}
		platforma = vASCII(platformaSistemy())
		if platforma == "" {
			platforma = ZapasnayaPlatforma
		}
	})
}

// Model — производитель и модель компьютера, например «ASUSTeK TUF GAMING B550-PLUS».
func Model() string { opredelit(); return model }

// Platforma — система с версией, например «Windows 10 Pro 22H2 (19045)».
func Platforma() string { opredelit(); return platforma }

// Zagolovki ставит четыре заголовка устройства на готовый запрос.
//
// deviceID — ГОТОВЫЙ постоянный идентификатор (hranenie.Nastroyki.DeviceID),
// свой новый тут не заводится: идентификатор рождается один раз при первом
// старте и живёт на диске, иначе сервер видел бы новое устройство после
// каждого запуска. Пустой deviceID — не ошибка (заголовок просто не ставится).
func Zagolovki(h http.Header, deviceID, versiyaPrilozheniya string) {
	if id := vASCII(deviceID); id != "" {
		h.Set("X-Device-Id", id)
	}
	h.Set("X-Device-Model", Model())
	h.Set("X-Device-Platform", Platforma())
	if v := vASCII(versiyaPrilozheniya); v != "" {
		h.Set("X-App-Version", v)
	}
}

// vASCII приводит значение к однострочному печатному ASCII.
//
// Зачем: Go пропускает в значение заголовка любые байты кроме управляющих, и
// кириллица из реестра («Домашний компьютер» в имени модели у сборщика) уехала
// бы на сервер сырым UTF-8 — там это кракозябры, а перевод строки в значении
// и вовсе разрывает запрос надвое. Всё, что не печатный ASCII, становится
// пробелом, пробелы схлопываются, длина режется по PredelZagolovka.
func vASCII(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	probel := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7e {
			probel = true
			continue
		}
		if c == ' ' {
			probel = true
			continue
		}
		if probel && b.Len() > 0 {
			b.WriteByte(' ')
		}
		probel = false
		b.WriteByte(c)
	}
	itog := b.String()
	if len(itog) > PredelZagolovka {
		itog = strings.TrimSpace(itog[:PredelZagolovka])
	}
	return itog
}

// musor — правда ли, что производитель прошил в SMBIOS заглушку вместо
// названия. На настольных сборках это норма: поля «системы» там пустые или
// заполнены строкой из примера, а настоящее имя знает только материнская
// плата (см. modelSistemy).
func musor(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return true
	}
	zaglushki := []string{
		"system manufacturer", "system product name", "system version",
		"to be filled by o.e.m.", "to be filled by oem", "default string",
		"not applicable", "not specified", "no enclosure", "invalid",
		"none", "n/a", "na", "oem", "o.e.m.", "unknown", "x.x", "-", "*",
	}
	for _, z := range zaglushki {
		if s == z {
			return true
		}
	}
	return false
}

// korotkiyProizvoditel убирает юридический хвост из названия конторы:
// «ASUSTeK COMPUTER INC.» — это ASUSTeK, а «Micro-Star International Co., Ltd.»
// — Micro-Star International. Хвост занимает половину заголовка и не говорит
// человеку ничего.
func korotkiyProizvoditel(s string) string {
	s = strings.TrimSpace(s)
	hvosty := []string{
		" computer inc", " computer, inc", " computer",
		" technology co", " technologies co",
		" co., ltd", " co.,ltd", " co. ltd", " co ltd",
		" corporation", " corp.", " corp",
		", inc", " inc.", " inc",
		" gmbh", " s.p.a", " s.a.", " b.v.", " ag",
	}
	nizhniy := strings.ToLower(s)
	rez := len(s)
	for _, h := range hvosty {
		if i := strings.Index(nizhniy, h); i > 0 && i < rez {
			rez = i
		}
	}
	korotko := strings.TrimRight(strings.TrimSpace(s[:rez]), " ,.-")
	if korotko == "" {
		return s
	}
	return korotko
}

// sobrat склеивает производителя и изделие в одну строку, не повторяя
// производителя дважды: у Lenovo изделие уже называется «Lenovo IdeaPad…».
func sobrat(proizvoditel, izdelie string) string {
	proizvoditel = strings.TrimSpace(proizvoditel)
	izdelie = strings.TrimSpace(izdelie)
	switch {
	case proizvoditel == "":
		return izdelie
	case izdelie == "":
		return proizvoditel
	case strings.HasPrefix(strings.ToLower(izdelie), strings.ToLower(proizvoditel)):
		return izdelie
	default:
		return proizvoditel + " " + izdelie
	}
}
