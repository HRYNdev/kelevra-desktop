package dom

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// Ключ WireGuard этого устройства (Curve25519).
//
// Рождается здесь один раз и живёт в файле под той же защитой, что
// настройки службы (hranenie.ZapisatZakryto: только система и
// администраторы). Наружу уходит только открытая половина — POST
// /k/<код>/dom/klyuch; закрытая попадает лишь в конфиг ядра (он закрыт так же,
// yadro.ZapisatKonfig) и не пишется ни в журнал, ни в /api, ни на сервер.
// Новый ключ в дом не попадает, пока владелец сети не подтвердит его в панели.

// Klyuch — пара ключей устройства. Закрытая половина — неэкспортируемое поле:
// fmt, %v и json.Marshal её не видят.
type Klyuch struct {
	Pub  string // открытый, base64 (как у wg pubkey)
	priv []byte
}

// Privatnyy — закрытый ключ в base64 для конфига ядра. Больше никуда.
func (k *Klyuch) Privatnyy() string {
	if k == nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(k.priv)
}

// String — для журнала: только открытая половина.
func (k *Klyuch) String() string {
	if k == nil {
		return "нет"
	}
	return "открытый " + k.Pub
}

// GoString — %#v тоже не выдаёт закрытую половину.
func (k *Klyuch) GoString() string { return "dom.Klyuch{" + k.String() + "}" }

// NovyyKlyuch — свежая пара: 32 случайных байта, зажатые как у wg genkey.
func NovyyKlyuch() (*Klyuch, error) {
	priv := make([]byte, 32)
	if _, err := rand.Read(priv); err != nil {
		return nil, err
	}
	zazhat(priv)
	return izZakrytogo(priv)
}

// zazhat — clamp Curve25519 (RFC 7748): ядро зажимает и само, но в файле и в
// конфиге ключ лежит ровно в том виде, что даёт wg genkey.
func zazhat(k []byte) {
	k[0] &= 248
	k[31] = (k[31] & 127) | 64
}

func izZakrytogo(priv []byte) (*Klyuch, error) {
	pk, err := ecdh.X25519().NewPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	return &Klyuch{Pub: base64.StdEncoding.EncodeToString(pk.PublicKey().Bytes()), priv: append([]byte(nil), priv...)}, nil
}

// Sostoyanie — что сервер ответил на открытый ключ.
const (
	SostZhdet        = "zhdet"        // ждёт подтверждения владельца сети
	SostPodtverzhden = "podtverzhden" // подтверждён
	SostOtozvan      = "otozvan"      // отозван
)

// zapisKlyucha — файл на диске (dom_klyuch.json).
type zapisKlyucha struct {
	Privatnyy string `json:"private_key"`
	Otkrytyy  string `json:"public_key"`
	// Otpravlen — открытый ключ, который сервер принял (2xx). Не равен Otkrytyy —
	// ещё не дошёл.
	Otpravlen string `json:"otpravlen,omitempty"`
	// Sostoyanie — что сервер сказал про ключ в ответе ("" — не сказал).
	Sostoyanie string `json:"sostoyanie,omitempty"`
	// Flag — включён ли этому устройству «Дом» по ответу сервера: 1 — да,
	// -1 — нет, 0 — сервер не сказал.
	Flag int `json:"dom,omitempty"`
}

// Hranilishche — ключ устройства на диске.
type Hranilishche struct {
	Put string // "" — только в памяти (проверки)

	mu sync.Mutex
	k  *Klyuch
	z  zapisKlyucha
}

// Klyuch — ключ устройства; нет — рождается и ложится на диск. Ключ, который
// не удалось сохранить, не отдаётся: иначе каждый запуск рождал бы новый, и
// владелец сети подтверждал бы ключи без конца.
func (h *Hranilishche) Klyuch() (*Klyuch, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.k != nil {
		return h.k, nil
	}
	if h.Put != "" {
		if b, err := os.ReadFile(h.Put); err == nil {
			var z zapisKlyucha
			k, perr := razobratZapis(b, &z)
			if perr == nil {
				h.k, h.z = k, z
				return k, nil
			}
			// Битый файл не затираем: откладываем рядом и рождаем новый ключ
			// (старый всё равно непригоден, а сервер получит новый на подтверждение).
			log.Printf("дом: файл ключа нечитаем (%v) — откладываю его и рождаю новый ключ", perr)
			_ = os.Rename(h.Put, h.Put+".bityy")
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("ключ дома не прочитан: %w", err)
		}
	}
	return h.rodit()
}

// rodit — новый ключ на диск и в память; под h.mu.
func (h *Hranilishche) rodit() (*Klyuch, error) {
	k, err := NovyyKlyuch()
	if err != nil {
		return nil, err
	}
	z := zapisKlyucha{Privatnyy: k.Privatnyy(), Otkrytyy: k.Pub}
	if err := h.zapisat(z); err != nil {
		return nil, fmt.Errorf("ключ дома не сохранён: %w", err)
	}
	h.k, h.z = k, z
	log.Printf("дом: рождён ключ устройства (%s)", k)
	return k, nil
}

