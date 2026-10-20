package sluzhba

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/tunnel"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
	"github.com/HRYNdev/kelevra-desktop/internal/zapasnoy"
	"github.com/HRYNdev/kelevra-desktop/internal/zhurnaly"
)

// Туннель не поднялся — и прокси вместо него нет.
//
// До 0.6.77 здесь была «последняя ступень лестницы»: туннель не встал —
// приложение поднимало системный прокси, и через Kelevra шли только браузеры
// («Частично»). Решение владельца 17.10: «нет никаких неполных режимов и не
// рабочих». Теперь туннель не встал — связь не поднята, человеку названа
// причина официальным тоном, за неудачной попыткой ничего не осталось.
//
// Проверки написаны на СЦЕНАРИЙ: что человек получил и что осталось на диске.

// oshibkaTunnelya — то же, что печатает настоящее ядро, когда сетевой адаптер
// создать не вышло (эту строку показывает и стенд облика, сцена 5_slomalos).
const oshibkaTunnelya = `FATAL[0000] start service: initialize inbound/tun[0]: configure tun interface: permission denied`

// stendSPravami — стенд лестницы, которому подменены права администратора.
// Без подмены туннель на проверяющей машине не воспроизвести вовсе:
// prava.Est() отвечает про настоящий процесс, а гонять проверки от
// администратора значит поднимать настоящий сетевой адаптер (см. поле
// Sluzhba.pravaDlyaStenda и запрет трогать сеть на машине человека).
func stendSPravami(t *testing.T) *Sluzhba {
	t.Helper()
	s := gotovStendLestnicy(t)
	s.pravaDlyaStenda = func() bool { return true }
	return s
}

// tipyVhodov — какие входы (tun/mixed/...) лежат в конфиге, приготовленном
// для ядра прямо сейчас.
func tipyVhodov(t *testing.T, s *Sluzhba) []string {
	t.Helper()
	b, err := os.ReadFile(s.Yadro.PutKonfiga())
	if err != nil {
		t.Fatalf("не прочитал конфиг с диска: %v", err)
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatalf("не разобрал конфиг с диска: %v", err)
	}
	vhody, _ := d["inbounds"].([]any)
	var tipy []string
	for _, v := range vhody {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		// Вход службы (internal/zapasnoy) — не прокси для человека: socks на
		// 127.0.0.1 только с паролем устройства, для повтора запросов самой
		// службы через туннель. Системе о нём не сообщается.
		if teg, _ := m["tag"].(string); teg == zapasnoy.TegVhoda {
			continue
		}
		tip, _ := m["type"].(string)
		tipy = append(tipy, tip)
	}
	return tipy
}

// Туннель не создался: связь НЕ поднимается вовсе, второй попытки «хотя бы
// браузерами» нет, человеку — официальная причина без советов «попробуйте
// ещё раз», значку — «отключено», за попыткой не осталось следа.
func TestTunnelNePodnyalsyaBezOtkataVProksi(t *testing.T) {
	s := stendSPravami(t)
	tunnel.Otmetit("tun125", 4242) // след прошлого сеанса — уборка обязана снять
	var podnyata *bool
	s.MetkaZashchity = func(p bool) { podnyata = &p }

	var vhody [][]string
	s.zapustitYadro = func(ctx context.Context) error {
		vhody = append(vhody, tipyVhodov(t, s))
		return fmt.Errorf("%s", oshibkaTunnelya)
	}

	err := s.PodnyatZashchitu(context.Background())
	if !errors.Is(err, OshibkaTunnelNePodnyalsya) {
		t.Fatalf("ждали OshibkaTunnelNePodnyalsya, получили %v", err)
	}
	if err.Error() != "Не удалось создать сетевое подключение Kelevra." {
		t.Fatalf("человеку сказано %q", err.Error())
	}
	for _, slovo := range []string{"ещё раз", "попробуйте", "браузер"} {
		if strings.Contains(strings.ToLower(err.Error()), slovo) {
			t.Fatalf("в отказе «%s»: %q", slovo, err.Error())
		}
	}
	// Сырая причина ядра не потеряна — она в цепочке ошибок.
	if !strings.Contains(fmt.Sprintf("%v", errors.Unwrap(errors.Join(err))), "") || !errorsContain(err, "configure tun interface") {
		t.Fatalf("сырая причина ядра потеряна: %v", err)
	}
	if len(vhody) != 1 {
		t.Fatalf("ядро запускали %d раз (%v) — второй попытки (прокси) быть не должно", len(vhody), vhody)
	}
	for _, tip := range vhody[0] {
		if tip != "tun" {
			t.Fatalf("в конфиге ядра вход %q — локального прокси на компьютере нет", tip)
		}
	}
	if _, _, est := tunnel.ProchestMetku(); est {
		t.Fatal("след неудачной попытки остался на диске — следующий запуск будет искать несуществующий адаптер")
	}
	if podnyata == nil || *podnyata {
		t.Fatal("значку не сказали, что связь не поднята")
	}
	s.zamok.Lock()
	otkaz := s.otkaz
	s.zamok.Unlock()
	if otkaz != OshibkaTunnelNePodnyalsya.Error() {
		t.Fatalf("окну отдан отказ %q", otkaz)
	}
}

