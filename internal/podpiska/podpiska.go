// Пакет podpiska: разговор с сервером Kelevra по коду доступа.
//
// Тот же протокол, что у мобильного клиента (kelevra-box):
//
//	POST https://<дверь>/k/<код>/ustroystvo -> {"profil": "https://<дверь>/u/<ключ>"} (Voyti)
//	GET  https://<дверь>/u/<ключ>           -> готовый config.json для sing-box
//	GET  https://<дверь>/u/<ключ>/info      -> сведения о подписке (срок, трафик)
//
// Старый путь по общему коду — GET /k/<код> и /k/<код>/info: им ходят двери без
// входа устройства и установки, вошедшие до него. Мобильный ходит на /k/,
// десктопный форк ходил на /s/ — поэтому старым путём пробуем оба префикса.
package podpiska

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
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

	// Pult — значение X-Pult (internal/pult): что клиент понял из пульта сервера.
	// Sostoyanie — значение X-Sostoyanie (пульс). Функциями по той же причине,
	// что Pasport. nil — заголовка нет.
	Pult       func() string
	Sostoyanie func() string
	// YadroTeg — тег сборки стоящего ядра (core-*), едет в User-Agent: ядро
	// десктопа меняется отдельно от приложения, и только по нему сервер знает,
	// что ядро шлёт в Reality (например, X25519MLKEM768). nil или пусто —
	// User-Agent прежний.
	YadroTeg func() string
	// Сроки входа устройства; ноль — SrokSoedineniyaVhoda, SrokOtvetaVhoda,
	// SrokPervogoProhoda (поля — для проверок).
	SrokSoedineniyaVhoda time.Duration
	SrokOtvetaVhoda      time.Duration
	SrokPervogoProhoda   time.Duration
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

// tegYadraVAgent — тег сборки ядра, который можно положить в User-Agent как есть.
var tegYadraVAgent = regexp.MustCompile(`^core-v[0-9A-Za-z.\-]{1,60}$`)

// agent — User-Agent запросов к серверу: "Kelevra-Desktop/<версия> (windows)",
// со стоящим ядром — "Kelevra-Desktop/<версия> (windows; core-v1.14.1-7.1)".
// Тег не по образцу core.yml не шлём: заголовок однострочный ASCII.
func agent(versiya string, yadroTeg func() string) string {
	ua := "Kelevra-Desktop/" + versiya + " (windows"
	if yadroTeg != nil {
		if t := yadroTeg(); tegYadraVAgent.MatchString(t) {
			ua += "; " + t
		}
	}
	return ua + ")"
}

func (k *Klient) zapros(ctx context.Context, url string) (*http.Request, error) {
	return k.zaprosMetodom(ctx, http.MethodGet, url)
}

func (k *Klient) zaprosMetodom(ctx context.Context, metod, url string) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, metod, url, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("User-Agent", agent(Versiya, k.YadroTeg))
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
	// Седьмой и восьмой: пульт и пульс (договор KelevraServer docs/PULT.md).
	if k.Pult != nil {
		ustroystvo.ZagolovokPulta(r.Header, k.Pult())
	}
	if k.Sostoyanie != nil {
		ustroystvo.ZagolovokSostoyaniya(r.Header, k.Sostoyanie())
	}
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

// kodDopustim — код доступа или ключ в том виде, в каком он стоит в пути адреса (сервер
// принимает ровно этот набор знаков и длину, см. KelevraServer server/app.py).
var kodDopustim = regexp.MustCompile(`^[A-Za-z0-9_-]{4,64}$`)

// ChistyyKod — код из ввода человека: без пробелов где угодно (код показывают группами
// «K7QM 2XRT 9PLA», так его и перепечатывают). Регистр и дефисы не трогаются: старые коды
// к регистру чувствительны, а новые сервер сам сводит к одному виду.
func ChistyyKod(vvod string) string {
	return strings.Join(strings.Fields(vvod), "")
}

// Сроки входа устройства (первый проход Voyti). Вход по коду в службе ограничен 30 с
// целиком (sluzhba.kod), и первый проход не должен съедать их, когда одна из дверей
// недоступна или режется по дороге: человек старой системы иначе ждал бы, пока
// истечёт 20-секундный срок клиента на чужой двери, а с двумя такими дверями вход
// падал бы по сроку, хотя его дверь отвечает за секунду.
const (
	// SrokSoedineniyaVhoda — сколько ждать соединения с дверью (TCP и TLS). Дверь,
	// которую режут или которая лежит, отсекается здесь.
	SrokSoedineniyaVhoda = 5 * time.Second
	// SrokOtvetaVhoda — сколько ждать ответа двери, с которой соединились: заведение
	// устройства на сервере включает выгрузку учётки в ядро и бывает небыстрым.
	SrokOtvetaVhoda = 10 * time.Second
	// SrokPervogoProhoda — весь первый проход по всем дверям. Остаток из 30 с —
	// второму проходу (старый путь).
	SrokPervogoProhoda = 12 * time.Second
)

