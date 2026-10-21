package pult

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Пробные ключи: боевые закрытые ключи мозга сюда не попадают (договор).
type stend struct {
	t      *testing.T
	priv   ed25519.PrivateKey
	chasy  time.Time
	komu   string
	reestr Reestr
}

func novyyStend(t *testing.T) *stend {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &stend{t: t, priv: priv, chasy: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC), komu: "ustroystvo-1",
		reestr: Reestr{
			{Imya: "opros.info_min", Tip: Celoe, Min: 5, Max: 240},
			{Imya: "zhurnal.chasto_do", Tip: Vremya},
			{Imya: "funkcii.avtorezhim", Tip: Flag},
			{Imya: "obnovleniya.kanal", Tip: Vybor, Varianty: []string{"obychnyy", "proba", "stop"}},
			{Imya: "avtomat.dns_domeny", Tip: Spisok, Min: 1, Max: 3},
			{Imya: "avtomat.dns_kontrolnyy", Tip: Stroka},
		}}
}

func (st *stend) pult(put string) *Pult {
	p := Zagruzit(put, st.reestr, map[string]bool{"prishli_zhurnal": true, "obnovi_profil": true})
	p.Klyuchi = []ed25519.PublicKey{st.priv.Public().(ed25519.PublicKey)}
	p.Seychas = func() time.Time { return st.chasy }
	return p
}

// blok подписывает данные как мозг: "kelevra-pult-v1\n" + байты dannye.
func (st *stend) blok(d map[string]any) json.RawMessage {
	return podpisatKlyuchom(st.t, st.priv, d)
}

func podpisatKlyuchom(t *testing.T, priv ed25519.PrivateKey, d map[string]any) json.RawMessage {
	dannye, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, append([]byte("kelevra-pult-v1\n"), dannye...))
	b, _ := json.Marshal(Blok{Dannye: base64.StdEncoding.EncodeToString(dannye), Podpis: base64.StdEncoding.EncodeToString(sig)})
	return b
}

func (st *stend) dannye(versiya int64, nastroyki map[string]any, komandy ...map[string]any) map[string]any {
	if komandy == nil {
		komandy = []map[string]any{}
	}
	return map[string]any{"v": 1, "versiya": versiya, "vydano": "2026-10-21T12:00:00Z", "komu": st.komu,
		"nastroyki": nastroyki, "komandy": komandy}
}

func trebovatOtkaz(t *testing.T, err error, prichina string) {
	t.Helper()
	o, ok := err.(*Otkaz)
	if !ok || o.Prichina != prichina {
		t.Fatalf("ждали отказ %q, получили %v", prichina, err)
	}
}

func TestVshityeKlyuchiIzDogovora(t *testing.T) {
	p := &Pult{}
	if n := len(p.klyuchi()); n != 2 {
		t.Fatalf("вшито ключей %d, по договору 2 (основной и запасной)", n)
	}
	if KlyuchiHex[0] != "89f9504b7dfa4bad83b8f781e8fb81225e5883739ac502804669429b1a6ab552" ||
		KlyuchiHex[1] != "56b5e97341b4aa0828a5660faee970aecbb25291ec1e9e57505bd462f5230e90" {
		t.Fatal("вшитые ключи разошлись с договором docs/PULT.md")
	}
	// Подпись пробным ключом вшитыми ключами не принимается.
	st := novyyStend(t)
	p = Zagruzit("", st.reestr, nil)
	_, err := p.Prinyat(st.blok(st.dannye(1, nil)), st.komu)
	trebovatOtkaz(t, err, "podpis")
}

func TestPodpisVernaIZapasnoyKlyuch(t *testing.T) {
	st := novyyStend(t)
	p := st.pult("")
	_, zapasnoy, _ := ed25519.GenerateKey(rand.Reader)
	p.Klyuchi = append(p.Klyuchi, zapasnoy.Public().(ed25519.PublicKey))
	if _, err := p.Prinyat(st.blok(st.dannye(1, map[string]any{"opros.info_min": 15})), st.komu); err != nil {
		t.Fatalf("основной ключ: %v", err)
	}
	if _, err := p.Prinyat(podpisatKlyuchom(t, zapasnoy, st.dannye(2, nil)), st.komu); err != nil {
		t.Fatalf("запасной ключ: %v", err)
	}
	if p.Versiya() != 2 {
		t.Fatalf("версия %d", p.Versiya())
	}
}