// errorsContain — есть ли в цепочке ошибок err текст chto.
func errorsContain(err error, chto string) bool {
	var otkaz otkazPodyoma
	if errors.As(err, &otkaz) {
		return strings.Contains(otkaz.prichina.Error(), chto)
	}
	return strings.Contains(err.Error(), chto)
}

// Нет прав администратора — ядро не запускают вовсе (и не качают): туннель
// без прав не поднять, а другого режима нет. Человеку — «нужны права».
func TestBezPravYadroNeZapuskaetsya(t *testing.T) {
	s := gotovStendLestnicy(t)
	s.pravaDlyaStenda = func() bool { return false }
	zapuskov := 0
	s.zapustitYadro = func(ctx context.Context) error {
		zapuskov++
		return nil
	}
	err := s.PodnyatZashchitu(context.Background())
	if !errors.Is(err, OshibkaNetPrav) {
		t.Fatalf("ждали OshibkaNetPrav, получили %v", err)
	}
	if err.Error() != "Для работы Kelevra нужны права администратора." {
		t.Fatalf("человеку сказано %q", err.Error())
	}
	if zapuskov != 0 {
		t.Fatalf("ядро запускали %d раз без прав — так поднимался прокси-режим", zapuskov)
	}
}

// Окно без прав рисует «Не подключено» и кнопку «Дать права» по полю
// nuzhny_prava; поля неполного режима из ответа ушли насовсем.
func TestSostoyanieBezPravGovoritNuzhnyPrava(t *testing.T) {
	s := gotovStendLestnicy(t)
	s.pravaDlyaStenda = func() bool { return false }
	r := httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil)
	w := httptest.NewRecorder()
	s.Obsluzhit().ServeHTTP(w, r)
	var o map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("ответ не JSON: %v: %s", err, w.Body.String())
	}
	if o["nuzhny_prava"] != true {
		t.Fatalf("nuzhny_prava = %v — окно не покажет «Дать права»", o["nuzhny_prava"])
	}
	for _, pole := range []string{"chastichnaya", "pochemu_chastichnaya", "mozhno_tun", "ruchnoy_proksi"} {
		if _, est := o[pole]; est {
			t.Fatalf("в ответе осталось поле неполного режима %q", pole)
		}
	}

	s.pravaDlyaStenda = func() bool { return true }
	w = httptest.NewRecorder()
	s.Obsluzhit().ServeHTTP(w, httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil))
	o = nil
	_ = json.Unmarshal(w.Body.Bytes(), &o)
	if o["nuzhny_prava"] != false {
		t.Fatalf("права есть, а nuzhny_prava = %v", o["nuzhny_prava"])
	}
}

// Отказ подъёма доходит до окна полем otkaz, а «Отключить» его гасит.
func TestOtkazVidenOknuIGasitsyaOtklyucheniem(t *testing.T) {
	s := stendSPravami(t)
	s.zapustitYadro = func(ctx context.Context) error { return fmt.Errorf("%s", oshibkaTunnelya) }
	_ = s.PodnyatZashchitu(context.Background())

	prochest := func() map[string]any {
		w := httptest.NewRecorder()
		s.Obsluzhit().ServeHTTP(w, httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil))
		var o map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &o)
		return o
	}
	if o := prochest(); o["otkaz"] != OshibkaTunnelNePodnyalsya.Error() {
		t.Fatalf("otkaz = %v — окно не скажет, почему нет подключения", o["otkaz"])
	}
	_ = s.OpustitZashchitu()
	if o := prochest(); o["otkaz"] != nil {
		t.Fatalf("после «Отключить» otkaz = %v — старая причина висела бы над выключенным кругом", o["otkaz"])
	}
}