func srokIli(d, poUmolchaniyu time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return poUmolchaniyu
}

// Voyti — вход по коду (KelevraServer docs/PRIGLASHENIE.md, образец — телефон,
// Priglashenie.vybratDver). Возвращает адрес подписки, который сохранять.
//
// Два прохода. Первый — вход устройства у дверей по порядку: POST /k/<код>/ustroystvo,
// дверь своей сети заводит эту машину и выдаёт ей свой ключ, адрес /u/<ключ>. Только если
// ни одна дверь машину так не завела, второй проход — старый путь /k/<код>: сперва у
// дверей, ответивших 404/405 (нет такого входа или код незнаком), потом у тех, до кого
// первый проход не дошёл за свой срок, и последними — у не ответивших (их могли резать
// только для входа или только напрямую — старый путь идёт полным сроком и через
// туннель, если он поднят).
//
// Почему не по одной двери, как телефон. Старый сервер семьи пересылает старый путь
// переехавшего человека в свою сеть: при обходе «дверь за дверью» первая же дверь отдала
// бы ему общий /k/<код>, и машина жила бы мимо ключа устройства — без лимита устройств и
// без отключения по одному. Человек старой системы второй дверью не узнан (404) и
// остаётся там же, где был.
//
// Почему первый проход по очереди, а не ко всем дверям сразу. Двери одной сети заводят
// устройство в одном мозге, и каждое заведение выдаёт новый ключ: два одновременных
// входа оставили бы машине ключ, который уже заменён. Время первого прохода держат
// сроки выше: недоступная дверь стоит до SrokSoedineniyaVhoda, весь проход — не дольше
// SrokPervogoProhoda.
func (k *Klient) Voyti(ctx context.Context, kod string, dveri []string) (string, error) {
	kod = ChistyyKod(kod)
	if kod == "" {
		return "", fmt.Errorf("пустой код доступа")
	}
	if !kodDopustim.MatchString(kod) {
		return "", fmt.Errorf("Код доступа содержит недопустимые символы")
	}
	var posledn error
	neTolko404 := false
	var stariePuti, neSprosheny, molchali []string
	ctx1, otmena1 := context.WithTimeout(ctx, srokIli(k.SrokPervogoProhoda, SrokPervogoProhoda))
	defer otmena1()
	for _, dver := range dveri {
		dver = strings.TrimSpace(dver)
		if dver == "" {
			continue
		}
		if ctx1.Err() != nil {
			// Срок первого прохода вышел — оставшимся дверям только старый путь.
			neSprosheny = append(neSprosheny, dver)
			continue
		}
		status, o, err := k.vhodUstroystva(ctx1, k.shema(), dver, kod)
		if err != nil {
			if status < 0 {
				return "", err
			}
			posledn, neTolko404 = err, true
			molchali = append(molchali, dver)
			continue
		}
		switch {
		case status == http.StatusOK:
			if k.adresKlyuchaGoden(o.Profil) {
				return strings.TrimSuffix(o.Profil, "/"), nil
			}
			// 200 без адреса /u/<ключ> — сбой двери, а не вход: такой адрес не сохраняем.
			posledn, neTolko404 = fmt.Errorf("дверь %s не выдала адрес устройства", dver), true
		case status == http.StatusNotFound || status == http.StatusMethodNotAllowed:
			stariePuti = append(stariePuti, dver)
		case o.Tekst != "":
			return "", &OshibkaVhoda{Tekst: o.Tekst}
		default:
			posledn, neTolko404 = fmt.Errorf("дверь %s ответила %d", dver, status), true
		}
	}
	// Порядок второго прохода: ответившие 404/405, затем не спрошенные за срок первого
	// прохода, и последними — не ответившие (вероятнее всего, лежат и сейчас).
	for _, dver := range append(append(stariePuti, neSprosheny...), molchali...) {
		if ctx.Err() != nil {
			break
		}
		// Входа устройства тут нет, код ему незнаком или дверь не ответила — спросим старым путём.
		_, adres, err := k.konfigNaDveri(ctx, dver, kod)
		if err == nil {
			return adres, nil
		}
		var nezn *OshibkaKoda
		if !errors.As(err, &nezn) {
			neTolko404 = true
		}
		posledn = err
	}
	if posledn == nil {
		return "", fmt.Errorf("нет ни одной двери")
	}
	if !neTolko404 {
		return "", &OshibkaKoda{Kod: http.StatusNotFound}
	}
	return "", posledn
}

