package sluzhba

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/zapisi"
)

// novyyNablyudatel — наблюдатель над журналом в папке теста, с местом чтения
// рядом (как у службы: файл места в папке данных).
func novyyNablyudatel(dir string) *nablyudatelYadra {
	return &nablyudatelYadra{
		put:   filepath.Join(dir, "yadro.log"),
		mesto: filepath.Join(dir, ImyaMestaChteniya),
		kody:  map[string]int{},
		imena: map[string]int{},
	}
}

func dopisat(t *testing.T, put, tekst string) {
	t.Helper()
	f, err := os.OpenFile(put, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(tekst); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// Перезапуск службы: прошлая копия разобрала журнал до какого-то места и
// записала его; следующая продолжает ровно оттуда — старое не перечитывается
// (ложная «беда»), а дописанное ядром, пока служба перезапускалась, не теряется.
func TestPosleRestartaChitaemSZapomnennogoMesta(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 21, 21, 0, 0, 0, time.UTC)
	staraya := novyyNablyudatel(dir)
	dopisat(t, staraya.put, "+0500 2026-10-21 21:00:00 INFO start\nstaroe-1\nstaroe-2\n")
	staraya.nachat(start)
	staraya.zahod(start.Add(10 * time.Second))
	staraya.zapomnitMesto(start.Add(20 * time.Second)) // штатная остановка

	// Пока службы нет, ядро пишет дальше.
	dopisat(t, staraya.put, "poka-sluzhby-net\n")

	novaya := novyyNablyudatel(dir)
	novaya.nachat(start.Add(30 * time.Second))
	dopisat(t, novaya.put, "posle-starta\n")
	stroki, err := novaya.prochitatNovoe()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(stroki, "|") != "poka-sluzhby-net|posle-starta" {
		t.Fatalf("после перезапуска прочитано %q, ждал только дописанное после прошлой копии", stroki)
	}
}

// Ротация во время перезапуска: новая служба подняла ядро, прошлый журнал
// отложен, новый файл дорос даже дальше старого смещения. Начало другое —
// значит файл другой, и он целиком написан после прошлой копии: с нуля.
func TestPosleRestartaNovyyFaylSNulya(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 21, 21, 0, 0, 0, time.UTC)
	staraya := novyyNablyudatel(dir)
	dopisat(t, staraya.put, "+0500 2026-10-21 21:00:00 INFO start\nstaroe\n")
	staraya.nachat(start)
	staraya.zahod(start.Add(10 * time.Second))
	staraya.zapomnitMesto(start.Add(10 * time.Second))

	if err := os.Rename(staraya.put, staraya.put+".proshlyy"); err != nil {
		t.Fatal(err)
	}
	dopisat(t, staraya.put, "+0500 2026-10-21 21:00:40 INFO start\nnovoe-1\nnovoe-2\nnovoe-3\n")

	novaya := novyyNablyudatel(dir)
	novaya.nachat(start.Add(50 * time.Second))
	stroki, err := novaya.prochitatNovoe()
	if err != nil {
		t.Fatal(err)
	}
	if len(stroki) != 4 || stroki[1] != "novoe-1" {
		t.Fatalf("новый журнал прочитан как %q, ждал все четыре строки с начала", stroki)
	}
}

// Усечение: файл стал короче запомненного смещения — читаем с нуля.
func TestPosleRestartaUsechennyyFaylSNulya(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 21, 21, 0, 0, 0, time.UTC)
	staraya := novyyNablyudatel(dir)
	dopisat(t, staraya.put, "+0500 2026-10-21 21:00:00 INFO start\ndlinnaya-staraya-stroka\n")
	staraya.nachat(start)
	staraya.zahod(start.Add(10 * time.Second))
	staraya.zapomnitMesto(start.Add(10 * time.Second))

	if err := os.WriteFile(staraya.put, []byte("korotko\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	novaya := novyyNablyudatel(dir)
	novaya.nachat(start.Add(20 * time.Second))
	stroki, _ := novaya.prochitatNovoe()
	if len(stroki) != 1 || stroki[0] != "korotko" {
		t.Fatalf("усечённый журнал прочитан как %q, ждал его с начала", stroki)
	}
}

// Место старше окна сводки — служба долго не работала: накопленное за это
// время легло бы в одно окно. Начинаем с конца файла, как без места.
func TestUstarevsheeMestoNachinaemSKonca(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 21, 21, 0, 0, 0, time.UTC)
	staraya := novyyNablyudatel(dir)
	dopisat(t, staraya.put, "+0500 2026-10-21 21:00:00 INFO start\n")
	staraya.nachat(start)
	staraya.zahod(start.Add(10 * time.Second))
	staraya.zapomnitMesto(start.Add(10 * time.Second))
	dopisat(t, staraya.put, "nakopilos-za-chas\n")

	novaya := novyyNablyudatel(dir)
	novaya.nachat(start.Add(time.Hour))
	dopisat(t, novaya.put, "svezhee\n")
	stroki, _ := novaya.prochitatNovoe()
	if len(stroki) != 1 || stroki[0] != "svezhee" {
		t.Fatalf("при устаревшем месте прочитано %q, ждал только свежее", stroki)
	}
}

