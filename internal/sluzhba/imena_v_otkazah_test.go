package sluzhba

import (
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
	"github.com/HRYNdev/kelevra-desktop/internal/zapisi"
)

// Строка отказа ровно в том виде, в каком её печатает ядро: адрес есть, имени
// нет. Взята из журнала 09.09.2026.
const strokaOtkazaSAdresom = `+0500 2026-09-09 20:09:15 ERROR [3572860262 103ms] connection: ` +
	`open connection to [2a02:6b8:a::a]:443 using outbound/direct[direct]: ` +
	`dial wlan0: connect: network is unreachable`

// Ответ служебного порта, где этот адрес назван по имени.
const otvetPortaSImenem = `{"connections": [
  {"metadata": {"host": "mc.yandex.ru", "destinationIP": "2a02:6b8:a::a", "remoteDestination": "77.88.55.88"}}
]}`

func nablyudatelDlyaProverki(t *testing.T, spravochnik *yadro.Imena) *nablyudatelYadra {
	t.Helper()
	return &nablyudatelYadra{
		pisatel:     zapisi.Novyy(t.TempDir()),
		kody:        map[string]int{},
		imena:       map[string]int{},
		spravochnik: spravochnik,
	}
}

// Главное: отказ, напечатанный по адресу, попадает в записи с именем сайта.
//
// До 11.09.2026 этого не было: в журналах телефонов отказы читались как
// «mc.yandex.ru», а в журналах компьютеров — как голые адреса, и поставить
// диагноз по компьютеру было нечем.
func TestOtkazPoAdresuPoluchaetImyaOtYadra(t *testing.T) {
	n := nablyudatelDlyaProverki(t, yadro.NovyeImenaNaChtenii(
		func() (string, error) { return otvetPortaSImenem, nil }))

	n.uchest(strokaOtkazaSAdresom)

	if n.otkazy != 1 {
		t.Fatalf("отказов насчитано %d, ждали 1", n.otkazy)
	}
	if n.imena["mc.yandex.ru"] != 1 {
		t.Errorf("имя сайта не попало в счётчик: %v", n.imena)
	}
}

// Ядро молчит или имени не знает — пишем по адресу, как раньше, и не врём.
func TestBezOtvetaYadraOstayotsyaAdres(t *testing.T) {
	n := nablyudatelDlyaProverki(t, yadro.NovyeImenaNaChtenii(
		func() (string, error) { return `{"connections": []}`, nil }))

	n.uchest(strokaOtkazaSAdresom)

	if n.otkazy != 1 {
		t.Fatalf("отказов насчитано %d, ждали 1", n.otkazy)
	}
	if len(n.imena) != 0 {
		t.Errorf("имя выдумано на пустом ответе: %v", n.imena)
	}
}

// Справочника нет вовсе (ядро не поднято) — разбор продолжает работать.
func TestBezSpravochnikaRazborRabotaet(t *testing.T) {
	n := nablyudatelDlyaProverki(t, nil)

	n.uchest(strokaOtkazaSAdresom)

	if n.otkazy != 1 {
		t.Errorf("отказов насчитано %d, ждали 1", n.otkazy)
	}
	if len(n.imena) != 0 {
		t.Errorf("без справочника имя взялось из ниоткуда: %v", n.imena)
	}
}

// Если ядро назвало имя прямо в строке, к служебному порту не ходим вовсе.
func TestImyaVStrokeNeTrebuetZaprosaKYadru(t *testing.T) {
	sprosov := 0
	n := nablyudatelDlyaProverki(t, yadro.NovyeImenaNaChtenii(func() (string, error) {
		sprosov++
		return otvetPortaSImenem, nil
	}))

	// Та же строка, но ядро напечатало имя, а не адрес.
	stroka := `+0500 2026-09-09 20:09:15 ERROR [3572860262 103ms] connection: ` +
		`open connection to example.org:443 using outbound/direct[direct]: ` +
		`dial wlan0: connect: network is unreachable`
	n.uchest(stroka)

	if sprosov != 0 {
		t.Errorf("сходили к ядру %d раз, хотя имя уже было в строке", sprosov)
	}
	if n.imena["example.org"] != 1 {
		t.Errorf("имя из строки потеряно: %v", n.imena)
	}
}