// vhodUstroystva — POST /k/<код>/ustroystvo на одной двери (вход устройства
// своей сети). status < 0 с ошибкой — запрос не собрать; 0 с ошибкой — сеть
// или срок: соединения нет за SrokSoedineniyaVhoda — дверь не ждём; соединились —
// ответ ждём до SrokOtvetaVhoda.
func (k *Klient) vhodUstroystva(ctx context.Context, shema, dver, kod string) (int, otvetVhoda, error) {
	var o otvetVhoda
	ctx, otmena := context.WithTimeout(ctx, srokIli(k.SrokOtvetaVhoda, SrokOtvetaVhoda))
	defer otmena()
	var soedineno atomic.Bool
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { soedineno.Store(true) },
	})
	taymer := time.AfterFunc(srokIli(k.SrokSoedineniyaVhoda, SrokSoedineniyaVhoda), func() {
		if !soedineno.Load() {
			otmena()
		}
	})
	defer taymer.Stop()
	req, err := k.zaprosMetodom(ctx, http.MethodPost, fmt.Sprintf("%s://%s/k/%s/ustroystvo", shema, dver, kod))
	if err != nil {
		return -1, o, SkrytKodVOshibke(err)
	}
	otvet, err := k.http().Do(req)
	if err != nil {
		return 0, o, SkrytKodVOshibke(err)
	}
	telo, _ := io.ReadAll(io.LimitReader(otvet.Body, 64<<10))
	otvet.Body.Close()
	_ = json.Unmarshal(telo, &o)
	return otvet.StatusCode, o, nil
}

// OshibkaPerehoda — команда pereydi_na_klyuch не выполнена; Kratko — слово для
// квитанции (oshibka-<Kratko>).
type OshibkaPerehoda struct {
	Kratko   string
	Prichina error
}

func (e *OshibkaPerehoda) Error() string {
	if e.Prichina != nil {
		return fmt.Sprintf("переход на свой ключ не удался (%s): %v", e.Kratko, e.Prichina)
	}
	return "переход на свой ключ не удался: " + e.Kratko
}

func (e *OshibkaPerehoda) Unwrap() error { return e.Prichina }

// PereytiNaKlyuch — команда пульта pereydi_na_klyuch: профиль по общему коду
// (/k/<код>, /s/<код>) переводится на свой ключ устройства тем же входом, что
// Voyti (POST /k/<код>/ustroystvo на той же двери), но без отката на старый
// путь. При 200 проверяет, что новый адрес отдаёт рабочий конфиг, и возвращает
// его: сохранить на месте старого — дело вызывающего. Любой отказ — ошибка, и
// старый адрес остаётся. adres — сохранённый адрес подписки; пусто — старая
// установка с кодом kod на зашитом хосте. uzhe — адрес и так на своём ключе.
func (k *Klient) PereytiNaKlyuch(ctx context.Context, adres, kod string) (novyy string, uzhe bool, err error) {
	shema, dver := k.shema(), k.host()
	if adres != "" {
		u, perr := url.Parse(adres)
		var chasti []string
		if perr == nil {
			chasti = strings.Split(strings.Trim(u.Path, "/"), "/")
		}
		if perr != nil || u.Host == "" || len(chasti) != 2 {
			return "", false, &OshibkaPerehoda{Kratko: "adres"}
		}
		if chasti[0] == "u" {
			return adres, true, nil
		}
		shema, dver, kod = u.Scheme, u.Host, chasti[1]
	}
	kod = strings.TrimSpace(kod)
	if kod == "" {
		return "", false, &OshibkaPerehoda{Kratko: "net_koda"}
	}
	status, o, err := k.vhodUstroystva(ctx, shema, dver, kod)
	switch {
	case err != nil:
		return "", false, &OshibkaPerehoda{Kratko: "set", Prichina: err}
	case status == http.StatusOK:
		// Как в Voyti: 200 без адреса /u/<ключ> — сбой двери, а не переход;
		// такой адрес на место старого не ставим.
		if !k.adresKlyuchaGoden(o.Profil) {
			return "", false, &OshibkaPerehoda{Kratko: "ne_klyuch"}
		}
	case o.Tekst != "":
		return "", false, &OshibkaPerehoda{Kratko: "otkaz", Prichina: errors.New(o.Tekst)}
	default:
		return "", false, &OshibkaPerehoda{Kratko: strconv.Itoa(status)}
	}
	novyy = strings.TrimSuffix(o.Profil, "/")
	if _, err := k.KonfigPoAdresu(ctx, novyy); err != nil {
		return "", false, &OshibkaPerehoda{Kratko: "konfig", Prichina: SkrytKodVOshibke(err)}
	}
	return novyy, false, nil
}

