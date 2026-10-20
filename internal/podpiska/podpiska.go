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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/ustroystvo"
)

// Host — сервер подписок Kelevra.
const Host = "subkv.chickenkiller.com"

// Puti — известные префиксы кода доступа, в порядке предпочтения.
var Puti = []string{"k", "s"}

// Dveri — адреса выдачи «из коробки», в том же порядке, что dveri.json у телефона:
// старый сервер семьи и выдача своей сети (мозг). Сервер может назвать свои в /info.
var Dveri = []string{"subkv.chickenkiller.com", "node2kv.chickenkiller.com"}

// Versiya подставляется при сборке (-ldflags "-X ...Versiya=…").
var Versiya = "0.1.0-rabota"

// Klient — с кем и как разговариваем. Пустой Klient работоспособен.
type Klient struct {
	Host     string
	Shema    string // "https" в бою; "http" оставлен только для проверок на своём стенде
	HTTP     *http.Client
	DeviceID string
	// Pasport — чем устройство описывает себя сверх имени и версии: права,
	// режим, возраст профиля, суммы файлов. Функцией по той же причине, что и
	// Trafik: клиент живёт всё время работы приложения, а режим человек может
	// переключить в любую минуту, и снимок, сделанный при сборке, уехал бы
	// врать. nil — законно, заголовок тогда не ставится.
	Pasport func() string

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
	return k.zaprosMetodom(ctx, http.MethodGet, url)
}

func (k *Klient) zaprosMetodom(ctx context.Context, metod, url string) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, metod, url, nil)
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
	// Шестой заголовок: паспорт устройства одной строкой (см. ZagolovokPasporta).
	ustroystvo.ZagolovokPasporta(r.Header, k.pasport())
	return r, nil
}

// pasport — описание себя, если его есть кому назвать. Тот же приём, что trafik.
func (k *Klient) pasport() string {
	if k.Pasport == nil {
		return ""
	}
	return k.Pasport()
}

// OshibkaKoda — сервер ответил, но код доступа он не знает.
type OshibkaKoda struct{ Kod int }

func (e *OshibkaKoda) Error() string {
	return fmt.Sprintf("сервер не знает такой код доступа (ответ %d)", e.Kod)
}

// Konfig качает конфиг ядра по коду доступа.
// Возвращает сырой JSON — ровно то, что нужно скормить sing-box.
func (k *Klient) Konfig(ctx context.Context, kod string) ([]byte, error) {
	telo, _, err := k.konfigNaDveri(ctx, k.host(), kod)
	return telo, err
}

// konfigNaDveri — Konfig у конкретной двери; второе значение — адрес, по которому профиль отдали.
func (k *Klient) konfigNaDveri(ctx context.Context, dver, kod string) ([]byte, string, error) {
	kod = strings.TrimSpace(kod)
	if kod == "" {
		return nil, "", fmt.Errorf("пустой код доступа")
	}
	var posledn error
	for _, p := range Puti {
		adres := fmt.Sprintf("%s://%s/%s/%s", k.shema(), dver, p, kod)
		telo, status, err := k.vzyat(ctx, adres)
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
		return telo, adres, nil
	}
	return nil, "", posledn
}

// KonfigPoAdresu качает конфиг по сохранённому адресу подписки (см. RazobratSsylku, Voyti).
func (k *Klient) KonfigPoAdresu(ctx context.Context, adres string) ([]byte, error) {
	telo, status, err := k.vzyat(ctx, adres)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound || status == http.StatusForbidden {
		return nil, &OshibkaKoda{Kod: status}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("сервер ответил %d", status)
	}
	if err := ProverKonfig(telo); err != nil {
		return nil, err
	}
	return telo, nil
}

// ssylka — адрес подписки: https://<дверь>/k/<код>, /s/<код> или /u/<ключ устройства>.
var ssylka = regexp.MustCompile(`^https?://([A-Za-z0-9.-]+(?::[0-9]+)?)/(k|s|u)/([A-Za-z0-9_-]{4,64})/?$`)

