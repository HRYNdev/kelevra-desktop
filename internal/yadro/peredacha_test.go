package yadro

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// yadroNaPodstavnomApi — Yadro, чей служебный порт отвечает как живое ядро.
// Настоящего процесса тут нет и не нужно: опознание спрашивает порт, а не
// операционную систему.
func yadroNaPodstavnomApi(t *testing.T, papka string, otvechaet bool) *Yadro {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		if !otvechaet {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Yadro{
		Bin:   filepath.Join(papka, "sing-box"),
		Papka: papka,
		Api:   strings.TrimPrefix(srv.URL, "http://"),
	}
}

// podstavitSvoyObrazVBin подменяет y.Bin на настоящий путь образа ТЕКУЩЕГО
// тестового процесса, каким его называет ОС (putObrazaProcessa). Нужна там,
// где тест выдаёт os.Getpid() за PID «своего» живого ядра: opoзнание с
// 12.09.2026 сверяет не только строку конфига, но и реальный путь образа
// процесса, а фиктивный путь из yadroNaPodstavnomApi ему по определению не
// принадлежит. Если ОС путь не назвала — тест недоказателен, а не обречён,
// поэтому пропускаем его явно, а не подделываем совпадение.
func podstavitSvoyObrazVBin(t *testing.T, y *Yadro) {
	t.Helper()
	obraz, znaem := putObrazaProcessa(os.Getpid())
	if !znaem {
		t.Skip("ОС не назвала путь образа собственного процесса на этой платформе/сборке")
	}
	y.Bin = obraz
}

// polozhitKonfig кладёт конфиг ядра и возвращает его отпечаток.
func polozhitKonfig(t *testing.T, papka, soderzhimoe string) string {
	t.Helper()
	if err := os.MkdirAll(papka, 0o755); err != nil {
		t.Fatal(err)
	}
	put := filepath.Join(papka, "config.json")
	if err := os.WriteFile(put, []byte(soderzhimoe), 0o600); err != nil {
		t.Fatal(err)
	}
	return OtpechatokKonfiga(put)
}

// Записка переживает запись и чтение целиком: по ней преемница узнаёт ядро.
func TestZapiskaPishetsyaIChitaetsya(t *testing.T) {
	papka := t.TempDir()
	hochu := Peredacha{
		PID: 4242, Bin: `C:\Kelevra\sing-box.exe`, Api: "127.0.0.1:9090",
		Otpechatok: "abc123", Adapter: "tun125", Kogda: time.Now(),
	}
	ZapisatPeredachu(papka, hochu)

	est, ok := ProchestPeredachu(papka)
	if !ok {
		t.Fatal("записку не прочитали")
	}
	if est.PID != hochu.PID || est.Bin != hochu.Bin || est.Adapter != hochu.Adapter ||
		est.Otpechatok != hochu.Otpechatok {
		t.Errorf("записка приехала другой: %+v", est)
	}

	UbratPeredachu(papka)
	if _, ok := ProchestPeredachu(papka); ok {
		t.Error("снятая записка всё ещё читается")
	}
}

// Записки нет — читать нечего, и это не беда: обычный холодный старт.
func TestZapiskiNetNeBeda(t *testing.T) {
	if _, ok := ProchestPeredachu(t.TempDir()); ok {
		t.Error("прочитали записку там, где её нет")
	}
}

// Отпечаток берётся с СОДЕРЖИМОГО, а не со времени правки. Конфиг
// пересобирается на каждом подключении, и по времени любое обновление
// выглядело бы сменой конфига — то есть рвало бы туннель зря.
func TestOtpechatokPoSoderzhimomuANePoVremeni(t *testing.T) {
	papka := t.TempDir()
	pervyy := polozhitKonfig(t, papka, `{"log":{"level":"info"}}`)

	// Тот же текст, записанный заново: время правки другое, содержимое то же.
	time.Sleep(10 * time.Millisecond)
	vtoroy := polozhitKonfig(t, papka, `{"log":{"level":"info"}}`)
	if pervyy != vtoroy {
		t.Errorf("отпечаток изменился без изменения содержимого: %s → %s", pervyy, vtoroy)
	}

	tretiy := polozhitKonfig(t, papka, `{"log":{"level":"debug"}}`)
	if tretiy == pervyy {
		t.Error("отпечаток не изменился при изменившемся конфиге")
	}
}

// Главный путь: живое ядро прошлой копии принимается, и связь не прерывается.
//
// ОСТОРОЖНО с номером процесса в этих проверках: берётся СВОЙ (os.Getpid),
// потому что опознанию нужен заведомо живой процесс, а поднимать настоящее
// ядро в проверке нельзя. Отсюда запрет: ни Ostanovit, ни PogasitChuzhoe
// здесь звать нельзя — они погасят сам тест.
//
// С 12.09.2026 opoзнание сверяет ещё и РЕАЛЬНЫЙ путь образа процесса (см.
// putObrazaProcessa в yadro.go), поэтому y.Bin здесь подменяется на
// настоящий путь тестового процесса — иначе фиктивный путь из
// yadroNaPodstavnomApi не совпал бы с собой же.
func TestZhivoeYadroPrinimaetsya(t *testing.T) {
	papka := t.TempDir()
	y := yadroNaPodstavnomApi(t, papka, true)
	podstavitSvoyObrazVBin(t, y)
	otpechatok := polozhitKonfig(t, papka, `{"log":{"level":"info"}}`)

	itog := y.Prinyat(Peredacha{
		PID: os.Getpid(), Bin: y.Bin, Api: y.Api,
		Otpechatok: otpechatok, Adapter: "tun125",
	}, otpechatok)

	if !itog.Prinyato {
		t.Fatalf("живое ядро не принято: %s", itog.Pochemu)
	}
	if y.Sost() != Rabotaet {
		t.Errorf("состояние после приёма: %v, ждали Rabotaet", y.Sost())
	}
	if y.PID() != "" && y.PID() != itoa(os.Getpid()) {
		t.Errorf("номер принятого процесса потерян: %q", y.PID())
	}
}

// Чужой бинарь не принимаем. Номера процессов система переиспользует, и без
// этой проверки под нашим номером могло бы оказаться что угодно.
func TestChuzhoyBinarNePrinimaetsya(t *testing.T) {
	papka := t.TempDir()
	y := yadroNaPodstavnomApi(t, papka, true)
	polozhitKonfig(t, papka, `{"log":{"level":"info"}}`)

	itog := y.Prinyat(Peredacha{
		PID: os.Getpid(), Bin: filepath.Join(papka, "chuzhoy-sing-box"), Api: y.Api,
	}, "")

	if itog.Prinyato {
		t.Error("приняли чужой бинарь")
	}
	if itog.Pochemu == "" {
		t.Error("отказ без причины — в журнале человека это будет тишина")
	}
}

// Молчащий служебный порт означает, что ядра нет, даже если процесс существует:
// трафик через такое ядро всё равно не идёт.
func TestMolchashcheeYadroNePrinimaetsya(t *testing.T) {
	papka := t.TempDir()
	y := yadroNaPodstavnomApi(t, papka, false) // порт отвечает отказом
	polozhitKonfig(t, papka, `{"log":{"level":"info"}}`)

	itog := y.Prinyat(Peredacha{PID: os.Getpid(), Bin: y.Bin, Api: y.Api}, "")

	if itog.Prinyato {
		t.Error("приняли ядро с молчащим служебным портом")
	}
	if itog.KonfigUstarel {
		t.Error("молчание порта выдано за устаревший конфиг — это разные беды")
	}
}

// Ядро живо и наше, но конфиг сменился: принимать нельзя (человек молча
// остался бы на прежних правилах), а рвать связь незачем. Вызывающий об этом
// узнаёт отдельным полем, а не общим отказом.
func TestUstarevshiyKonfigNazyvaetsyaOtdelno(t *testing.T) {
	papka := t.TempDir()
	y := yadroNaPodstavnomApi(t, papka, true)
	podstavitSvoyObrazVBin(t, y)
	polozhitKonfig(t, papka, `{"log":{"level":"debug"}}`) // конфиг УЖЕ другой

	itog := y.Prinyat(Peredacha{
		PID: os.Getpid(), Bin: y.Bin, Api: y.Api,
		Otpechatok: "otpechatok-proshlogo-konfiga",
	}, "otpechatok-novogo-konfiga")

	if itog.Prinyato {
		t.Error("приняли ядро с устаревшим конфигом — человек остался бы на старых правилах")
	}
	if !itog.KonfigUstarel {
		t.Errorf("устаревший конфиг не назван своим именем: %+v", itog)
	}
}

// Пустая записка (нет номера процесса) — не повод что-либо делать.
func TestPustayaZapiskaNePrinimaetsya(t *testing.T) {
	papka := t.TempDir()
	y := yadroNaPodstavnomApi(t, papka, true)
	if itog := y.Prinyat(Peredacha{}, ""); itog.Prinyato {
		t.Error("приняли пустую записку")
	}
}

// Записка живого ядра узнаётся дёшево и до сборки Yadro: этим стартующая
// копия отличает работающую связь от следа аварии.
func TestZhivoPoZapiskeOtlichaetZhivoeOtMyortvogo(t *testing.T) {
	papka := t.TempDir()
	y := yadroNaPodstavnomApi(t, papka, true)
	ZapisatPeredachu(papka, Peredacha{PID: os.Getpid(), Bin: y.Bin, Api: y.Api})

	if _, zhivo := ZhivoPoZapiske(papka); !zhivo {
		t.Error("живое ядро не узнано по записке")
	}

	// Порт, на котором никого нет.
	ZapisatPeredachu(papka, Peredacha{PID: os.Getpid(), Bin: y.Bin, Api: "127.0.0.1:1"})
	if _, zhivo := ZhivoPoZapiske(papka); zhivo {
		t.Error("мёртвое ядро выдано за живое — стартующая копия сочтёт след аварии связью")
	}
}

// itoa — маленький помощник, чтобы не тащить strconv в тест ради одной строки.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Имя сетевого адаптера на отпечаток не влияет.
//
// Замер на стенде 11.09.2026: ядро работало на «tun126» (имя подобрали, потому
// что «tun125» было занято остатком прошлой попытки), а новая копия собирала
// конфиг с «tun125» из профиля. Побайтно разные, по смыслу — один конфиг.
// Без этой нормализации приём живого ядра каждый раз вырождался в смену, и
// туннель терялся там, где мог не теряться.
func TestImyaAdapteraNeVliyaetNaOtpechatok(t *testing.T) {
	naTun125 := []byte(`{"inbounds":[{"type":"tun","interface_name":"tun125","mtu":1420}],"log":{"level":"info"}}`)
	naTun126 := []byte(`{"inbounds":[{"type":"tun","interface_name":"tun126","mtu":1420}],"log":{"level":"info"}}`)

	if OtpechatokKonfigaBayt(naTun125) != OtpechatokKonfigaBayt(naTun126) {
		t.Error("отпечатки разошлись из-за одного лишь имени адаптера")
	}

	// А настоящая разница обязана отпечаток менять.
	drugoyMtu := []byte(`{"inbounds":[{"type":"tun","interface_name":"tun125","mtu":1280}],"log":{"level":"info"}}`)
	if OtpechatokKonfigaBayt(naTun125) == OtpechatokKonfigaBayt(drugoyMtu) {
		t.Error("отпечаток не заметил изменившийся размер пакета")
	}
}

// Мусор вместо конфига не роняет и не выдаёт пустоту за совпадение.
func TestOtpechatokNaMusore(t *testing.T) {
	if OtpechatokKonfigaBayt(nil) != "" {
		t.Error("у пустого конфига появился отпечаток")
	}
	// Не JSON — считаем побайтно, это честнее, чем притвориться, что совпало.
	a := OtpechatokKonfigaBayt([]byte("не json"))
	b := OtpechatokKonfigaBayt([]byte("тоже не json"))
	if a == "" || a == b {
		t.Error("разный мусор дал одинаковый или пустой отпечаток")
	}
}

// Имя адаптера читается из того же конфига, с которым стартует ядро.
func TestImyaAdapteraChitaetsyaIzKonfiga(t *testing.T) {
	papka := t.TempDir()
	put := filepath.Join(papka, "config.json")
	if err := os.WriteFile(put, []byte(
		`{"inbounds":[{"type":"mixed"},{"type":"tun","interface_name":"tun126"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ImyaAdapteraIzKonfiga(put); got != "tun126" {
		t.Errorf("got %q, ждали tun126", got)
	}

	// Прокси-режим: туннеля нет вовсе, имени взяться неоткуда.
	bezTunnelya := filepath.Join(papka, "bez.json")
	if err := os.WriteFile(bezTunnelya, []byte(`{"inbounds":[{"type":"mixed"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ImyaAdapteraIzKonfiga(bezTunnelya); got != "" {
		t.Errorf("в прокси-режиме нашлось имя адаптера: %q", got)
	}
}
