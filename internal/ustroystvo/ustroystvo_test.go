package ustroystvo

import (
	"net/http"
	"strings"
	"testing"
)

// Заголовок обязан доехать до сервера целым. Кириллица и перевод строки в
// значении — два способа этого не добиться: первая приезжает кракозябрами,
// второй разрывает запрос надвое.
func TestZagolovokVsegdaOdnostrochnyyASCII(t *testing.T) {
	sluchai := []struct{ dano, zhdem string }{
		{"ASUSTeK TUF GAMING B550-PLUS", "ASUSTeK TUF GAMING B550-PLUS"},
		{"Домашний компьютер", ""},
		{"ASUS\r\nX-Injected: 1", "ASUS X-Injected: 1"},
		{"  ASUS   TUF  B550  ", "ASUS TUF B550"},
		{"Windows · ПК", "Windows"},
		{"", ""},
	}
	for _, s := range sluchai {
		if got := vASCII(s.dano); got != s.zhdem {
			t.Errorf("vASCII(%q) = %q, ждали %q", s.dano, got, s.zhdem)
		}
	}
	dlinnoe := vASCII(strings.Repeat("A", PredelZagolovka*3))
	if len(dlinnoe) > PredelZagolovka {
		t.Errorf("длина %d, потолок %d", len(dlinnoe), PredelZagolovka)
	}
}

// Заглушки производителя («System Product Name») — не имя модели, а признак
// того, что спрашивать надо материнскую плату.
func TestZaglushkiSchitayutsyaMusorom(t *testing.T) {
	for _, s := range []string{"", "  ", "System manufacturer", "SYSTEM PRODUCT NAME",
		"To Be Filled By O.E.M.", "Default string", "None", "n/a"} {
		if !musor(s) {
			t.Errorf("musor(%q) = false, ждали true", s)
		}
	}
	for _, s := range []string{"ASUSTeK COMPUTER INC.", "TUF GAMING B550-PLUS", "LENOVO", "20QDS00L00"} {
		if musor(s) {
			t.Errorf("musor(%q) = true, ждали false", s)
		}
	}
}

func TestImyaModeliCheloveskoe(t *testing.T) {
	sluchai := []struct{ proizvoditel, izdelie, zhdem string }{
		{"ASUSTeK COMPUTER INC.", "TUF GAMING B550-PLUS", "ASUSTeK TUF GAMING B550-PLUS"},
		{"Micro-Star International Co., Ltd.", "MS-7C56", "Micro-Star International MS-7C56"},
		{"LENOVO", "LENOVO IdeaPad 5", "LENOVO IdeaPad 5"}, // производитель не повторяется дважды
		{"Dell Inc.", "XPS 15 9520", "Dell XPS 15 9520"},
		{"", "MS-7C56", "MS-7C56"},
		{"ASUSTeK COMPUTER INC.", "", "ASUSTeK"},
	}
	for _, s := range sluchai {
		got := sobrat(korotkiyProizvoditel(s.proizvoditel), s.izdelie)
		if got != s.zhdem {
			t.Errorf("sobrat(%q, %q) = %q, ждали %q", s.proizvoditel, s.izdelie, got, s.zhdem)
		}
	}
}

// Четыре заголовка устройства — ровно те, что ждёт сервер, и ни один из них
// не пустой (пустая модель на сервере неотличима от старого клиента).
func TestZagolovkiStavyatsyaVse(t *testing.T) {
	h := http.Header{}
	Zagolovki(h, "0123456789abcdef", "0.6.30")
	if h.Get("X-Device-Id") != "0123456789abcdef" {
		t.Errorf("X-Device-Id = %q", h.Get("X-Device-Id"))
	}
	if h.Get("X-App-Version") != "0.6.30" {
		t.Errorf("X-App-Version = %q", h.Get("X-App-Version"))
	}
	// Печатаем то, что реально определилось на этой машине: `go test -v` —
	// самый дешёвый способ увидеть, чем копия представляется серверу, не
	// запуская приложение.
	t.Logf("эта машина представляется как: модель %q, платформа %q",
		h.Get("X-Device-Model"), h.Get("X-Device-Platform"))
	if h.Get("X-Device-Model") == "" {
		t.Error("X-Device-Model пуст — сервер не отличит нас от старого клиента")
	}
	if h.Get("X-Device-Platform") == "" {
		t.Error("X-Device-Platform пуст")
	}
	// Без идентификатора заголовка просто нет — это не ошибка.
	bez := http.Header{}
	Zagolovki(bez, "", "0.6.30")
	if _, est := bez["X-Device-Id"]; est {
		t.Error("пустой DeviceID не должен рождать заголовок")
	}
}