func TestPodpisChuzhaya(t *testing.T) {
	st := novyyStend(t)
	p := st.pult("")
	_, chuzhoy, _ := ed25519.GenerateKey(rand.Reader)
	_, err := p.Prinyat(podpisatKlyuchom(t, chuzhoy, st.dannye(5, map[string]any{"opros.info_min": 15})), st.komu)
	trebovatOtkaz(t, err, "podpis")
	if z := p.Zagolovok(); z != "v=1;umeet=1;versiya=0;oshibki=pult:podpis" {
		t.Fatalf("X-Pult после отказа: %q", z)
	}
	if _, est := p.Celoe("opros.info_min"); est {
		t.Fatal("настройка из отвергнутого пульта применилась")
	}
}

func TestPodpisIsporchena(t *testing.T) {
	st := novyyStend(t)
	p := st.pult("")
	var b Blok
	_ = json.Unmarshal(st.blok(st.dannye(1, nil)), &b)
	syroe, _ := base64.StdEncoding.DecodeString(b.Dannye)
	syroe[len(syroe)-2] ^= 1 // байт данных после подписи
	b.Dannye = base64.StdEncoding.EncodeToString(syroe)
	bb, _ := json.Marshal(b)
	_, err := p.Prinyat(bb, st.komu)
	trebovatOtkaz(t, err, "podpis")
	for _, musor := range []string{`{}`, `"строка"`, `{"dannye":"%%%","podpis":"%%%"}`} {
		_, err := p.Prinyat(json.RawMessage(musor), st.komu)
		trebovatOtkaz(t, err, "podpis")
	}
}

func TestAdresatIVersiyaProtokola(t *testing.T) {
	st := novyyStend(t)
	p := st.pult("")
	d := st.dannye(1, nil)
	d["komu"] = "chuzhoe-ustroystvo"
	_, err := p.Prinyat(st.blok(d), st.komu)
	trebovatOtkaz(t, err, "komu")
	d = st.dannye(1, nil)
	d["v"] = 2
	_, err = p.Prinyat(st.blok(d), st.komu)
	trebovatOtkaz(t, err, "v")
	if !strings.Contains(p.Zagolovok(), "oshibki=pult:v") {
		t.Fatalf("X-Pult: %q", p.Zagolovok())
	}
	// Принятый следом пульт снимает слово об отказе.
	if _, err := p.Prinyat(st.blok(st.dannye(1, nil)), st.komu); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(p.Zagolovok(), "oshibki") {
		t.Fatalf("отказ остался в X-Pult после принятого пульта: %q", p.Zagolovok())
	}
}

func TestVersiyaNeNizheSokhranennoy(t *testing.T) {
	st := novyyStend(t)
	p := st.pult("")
	if _, err := p.Prinyat(st.blok(st.dannye(10, map[string]any{"opros.info_min": 30})), st.komu); err != nil {
		t.Fatal(err)
	}
	// Та же версия — принимается (сервер шлёт её на каждый опрос).
	if _, err := p.Prinyat(st.blok(st.dannye(10, map[string]any{"opros.info_min": 30})), st.komu); err != nil {
		t.Fatalf("та же версия отвергнута: %v", err)
	}
	_, err := p.Prinyat(st.blok(st.dannye(9, map[string]any{"opros.info_min": 60})), st.komu)
	trebovatOtkaz(t, err, "versiya")
	if v, _ := p.Celoe("opros.info_min"); v != 30 {
		t.Fatalf("старая версия переписала настройку: %d", v)
	}
	if !strings.HasPrefix(p.Zagolovok(), "v=1;umeet=1;versiya=10;") {
		t.Fatalf("X-Pult: %q", p.Zagolovok())
	}
}

