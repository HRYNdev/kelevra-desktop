package dom

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Пробные ключи: боевые закрытые ключи мозга сюда не попадают (договор пульта).
func probnyyKlyuch(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func otkrytye(priv ...ed25519.PrivateKey) []ed25519.PublicKey {
	var k []ed25519.PublicKey
	for _, p := range priv {
		k = append(k, p.Public().(ed25519.PublicKey))
	}
	return k
}

func dannyeBloka(versiya int64) map[string]any {
	return map[string]any{
		"adres": "100.77.0.5/32", "uzel_pub": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)),
		"most": "100.76.0.1:51820", "seti": []any{"100.77.0.0/16"}, "dns": "100.77.0.1",
		"zona": "dom", "mtu": 1280, "versiya": versiya, "komu": "01-7", "pub": svoyPubTest,
	}
}

// vKonverte — блок как пульт: {"dannye","podpis"}, подписано Metka + dannye.
func vKonverte(t *testing.T, priv ed25519.PrivateKey, d map[string]any) []byte {
	t.Helper()
	dannye, _ := json.Marshal(d)
	sig := ed25519.Sign(priv, append([]byte(Metka), dannye...))
	b, _ := json.Marshal(map[string]string{
		"dannye": base64.StdEncoding.EncodeToString(dannye), "podpis": base64.StdEncoding.EncodeToString(sig),
	})
	return b
}

// ploskiy — блок договора как его подпишет мозг на Python:
// json.dumps(blok, sort_keys=True, separators=(",", ":"), ensure_ascii=False).
// Ровно эта строка — дословно, без участия kanonicheskiy.
func ploskiy(t *testing.T, priv ed25519.PrivateKey, versiya int64) []byte {
	t.Helper()
	pub := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	kanon := fmt.Sprintf(`{"adres":"100.77.0.5/32","dns":"100.77.0.1","komu":"01-7","most":"100.76.0.1:51820","mtu":1280,"pub":"%s","seti":["100.77.0.0/16"],"uzel_pub":"%s","versiya":%d,"zona":"dom"}`, svoyPubTest, pub, versiya)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, append([]byte(Metka), kanon...)))
	// Сервер отдаёт поля в своём порядке и с пробелами — подпись от этого не зависит.
	return []byte(fmt.Sprintf(`{"versiya": %d, "zona": "dom", "adres": "100.77.0.5/32", "uzel_pub": "%s", "most": "100.76.0.1:51820", "seti": ["100.77.0.0/16"], "dns": "100.77.0.1", "mtu": 1280, "komu": "01-7", "pub": "%s", "podpis": "%s"}`, versiya, pub, svoyPubTest, sig))
}

func TestRazobratObeFormy(t *testing.T) {
	priv := probnyyKlyuch(t)
	for imya, syroe := range map[string][]byte{
		"конверт": vKonverte(t, priv, dannyeBloka(3)),
		"плоский": ploskiy(t, priv, 3),
	} {
		b, err := Razobrat(syroe, otkrytye(probnyyKlyuch(t), priv), "01-7")
		if err != nil {
			t.Fatalf("%s: %v", imya, err)
		}
		host, port := b.MostHostPort()
		if b.Adres != "100.77.0.5/32" || host != "100.76.0.1" || port != 51820 || b.Mtu() != 1280 || b.Versiya != 3 || b.Zona != "dom" {
			t.Fatalf("%s: %+v", imya, b)
		}
	}
}