// Битый файл места — не повод читать журнал с нуля: начинаем с конца.
func TestBitoeMestoNachinaemSKonca(t *testing.T) {
	dir := t.TempDir()
	n := novyyNablyudatel(dir)
	dopisat(t, n.put, "staroe-1\nstaroe-2\n")
	if err := os.WriteFile(n.mesto, []byte("{не json"), 0o644); err != nil {
		t.Fatal(err)
	}
	n.nachat(time.Now())
	dopisat(t, n.put, "novoe\n")
	stroki, _ := n.prochitatNovoe()
	if len(stroki) != 1 || stroki[0] != "novoe" {
		t.Fatalf("при битом месте прочитано %q, ждал только новое", stroki)
	}
}

// Место пишется только когда сдвинулось: заход без новых строк файл места не
// трогает (служба заглядывает в журнал раз в 10 секунд круглые сутки).
func TestMestoNePishetsyaBezNovogo(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 21, 21, 0, 0, 0, time.UTC)
	n := novyyNablyudatel(dir)
	n.nachat(start) // журнала ещё нет: новое ядро заведёт его
	dopisat(t, n.put, "+0500 2026-10-21 21:00:00 INFO start\nstroka\n")
	n.zahod(start.Add(10 * time.Second))
	m1, ok := prochestMesto(n.mesto)
	if !ok {
		t.Fatal("после чтения места на диске нет")
	}
	n.zahod(start.Add(20 * time.Second))
	m2, _ := prochestMesto(n.mesto)
	if !m2.Kogda.Equal(m1.Kogda) {
		t.Fatalf("место переписано без новых строк: %v → %v", m1.Kogda, m2.Kogda)
	}
	if m1.Smeshenie != int64(len("+0500 2026-10-21 21:00:00 INFO start\nstroka\n")) {
		t.Fatalf("смещение места %d, ждал конец прочитанного", m1.Smeshenie)
	}
}

// Шум IPv6 (машина без пути по шестой версии) «бедой» не считается: так
// выглядели окна ноутбука 07.10 — 1023 отказа из 1466 соединений, почти все
// «unreachable network» по адресам IPv6, по IPv4 ни одного.
func TestShumV6NeBeda(t *testing.T) {
	if bedaZaOkno(1466, 1023, 1015) {
		t.Fatal("окно ноутбука 07.10 (почти весь отказ — шум IPv6) названо бедой")
	}
	if !bedaZaOkno(100, 30, 0) {
		t.Fatal("30 настоящих отказов из 100 — беда, а порог промолчал")
	}
	if !bedaZaOkno(1100, 1030, 1000) {
		t.Fatal("30 настоящих отказов из 100 рядом с шумом — всё ещё беда")
	}
	if bedaZaOkno(500, 500, 500) {
		t.Fatal("одни отказы IPv6 без пути — не беда")
	}
}

// Окно целиком: строки шума IPv6 в журнале → в сводке они видны отдельным
// полем, а строки «это уже беда» в журнале службы нет.
func TestOknoSShumomV6BezBedy(t *testing.T) {
	dir := t.TempDir()
	n := novyyNablyudatel(dir)
	n.pisatel = zapisi.Novyy(dir)
	vhod := "+0500 2026-10-07 09:48:00 INFO [1 0ms] inbound/tun[tun-in]: inbound connection to [2a00::1]:443\n"
	shum := "+0500 2026-10-07 09:48:00 ERROR [1 10ms] connection: open connection to www.example.com:443 using outbound/direct[direct]: dial tcp [2a00::1]:443: connectex: A socket operation was attempted to an unreachable network.\n"
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString(vhod)
		b.WriteString(shum)
	}
	for i := 0; i < 50; i++ {
		b.WriteString("+0500 2026-10-07 09:48:00 INFO [2 0ms] inbound/tun[tun-in]: inbound connection to 1.2.3.4:443\n")
	}
	dopisat(t, n.put, b.String())

	var zhurnal bytes.Buffer
	log.SetOutput(&zhurnal)
	defer log.SetOutput(os.Stderr)

	start := time.Now()
	n.zahod(start)
	n.zahod(start.Add(OknoSvodkiYadra))

	if strings.Contains(zhurnal.String(), "беда") {
		t.Fatalf("шум IPv6 дал «беду»: %s", zhurnal.String())
	}
	telo, err := os.ReadFile(filepath.Join(dir, zapisi.ImyaFayla))
	if err != nil {
		t.Fatal(err)
	}
	var svodka map[string]any
	for _, stroka := range strings.Split(strings.TrimSpace(string(telo)), "\n") {
		var z map[string]any
		if json.Unmarshal([]byte(stroka), &z) == nil && z["t"] == "svodka" {
			svodka = z
		}
	}
	if svodka == nil {
		t.Fatalf("сводки нет в записях:\n%s", telo)
	}
	if svodka["otkazy"] != float64(50) || svodka["shum_v6"] != float64(50) || svodka["soed"] != float64(100) {
		t.Fatalf("сводка %v: ждал otkazy=50, shum_v6=50, soed=100", svodka)
	}
}