func TestGranicyITipy(t *testing.T) {
	st := novyyStend(t)
	sluchai := []struct {
		klyuch   string
		znach    any
		prinyato bool
	}{
		{"opros.info_min", 4, false},
		{"opros.info_min", 5, true},
		{"opros.info_min", 240, true},
		{"opros.info_min", 241, false},
		{"opros.info_min", 15.0, true},
		{"opros.info_min", 15.5, false},
		{"opros.info_min", "15", false},
		{"funkcii.avtorezhim", false, true},
		{"funkcii.avtorezhim", 0, false},
		{"zhurnal.chasto_do", "2026-10-22T12:00:00Z", true},
		{"zhurnal.chasto_do", "завтра", false},
		{"obnovleniya.kanal", "proba", true},
		{"obnovleniya.kanal", "beta", false},
		{"avtomat.dns_domeny", []string{"a.ru", "b.ru"}, true},
		{"avtomat.dns_domeny", []string{}, false},
		{"avtomat.dns_domeny", []string{"a", "b", "c", "d"}, false},
		{"avtomat.dns_kontrolnyy", "gosuslugi.ru", true},
		{"avtomat.dns_kontrolnyy", "", false},
		{"neznakomyy.klyuch", 1, false},
	}
	for i, sl := range sluchai {
		p := st.pult("")
		_, err := p.Prinyat(st.blok(st.dannye(int64(i+1), map[string]any{sl.klyuch: sl.znach})), st.komu)
		if err != nil {
			t.Fatalf("%s=%v: пульт целиком отвергнут: %v", sl.klyuch, sl.znach, err)
		}
		_, est := p.znachenie(sl.klyuch)
		oshibka := strings.Contains(p.Zagolovok(), "oshibki="+sl.klyuch)
		if est != sl.prinyato || oshibka == sl.prinyato {
			t.Errorf("%s=%v: принят %v, в oshibki %v; ждали принят %v", sl.klyuch, sl.znach, est, oshibka, sl.prinyato)
		}
	}
}

func TestZnacheniyaBezPultaIzKoda(t *testing.T) {
	var p *Pult // службы, собранные мимо Novaya
	if _, est := p.Celoe("opros.info_min"); est {
		t.Fatal("nil-пульт дал значение")
	}
	if p.Dlitelnost("x", time.Minute, 7*time.Second) != 7*time.Second || !p.FlagIli("x", true) || p.StrokaIli("x", "obychnyy") != "obychnyy" {
		t.Fatal("nil-пульт не отдал значения из кода")
	}
	if p.Zagolovok() != "" || p.Versiya() != 0 || p.Obnovlenie() != nil {
		t.Fatal("nil-пульт что-то сообщает")
	}
	st := novyyStend(t)
	p = st.pult("")
	if z := p.Zagolovok(); z != "v=1;umeet=1;versiya=0" {
		t.Fatalf("X-Pult без принятого пульта: %q", z)
	}
	// Пустой блок /info — слой 2 держится, ничего не меняется.
	if it, err := p.Prinyat(nil, st.komu); err != nil || it.Versiya != 0 {
		t.Fatal("пустой блок что-то изменил")
	}
}

func komanda(id, chto string, do time.Time) map[string]any {
	return map[string]any{"id": id, "chto": chto, "do": do.Format(time.RFC3339), "param": map[string]any{}}
}

func TestKomandyIKvitancii(t *testing.T) {
	st := novyyStend(t)
	p := st.pult("")
	zavtra := st.chasy.Add(24 * time.Hour)
	it, err := p.Prinyat(st.blok(st.dannye(1, nil,
		komanda("k1", "prishli_zhurnal", zavtra),
		komanda("k2", "stantsuy", zavtra),
		komanda("k3", "obnovi_profil", st.chasy.Add(-time.Minute)),
		map[string]any{"id": "k4", "chto": "prishli_zhurnal", "do": "когда-нибудь"},
	)), st.komu)
	if err != nil {
		t.Fatal(err)
	}
	if len(it.Komandy) != 1 || it.Komandy[0].ID != "k1" {
		t.Fatalf("к выполнению %v, ждали только k1", it.Komandy)
	}
	p.Zavershit("k1", "ok")
	z := p.Zagolovok()
	for _, nado := range []string{"k1:ok", "k2:ne_umeyu", "k3:istekla", "k4:oshibka-srok"} {
		if !strings.Contains(z, nado) {
			t.Errorf("в X-Pult нет %s: %q", nado, z)
		}
	}
	// Повтор той же id второй раз не выполняется.
	it, _ = p.Prinyat(st.blok(st.dannye(2, nil, komanda("k1", "prishli_zhurnal", zavtra))), st.komu)
	if len(it.Komandy) != 0 {
		t.Fatal("команда k1 выполнилась второй раз")
	}
	if !strings.Contains(p.Zagolovok(), "kvit=k1:ok") || strings.Contains(p.Zagolovok(), "k2") {
		t.Fatalf("квитанции после второго пульта: %q", p.Zagolovok())
	}
	// Сервер перестал присылать — квитанции нет, но id помнится.
	_, _ = p.Prinyat(st.blok(st.dannye(3, nil)), st.komu)
	if strings.Contains(p.Zagolovok(), "kvit") {
		t.Fatalf("квитанция держится после того, как сервер перестал слать команду: %q", p.Zagolovok())
	}
	it, _ = p.Prinyat(st.blok(st.dannye(4, nil, komanda("k1", "prishli_zhurnal", zavtra))), st.komu)
	if len(it.Komandy) != 0 {
		t.Fatal("через сутки после выполнения id забыт")
	}
	// Больше 7 суток — забыт.
	st.chasy = st.chasy.Add(8 * 24 * time.Hour)
	_, _ = p.Prinyat(st.blok(st.dannye(5, nil)), st.komu)
	p.mu.Lock()
	n := len(p.z.Vypolneno)
	p.mu.Unlock()
	if n != 0 {
		t.Fatalf("через 8 суток помнится %d команд", n)
	}
}