func TestRazobratBitayaPodpis(t *testing.T) {
	priv, chuzhoy := probnyyKlyuch(t), probnyyKlyuch(t)
	// Подписано чужим ключом.
	if _, err := Razobrat(vKonverte(t, chuzhoy, dannyeBloka(1)), otkrytye(priv), ""); !otkaz(err, "podpis") {
		t.Fatalf("чужая подпись принята: %v", err)
	}
	// Подменено поле после подписи (конверт).
	var k map[string]string
	_ = json.Unmarshal(vKonverte(t, priv, dannyeBloka(1)), &k)
	d, _ := base64.StdEncoding.DecodeString(k["dannye"])
	d = bytes.Replace(d, []byte("100.77.0.5/32"), []byte("100.77.0.6/32"), 1)
	k["dannye"] = base64.StdEncoding.EncodeToString(d)
	b, _ := json.Marshal(k)
	if _, err := Razobrat(b, otkrytye(priv), ""); !otkaz(err, "podpis") {
		t.Fatalf("подменённый конверт принят: %v", err)
	}
	// Подменено поле после подписи (плоский).
	p := bytes.Replace(ploskiy(t, priv, 1), []byte(`"most": "100.76.0.1:51820"`), []byte(`"most": "203.0.113.9:51820"`), 1)
	if _, err := Razobrat(p, otkrytye(priv), ""); !otkaz(err, "podpis") {
		t.Fatalf("подменённый плоский блок принят: %v", err)
	}
	// Подпись пульта к блоку дома не подходит (своя метка).
	dannye, _ := json.Marshal(dannyeBloka(1))
	sig := ed25519.Sign(priv, append([]byte("kelevra-pult-v1\n"), dannye...))
	b, _ = json.Marshal(map[string]string{"dannye": base64.StdEncoding.EncodeToString(dannye), "podpis": base64.StdEncoding.EncodeToString(sig)})
	if _, err := Razobrat(b, otkrytye(priv), ""); !otkaz(err, "podpis") {
		t.Fatalf("подпись пульта принята за подпись дома: %v", err)
	}
	// Без подписи вовсе.
	if _, err := Razobrat([]byte(`{"adres":"100.77.0.5/32"}`), otkrytye(priv), ""); !otkaz(err, "podpis") {
		t.Fatalf("блок без подписи принят: %v", err)
	}
}

func TestRazobratSoderzhimoe(t *testing.T) {
	priv := probnyyKlyuch(t)
	for pole, plohoe := range map[string]any{
		"adres":    "10.0.0.5/32",
		"seti":     []any{"0.0.0.0/0"},
		"most":     "100.76.0.1",
		"uzel_pub": "ne-klyuch",
		"dns":      "1.1.1.1",
		"zona":     "Дом",
		"mtu":      9000,
	} {
		d := dannyeBloka(1)
		d[pole] = plohoe
		if _, err := Razobrat(vKonverte(t, priv, d), otkrytye(priv), "01-7"); !otkaz(err, pole) {
			t.Errorf("%s=%v принято: %v", pole, plohoe, err)
		}
	}
	// komu — слот адресата: чужой, без komu и при неизвестном своём слоте — не наш.
	d := dannyeBloka(1)
	d["komu"] = "01-8"
	if _, err := Razobrat(vKonverte(t, priv, d), otkrytye(priv), "01-7"); !otkaz(err, "komu") {
		t.Fatalf("блок чужого слота принят: %v", err)
	}
	delete(d, "komu")
	if _, err := Razobrat(vKonverte(t, priv, d), otkrytye(priv), "01-7"); !otkaz(err, "komu") {
		t.Fatalf("блок без адресата принят: %v", err)
	}
	if _, err := Razobrat(vKonverte(t, priv, dannyeBloka(1)), otkrytye(priv), ""); !otkaz(err, "komu") {
		t.Fatalf("блок принят при неизвестном своём слоте: %v", err)
	}
}

// Слот стал известен позже — блок с диска перечитывается и сверяется заново.
func TestKeshSlotStalIzvesten(t *testing.T) {
	priv := probnyyKlyuch(t)
	put := filepath.Join(t.TempDir(), "dom_blok.json")
	k := &Kesh{Put: put, Klyuchi: otkrytye(priv), Komu: "01-7", SvoyPub: svoyPubF}
	if _, err := k.Prinyat(vKonverte(t, priv, dannyeBloka(1))); err != nil {
		t.Fatal(err)
	}
	k2 := &Kesh{Put: put, Klyuchi: otkrytye(priv), SvoyPub: svoyPubF}
	if k2.Blok() != nil {
		t.Fatal("блок принят без своего слота")
	}
	k2.UstanovitKomu("01-7")
	if k2.Blok() == nil {
		t.Fatal("после слота блок с диска не принят")
	}
	k2.UstanovitKomu("01-8")
	if k2.Blok() != nil {
		t.Fatal("блок принят для другого слота")
	}
}