// Журнал без времени в строках (log.timestamp выключен): начало у разных
// стартов ядра одинаковое. Новый файл, доросший до старого смещения, всё
// равно узнаётся как новый — по номеру файла — и читается с нуля.
func TestNovyyFaylBezVremeniVZhurnale(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 21, 21, 0, 0, 0, time.UTC)
	odinakovo := "INFO[0000] sing-box started\nINFO[0000] inbound/tun[tun-in]: started\n"
	staraya := novyyNablyudatel(dir)
	staraya.nachat(start)
	dopisat(t, staraya.put, odinakovo)
	staraya.zahod(start.Add(10 * time.Second))

	if err := os.Rename(staraya.put, staraya.put+".proshlyy"); err != nil {
		t.Fatal(err)
	}
	dopisat(t, staraya.put, odinakovo+"novoe\n")

	novaya := novyyNablyudatel(dir)
	novaya.nachat(start.Add(30 * time.Second))
	stroki, _ := novaya.prochitatNovoe()
	if len(stroki) != 3 {
		t.Fatalf("новый журнал с тем же началом прочитан как %q, ждал все три строки с нуля", stroki)
	}
}

// Тот же номер файла (файл переписан на месте), то же начало, но перед
// смещением лежит не то, что прошлая копия прочла: файл другой по
// содержанию — с нуля. Это проверка хвоста отдельно от номера.
func TestPerepisannyyNaMesteFaylSNulya(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 21, 21, 0, 0, 0, time.UTC)
	// Начало длиннее отпечатка начала и одинаковое — различает только хвост.
	nachalo := strings.Repeat("INFO[0000] odinakovo\n", 500)
	staraya := novyyNablyudatel(dir)
	staraya.nachat(start)
	dopisat(t, staraya.put, nachalo+"ERROR [111 5ms] staroe\n")
	staraya.zahod(start.Add(10 * time.Second))

	if err := os.WriteFile(staraya.put, []byte(nachalo+"ERROR [222 7ms] drugoe\nesche\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	novaya := novyyNablyudatel(dir)
	novaya.nachat(start.Add(20 * time.Second))
	stroki, _ := novaya.prochitatNovoe()
	if len(stroki) != 502 {
		t.Fatalf("переписанный журнал прочитан как %d строк, ждал все 502 с нуля", len(stroki))
	}
}

// Место пишется не чаще раза в минуту, сколько бы ядро ни писало.
func TestMestoPishetsyaRazVMinutu(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 21, 21, 0, 0, 0, time.UTC)
	n := novyyNablyudatel(dir)
	n.nachat(start)
	dopisat(t, n.put, "a\n")
	n.zahod(start)
	dopisat(t, n.put, "b\n")
	n.zahod(start.Add(10 * time.Second))
	if m, _ := prochestMesto(n.mesto); m.Smeshenie != 2 {
		t.Fatalf("через 10 с место переписано (смещение %d), ждал прежнее 2", m.Smeshenie)
	}
	dopisat(t, n.put, "c\n")
	n.zahod(start.Add(ShagZapisiMesta))
	if m, _ := prochestMesto(n.mesto); m.Smeshenie != 6 {
		t.Fatalf("через минуту место не записано (смещение %d), ждал 6", m.Smeshenie)
	}
}

// Уход службы: место пишется сразу, синхронно, хотя минута не прошла, и
// слежка больше не читает (иначе уходящая копия перезаписала бы место после
// того, как новая уже с него начала). Уход сорвался — слежка читает дальше.
func TestUhodSluzhbyZapisyvaetMestoSrazu(t *testing.T) {
	s := &Sluzhba{}
	s.ZapomnitMestoChteniyaYadra() // слежки нет — тихо ничего

	dir := t.TempDir()
	start := time.Now()
	n := novyyNablyudatel(dir)
	n.nachat(start)
	s.slezhkaYadra.Store(n)
	dopisat(t, n.put, "a\n")
	n.zahod(start)
	dopisat(t, n.put, "b\n")
	n.zahod(start.Add(10 * time.Second))

	s.ZapomnitMestoChteniyaYadra()
	if m, _ := prochestMesto(n.mesto); m.Smeshenie != 4 {
		t.Fatalf("при уходе место %d, ждал 4 (всё прочитанное)", m.Smeshenie)
	}
	dopisat(t, n.put, "c\n")
	n.zahod(start.Add(2 * ShagZapisiMesta))
	if n.smeshenie != 4 {
		t.Fatalf("после ухода слежка читала дальше: смещение %d", n.smeshenie)
	}
	s.prodolzhitSlezhkuZaYadrom()
	n.zahod(start.Add(3 * ShagZapisiMesta))
	if n.smeshenie != 6 {
		t.Fatalf("после сорванного ухода слежка не читает: смещение %d", n.smeshenie)
	}
}