// Pererodit — сервер отозвал ключ pub (тот же ключ он больше не примет):
// рождается новый. Прежний файл откладывается рядом (.otozvan), а не
// затирается. Ключ уже не тот — ничего не делает.
func (h *Hranilishche) Pererodit(pub string) (*Klyuch, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.k == nil || h.k.Pub != pub {
		return h.k, nil
	}
	if h.Put != "" {
		_ = os.Remove(h.Put + ".otozvan")
		if err := os.Rename(h.Put, h.Put+".otozvan"); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("отозванный ключ не отложен: %w", err)
		}
	}
	log.Printf("дом: ключ отозван владельцем сети (%s) — рождаю новый", h.k)
	return h.rodit()
}

// SbrositOtpravku — спросить сервер про ключ заново (например, вход домой
// сняли: узнать, не отозван ли ключ).
func (h *Hranilishche) SbrositOtpravku() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.k == nil || h.z.Otpravlen == "" {
		return
	}
	z := h.z
	z.Otpravlen, z.Sostoyanie, z.Flag = "", "", 0
	if err := h.zapisat(z); err != nil {
		log.Printf("дом: отметка об отправке ключа не сброшена: %v", err)
	}
	h.z = z
}

func razobratZapis(b []byte, z *zapisKlyucha) (*Klyuch, error) {
	if err := json.Unmarshal(b, z); err != nil {
		return nil, err
	}
	priv, err := base64.StdEncoding.DecodeString(z.Privatnyy)
	if err != nil || len(priv) != 32 {
		return nil, fmt.Errorf("закрытый ключ не 32 байта")
	}
	k, err := izZakrytogo(priv)
	if err != nil {
		return nil, err
	}
	if k.Pub != z.Otkrytyy {
		return nil, fmt.Errorf("открытый ключ в файле не от этого закрытого")
	}
	return k, nil
}

// zapisat — под h.mu.
func (h *Hranilishche) zapisat(z zapisKlyucha) error {
	if h.Put == "" {
		return nil
	}
	b, err := json.MarshalIndent(z, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.Put), 0o755); err != nil {
		return err
	}
	return hranenie.ZapisatZakryto(h.Put, b)
}

// NuzhnoOtpravit — открытый ключ сервер ещё не принял.
func (h *Hranilishche) NuzhnoOtpravit() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.k == nil || h.z.Otpravlen != h.k.Pub
}

// Otmetit — сервер принял открытый ключ pub; sost — что он про него сказал,
// flag — включён ли устройству «Дом» (1 / -1, 0 — не сказал).
func (h *Hranilishche) Otmetit(pub, sost string, flag int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.k == nil || pub != h.k.Pub {
		return
	}
	z := h.z
	z.Otpravlen, z.Sostoyanie = pub, sost
	if flag != 0 {
		z.Flag = flag
	}
	if z == h.z {
		return
	}
	if err := h.zapisat(z); err != nil {
		log.Printf("дом: отметка об отправке ключа не сохранена: %v", err)
	}
	h.z = z
}

// Sostoyanie — что сервер сказал про принятый ключ ("" — не отправлен или не сказал).
func (h *Hranilishche) Sostoyanie() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.k == nil || h.z.Otpravlen != h.k.Pub {
		return ""
	}
	return h.z.Sostoyanie
}

// ZhdetPodtverzhdeniya — ключ принят, ждёт владельца сети, и «Дом» этому
// устройству включён (или сервер про флаг не сказал). Без флага ждать нечего:
// владелец «Дом» этому устройству не включал.
func (h *Hranilishche) ZhdetPodtverzhdeniya() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.k != nil && h.z.Otpravlen == h.k.Pub && h.z.Sostoyanie == SostZhdet && h.z.Flag >= 0
}

// Otpechatok — отпечаток открытого ключа для сверки глазами: первые 16 знаков
// base64, четырьмя группами по 4 («AB12 CD34 EF56 GH78»). Тот же вид у мозга
// и в панели у кнопки «Подтвердить ключ» (договор, «Безопасность» п.4).
// Короче 16 знаков — пусто.
func Otpechatok(pub string) string {
	pub = strings.TrimSpace(pub)
	if len(pub) < 16 {
		return ""
	}
	return pub[0:4] + " " + pub[4:8] + " " + pub[8:12] + " " + pub[12:16]
}

// OtpechatokPrinyatogo — отпечаток ключа, который сервер уже принял (ждёт
// подтверждения или подтверждён); пусто — ключ ещё не дошёл.
func (h *Hranilishche) OtpechatokPrinyatogo() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.k == nil || h.z.Otpravlen != h.k.Pub || h.z.Sostoyanie == SostOtozvan {
		return ""
	}
	return Otpechatok(h.k.Pub)
}