func otkaz(err error, prichina string) bool {
	var o *Otkaz
	return errors.As(err, &o) && o.Prichina == prichina
}

func TestKeshNaDiske(t *testing.T) {
	priv := probnyyKlyuch(t)
	put := filepath.Join(t.TempDir(), "dom_blok.json")
	k := &Kesh{Put: put, Klyuchi: otkrytye(priv), Komu: "01-7", SvoyPub: svoyPubF}
	if k.Blok() != nil {
		t.Fatal("пустой кеш отдал блок")
	}
	itog, err := k.Prinyat(vKonverte(t, priv, dannyeBloka(2)))
	if err != nil || !itog.Izmenilsya {
		t.Fatalf("первый блок: %v %v", itog, err)
	}
	// Тот же блок ещё раз — не изменение.
	if itog, _ := k.Prinyat(vKonverte(t, priv, dannyeBloka(2))); itog.Izmenilsya {
		t.Fatal("тот же блок (новая подпись) посчитан изменением")
	}
	// Без мозга: новый процесс читает блок с диска.
	k2 := &Kesh{Put: put, Klyuchi: otkrytye(priv), Komu: "01-7", SvoyPub: svoyPubF}
	if b := k2.Blok(); b == nil || b.Versiya != 2 {
		t.Fatalf("блок с диска: %v", b)
	}
	// Откат версии не принимается, прежний остаётся.
	if _, err := k2.Prinyat(vKonverte(t, priv, dannyeBloka(1))); !otkaz(err, "versiya") {
		t.Fatalf("старая версия принята: %v", err)
	}
	// Битая подпись — прежний блок остаётся.
	if _, err := k2.Prinyat(vKonverte(t, probnyyKlyuch(t), dannyeBloka(5))); err == nil {
		t.Fatal("блок с чужой подписью принят")
	}
	if b := k2.Blok(); b == nil || b.Versiya != 2 {
		t.Fatalf("после отказа блок сменился: %v", b)
	}
	// Подменённый на диске файл не применяется.
	syroe, _ := os.ReadFile(put)
	var kv map[string]string
	_ = json.Unmarshal(syroe, &kv)
	dd, _ := base64.StdEncoding.DecodeString(kv["dannye"])
	kv["dannye"] = base64.StdEncoding.EncodeToString(bytes.Replace(dd, []byte("100.76.0.1"), []byte("100.76.0.9"), 1))
	b, _ := json.Marshal(kv)
	_ = os.WriteFile(put, b, 0o600)
	if (&Kesh{Put: put, Klyuchi: otkrytye(priv), Komu: "01-7", SvoyPub: svoyPubF}).Blok() != nil {
		t.Fatal("подменённый на диске блок применён")
	}
	// Сводка без dom — мозг дом снял: кеш и файл уходят.
	k3 := &Kesh{Put: filepath.Join(t.TempDir(), "b.json"), Klyuchi: otkrytye(priv), Komu: "01-7", SvoyPub: svoyPubF}
	_, _ = k3.Prinyat(vKonverte(t, priv, dannyeBloka(1)))
	if itog, err := k3.Prinyat(nil); err != nil || !itog.Izmenilsya || k3.Blok() != nil {
		t.Fatalf("снятие блока: %v %v", itog, err)
	}
	if _, err := os.Stat(k3.Put); !os.IsNotExist(err) {
		t.Fatal("снятый блок остался на диске")
	}
}