func TestKvitanciyaBezRazdeliteleyVId(t *testing.T) {
	st := novyyStend(t)
	p := st.pult("")
	_, _ = p.Prinyat(st.blok(st.dannye(1, nil, komanda("k;1=x:y, z", "stantsuy", st.chasy.Add(time.Hour)))), st.komu)
	if z := p.Zagolovok(); !strings.Contains(z, "kvit=k1xyz:ne_umeyu") {
		t.Fatalf("id с разделителями попал в заголовок как есть: %q", z)
	}
}

func TestPredelKvitanciy(t *testing.T) {
	st := novyyStend(t)
	p := st.pult("")
	var k []map[string]any
	for i := 0; i < 50; i++ {
		k = append(k, komanda("k"+strings.Repeat("x", i), "stantsuy", st.chasy.Add(time.Hour)))
	}
	_, _ = p.Prinyat(st.blok(st.dannye(1, nil, k...)), st.komu)
	if n := strings.Count(p.Zagolovok(), ":ne_umeyu"); n != PredelKvitanciy {
		t.Fatalf("квитанций в заголовке %d, предел %d", n, PredelKvitanciy)
	}
}

func TestIspytanieOtkat(t *testing.T) {
	st := novyyStend(t)
	p := st.pult("")
	// Версия 1 — проверенные настройки.
	_, _ = p.Prinyat(st.blok(st.dannye(1, map[string]any{"opros.info_min": 30})), st.komu)
	st.chasy = st.chasy.Add(11 * time.Minute)
	p.Otmetit(Nablyudenie{Set: true, Nuzhen: true, STrafikom: true})
	if p.ProveritIspytanie() || p.IspytanieIdet() {
		t.Fatal("испытание с трафиком не прошло")
	}
	// Версия 2 — плохие: сеть была, туннель нужен, трафика не было.
	it, _ := p.Prinyat(st.blok(st.dannye(2, map[string]any{"opros.info_min": 5})), st.komu)
	if !it.IzmenilisNastroyki || !p.IspytanieIdet() {
		t.Fatal("новые настройки не встали на испытание")
	}
	if v, _ := p.Celoe("opros.info_min"); v != 5 {
		t.Fatal("на испытании действуют не новые настройки")
	}
	p.Otmetit(Nablyudenie{Set: true, Nuzhen: true})
	st.chasy = st.chasy.Add(9 * time.Minute)
	if p.ProveritIspytanie() {
		t.Fatal("откат раньше 10 минут")
	}
	st.chasy = st.chasy.Add(2 * time.Minute)
	if !p.ProveritIspytanie() {
		t.Fatal("отката нет")
	}
	if v, _ := p.Celoe("opros.info_min"); v != 30 {
		t.Fatalf("после отката %d, ждали прошлые 30", v)
	}
	if !strings.Contains(p.Zagolovok(), "oshibki=pult:otkat") || !strings.Contains(p.Zagolovok(), "versiya=2") {
		t.Fatalf("X-Pult после отката: %q", p.Zagolovok())
	}
	// Плохая версия второй раз не применяется.
	it, _ = p.Prinyat(st.blok(st.dannye(2, map[string]any{"opros.info_min": 5})), st.komu)
	if it.IzmenilisNastroyki {
		t.Fatal("плохая версия применилась снова")
	}
	// Новая версия — снова на испытание, слово об откате снято.
	it, _ = p.Prinyat(st.blok(st.dannye(3, map[string]any{"opros.info_min": 5})), st.komu)
	if !it.IzmenilisNastroyki || strings.Contains(p.Zagolovok(), "otkat") {
		t.Fatalf("новая версия: изменились %v, X-Pult %q", it.IzmenilisNastroyki, p.Zagolovok())
	}
}

