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
		// Служба Windows работает под системной учётной записью, и её
		// %LOCALAPPDATA% — это профиль системы, куда человек никогда не
		// заглянет, а окно из своего сеанса не попадёт вовсе. Поэтому как
		// только заведена общая папка, в неё смотрят ОБЕ части: и служба, и
		// окно. Правило простое и одинаковое для всех процессов — «есть общая,
		// значит она и есть папка данных», — потому что развилка «я служба или
		// окно» здесь давала бы двум процессам разные ответы на один вопрос.
		if o := PapkaObshchaya(); o != "" {
			if st, err := os.Stat(o); err == nil && st.IsDir() {
				return o
			}
		}
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

// PapkaObshchaya — общая папка данных, одна на машину: %PROGRAMDATA%\Kelevra.
//
// Заводится вместе со службой Windows. До неё данные лежали в профиле человека,
// и это было верно, пока всё работало в его сеансе. Служба работает под
// системной учётной записью, а туннель нужен один на компьютер, а не по одному
// на каждого вошедшего, — поэтому конфиг ядра, настройки и метка запуска
// переезжают в общее место.
//
// Пустая строка означает «на этой системе такого понятия нет»: вызывающий
// обязан молча продолжить со старой папкой, а не считать это отказом.
func PapkaObshchaya() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	d := os.Getenv("PROGRAMDATA")
	if d == "" {
		return ""
	}
	return filepath.Join(d, "Kelevra")
}

// PapkaYadra — куда кладётся бинарь sing-box и его рабочий конфиг.
func PapkaYadra() string { return filepath.Join(Papka(), "yadro") }

// PutKonfiga — рабочий конфиг ядра, который пишет приложение.
func PutKonfiga() string { return filepath.Join(PapkaYadra(), "config.json") }

// PutZhurnala — журнал приложения. Его путь знают двое: тот, кто в журнал
// пишет, и окно, которое даёт человеку этот журнал прислать.
func PutZhurnala() string { return filepath.Join(Papka(), "kelevra.log") }

// ZapasnayaPapkaZhurnala — куда уезжает журнал, если своя папка приложения
// недоступна (нет прав, антивирус). Знают это двое: тот, кто журнал открывает
// (cmd/kelevra/zhurnal.go), и суточная отправка журналов (internal/zhurnaly) —
// она обязана заглянуть и сюда, иначе именно те отказы, ради которых
// запасной путь и заведён, разработчику никогда не доедут.
func ZapasnayaPapkaZhurnala() string { return filepath.Join(os.TempDir(), "Kelevra") }