// RazobratSsylku: ввод человека — ссылка на подписку? Да — адрес в виде, в котором его
// хранить (без хвоста /info, параметров и пробелов), и последняя часть (код или ключ) для
// маски в окне. Нет — ok=false, и ввод считается кодом доступа.
func RazobratSsylku(vvod string) (adres, hvost string, ok bool) {
	v := strings.TrimSpace(vvod)
	if u, err := url.Parse(v); err == nil && u.Scheme != "" {
		u.RawQuery, u.Fragment = "", ""
		v = strings.TrimSuffix(strings.TrimSuffix(u.String(), "/"), "/info")
	}
	m := ssylka.FindStringSubmatch(v)
	if m == nil {
		return "", "", false
	}
	return strings.TrimSuffix(v, "/"), m[3], true
}

// OshibkaVhoda — дверь узнала код, но в устройство отказала (лимит устройств, доступ
// закрыт). Tekst — слова сервера для человека.
type OshibkaVhoda struct{ Tekst string }

func (e *OshibkaVhoda) Error() string { return e.Tekst }

// otvetVhoda — ответ POST /k/<код>/ustroystvo (KelevraServer docs/PRIGLASHENIE.md).
type otvetVhoda struct {
	Profil string `json:"profil"`
	Slot   string `json:"slot"`
	Tekst  string `json:"tekst"`
}

// Voyti — вход по коду, как у телефона (Priglashenie.vybratDver): двери по порядку,
// у каждой сперва вход устройства (POST /k/<код>/ustroystvo — дверь своей сети выдаёт
// ключ этой машины и адрес /u/<ключ>), а если дверь такого входа не знает (404/405) —
// старый путь /k/<код>. Возвращает адрес подписки, который сохранять.
func (k *Klient) Voyti(ctx context.Context, kod string, dveri []string) (string, error) {
	kod = strings.TrimSpace(kod)
	if kod == "" {
		return "", fmt.Errorf("пустой код доступа")
	}
	var posledn error
	neTolko404 := false
	for _, dver := range dveri {
		dver = strings.TrimSpace(dver)
		if dver == "" {
			continue
		}
		req, err := k.zaprosMetodom(ctx, http.MethodPost, fmt.Sprintf("%s://%s/k/%s/ustroystvo", k.shema(), dver, kod))
		if err != nil {
			return "", err
		}
		otvet, err := k.http().Do(req)
		if err != nil {
			posledn, neTolko404 = SkrytKodVOshibke(err), true
			continue
		}
		telo, _ := io.ReadAll(io.LimitReader(otvet.Body, 64<<10))
		otvet.Body.Close()
		var o otvetVhoda
		_ = json.Unmarshal(telo, &o)
		switch {
		case otvet.StatusCode == http.StatusOK && o.Profil != "":
			return o.Profil, nil
		case otvet.StatusCode == http.StatusNotFound || otvet.StatusCode == http.StatusMethodNotAllowed:
			// Входа устройства тут нет или код ему незнаком — спросим старым путём.
			_, adres, err := k.konfigNaDveri(ctx, dver, kod)
			if err == nil {
				return adres, nil
			}
			var nezn *OshibkaKoda
			if !errors.As(err, &nezn) {
				neTolko404 = true
			}
			posledn = err
		case o.Tekst != "":
			return "", &OshibkaVhoda{Tekst: o.Tekst}
		default:
			posledn, neTolko404 = fmt.Errorf("дверь %s ответила %d", dver, otvet.StatusCode), true
		}
	}
	if posledn == nil {
		return "", fmt.Errorf("нет ни одной двери")
	}
	if !neTolko404 {
		return "", &OshibkaKoda{Kod: http.StatusNotFound}
	}
	return "", posledn
}

// Svedeniya — что сервер рассказывает о подписке.
type Svedeniya struct {
	Imya      string `json:"name"`
	Aktivna   bool   `json:"active"`
	Do        int64  `json:"expires"`
	LimitBayt int64  `json:"limit_bytes"`
	SyedenoB  int64  `json:"used_bytes"`
	Otvet     string `json:"reply"`
	// ZhurnalSrochno — сервер просит прислать журнал СЕЙЧАС, не дожидаясь
	// ночной посылки.
	//
	// Зачем поле, а не «попросить человека». Журнал уезжает раз в сутки, и
	// дважды подряд разбор живой беды упирался в это: 08.09 у двух человек
	// семьи связь легла днём, а факты появились бы только ночью. Просить
	// человека скопировать журнал из окна — значит дёргать того, кто и так
	// сидит без интернета посреди рабочего дня; прямо сказано 08.09.
	//
	// Клиент ходит за этой сводкой сам, раз в час и при каждом открытии окна,
	// поэтому просьба доезжает без всякого участия человека.
	ZhurnalSrochno bool `json:"send_log_now,omitempty"`
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
	// Dveri — адреса выдачи, которые сервер советует для входа по коду (своя сеть).
	Dveri []string `json:"dveri,omitempty"`
	// Panel — эта машина принадлежит админу своей сети: в окне есть кнопка «Панель сети».
	Panel bool `json:"panel,omitempty"`
}

