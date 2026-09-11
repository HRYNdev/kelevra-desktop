package yadro

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Ответ служебного порта ядра в том виде, в каком он приходит на самом деле:
// список соединений, имя сайта лежит в metadata.host.
const otvetYadra = `{
  "downloadTotal": 123,
  "uploadTotal": 45,
  "connections": [
    {"metadata": {"host": "mc.yandex.ru", "destinationIP": "2a02:6b8:a::a", "remoteDestination": "77.88.55.88"}},
    {"metadata": {"host": "www.gstatic.com", "destinationIP": "142.250.150.94", "remoteDestination": ""}},
    {"metadata": {"host": "", "destinationIP": "10.0.0.1", "remoteDestination": "10.0.0.1"}},
    {"metadata": {"host": "null", "destinationIP": "10.0.0.2", "remoteDestination": "null"}}
  ]
}`

func novyeNaOtvete(telo string) *Imena {
	return &Imena{
		svezhie:    map[string]string{},
		proshlye:   map[string]string{},
		Potolok:    256,
		NeChashche: 5 * time.Second,
		Chitat:     func() (string, error) { return telo, nil },
	}
}

// Главное, ради чего всё затевалось: по адресу из строки отказа получить имя.
func TestImyaBerjotsyaUYadraPoAdresu(t *testing.T) {
	im := novyeNaOtvete(otvetYadra)

	if got := im.Imya("2a02:6b8:a::a"); got != "mc.yandex.ru" {
		t.Errorf("по адресу назначения: got %q, ждали mc.yandex.ru", got)
	}
	// Тот же сайт, но по исходному адресу: при подмене адресов они расходятся,
	// а отказ может быть напечатан по любому из них.
	if got := im.Imya("77.88.55.88"); got != "mc.yandex.ru" {
		t.Errorf("по исходному адресу: got %q, ждали mc.yandex.ru", got)
	}
	if got := im.Imya("142.250.150.94"); got != "www.gstatic.com" {
		t.Errorf("второй сайт: got %q, ждали www.gstatic.com", got)
	}
}

// Пустое и «null» именами не считаются: соврать именем хуже, чем отдать адрес.
func TestPustoeImyaNeZapominaetsya(t *testing.T) {
	im := novyeNaOtvete(otvetYadra)
	im.Imya("2a02:6b8:a::a") // прогрев

	for _, adres := range []string{"10.0.0.1", "10.0.0.2"} {
		if got := im.Imya(adres); got != "" {
			t.Errorf("%s: got %q, ждали пусто", adres, got)
		}
	}
}

// Неизвестный адрес — пусто, а не догадка.
func TestNeizvestnyyAdresDayotPusto(t *testing.T) {
	im := novyeNaOtvete(otvetYadra)
	if got := im.Imya("1.2.3.4"); got != "" {
		t.Errorf("got %q, ждали пусто", got)
	}
	if got := im.Imya(""); got != "" {
		t.Errorf("пустой адрес: got %q, ждали пусто", got)
	}
}

// Ядро не тревожим чаще положенного: триста отказов за окно не должны
// превратиться в триста запросов.
func TestYadroNeSprashivaemChashchePolozhennogo(t *testing.T) {
	sprosov := 0
	im := novyeNaOtvete(otvetYadra)
	im.Chitat = func() (string, error) {
		sprosov++
		return otvetYadra, nil
	}

	// Триста промахов подряд по неизвестным адресам.
	for i := 0; i < 300; i++ {
		im.Imya(fmt.Sprintf("9.9.9.%d", i%250))
	}
	if sprosov != 1 {
		t.Errorf("сходили к ядру %d раз, ждали 1", sprosov)
	}

	// Прошло больше положенного — можно снова.
	im.zamok.Lock()
	im.sprashivali = time.Now().Add(-2 * im.NeChashche)
	im.zamok.Unlock()
	im.Imya("8.8.8.8")
	if sprosov != 2 {
		t.Errorf("после паузы сходили %d раз, ждали 2", sprosov)
	}
}

// Отказ ядра не роняет и не засоряет: молча отдаём пусто.
func TestOtkazYadraNeLomaet(t *testing.T) {
	im := novyeNaOtvete("")
	im.Chitat = func() (string, error) { return "", errors.New("ядро молчит") }
	if got := im.Imya("1.2.3.4"); got != "" {
		t.Errorf("got %q, ждали пусто", got)
	}

	// Мусор вместо JSON — тоже не беда.
	im2 := novyeNaOtvete("не json вовсе")
	if got := im2.Imya("1.2.3.4"); got != "" {
		t.Errorf("на мусоре got %q, ждали пусто", got)
	}
}

// Память не растёт без края: при переполнении поколение сменяется.
func TestPamyatNeRastyotBezKraya(t *testing.T) {
	var svyazi []string
	for i := 0; i < 50; i++ {
		svyazi = append(svyazi, fmt.Sprintf(
			`{"metadata": {"host": "sayt%d.ru", "destinationIP": "10.1.%d.%d"}}`, i, i/250, i%250))
	}
	telo := `{"connections": [` + strings.Join(svyazi, ",") + `]}`

	im := novyeNaOtvete(telo)
	im.Potolok = 10
	im.Imya("10.1.0.0")

	if pomnim := im.skolkoPomnim(); pomnim > 2*im.Potolok+10 {
		t.Errorf("помним %d имён при потолке %d — поколения не сменяются", pomnim, im.Potolok)
	}
}

// Имя, которое всё ещё спрашивают, переживает смену поколений.
func TestNuzhnoeImyaPerezhivaetSmenuPokoleniy(t *testing.T) {
	im := novyeNaOtvete(otvetYadra)
	im.Potolok = 2

	// Первый заход кладёт имена и тут же переполняет свежее поколение.
	if got := im.Imya("2a02:6b8:a::a"); got != "mc.yandex.ru" {
		t.Fatalf("до смены поколений: got %q", got)
	}
	// Имя ушло в прошлое поколение, но спросить его всё ещё можно.
	if got := im.Imya("2a02:6b8:a::a"); got != "mc.yandex.ru" {
		t.Errorf("после смены поколений: got %q, ждали mc.yandex.ru", got)
	}
}

// Справочника может не быть вовсе (ядро не поднято) — это не повод падать.
func TestPustoySpravochnikNePadaet(t *testing.T) {
	var im *Imena
	if got := im.Imya("1.2.3.4"); got != "" {
		t.Errorf("got %q, ждали пусто", got)
	}
}