// PutOtmetokZhurnalov — сколько байт из каждого файла журнала уже улетело.
// Отдельный файл от nastroyki.json нарочно: отметки переписываются каждой
// отправкой и ничего не значат для человека, а настройки правит окно.
func PutOtmetokZhurnalov() string { return filepath.Join(Papka(), "otpravka_zhurnalov.json") }

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
	// RuchnoyVybor — человек САМ отказался от автомата: выключил тумблер
	// /api/avtorezhim или нажал «Отключить» при работающем автомате. Пока
	// поле ложно, кнопка «Подключиться» включает авторежим сама и дальше
	// решает клиент (заказ 27.08: нажал «подключить» — программа сама
	// определяет: дома «режим ожидания», вне дома включается). Как только
	// человек сказал «нет» — клиент больше не решает за него ничего, пока
	// тот сам не вернёт тумблер.
	//
	// Отдельное поле от Avtorezhim, а не «Avtorezhim == false»: без него
	// «ещё ни разу не выбирал» и «выбрал вручную» неотличимы, и кнопка либо
	// никогда не включала бы автомат (как было — жалоба четырежды), либо
	// включала бы его каждым нажатием, отняв у человека ручной режим
	// навсегда (регрессия #84, из-за которой развилку и завели).
	//
	// Тот же приём, что Settings.autoModeEnabled + chooseManually() на
	// телефоне: авторежим там режим по умолчанию, а ручной включается
	// отдельным осознанным действием человека.
	RuchnoyVybor bool `json:"ruchnoy_vybor,omitempty"`
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
	// PravaZaprosheny — приложение уже один раз само спросило права
	// администратора (см. internal/sluzhba.go: zaprositPravaAvtomaticheskiEsliNado)
	// при первом успешном подключении, вместо того чтобы ждать, пока человек
	// нажмёт «Включить для всех программ» сам. Указатель, а НЕ bool: это
	// единственный способ отличить «поля не было в файле вообще» (старая
	// версия, файл существовал ДО этого поля) от «явно false» (только что
	// поставленный с нуля инсталл, которого спросить ещё только предстоит) —
	// json.Unmarshal молча превращает отсутствующее bool-поле в false, и на
	// голом bool эти два случая неразличимы. Zagruzit разводит их сама: nil
	// после чтения существующего файла — доказательство, что файл старше
	// этого поля, и трогать человека всплывающим UAC без его выбора нельзя
	// (миграция ставит true, «уже спрашивали»); nil при отсутствии файла —
	// подлинно чистый инсталл, спросить НУЖНО (миграция ставит false).
	PravaZaprosheny *bool `json:"prava_zaprosheny,omitempty"`
	// Поля otpravlyat_zhurnaly здесь БОЛЬШЕ НЕТ. Оно держало тумблер
	// «Отправлять логи разработчику» в настройках; жалоба 01.09: зачем-то есть
	// кнопка не отправлять данные разработчику, хотя об этом уже говорили —
	// тем же днём тумблер убрали и на телефоне. Отправка безусловна.
	// Старый файл настроек с этим полем читается как и раньше: лишние ключи
	// json.Unmarshal молча пропускает, а Sohranit перепишет файл уже без него.
	// Ключ намеренно НЕ переиспользовать под другой смысл — на дисках у людей
	// он ещё лежит со старым значением.
	// OtpravkaZhurnalovKogda — когда отправка удалась в последний раз, unix.
	// Живёт на диске, а не в памяти процесса: приложение висит в трее
	// неделями, но и перезапускается (обновлением, UAC) — без диска каждый
	// перезапуск означал бы «сегодня ещё не слали» и повторную посылку.
	OtpravkaZhurnalovKogda int64 `json:"otpravka_zhurnalov_kogda,omitempty"`
	// TrafikBayt — сколько байт ЭТА машина прогнала через ядро за всё время.
	//
	// Зачем считать самим. Сервер видит расход только по ключу доступа целиком,
	// а под одним ключом ходят и телефон, и компьютер — по его цифре нельзя
	// сказать, кто именно скачал. Различить это может лишь само устройство,
	// поэтому итог живёт здесь и уезжает заголовком X-Device-Traffic
	// (internal/ustroystvo).
	//
	// На диске, а не в памяти процесса: приложение перезапускается обновлением
	// и UAC по нескольку раз в неделю, а расход — величина «за всё время», и
	// каждый перезапуск обнулял бы её вместе с процессом.
	TrafikBayt int64 `json:"trafik_bayt,omitempty"`
	// TrafikYadraBayt — ПОСЛЕДНЕЕ показание счётчика ядра, по которому считался
	// прирост. Своё поле рядом с итогом, потому что счётчик ядра живёт только
	// в самом ядре и обнуляется с его перезапуском: без сохранённого показания
	// первый же опрос после нашего рестарта при ЖИВОМ ядре засчитал бы весь его
	// накопленный итог второй раз (двойной счёт), а сравнить не с чем.
	TrafikYadraBayt int64 `json:"trafik_yadra_bayt,omitempty"`
}

// TrafikUstroystva — накопленный расход этой машины, байт.
//
// Под тем же zamok, что и остальные поля Nastroyki: значение читает поток
// HTTP-запроса к подписке, а пишет фоновый счётчик — см. UzheSprosiliPrava,
// там же про то, почему замок один на всю структуру.
func (n *Nastroyki) TrafikUstroystva() int64 {
	zamok.Lock()
	defer zamok.Unlock()
	return n.TrafikBayt
}