func TestIspytanieBezNuzhdyVTunnelePrikhodit(t *testing.T) {
	// Дома туннель не нужен (автомат держит его опущенным) — это не провал.
	st := novyyStend(t)
	p := st.pult("")
	_, _ = p.Prinyat(st.blok(st.dannye(1, map[string]any{"opros.info_min": 30})), st.komu)
	p.Otmetit(Nablyudenie{Set: true})
	st.chasy = st.chasy.Add(10 * time.Minute)
	if p.ProveritIspytanie() {
		t.Fatal("откат там, где туннель и не был нужен")
	}
	if v, _ := p.Celoe("opros.info_min"); v != 30 {
		t.Fatal("прошедшие испытание настройки не держатся")
	}
}

func TestOtkatPoPadeniyam(t *testing.T) {
	st := novyyStend(t)
	put := filepath.Join(t.TempDir(), "pult.json")
	p := st.pult(put)
	_, _ = p.Prinyat(st.blok(st.dannye(7, map[string]any{"opros.info_min": 20})), st.komu)
	for i := 1; i <= 3; i++ {
		p = st.pult(put) // перезапуск процесса посреди испытания
		otkat := p.Zapusk()
		if otkat != (i == 3) {
			t.Fatalf("перезапуск %d: откат %v", i, otkat)
		}
	}
	if _, est := p.Celoe("opros.info_min"); est {
		t.Fatal("после трёх падений настройки не откатились к коду")
	}
	if !strings.Contains(p.Zagolovok(), "pult:otkat") {
		t.Fatalf("X-Pult: %q", p.Zagolovok())
	}
	// Откат пережил перезапуск, плохая версия помнится.
	p = st.pult(put)
	if p.Zapusk() || p.IspytanieIdet() {
		t.Fatal("испытание продолжилось после отката")
	}
	if it, _ := p.Prinyat(st.blok(st.dannye(7, map[string]any{"opros.info_min": 20})), st.komu); it.IzmenilisNastroyki {
		t.Fatal("плохая версия применилась после перезапуска")
	}
}

func TestStartBezSetiSDiska(t *testing.T) {
	st := novyyStend(t)
	put := filepath.Join(t.TempDir(), "pult.json")
	p := st.pult(put)
	_, _ = p.Prinyat(st.blok(st.dannye(3, map[string]any{"opros.info_min": 20, "funkcii.avtorezhim": false},
		komanda("k9", "prishli_zhurnal", st.chasy.Add(time.Hour)))), st.komu)
	p.Zavershit("k9", "oshibka-set")
	p2 := st.pult(put)
	if v, _ := p2.Celoe("opros.info_min"); v != 20 || p2.FlagIli("funkcii.avtorezhim", true) {
		t.Fatal("слой 2 не поднялся с диска")
	}
	if z := p2.Zagolovok(); !strings.Contains(z, "versiya=3") || !strings.Contains(z, "k9:oshibka-set") {
		t.Fatalf("X-Pult с диска: %q", z)
	}
	// Реестр сузился (ключ ушёл из кода) — с диска незнакомое не применяется.
	p3 := Zagruzit(put, Reestr{{Imya: "funkcii.avtorezhim", Tip: Flag}}, nil)
	if _, est := p3.Celoe("opros.info_min"); est {
		t.Fatal("незнакомый ключ с диска применился")
	}
}

func TestObnovlenieVPulte(t *testing.T) {
	st := novyyStend(t)
	p := st.pult("")
	d := st.dannye(1, nil)
	d["obnovlenie"] = map[string]any{"versiya_imya": "0.6.90", "url": "https://primer/Kelevra.exe", "sha256": strings.Repeat("ab", 32), "razmer": 10}
	it, err := p.Prinyat(st.blok(d), st.komu)
	if err != nil || !it.NovoeObnovlenie {
		t.Fatalf("блок obnovlenie не замечен: %v %v", it, err)
	}
	if o := p.Obnovlenie(); o.Nomer() != "0.6.90" || o.Razmer != 10 {
		t.Fatalf("блок: %+v", o)
	}
	d["versiya"] = 2
	if it, _ := p.Prinyat(st.blok(d), st.komu); it.NovoeObnovlenie {
		t.Fatal("тот же блок — снова «новое обновление»")
	}
	p.OshibkaObnovleniya("hesh")
	if !strings.Contains(p.Zagolovok(), "obnovlenie:hesh") {
		t.Fatalf("X-Pult: %q", p.Zagolovok())
	}
	// Блок сборки Android (versiya_kod — число) Desktop разбирает без сбоя.
	d["versiya"] = 3
	d["obnovlenie"] = map[string]any{"versiya_kod": 711147, "versiya_imya": "1.14.147", "url": "https://primer/a.apk", "sha256": strings.Repeat("cd", 32)}
	if _, err := p.Prinyat(st.blok(d), st.komu); err != nil {
		t.Fatalf("блок с числовым versiya_kod: %v", err)
	}
	if o := p.Obnovlenie(); o.VersiyaKod != 711147 || o.Nomer() != "1.14.147" {
		t.Fatalf("блок: %+v", o)
	}
}

