// Пакет hranenie: где приложение держит свои файлы и настройки.
//
// Всё пользовательское живёт в одной папке рядом с профилем пользователя,
// а не рядом с .exe: программу можно положить куда угодно и обновить заменой файла.
package hranenie

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Papka — корень данных приложения.
// Windows: %LOCALAPPDATA%\Kelevra, остальное: ~/.local/share/kelevra (для отладки на сервере).
func Papka() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "Kelevra")
		}
	}
	if d := os.Getenv("KELEVRA_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".kelevra"
	}
	return filepath.Join(home, ".local", "share", "kelevra")
}

// PapkaYadra — куда кладётся бинарь sing-box и его рабочий конфиг.
func PapkaYadra() string { return filepath.Join(Papka(), "yadro") }

// PutKonfiga — рабочий конфиг ядра, который пишет приложение.
func PutKonfiga() string { return filepath.Join(PapkaYadra(), "config.json") }

// PutZhurnala — журнал приложения. Его путь знают двое: тот, кто в журнал
// пишет, и окно, которое даёт человеку этот журнал прислать.
func PutZhurnala() string { return filepath.Join(Papka(), "kelevra.log") }

// PutProfilya — профиль, как его прислал сервер подписки, без наших правок.
// Хранится отдельно от рабочего конфига: правки зависят от прав, а права
// меняются между запусками, поэтому исходник нужен целым.
func PutProfilya() string { return filepath.Join(Papka(), "profil.json") }

// PutYadra — исполняемый файл ядра.
func PutYadra() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(PapkaYadra(), "sing-box.exe")
	}
	return filepath.Join(PapkaYadra(), "sing-box")
}

// Nastroyki — то немногое, что приложение помнит между запусками.
type Nastroyki struct {
	Kod           string `json:"kod"`           // код доступа
	DeviceID      string `json:"device_id"`     // постоянный идентификатор устройства
	Avtopodklyuch bool   `json:"avtopodklyuch"` // подключаться сразу при запуске
	ObnovlyatMin  int    `json:"obnovlyat_min"` // как часто перекачивать профиль, минут
}

var zamok sync.Mutex

func putNastroek() string { return filepath.Join(Papka(), "nastroyki.json") }

// Zagruzit читает настройки; отсутствие файла — не ошибка, вернутся значения по умолчанию.
func Zagruzit() (*Nastroyki, error) {
	zamok.Lock()
	defer zamok.Unlock()
	n := &Nastroyki{ObnovlyatMin: 60}
	b, err := os.ReadFile(putNastroek())
	if err != nil {
		if os.IsNotExist(err) {
			n.DeviceID = novyyID()
			return n, nil
		}
		return n, err
	}
	if err := json.Unmarshal(b, n); err != nil {
		// Битый файл не должен запирать приложение: начинаем с чистого.
		return &Nastroyki{ObnovlyatMin: 60, DeviceID: novyyID()}, nil
	}
	if n.DeviceID == "" {
		n.DeviceID = novyyID()
	}
	if n.ObnovlyatMin <= 0 {
		n.ObnovlyatMin = 60
	}
	return n, nil
}

// Sohranit пишет настройки атомарно: чтобы обрыв не оставил половину файла.
func Sohranit(n *Nastroyki) error {
	zamok.Lock()
	defer zamok.Unlock()
	if err := os.MkdirAll(Papka(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	vremenny := putNastroek() + ".tmp"
	if err := os.WriteFile(vremenny, b, 0o600); err != nil {
		return err
	}
	return os.Rename(vremenny, putNastroek())
}

func novyyID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}