// UchestPokazanieYadra прибавляет к итогу прирост с прошлого опроса и
// возвращает новый итог. pokazanie — накопительный счётчик ядра (вверх+вниз).
//
// Развилка тут одна и вся суть в ней: показание МЕНЬШЕ прошлого значит, что
// ядро перезапустилось и считает с нуля, — тогда прирост это всё показание
// целиком, а не разность (она была бы отрицательной и съела бы уже
// посчитанное). Итог обязан только расти: сервер верит устройству на слово и
// не проверяет цифру на монотонность, поэтому проверять её здесь.
func (n *Nastroyki) UchestPokazanieYadra(pokazanie int64) int64 {
	zamok.Lock()
	defer zamok.Unlock()
	if pokazanie < 0 { // ядро отдало бессмыслицу — не трогаем ни итог, ни отметку
		return n.TrafikBayt
	}
	prirost := pokazanie - n.TrafikYadraBayt
	if prirost < 0 {
		prirost = pokazanie
	}
	n.TrafikBayt += prirost
	n.TrafikYadraBayt = pokazanie
	return n.TrafikBayt
}

// OtmetitOtpravkuZhurnalov запоминает момент удавшейся отправки.
func (n *Nastroyki) OtmetitOtpravkuZhurnalov(kogda int64) {
	zamok.Lock()
	defer zamok.Unlock()
	n.OtpravkaZhurnalovKogda = kogda
}

// KogdaOtpravlyaliZhurnaly — 0, если не отправляли ни разу.
func (n *Nastroyki) KogdaOtpravlyaliZhurnaly() int64 {
	zamok.Lock()
	defer zamok.Unlock()
	return n.OtpravkaZhurnalovKogda
}

// UzheSprosiliPrava читает PravaZaprosheny без риска разыменовать nil.
// nil — то же, что и false, но такого после Zagruzit не остаётся: миграция
// разводит nil на true (старый файл) или false (чистый инсталл) сама. Метод
// нужен, чтобы вызывающий код не разыменовывал указатель сам.
//
// Берёт тот же zamok, что Zagruzit/Sohranit: указатель PravaZaprosheny читает
// HTTP-обработчик sostoyanie, а пишет фоновая горутина
// internal/sluzhba.zaprositPravaAvtomaticheskiEsliNado — без общего замка это
// гонка данных на самом указателе (go test -race валил
// TestPervoePodklyuchenieSamoSprashivaetPrava). zamok уже существует и уже
// охраняет весь Nastroyki целиком на файловых путях — переиспользуем его для
// поля, а не заводим второй мьютекс: будущим полям Nastroyki с тем же риском
// достаточно писать через такой же метод-сеттер.
func (n *Nastroyki) UzheSprosiliPrava() bool {
	zamok.Lock()
	defer zamok.Unlock()
	return n.PravaZaprosheny != nil && *n.PravaZaprosheny
}

// OtmetitPravaZaprosheny помечает «права уже спрашивали» — и то, что человек
// согласился, и то, что отказал: поле значит сам факт вопроса, а не его
// исход (Prava/prava.Est() отдельно отвечает на «есть ли права сейчас»).
// Пишет под тем же zamok, что и чтение в UzheSprosiliPrava — см. комментарий
// там же.
func (n *Nastroyki) OtmetitPravaZaprosheny() {
	zamok.Lock()
	defer zamok.Unlock()
	zaprosheno := true
	n.PravaZaprosheny = &zaprosheno
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
			// Файла не было вовсе — подлинно чистый инсталл. Спросить права
			// самим при первом подключении НУЖНО, попап никого не пугает:
			// человек только что поставил приложение и ждёт от него дела.
			n.DeviceID = novyyID()
			zaprosheno := false
			n.PravaZaprosheny = &zaprosheno
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
	if n.PravaZaprosheny == nil {
		// Файл существовал ДО появления этого поля — то есть человек уже
		// поставил и запускал более старую версию приложения. Считаем, что
		// его уже «спрашивали» (на самом деле нет, но и заново спрашивать
		// нельзя): внезапный UAC-попап на ровном месте на существующей
		// установке выглядит как malware, а не как забота.
		uzhe := true
		n.PravaZaprosheny = &uzhe
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