// adresKlyuchaGoden — ответ входа устройства несёт адрес вида https://<дверь>/u/<ключ>
// (http — только когда и сам клиент ходит по http: свой стенд).
func (k *Klient) adresKlyuchaGoden(profil string) bool {
	m := ssylka.FindStringSubmatch(strings.TrimSuffix(strings.TrimSpace(profil), "/"))
	if m == nil || m[2] != "u" {
		return false
	}
	return strings.HasPrefix(profil, "https://") || (k.shema() == "http" && strings.HasPrefix(profil, "http://"))
}

// KlyuchUstroystva — ключ устройства из адреса подписки /u/<ключ>; "" — подписка по
// общему коду (/k/, /s/) или адреса нет.
func KlyuchUstroystva(adres string) string {
	m := ssylka.FindStringSubmatch(strings.TrimSuffix(strings.TrimSpace(adres), "/"))
	if m == nil || m[2] != "u" {
		return ""
	}
	return m[3]
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
	// Pult — блок пульта (internal/pult) как пришёл; проверяет и разбирает служба.
	Pult json.RawMessage `json:"pult,omitempty"`
	// Syroe — тело ответа целиком: служба хранит его на диске (hranenie.PutSvodki).
	Syroe []byte `json:"-"`
	// ETag — заголовок ETag ответа как есть ("" — сервер не прислал). Лёгкий
	// опрос повторяет его дословно в If-None-Match (договор PULT, «Провод»).
	ETag string `json:"-"`
}