func TestProverochnyyKlyuchTolkoSFlagom(t *testing.T) {
	if ProverochnyyKlyuchHex != "" {
		t.Fatal("в обычной сборке проверочный ключ пульта задан — он только для стенда (-ldflags -X …ProverochnyyKlyuchHex)")
	}
	if n := len(VshityeKlyuchi()); n != 2 {
		t.Fatalf("без флага стенда ключей %d, ждали 2 боевых", n)
	}
	st := novyyStend(t)
	p := Zagruzit("", st.reestr, nil)
	p.Seychas = func() time.Time { return st.chasy }
	if _, err := p.Prinyat(st.blok(st.dannye(1, nil)), st.komu); err == nil {
		t.Fatal("блок, подписанный не боевым ключом, принят без флага стенда")
	}
	ProverochnyyKlyuchHex = hexKlyucha(st.priv)
	defer func() { ProverochnyyKlyuchHex = "" }()
	if n := len(VshityeKlyuchi()); n != 3 {
		t.Fatalf("с флагом стенда ключей %d, ждали 3", n)
	}
	if _, err := p.Prinyat(st.blok(st.dannye(1, nil)), st.komu); err != nil {
		t.Fatalf("сборка стенда не приняла блок проверочного ключа: %v", err)
	}
}

func hexKlyucha(priv ed25519.PrivateKey) string {
	const h = "0123456789abcdef"
	var b strings.Builder
	for _, c := range priv.Public().(ed25519.PublicKey) {
		b.WriteByte(h[c>>4])
		b.WriteByte(h[c&15])
	}
	return b.String()
}

func TestTablicaReestra(t *testing.T) {
	st := novyyStend(t)
	tab := st.reestr.Tablica("# т")
	for _, nado := range []string{"`opros.info_min` | int | 5–240", "obychnyy \\| proba \\| stop", "1–3 шт."} {
		if !strings.Contains(tab, nado) {
			t.Errorf("в таблице нет %q", nado)
		}
	}
}

// Штатный выход (Vyhod) перед перезапуском падением не считается.
func TestShtatnyyVyhodNePadenie(t *testing.T) {
	st := novyyStend(t)
	put := filepath.Join(t.TempDir(), "pult.json")
	p := st.pult(put)
	_, _ = p.Prinyat(st.blok(st.dannye(1, map[string]any{"opros.info_min": 20})), st.komu)
	for i := 0; i < 5; i++ {
		p.Vyhod()
		p = st.pult(put)
		if p.Zapusk() {
			t.Fatalf("штатный перезапуск %d дал откат", i+1)
		}
	}
	if v, _ := p.Celoe("opros.info_min"); v != 20 || !p.IspytanieIdet() {
		t.Fatal("после штатных перезапусков настройки или испытание потерялись")
	}
}

// Пока команда выполняется, её квитанции в X-Pult нет: иначе запрос посреди
// выполнения унёс бы серверу «oshibka-prervano», и мозг закрыл бы команду
// неудачей. После перезапуска (процесс умер посреди) — именно prervano.
func TestKomandaVProcesseBezKvitancii(t *testing.T) {
	st := novyyStend(t)
	put := filepath.Join(t.TempDir(), "pult.json")
	p := st.pult(put)
	zavtra := st.chasy.Add(24 * time.Hour)
	if _, err := p.Prinyat(st.blok(st.dannye(1, nil, komanda("k1", "prishli_zhurnal", zavtra))), st.komu); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(p.Zagolovok(), "k1") {
		t.Fatalf("квитанция идущей команды ушла: %q", p.Zagolovok())
	}
	p2 := st.pult(put) // процесс умер посреди команды
	if !strings.Contains(p2.Zagolovok(), "k1:oshibka-prervano") {
		t.Fatalf("после перезапуска нет prervano: %q", p2.Zagolovok())
	}
	p.Zavershit("k1", "ok")
	if !strings.Contains(p.Zagolovok(), "kvit=k1:ok") {
		t.Fatalf("после выполнения: %q", p.Zagolovok())
	}
}