func TestKlyuchRozhdaetsyaOdinRaz(t *testing.T) {
	put := filepath.Join(t.TempDir(), "dom_klyuch.json")
	var zhurnal bytes.Buffer
	log.SetOutput(&zhurnal)
	defer log.SetOutput(os.Stderr)

	h := &Hranilishche{Put: put}
	k1, err := h.Klyuch()
	if err != nil {
		t.Fatal(err)
	}
	priv, _ := base64.StdEncoding.DecodeString(k1.Privatnyy())
	if len(priv) != 32 || priv[0]&7 != 0 || priv[31]&128 != 0 || priv[31]&64 == 0 {
		t.Fatalf("закрытый ключ не зажат как у wg genkey: % x", priv)
	}
	// Открытый — от этого закрытого (X25519).
	pk, _ := ecdh.X25519().NewPrivateKey(priv)
	if base64.StdEncoding.EncodeToString(pk.PublicKey().Bytes()) != k1.Pub {
		t.Fatal("открытый ключ не от закрытого")
	}
	// Новый процесс — тот же ключ.
	k2, err := (&Hranilishche{Put: put}).Klyuch()
	if err != nil || k2.Pub != k1.Pub || k2.Privatnyy() != k1.Privatnyy() {
		t.Fatalf("ключ родился второй раз: %v", err)
	}
	// Закрытая половина не уходит ни в журнал, ни в fmt, ни в JSON.
	j, _ := json.Marshal(k1)
	for imya, s := range map[string]string{
		"журнал": zhurnal.String(), "%v": fmt.Sprintf("%v", k1), "%+v": fmt.Sprintf("%+v", *k1),
		"%#v": fmt.Sprintf("%#v", k1), "json": string(j),
	} {
		if strings.Contains(s, k1.Privatnyy()) {
			t.Fatalf("закрытый ключ в %s: %s", imya, s)
		}
	}
	if !strings.Contains(zhurnal.String(), k1.Pub) {
		t.Fatalf("рождение ключа не в журнале: %s", zhurnal.String())
	}
}

func TestKlyuchBityyFaylOtkladyvaetsya(t *testing.T) {
	put := filepath.Join(t.TempDir(), "dom_klyuch.json")
	_ = os.WriteFile(put, []byte(`{"private_key":"AAAA","public_key":"BBBB"}`), 0o600)
	k, err := (&Hranilishche{Put: put}).Klyuch()
	if err != nil || k.Pub == "" {
		t.Fatalf("новый ключ вместо битого: %v", err)
	}
	if b, err := os.ReadFile(put + ".bityy"); err != nil || !bytes.Contains(b, []byte("AAAA")) {
		t.Fatalf("битый файл не отложен: %v", err)
	}
}

func TestOtmetkaOtpravki(t *testing.T) {
	put := filepath.Join(t.TempDir(), "dom_klyuch.json")
	h := &Hranilishche{Put: put}
	k, _ := h.Klyuch()
	if !h.NuzhnoOtpravit() || h.Sostoyanie() != "" {
		t.Fatal("свежий ключ не ждёт отправки")
	}
	h.Otmetit("chuzhoy", SostZhdet, 1)
	if !h.NuzhnoOtpravit() {
		t.Fatal("отметка чужого ключа засчитана")
	}
	h.Otmetit(k.Pub, SostZhdet, -1)
	if h.ZhdetPodtverzhdeniya() {
		t.Fatal("«Дом» не включён, а устройство «ждёт подтверждения»")
	}
	h.Otmetit(k.Pub, SostZhdet, 1)
	h2 := &Hranilishche{Put: put}
	_, _ = h2.Klyuch()
	if h2.NuzhnoOtpravit() || h2.Sostoyanie() != SostZhdet || !h2.ZhdetPodtverzhdeniya() {
		t.Fatalf("отметка не пережила перезапуск: %v %q", h2.NuzhnoOtpravit(), h2.Sostoyanie())
	}
	h2.SbrositOtpravku()
	if !h2.NuzhnoOtpravit() || h2.ZhdetPodtverzhdeniya() {
		t.Fatal("сброс отметки не сработал")
	}
}

// Отозванный ключ: рождается новый, прежний файл отложен, а не затёрт.
func TestKlyuchOtozvanPererozhdaetsya(t *testing.T) {
	put := filepath.Join(t.TempDir(), "dom_klyuch.json")
	h := &Hranilishche{Put: put}
	k1, _ := h.Klyuch()
	if k, _ := h.Pererodit("ne-tot"); k == nil || k.Pub != k1.Pub {
		t.Fatal("чужой отзыв сменил ключ")
	}
	k2, err := h.Pererodit(k1.Pub)
	if err != nil || k2.Pub == k1.Pub {
		t.Fatalf("новый ключ не родился: %v", err)
	}
	if b, err := os.ReadFile(put + ".otozvan"); err != nil || !bytes.Contains(b, []byte(k1.Pub)) {
		t.Fatalf("отозванный ключ не отложен: %v", err)
	}
	if k3, _ := (&Hranilishche{Put: put}).Klyuch(); k3.Pub != k2.Pub {
		t.Fatal("после перезапуска не новый ключ")
	}
}