// RazobratSvedeniya — сводка /info из тела ответа (или с диска).
func RazobratSvedeniya(telo []byte) (*Svedeniya, error) {
	s := &Svedeniya{}
	if err := json.Unmarshal(telo, s); err != nil {
		return nil, fmt.Errorf("сведения о подписке нечитаемы: %w", err)
	}
	s.Syroe = telo
	return s, nil
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

// kodVAdrese — путь вида /k/<код>, /s/<код>, /u/<ключ>, /i/<приглашение>,
// /panel/vhod/<пропуск> внутри строки (адрес целиком или текст ошибки). Захватывает
// префикс, код и то, что за ним.
var kodVAdrese = regexp.MustCompile(`(/(?:k|s|u|i|panel/vhod)/)([A-Za-z0-9_-]{4,64})([/?"\s:]|$)`)

// SkrytKod прячет код доступа (и ключ устройства, пропуск в панель) в произвольной
// строке — адресе или тексте ошибки. Нужна везде, где адрес с кодом может попасть в
// журнал: журнал уезжает на сервер, а в нём без этого лежал бы рабочий код целиком
// («вход по коду не удался: Post https://…/k/<КОД>/ustroystvo»).
//
// Вид — первые 2 знака и «…». Сервер (KelevraServer server/app.py, maska_koda и
// maska_klyucha) оставляет первые 4, сборки клиента с 0.6.82 — последние 2:
// начало не длиннее серверного не добавляет к серверной строке ничего, а с прежней
// клиентской маской в одном архиве журналов устройства складывается не больше чем в 4
// знака из 12. Начало, а не конец — строку клиента можно сопоставить со строкой сервера.
// Код короче 8 знаков (настоящие — 12 и длиннее) прячется целиком. В окне человеку код
// показывает Maska — там вид общий с телефоном.
func SkrytKod(tekst string) string {
	return kodVAdrese.ReplaceAllStringFunc(tekst, func(m string) string {
		g := kodVAdrese.FindStringSubmatch(m)
		if g[1] == "/k/" && g[2] == "health" {
			return m // проверка живости сервера, не код
		}
		return g[1] + maskaZhurnala(g[2]) + g[3]
	})
}

// maskaZhurnala — код в журнале: первые 2 знака и «…» (короче 8 — только «…»).
func maskaZhurnala(kod string) string {
	if len(kod) < 8 {
		return "…"
	}
	return kod[:2] + "…"
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
	o, err := k.vzyatOtvet(ctx, u, "")
	if err != nil {
		return nil, err
	}
	if o.status != http.StatusOK {
		return nil, fmt.Errorf("сервер ответил %d", o.status)
	}
	s, err := RazobratSvedeniya(o.telo)
	if s != nil {
		s.ETag = o.etag
	}
	return s, err
}

// SvedeniyaEsliNovoe — лёгкий опрос /info (договор PULT, «Провод»): etag —
// ETag прошлого ответа /info ДОСЛОВНО, как его прислал сервер ("" — спросить
// без условия). 304 — (nil, nil): с того ответа не изменилось ничего.
func (k *Klient) SvedeniyaEsliNovoe(ctx context.Context, adres, etag string) (*Svedeniya, error) {
	u := strings.TrimSuffix(adres, "/") + "/info"
	if k.DeviceID != "" {
		u += "?device=" + k.DeviceID
	}
	o, err := k.vzyatOtvet(ctx, u, etag)
	if err != nil {
		return nil, err
	}
	if o.status == http.StatusNotModified {
		return nil, nil
	}
	if o.status != http.StatusOK {
		return nil, fmt.Errorf("сервер ответил %d", o.status)
	}
	s, err := RazobratSvedeniya(o.telo)
	if s != nil {
		s.ETag = o.etag
	}
	return s, err
}

// OshibkaZhdat — долгий опрос ответил не 200/204: Status — код (403 —
// выключен мозгом, 429 — занято, 404 — не знает), RetryAfter — сколько ждать
// по заголовку Retry-After (0 — не прислал).
type OshibkaZhdat struct {
	Status     int
	RetryAfter time.Duration
}

func (e *OshibkaZhdat) Error() string { return fmt.Sprintf("долгий опрос: сервер ответил %d", e.Status) }

// ZhdatPult — долгий опрос пульта (GET <адрес>/pult/zhdat?versiya=N): сервер
// держит запрос до ~55 с. 200 — у устройства версия выше N (true: пора
// спросить /info); 204 — срок вышел, нового нет (false). Прочие коды —
// *OshibkaZhdat; сеть — обычная ошибка.
func (k *Klient) ZhdatPult(ctx context.Context, adres string, versiya int64) (bool, error) {
	u := fmt.Sprintf("%s/pult/zhdat?versiya=%d", strings.TrimSuffix(adres, "/"), versiya)
	o, err := k.vzyatOtvet(ctx, u, "")
	if err != nil {
		return false, err
	}
	switch o.status {
	case http.StatusOK:
		return true, nil
	case http.StatusNoContent, http.StatusNotModified:
		return false, nil
	}
	oz := &OshibkaZhdat{Status: o.status}
	if s, err := strconv.Atoi(strings.TrimSpace(o.retryAfter)); err == nil && s > 0 {
		oz.RetryAfter = time.Duration(s) * time.Second
	}
	return false, oz
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
		o, err := k.vzyatOtvet(ctx, url, "")
		telo, status, etag := o.telo, o.status, o.etag
		if err != nil {
			posledn = err
			continue
		}
		if status != http.StatusOK {
			posledn = fmt.Errorf("сервер ответил %d", status)
			continue
		}
		s, err := RazobratSvedeniya(telo)
		if err != nil {
			posledn = err
			continue
		}
		s.ETag = etag
		return s, nil
	}
	return nil, posledn
}

func (k *Klient) vzyat(ctx context.Context, url string) ([]byte, int, error) {
	o, err := k.vzyatOtvet(ctx, url, "")
	return o.telo, o.status, err
}

// otvetGet — что из ответа GET нужно пульту сверх тела и кода.
type otvetGet struct {
	telo       []byte
	status     int
	etag       string
	retryAfter string
}

// vzyatOtvet — GET; ifNoneMatch непустой — уходит заголовком If-None-Match как есть.
func (k *Klient) vzyatOtvet(ctx context.Context, url, ifNoneMatch string) (otvetGet, error) {
	req, err := k.zapros(ctx, url)
	if err != nil {
		return otvetGet{}, err
	}
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	otvet, err := k.http().Do(req)
	if err != nil {
		return otvetGet{}, SkrytKodVOshibke(err)
	}
	defer otvet.Body.Close()
	o := otvetGet{status: otvet.StatusCode, etag: otvet.Header.Get("ETag"), retryAfter: otvet.Header.Get("Retry-After")}
	// Потолок в 8 МБ: конфиг заведомо меньше, а битый сервер не должен съесть память.
	o.telo, err = io.ReadAll(io.LimitReader(otvet.Body, 8<<20))
	return o, err
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