// PropuskPaneli — одноразовая ссылка в панель своей сети для этой машины: ключ устройства из
// адреса подписки (/u/<ключ>) предъявляется мозгу. Панель только у подписки, полученной входом по коду.
func (k *Klient) PropuskPaneli(ctx context.Context, adres string) (string, error) {
	u, err := url.Parse(adres)
	chasti := strings.Split(strings.Trim(u.Path, "/"), "/")
	if err != nil || u.Host == "" || len(chasti) != 2 || chasti[0] != "u" {
		return "", fmt.Errorf("Панель доступна только для подписки, полученной входом по коду")
	}
	req, err := k.zaprosMetodom(ctx, http.MethodPost, fmt.Sprintf("%s://%s/panel/api/v1/vhod/ustroystvo", k.shema(), u.Host))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Device-Key", chasti[1])
	otvet, err := k.http().Do(req)
	if err != nil {
		return "", fmt.Errorf("Нет связи с панелью: %w", err)
	}
	defer otvet.Body.Close()
	var o struct {
		Ssylka  string `json:"ssylka"`
		Oshibka string `json:"oshibka"`
	}
	_ = json.NewDecoder(io.LimitReader(otvet.Body, 64<<10)).Decode(&o)
	switch {
	case otvet.StatusCode == http.StatusOK && strings.HasPrefix(o.Ssylka, "https://"):
		return o.Ssylka, nil
	case otvet.StatusCode == http.StatusForbidden:
		return "", fmt.Errorf("Нет доступа к панели с этого устройства")
	case otvet.StatusCode >= 500:
		return "", fmt.Errorf("Панель недоступна")
	case o.Oshibka != "":
		return "", fmt.Errorf("%s", o.Oshibka)
	}
	return "", fmt.Errorf("Ошибка запроса · %d", otvet.StatusCode)
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

// kodVAdrese — путь вида /k/<код>, /s/<код>, /u/<ключ> внутри строки (адрес целиком или
// текст ошибки). Захватывает префикс, код и то, что за ним.
var kodVAdrese = regexp.MustCompile(`(/(?:k|s|u)/)([A-Za-z0-9_-]{4,64})([/?"\s:]|$)`)

// SkrytKod прячет код доступа (и ключ устройства) в произвольной строке — адресе
// или тексте ошибки — тем же видом, что Maska. Нужна везде, где адрес с кодом может
// попасть в журнал: журнал уезжает на сервер, а в нём без этого лежал бы рабочий код
// целиком («вход по коду не удался: Post https://…/k/<КОД>/ustroystvo»).
func SkrytKod(tekst string) string {
	return kodVAdrese.ReplaceAllStringFunc(tekst, func(m string) string {
		g := kodVAdrese.FindStringSubmatch(m)
		return g[1] + Maska(g[2]) + g[3]
	})
}

// SkrytKodVOshibke — ошибка сети (*url.Error) носит адрес с кодом в тексте; отдаёт
// ту же ошибку с замаскированным адресом, цепочка errors.Is/As сохраняется.
// Прочие ошибки возвращаются как есть.
func SkrytKodVOshibke(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return &url.Error{Op: ue.Op, URL: SkrytKod(ue.URL), Err: ue.Err}
	}
	return err
}

// SvedeniyaPoAdresu — то же, что Svedeniya, по сохранённому адресу подписки (+ /info).
func (k *Klient) SvedeniyaPoAdresu(ctx context.Context, adres string) (*Svedeniya, error) {
	u := strings.TrimSuffix(adres, "/") + "/info"
	if k.DeviceID != "" {
		u += "?device=" + k.DeviceID
	}
	telo, status, err := k.vzyat(ctx, u)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("сервер ответил %d", status)
	}
	s := &Svedeniya{}
	if err := json.Unmarshal(telo, s); err != nil {
		return nil, fmt.Errorf("сведения о подписке нечитаемы: %w", err)
	}
	return s, nil
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
		return nil, 0, SkrytKodVOshibke(err)
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