func TestOtpechatokKlyucha(t *testing.T) {
	if got := Otpechatok("AB12CD34EF56GH78ijkl9012mnop3456qrst7890uvw="); got != "AB12 CD34 EF56 GH78" {
		t.Fatalf("отпечаток %q, ждали «AB12 CD34 EF56 GH78»", got)
	}
	if Otpechatok("AB12CD34EF56GH7") != "" {
		t.Fatal("короткий ключ дал отпечаток")
	}
	h := &Hranilishche{Put: filepath.Join(t.TempDir(), "k.json")}
	k, _ := h.Klyuch()
	vid := regexp.MustCompile(`^[A-Za-z0-9+/]{4}( [A-Za-z0-9+/]{4}){3}$`)
	if !vid.MatchString(Otpechatok(k.Pub)) || !strings.HasPrefix(k.Pub, strings.ReplaceAll(Otpechatok(k.Pub), " ", "")) {
		t.Fatalf("отпечаток %q не по виду ключа %s", Otpechatok(k.Pub), k.Pub)
	}
	if h.OtpechatokPrinyatogo() != "" {
		t.Fatal("отпечаток до отправки ключа")
	}
	h.Otmetit(k.Pub, SostZhdet, 1)
	if h.OtpechatokPrinyatogo() != Otpechatok(k.Pub) {
		t.Fatal("нет отпечатка, пока ключ ждёт подтверждения")
	}
	h.Otmetit(k.Pub, SostPodtverzhden, 1)
	if h.OtpechatokPrinyatogo() != Otpechatok(k.Pub) {
		t.Fatal("нет отпечатка после подтверждения")
	}
	h.Otmetit(k.Pub, SostOtozvan, -1)
	if h.OtpechatokPrinyatogo() != "" {
		t.Fatal("отпечаток отозванного ключа")
	}
}

// Свой открытый ключ в пробных блоках.
var svoyPubTest = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))

func svoyPubF() string { return svoyPubTest }

