package yadro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// Ядро лежит в одном репозитории с приложением, и релизов приложения там уже за
// тридцать. GitHub отдаёт список по дате коммита тега, поэтому единственная
// сборка ядра уезжает в самый хвост — окно запроса обязано её доставать.
//
// Стенд намеренно берёт РАЗМЕР ОКНА из боевой константы Relizy, а не из своей
// цифры: иначе он зеленел бы при любом per_page и мерил не то, что чинится.
// Живой замер 25.08: при per_page=20 приложение отвечало «в сборках ядра нет
// файла sing-box-windows-amd64.zip», хотя сборка была на месте.
func TestYadroNahoditsyaVHvosteSpiska(t *testing.T) {
	const vsegoRelizov = 32 // столько их в репозитории на 25.08
	const mestoYadra = 31   // core-релиз последний: приложение выпускается чаще

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		okno, err := strconv.Atoi(r.URL.Query().Get("per_page"))
		if err != nil || okno <= 0 {
			okno = 30 // столько GitHub отдаёт без per_page
		}
		type asset struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}
		type reliz struct {
			Tag    string  `json:"tag_name"`
			Assets []asset `json:"assets"`
		}
		var otvet []reliz
		for i := 0; i < vsegoRelizov && i < okno; i++ {
			if i == mestoYadra {
				otvet = append(otvet, reliz{
					Tag:    "core-v1.14.0-beta.4-1",
					Assets: []asset{{Name: ImyaArhiva(), URL: "https://example.invalid/" + ImyaArhiva()}},
				})
				continue
			}
			otvet = append(otvet, reliz{
				Tag:    fmt.Sprintf("app-v0.6.%d", vsegoRelizov-i),
				Assets: []asset{{Name: "Kelevra.exe", URL: "https://example.invalid/Kelevra.exe"}},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(otvet)
	}))
	defer srv.Close()

	boevoy, err := url.Parse(Relizy)
	if err != nil {
		t.Fatalf("боевой адрес списка не разобрать: %v", err)
	}
	y := &Yadro{Spisok: srv.URL + "?" + boevoy.RawQuery}

	ssylka, err := y.ssylkaNaYadro(context.Background())
	if err != nil {
		t.Fatalf("ядро на %d-м месте из %d не найдено при окне %q: %v\n"+
			"именно так приложение остаётся без ядра у человека",
			mestoYadra, vsegoRelizov, boevoy.RawQuery, err)
	}
	if hochu := "https://example.invalid/" + ImyaArhiva(); ssylka != hochu {
		t.Fatalf("ссылка на ядро %q, ждал %q", ssylka, hochu)
	}
}
