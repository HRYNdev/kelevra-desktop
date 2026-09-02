// Пакет podpiska: разговор с сервером Kelevra по коду доступа.
//
// Тот же протокол, что у мобильного клиента (kelevra-box):
//
//	GET https://<хост>/k/<код>       -> готовый config.json для sing-box
//	GET https://<хост>/k/<код>/info  -> сведения о подписке (срок, трафик)
//
// Мобильный ходит на /k/, десктопный форк ходил на /s/ — поэтому пробуем оба
// пути по очереди, пока сервер не ответит.
package podpiska

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/ustroystvo"
)

// Host — сервер подписок Kelevra.
const Host = "subkv.chickenkiller.com"

// Puti — известные префиксы кода доступа, в порядке предпочтения.
var Puti = []string{"k", "s"}

// Versiya подставляется при сборке (-ldflags "-X ...Versiya=…").
var Versiya = "0.1.0-rabota"

// Klient — с кем и как разговариваем. Пустой Klient работоспособен.
type Klient struct {
	Host     string
	Shema    string // "https" в бою; "http" оставлен только для проверок на своём стенде
	HTTP     *http.Client
	DeviceID string
	// Trafik — сколько байт эта машина прогнала через ядро за всё время.
	// Функцией, а не числом: клиент живёт всё время работы приложения, а
	// расход растёт каждую минуту — записанное при сборке число уехало бы на
	// сервер устаревшим на неделю. nil — законно (стенды, ранний старт до
	// первого замера): заголовок тогда просто не ставится.
	Trafik func() int64
}

// trafik — расход, если его есть кому назвать. Отдельный метод, чтобы место
// вызова не разыменовывало nil (тот же приём, что shema/host/http выше).
func (k *Klient) trafik() int64 {
	if k.Trafik == nil {
		return 0
	}
	return k.Trafik()
}

func (k *Klient) shema() string {
	if k.Shema != "" {
		return k.Shema
	}
	return "https"
}

func (k *Klient) host() string {
	if k.Host != "" {
		return k.Host
	}
	return Host
}