// Договор, «Безопасность» п.3: блок на чужой ключ не применяется; ключ
// перерождается только по подписанному отзыву своего ключа; наибольшая
// принятая версия переживает снятие блока и перезапуск.
func TestKeshBezopasnost(t *testing.T) {
	priv := probnyyKlyuch(t)
	papka := t.TempDir()
	novyy := func() *Kesh {
		return &Kesh{Put: filepath.Join(papka, "b.json"), PutPamyati: filepath.Join(papka, "p.json"),
			Klyuchi: otkrytye(priv), Komu: "01-7", SvoyPub: svoyPubF}
	}
	k := novyy()
	if k.Vklyuchalsya() {
		t.Fatal("вход домой «включался» без блока")
	}
	if _, err := k.Prinyat(vKonverte(t, priv, dannyeBloka(3))); err != nil {
		t.Fatal(err)
	}
	if !k.Vklyuchalsya() || k.MaxVersiya() != 3 {
		t.Fatalf("память: %v %d", k.Vklyuchalsya(), k.MaxVersiya())
	}
	// Чужой ключ — не применяется, прежний остаётся.
	d := dannyeBloka(4)
	d["pub"] = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{6}, 32))
	itog, err := k.Prinyat(vKonverte(t, priv, d))
	if !otkaz(err, "pub") || !itog.ChuzhoyKlyuch || k.Blok() == nil || k.Blok().Versiya != 3 {
		t.Fatalf("блок на чужой ключ: %+v %v", itog, err)
	}
	// Снятие (неподписанное) не стирает версию: старый подписанный блок после
	// снятия и перезапуска не возвращается.
	if itog, _ := k.Prinyat(nil); !itog.Izmenilsya || k.Blok() != nil {
		t.Fatal("снятие не сработало")
	}
	k = novyy()
	if !k.Vklyuchalsya() || k.MaxVersiya() != 3 {
		t.Fatalf("память после снятия и перезапуска: %v %d", k.Vklyuchalsya(), k.MaxVersiya())
	}
	if _, err := k.Prinyat(vKonverte(t, priv, dannyeBloka(2))); !otkaz(err, "versiya") {
		t.Fatalf("старый блок после снятия принят: %v", err)
	}
	// Подписанный отзыв своего ключа.
	if _, err := k.Prinyat(vKonverte(t, priv, dannyeBloka(5))); err != nil {
		t.Fatal(err)
	}
	otzyv := map[string]any{"komu": "01-7", "pub": svoyPubTest, "otozvan": true, "versiya": 6}
	itog, err = k.Prinyat(vKonverte(t, priv, otzyv))
	if err != nil || !itog.Otozvan || !itog.Izmenilsya || k.Blok() != nil {
		t.Fatalf("подписанный отзыв: %+v %v", itog, err)
	}
	// Отзыв чужого (прежнего) ключа — не перерождение, но и не действующий блок.
	if _, err := k.Prinyat(vKonverte(t, priv, dannyeBloka(7))); err != nil {
		t.Fatal(err)
	}
	otzyv["pub"], otzyv["versiya"] = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{6}, 32)), 8
	itog, err = k.Prinyat(vKonverte(t, priv, otzyv))
	if err != nil || itog.Otozvan || k.Blok() != nil {
		t.Fatalf("отзыв прежнего ключа: %+v %v", itog, err)
	}
	// Отзыв без подписи своим ключом не принимается вовсе.
	if _, err := k.Prinyat(vKonverte(t, probnyyKlyuch(t), map[string]any{"komu": "01-7", "pub": svoyPubTest, "otozvan": true, "versiya": 9})); !otkaz(err, "podpis") {
		t.Fatalf("неподписанный отзыв: %v", err)
	}
	// Блок на диске на чужой ключ (ключ переродился) не берётся.
	if _, err := k.Prinyat(vKonverte(t, priv, dannyeBloka(9))); err != nil {
		t.Fatal(err)
	}
	k2 := novyy()
	k2.SvoyPub = func() string { return "drugoy" }
	if k2.Blok() != nil {
		t.Fatal("блок с диска на прежний ключ применён")
	}
}

// Договор, «Безопасность» п.14: память версий в двух файлах; потерян один —
// восстанавливается из другого; потеряны оба при блоке на диске — блок не
// принимается (ни с диска, ни свежий) до нового начала.
func TestPamyatVDvuhFaylah(t *testing.T) {
	priv := probnyyKlyuch(t)
	papka := t.TempDir()
	pam := filepath.Join(papka, "p.json")
	novyy := func() *Kesh {
		return &Kesh{Put: filepath.Join(papka, "b.json"), PutPamyati: pam,
			Klyuchi: otkrytye(priv), Komu: "01-7", SvoyPub: svoyPubF}
	}
	k := novyy()
	if !k.BezPamyati() || k.PamyatPoteryana() {
		t.Fatal("свежая установка — без памяти, но не потеря")
	}
	if _, err := k.Prinyat(vKonverte(t, priv, dannyeBloka(5))); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{pam, pam + ".zapas"} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("нет файла памяти %s", p)
		}
	}
	// Основной потерян — версия из запасного, оба записаны снова.
	_ = os.Remove(pam)
	k = novyy()
	if k.MaxVersiya() != 5 || k.Blok() == nil {
		t.Fatalf("из запасного: версия %d", k.MaxVersiya())
	}
	if _, err := os.Stat(pam); err != nil {
		t.Fatal("основной файл не восстановлен")
	}
	// Оба потеряны, блок на диске есть — потеря.
	_ = os.Remove(pam)
	_ = os.Remove(pam + ".zapas")
	k = novyy()
	if !k.PamyatPoteryana() || k.Blok() != nil {
		t.Fatal("потеря обеих памятей не замечена")
	}
	if _, err := k.Prinyat(vKonverte(t, priv, dannyeBloka(6))); !otkaz(err, "pamyat") {
		t.Fatalf("после потери памяти блок принят: %v", err)
	}
	k.NachatZanovo()
	if k.PamyatPoteryana() || !k.Vklyuchalsya() || k.Blok() != nil {
		t.Fatal("новое начало не сработало")
	}
}
