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
//
// KELEVRA_DIR перекрывает всё и на ЛЮБОЙ системе. Так было не всегда: до 22.08
// на Windows первым стояло %LOCALAPPDATA%, и переменная там не значила ничего.
// Из-за этого windows-тесты, которые честно ставят себе t.TempDir(), все до
// единого писали в ЖИВУЮ папку приложения: `nastroyki.json` в стенде накопил
// `"uzly": {"Соединение": "Комната"}` от одного теста, и следующий прогон
// другого теста читал этот чужой выбор вместо профиля (стенд краснел на
// TestUzlySoStatikaPokaYadroStoit). На настоящей машине это означало бы, что
// прогон тестов затирает человеку его собственные настройки и профиль.
func Papka() string {
	if d := os.Getenv("KELEVRA_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "Kelevra")
		}
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
	// Avtorezhim — переключать защиту самому по смене сети (дома/не дома).
	// По умолчанию выключен: авторежим сам дёргает VPN пользователя, включать
	// это молча за него нельзя — отсутствующее поле в старом файле настроек
	// разбирается json.Unmarshal в false, ничего доделывать не нужно.
	Avtorezhim bool `json:"avtorezhim"`
	// Uzly — выбор узла в каждой группе (ключ — имя группы), сделанный из окна.
	// Пока ядро работает, тот же выбор помнит само ядро (cache_file в профиле,
	// см. konfig.zapomnitVybor) — но ЭТО хранилище живёт и когда ядро ещё не
	// запускалось ни разу: без него выбор «до Подключить» был бы декорацией.
	Uzly map[string]string `json:"uzly,omitempty"`
	// ObyavlennoeObnovlenie — версия, про которую фоновая проверка уже
	// показала пузырь в трее (internal/sluzhba.ProveritObnovlenieFonom).
	// На диске, а не только в памяти процесса: копия висит в трее неделями и
	// её могут перезапустить (в том числе само обновление себя, см.
	// cmd/kelevra/obnovlenie.go) — перезапуск не повод сказать про ТУ ЖЕ
	// версию ещё раз, человек её уже видел. Новая, ещё не объявленная версия
	// (строка не совпадает) при этом обязана прозвучать заново — поле не
	// запирает уведомления навсегда, а помнит только последнюю названную.
	ObyavlennoeObnovlenie string `json:"obyavlennoe_obnovlenie,omitempty"`
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