func (k *Klient) http() *http.Client {
	if k.HTTP != nil {
		return k.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (k *Klient) zapros(ctx context.Context, url string) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("User-Agent", "Kelevra-Desktop/"+Versiya+" (windows)")
	// x-hwid/x-device-os остаются: сервер понимает их как запасной путь, а
	// снести их отсюда значит на день-другой (пока обновление доедет до всех
	// копий) оставить старые клиенты вовсе безымянными.
	if k.DeviceID != "" {
		r.Header.Set("x-hwid", k.DeviceID)
		r.Header.Set("x-device-os", "windows")
	}
	// Четыре заголовка, которыми устройство называет себя — ровно те же, что
	// уже шлёт android-клиент (X-Device-Id / X-Device-Model / X-Device-Platform /
	// X-App-Version). Через zapros идут ОБА запроса к серверу (Konfig и
	// Svedeniya), поэтому точка правки тут одна.
	// X-Device-Traffic — пятый заголовок: расход ЭТОЙ машины. Сервер считает
	// трафик по ключу доступа целиком и различить телефон с компьютером не
	// может — цифру даёт устройство.
	ustroystvo.ZagolovkiSTrafikom(r.Header, k.DeviceID, Versiya, k.trafik())
	return r, nil
}

// OshibkaKoda — сервер ответил, но код доступа он не знает.
type OshibkaKoda struct{ Kod int }

func (e *OshibkaKoda) Error() string {
	return fmt.Sprintf("сервер не знает такой код доступа (ответ %d)", e.Kod)
}

// Konfig качает конфиг ядра по коду доступа.
// Возвращает сырой JSON — ровно то, что нужно скормить sing-box.
func (k *Klient) Konfig(ctx context.Context, kod string) ([]byte, error) {
	kod = strings.TrimSpace(kod)
	if kod == "" {
		return nil, fmt.Errorf("пустой код доступа")
	}
	var posledn error
	for _, p := range Puti {
		telo, status, err := k.vzyat(ctx, fmt.Sprintf("%s://%s/%s/%s", k.shema(), k.host(), p, kod))
		if err != nil {
			posledn = err
			continue
		}
		if status == http.StatusNotFound || status == http.StatusForbidden {
			posledn = &OshibkaKoda{Kod: status}
			continue // может быть просто не тот префикс пути
		}
		if status != http.StatusOK {
			posledn = fmt.Errorf("сервер ответил %d", status)
			continue
		}
		if err := ProverKonfig(telo); err != nil {
			posledn = err
			continue
		}
		return telo, nil
	}
	return nil, posledn
}

// Svedeniya — что сервер рассказывает о подписке.
type Svedeniya struct {
	Imya      string `json:"name"`
	Aktivna   bool   `json:"active"`
	Do        int64  `json:"expires"`
	LimitBayt int64  `json:"limit_bytes"`
	SyedenoB  int64  `json:"used_bytes"`
	Otvet     string `json:"reply"`
	// Chelovek и Ustroystvo сервер добавил в /info после того, как клиент
	// научился называть себя (см. zapros выше): по X-Device-Id он знает, чей
	// это ключ и какая именно машина сейчас спрашивает, и отдаёт оба имени
	// готовыми. Указатели, а не значения: СТАРЫЙ сервер этих полей не шлёт
	// вовсе, и nil — законный ответ, окно тогда просто не рисует строку.
	Chelovek   *Imenovannyy `json:"person,omitempty"`
	Ustroystvo *Imenovannyy `json:"device,omitempty"`
	// Ustroystva — ВСЕ устройства этого ключа доступа, не только текущее.
	// Сервер собирает список из своего реестра и помечает наше флагом Svoyo.
	// Старый сервер поля не шлёт — пустой срез законен, окно тогда рисует
	// одну строку про текущую машину, как и раньше.
	Ustroystva []Ustroystvo `json:"devices,omitempty"`
}

// Ustroystvo — одна строка списка «кто ещё ходит под этим кодом».
//
// Дат сервер шлёт две и обе в ISO-8601 с зоной. Держим их строками ровно
// такими, какими пришли, а наружу отдаём unix (см. методы ниже): окну нужно
// считать «был два часа назад», а не разбирать формат, и заводить ради этого
// свой тип времени в JSON-ответе службы — лишний слой на пустом месте.
type Ustroystvo struct {
	Svoyo      bool   `json:"self"`
	Imya       string `json:"name"`
	Vid        string `json:"kind"` // phone | laptop | desktop — для значка
	Platforma  string `json:"platform"`
	Versiya    string `json:"app_version"`
	Vpervye    string `json:"first_seen"`
	Poslednee  string `json:"last_seen"`
	TrafikBayt int64  `json:"traffic_bytes"`
}

// vremyaISO разбирает время сервера. Пустая или битая строка — ноль: строка
// «был тогда-то» тогда просто не рисуется, и это лучше выдуманной даты.
func vremyaISO(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	for _, shablon := range []string{time.RFC3339, "2006-01-02T15:04:05Z0700", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(shablon, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}

// PosledneeUnix и VpervyeUnix — те же две даты, но числом для окна.
func (u Ustroystvo) PosledneeUnix() int64 { return vremyaISO(u.Poslednee) }
func (u Ustroystvo) VpervyeUnix() int64   { return vremyaISO(u.Vpervye) }

// Imenovannyy — общая форма для person и device: у обоих сервер отдаёт
// единственное поле name.
type Imenovannyy struct {
	Imya string `json:"name"`
}

// ImyaCheloveka и ImyaUstroystva читают имена, не разыменовывая nil: полей
// может не быть (старый сервер), и вызывающему коду это знать незачем.
func (s *Svedeniya) ImyaCheloveka() string {
	if s == nil || s.Chelovek == nil {
		return ""
	}
	return strings.TrimSpace(s.Chelovek.Imya)
}

func (s *Svedeniya) ImyaUstroystva() string {
	if s == nil || s.Ustroystvo == nil {
		return ""
	}
	return strings.TrimSpace(s.Ustroystvo.Imya)
}

// Maska — код доступа в виде, который МОЖНО показать на экране: три звёздочки
// и последние два знака. Ровно та же маска, что на телефоне (Kelevra.kt,
// maskCode) — человек, сверяя окно с телефоном, обязан видеть одно и то же.
//
// Зачем маска вообще. Вкладка «Подписка» должна отвечать на вопрос «а этот ли
// ключ у меня стоит», и двух последних знаков для этого хватает: свой ключ
// человек узнаёт, а чужой по ним не соберёшь. Открытым текстом код в окне не
// показывается нигде — окно висит на экране, его снимают на телефон и шлют
// мне в поддержку, и вместе со снимком уехал бы рабочий доступ к подписке.
//
// Код короче трёх знаков маскировать нечем — отдаём звёздочки без хвоста, а
// не сам код: пусть лучше строка бесполезна, чем выдаёт ключ целиком.
func Maska(kod string) string {
	kod = strings.TrimSpace(kod)
	if kod == "" {
		return ""
	}
	znaki := []rune(kod)
	if len(znaki) < 3 {
		return "***"
	}
	return "***" + string(znaki[len(znaki)-2:])
}

// Svedeniya спрашивает состояние подписки. Ошибка здесь не мешает подключению.
func (k *Klient) Svedeniya(ctx context.Context, kod string) (*Svedeniya, error) {
	kod = strings.TrimSpace(kod)
	if kod == "" {
		return nil, fmt.Errorf("пустой код доступа")
	}
	var posledn error
	for _, p := range Puti {
		url := fmt.Sprintf("%s://%s/%s/%s/info", k.shema(), k.host(), p, kod)
		if k.DeviceID != "" {
			url += "?device=" + k.DeviceID
		}
		telo, status, err := k.vzyat(ctx, url)
		if err != nil {
			posledn = err
			continue
		}
		if status != http.StatusOK {
			posledn = fmt.Errorf("сервер ответил %d", status)
			continue
		}
		s := &Svedeniya{}
		if err := json.Unmarshal(telo, s); err != nil {
			posledn = fmt.Errorf("сведения о подписке нечитаемы: %w", err)
			continue
		}
		return s, nil
	}
	return nil, posledn
}

func (k *Klient) vzyat(ctx context.Context, url string) ([]byte, int, error) {
	req, err := k.zapros(ctx, url)
	if err != nil {
		return nil, 0, err
	}
	otvet, err := k.http().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer otvet.Body.Close()
	// Потолок в 8 МБ: конфиг заведомо меньше, а битый сервер не должен съесть память.
	telo, err := io.ReadAll(io.LimitReader(otvet.Body, 8<<20))
	if err != nil {
		return nil, otvet.StatusCode, err
	}
	return telo, otvet.StatusCode, nil
}

// ProverKonfig — дешёвая проверка, что нам вернули конфиг ядра, а не страницу с ошибкой.
// Настоящую проверку делает само ядро при запуске.
func ProverKonfig(telo []byte) error {
	var v map[string]any
	if err := json.Unmarshal(telo, &v); err != nil {
		return fmt.Errorf("ответ сервера не похож на конфиг: %w", err)
	}
	if _, есть := v["outbounds"]; !есть {
		return fmt.Errorf("в ответе сервера нет ни одного узла (outbounds)")
	}
	return nil
}
