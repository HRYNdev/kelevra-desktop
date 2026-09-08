package pravila

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Встроенный комплект обязан покрывать ВСЕ наборы боевого профиля — поимённо,
// а не по счёту.
//
// Как проверка по числу подвела 08.09. Она сторожила «22 тега по числу
// route.rule_set боевого профиля», профиль вырос до 23 (добавился
// blocked-domains), и число разошлось молча. Цена: на машине человека не
// скачались удалённые правила, подстраховка на встроенных отказалась
// применяться («во встроенном комплекте нет правила blocked-domains»), и
// связь поднялась СОВСЕМ БЕЗ ПРАВИЛ — весь трафик, включая российские сайты,
// ушёл в туннель. Сайты начали отвечать «выключите VPN».
//
// Поимённый список ловит это сразу и говорит, какого именно набора не хватает.
func TestKomplektPokryvaetBoevoyProfil(t *testing.T) {
	// Теги боевого профиля (route.rule_set в rules.json на сервере).
	// Добавили набор на сервере — добавьте файл сюда и в komplekt/.
	nuzhny := []string{
		"ads", "main-domains", "main-subnets", "russia_inside", "youtube",
		"telegram", "discord", "meta", "twitter", "google_ai", "google_play",
		"cloudflare", "cloudfront", "digitalocean", "hetzner", "ovh", "hodca",
		"roblox", "anime", "hdrezka", "tiktok", "geoblock", "blocked-domains",
	}
	est := map[string]bool{}
	for _, t := range Tegi() {
		est[t] = true
	}
	var net []string
	for _, n := range nuzhny {
		if !est[n] {
			net = append(net, n)
		}
	}
	if len(net) > 0 {
		t.Fatalf("во встроенном комплекте нет наборов %v — подстраховка не применится, "+
			"и связь поднимется вовсе без правил (весь трафик в туннель)", net)
	}
}

func TestData(t *testing.T) {
	d := Data()
	if d == "" {
		t.Fatal("дата снимка пуста — человеку в окне будет нечего показать")
	}
	if _, err := time.Parse("2006-01-02", d); err != nil {
		t.Fatalf("data.txt не похож на дату ГГГГ-ММ-ДД: %q (%v)", d, err)
	}
}

func TestRazlozhitVseTegi(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pravila")
	m, err := Razlozhit(dir)
	if err != nil {
		t.Fatal(err)
	}
	tegi := Tegi()
	if len(m) != len(tegi) {
		t.Fatalf("разложено %d файлов, тегов %d", len(m), len(tegi))
	}
	for _, teg := range tegi {
		put, est := m[teg]
		if !est {
			t.Fatalf("тег %q не разложен", teg)
		}
		svedeniya, err := os.Stat(put)
		if err != nil {
			t.Fatalf("файл %s не создан: %v", put, err)
		}
		if svedeniya.Size() == 0 {
			t.Fatalf("файл %s пуст", put)
		}
		if !filepath.IsAbs(put) {
			t.Fatalf("путь %q не абсолютный", put)
		}
	}
}

// Идемпотентность: второй вызов не должен переписывать уже лежащие файлы —
// иначе Razlozhit на каждом перезапуске ядра будет насиловать диск.
func TestRazlozhitIdempotentno(t *testing.T) {
	dir := t.TempDir()
	if _, err := Razlozhit(dir); err != nil {
		t.Fatal(err)
	}
	teg := Tegi()[0]
	put := filepath.Join(dir, teg+".srs")
	do := time.Now().Add(-time.Hour)
	if err := os.Chtimes(put, do, do); err != nil {
		t.Fatal(err)
	}
	if _, err := Razlozhit(dir); err != nil {
		t.Fatal(err)
	}
	svedeniya, err := os.Stat(put)
	if err != nil {
		t.Fatal(err)
	}
	if !svedeniya.ModTime().Equal(do) {
		t.Fatalf("mtime изменился — файл переписан, хотя содержимое не менялось: было %v, стало %v", do, svedeniya.ModTime())
	}
}

// Если файл на диске испорчен (не совпадает со встроенным), Razlozhit обязан
// его перезаписать — иначе комплект молча остаётся битым.
func TestRazlozhitLechitPorchu(t *testing.T) {
	dir := t.TempDir()
	if _, err := Razlozhit(dir); err != nil {
		t.Fatal(err)
	}
	teg := Tegi()[0]
	put := filepath.Join(dir, teg+".srs")
	if err := os.WriteFile(put, []byte("испорчено"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Razlozhit(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(m[teg])
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "испорчено" {
		t.Fatal("порча не вылечена — Razlozhit не переписал файл, отличающийся от встроенного")
	}
}
