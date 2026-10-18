// Пакет podpis: подпись выпусков. Скачанное обновление ставится только если
// рядом лежит подпись нашего ключа и она сходится с файлом.
//
// Зачем. Обновление приходит с публичной страницы релизов. Кто получит право
// писать в релизы, мог подложить свой Kelevra.exe, и все копии поставили бы его
// сами; под службой Windows он запускается от системы. Подпись переносит доверие
// с учётной записи GitHub на ключ, чья закрытая половина лежит только в секрете
// сборки (KELEVRA_SIGN_KEY), а открытая вшита в приложение.
//
// Что подписывается: не сами байты, а строка «метка, имя файла, sha256 файла».
// Имя входит в подпись, чтобы подпись ядра нельзя было выдать за подпись
// приложения (и наоборот). Файл хэшируется потоком, целиком в память не читается.
package podpis

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// KlyuchHex — открытый ключ проверки подписи, 32 байта в шестнадцатеричном виде.
// var, а не const: стендовая сборка подменяет его своим тестовым ключом через
// -ldflags "-X github.com/HRYNdev/kelevra-desktop/internal/podpis.KlyuchHex=...".
// У выпускной сборки значение ниже.
var KlyuchHex = "f31f77c374013c6bdc4548896672868722a1ffb611d86604d78a4bb37d8753d1"

// SufiksFayla — расширение файла подписи рядом с выпускным файлом.
const SufiksFayla = ".sig"

// metka отделяет наши подписи от любых других, сделанных тем же ключом.
const metka = "kelevra-podpis-v1"

// PredelRazmera — предел размера файла подписи: настоящая занимает около 90 байт.
const PredelRazmera = 4 << 10

var (
	// ErrNetPodpisi — подписи нет (или она пустая).
	ErrNetPodpisi = errors.New("нет файла подписи")
	// ErrNeVerna — подпись есть, но с файлом или ключом не сходится.
	ErrNeVerna = errors.New("подпись не сходится с файлом")
)

// Klyuch разбирает открытый ключ из шестнадцатеричной строки.
func Klyuch(hexStroka string) (ed25519.PublicKey, error) {
	b, err := hex.DecodeString(strings.TrimSpace(hexStroka))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("открытый ключ подписи вшит неверно")
	}
	return ed25519.PublicKey(b), nil
}

func soobshchenie(imya string, hesh []byte) []byte {
	return []byte(metka + "\n" + imya + "\n" + hex.EncodeToString(hesh))
}

func heshFayla(put string) ([]byte, error) {
	f, err := os.Open(put)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// Podpisat подписывает файл закрытым ключом (seed в base64, как лежит в секрете
// сборки) и возвращает текст для файла .sig. imya — имя, под которым файл
// выкладывается в релиз.
func Podpisat(seedBase64, imya, put string) (string, error) {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(seedBase64))
	if err != nil || len(seed) != ed25519.SeedSize {
		return "", errors.New("закрытый ключ подписи не разобрать (ждём 32 байта в base64)")
	}
	hesh, err := heshFayla(put)
	if err != nil {
		return "", err
	}
	priv := ed25519.NewKeyFromSeed(seed)
	sig := ed25519.Sign(priv, soobshchenie(imya, hesh))
	return base64.StdEncoding.EncodeToString(sig) + "\n", nil
}

// Proverit сверяет файл с подписью вшитым ключом.
func Proverit(put, imya string, sig []byte) error {
	klyuch, err := Klyuch(KlyuchHex)
	if err != nil {
		return err
	}
	return ProveritKlyuchom(klyuch, put, imya, sig)
}

// ProveritKlyuchom — то же с явным ключом (для тестов и стенда).
func ProveritKlyuchom(klyuch ed25519.PublicKey, put, imya string, sig []byte) error {
	tekst := strings.TrimSpace(string(sig))
	if tekst == "" {
		return ErrNetPodpisi
	}
	syroe, err := base64.StdEncoding.DecodeString(tekst)
	if err != nil || len(syroe) != ed25519.SignatureSize {
		return fmt.Errorf("%w: файл подписи испорчен", ErrNeVerna)
	}
	hesh, err := heshFayla(put)
	if err != nil {
		return err
	}
	if !ed25519.Verify(klyuch, soobshchenie(imya, hesh), syroe) {
		return ErrNeVerna
	}
	return nil
}