// Когда туннель поднялся, всё как раньше: одна попытка, режим туннеля, след
// на диске, отказа нет.
func TestUdavshiysyaTunnelOdnaPopytka(t *testing.T) {
	s := stendSPravami(t)

	popytok := 0
	s.zapustitYadro = func(ctx context.Context) error {
		popytok++
		return nil
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("туннель поднялся, а метод вернул ошибку: %v", err)
	}
	if popytok != 1 {
		t.Fatalf("ядро поднимали %d раз(а) — на удавшемся туннеле попытка одна", popytok)
	}
	s.zamok.Lock()
	k := s.kartina
	otkaz := s.otkaz
	s.zamok.Unlock()
	if k.Rezhim != konfig.Tunnel {
		t.Fatalf("режим %q", k.Rezhim)
	}
	if otkaz != "" {
		t.Fatalf("туннель поднят, а отказ %q", otkaz)
	}
	if _, _, est := tunnel.ProchestMetku(); !est {
		t.Fatal("туннель подняли, а следа на диске нет — после жёсткой смерти проверять будет нечего")
	}
}

// Провал подъёма убирает за собой ВСЁ: след туннеля, работающее ядро, и
// заодно системный прокси, если его оставила старая версия (беда 08.09:
// запись прокси на мёртвое ядро — браузер мёртв при живой сети).
func TestProvalUbiraetZaSoboy(t *testing.T) {
	s := stendSPravami(t)
	tunnel.Otmetit("tun125", 4242)
	s.zapustitYadro = func(ctx context.Context) error { return fmt.Errorf("%s", oshibkaTunnelya) }

	if err := s.PodnyatZashchitu(context.Background()); err == nil {
		t.Fatal("туннель не поднялся, а служба отчиталась успехом")
	}
	if _, _, est := tunnel.ProchestMetku(); est {
		t.Fatal("след попытки остался на диске: следующий запуск будет искать адаптер, которого нет")
	}
	if s.Yadro != nil && s.Yadro.Sost() == yadro.Rabotaet {
		t.Fatal("ядро числится работающим после провала")
	}
}

// Подъём туннеля убирает системный прокси старой версии (прокси-режим до
// 0.6.77): у обновившегося из прокси-режима запись иначе указывала бы на
// мёртвый порт 2412, и браузеры молчали бы при живом туннеле.
func TestPodyomTunnelyaUbiraetProksiStaroyVersii(t *testing.T) {
	s := stendSPravami(t)
	s.zapustitYadro = func(ctx context.Context) error { return nil }
	// Метка старой версии на диске: её обязана убрать уборка (zakrepitTunnel →
	// proksi.UbratSled; на проверке реестр не трогается — KELEVRA_PROKSI=net).
	putMetki := filepath.Join(os.Getenv("KELEVRA_DIR"), "proksi.json")
	if err := os.WriteFile(putMetki, []byte(`{"adres":"127.0.0.1:2412"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(putMetki); !os.IsNotExist(err) {
		t.Fatal("метка прокси старой версии осталась — уборка при подъёме туннеля не прошла")
	}
}

// Без прав живое ядро прошлой копии — прокси-режим старой версии: принимать
// его нельзя, оно гасится, а системный прокси старой версии убирается.
func TestBezPravZhivoeYadroStaroyVersiiNePrinimaetsya(t *testing.T) {
	s := gotovStendLestnicy(t)
	s.pravaDlyaStenda = func() bool { return false }
	ubrano := 0
	s.snyatProksiDlyaStenda = func() { ubrano++ }
	s.PrinyatZhivoeYadro(context.Background(), "")
	if ubrano == 0 {
		t.Fatal("без прав приём ядра не убрал системный прокси старой версии")
	}
	if s.Yadro.Sost() == yadro.Rabotaet {
		t.Fatal("без прав ядро прошлой копии принято — человек остался бы в прокси-режиме")
	}
}

// После провала туннеля журнал уходит СРАЗУ, а не ночью по расписанию.
//
// Дважды подряд разбор беды на живой машине упирался в одно и то же: человеку
// плохо сейчас, а журнал уезжает раз в сутки около двух ночи.
func TestPosleProvalaZhurnalUhoditSrazu(t *testing.T) {
	s := stendSPravami(t)
	s.snyatProksiDlyaStenda = func() {}

	prishlo := make(chan struct{}, 4)
	priyomnik := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case prishlo <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer priyomnik.Close()

	putZhurnala := filepath.Join(t.TempDir(), "kelevra.log")
	if err := os.WriteFile(putZhurnala, []byte("связь не поднялась"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.otpravshchikZhurnalovDlyaStenda = &zhurnaly.Otpravshchik{
		Adres:    priyomnik.URL + "/logs",
		DeviceID: "stend",
		Versiya:  "stend",
		Puti:     []string{putZhurnala},
		PutMetok: filepath.Join(t.TempDir(), "otmetki.json"),
	}

	s.zapustitYadro = func(ctx context.Context) error {
		return fmt.Errorf("%s", oshibkaTunnelya)
	}
	if err := s.PodnyatZashchitu(context.Background()); err == nil {
		t.Fatal("туннель не поднялся, а служба отчиталась успехом")
	}

	select {
	case <-prishlo:
	case <-time.After(5 * time.Second):
		t.Fatal("журнал не ушёл после провала — разбирать беду будет нечем до следующей ночи")
	}
}

// Сервер попросил журнал — клиент отдаёт его сам, не трогая человека
// (08.09: человеку не до пересылки журнала руками).
func TestServerPoprosilZhurnalKlientOtpravlyaet(t *testing.T) {
	s := gotovStendLestnicy(t)

	prishlo := make(chan struct{}, 4)
	priyomnik := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case prishlo <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer priyomnik.Close()

	putZhurnala := filepath.Join(t.TempDir(), "kelevra.log")
	if err := os.WriteFile(putZhurnala, []byte("что было в тот день"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.otpravshchikZhurnalovDlyaStenda = &zhurnaly.Otpravshchik{
		Adres:    priyomnik.URL + "/logs",
		DeviceID: "stend",
		Versiya:  "stend",
		Puti:     []string{putZhurnala},
		PutMetok: filepath.Join(t.TempDir(), "otmetki.json"),
	}

	svedeniya := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"name":"stend","active":true,"send_log_now":true}`)
	}))
	defer svedeniya.Close()
	adres := strings.TrimPrefix(svedeniya.URL, "http://")
	s.Podpiska.Shema, s.Podpiska.Host = "http", adres
	s.Nastroyki.Kod = "stend"

	s.ObnovitSvedeniya(context.Background())

	select {
	case <-prishlo:
	case <-time.After(5 * time.Second):
		t.Fatal("сервер попросил журнал, а клиент его не прислал — разбор беды снова упрётся в человека")
	}
}

// Авторежим не гасит связь, пока она поднимается (замер 08.09), — и гасит,
// когда подъём кончился. Проверяются ОБЕ стороны.
func TestAvtorezhimNeGasitPodyomNaPolputi(t *testing.T) {
	s := stendSPravami(t)
	snyatiy := 0
	s.snyatProksiDlyaStenda = func() { snyatiy++ }
	s.zashchitaPodnyataDlyaStenda = func() bool { return true }

	s.zamok.Lock()
	s.podyomIdet = true
	s.zamok.Unlock()

	s.avtorezhimKolbek(context.Background(), avtorezhim.Doma, false)
	if snyatiy != 0 {
		t.Fatal("авторежим погасил связь посреди подъёма — подъём провалится, и человек увидит беду на ровном месте")
	}

	s.zamok.Lock()
	s.podyomIdet = false
	s.zamok.Unlock()

	s.avtorezhimKolbek(context.Background(), avtorezhim.Doma, true)
	if snyatiy == 0 {
		t.Fatal("подъём кончился, дома, а связь так и осталась поднятой — правило «не мешать» съело само действие")
	}
}
