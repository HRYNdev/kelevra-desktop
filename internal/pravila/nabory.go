package pravila

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Свои наборы и подбор правил по частям (0.6.93).
//
// С единым списком правил сети (мозг, таблица pravila) профиль несёт, кроме
// 23 двоичных наборов с сервера правил, ещё и СВОИ: svoi-tunnel, svoi-mimo,
// svoi-blok, svoi-razreshit. Они отдаются выдачей по ключу устройства в
// формате source (JSON) и меняются когда угодно: человек добавил домен в
// панели — через пару минут он должен работать у всех, без перезапуска ядра.
// С двух дверей приходит один и тот же набор: svoi-tunnel и svoi-tunnel-2.
//
// До 0.6.93 здесь было три препятствия:
//   - кеш принимал только .srs, и JSON-набор в него не ложился никогда;
//   - кеш и встроенный комплект применялись только целиком: не хватает
//     одного тега — нет ни одного, а своих наборов в комплекте нет и быть не
//     может (их содержимое меняется каждый день);
//   - свежие файлы ждали переподключения.
//
// Теперь набор берётся по частям: что есть в кеше — из кеша; чего нет — из
// комплекта; своего набора нет нигде — пустая заготовка прямо в папке кеша.
// Ядро читает наборы из кеша как type:"local" и само следит за файлом:
// сверка подменяет файл целиком (переименованием), и ядро перечитывает его на
// ходу (sing-box route/rule/rule_set_local.go, fswatch).

// Виды наборов на диске — те же слова, что поле format у sing-box.
const (
	FormatBinary = "binary"
	FormatSource = "source"
)

// rasshirenie — расширение файла набора по его виду. Пустой вид — .srs:
// так лежал кеш до 0.6.93.
func rasshirenie(format string) string {
	if format == FormatSource {
		return ".json"
	}
	return ".srs"
}

// FormatPoPuti — вид набора по расширению файла: .json — source, прочее —
// binary. Так конфиг ядра узнаёт, каким format описать local-набор.
func FormatPoPuti(put string) string {
	if strings.EqualFold(filepath.Ext(put), ".json") {
		return FormatSource
	}
	return FormatBinary
}

// UznatFormat — вид набора по содержимому файла: двоичный (подпись SRS),
// source (JSON-объект с полем rules) или "" — не набор правил вовсе
// (страница ошибки чужого прокси, заглушка провайдера).
func UznatFormat(put string) string {
	if pohozheNaSRS(put) {
		return FormatBinary
	}
	b, err := os.ReadFile(put)
	if err != nil {
		return ""
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	var nabor struct {
		Rules []json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(b, &nabor); err != nil || len(nabor.Rules) == 0 {
		return ""
	}
	return FormatSource
}

// PrefiksSvoih — с чего начинаются теги своих наборов.
const PrefiksSvoih = "svoi-"

// Svoy — набор из единого списка правил сети (svoi-*): его нет и не может
// быть во встроенном комплекте, и без него ядро всё равно должно подняться.
func Svoy(teg string) bool { return strings.HasPrefix(teg, PrefiksSvoih) }

var hvostDveri = regexp.MustCompile(`-\d+$`)

// Gruppa — общее имя одного набора с разных дверей: svoi-tunnel и
// svoi-tunnel-2 — группа svoi-tunnel. Есть один — значит есть оба: у них одно
// содержимое. У чужих наборов группа — сам тег.
func Gruppa(teg string) string {
	if !Svoy(teg) {
		return teg
	}
	return hvostDveri.ReplaceAllString(teg, "")
}

// Zaglushka — пустой набор в формате source. Совсем пустой ядро не
// принимает, поэтому одно правило, которому ничто не равно (сервер отдаёт
// такую же заглушку на пустой список).
var Zaglushka = []byte(`{"version":2,"rules":[{"domain_suffix":["svoi.invalid"]}]}` + "\n")

// Podbor — откуда ядру брать каждый набор правил при старте.
type Podbor struct {
	// Puti — тег -> файл на диске. Наборов, которых здесь нет, ядро качает
	// само (остаются type:"remote").
	Puti map[string]string
	// IzKesha — наборы, скачанные этой машиной.
	IzKesha []string
	// IzKomplekta — наборы из встроенного в приложение комплекта.
	IzKomplekta []string
	// Zagotovki — свои наборы, которых ещё нет в кеше: пустая заглушка (или
	// копия той же группы с другой двери) прямо в папке кеша. Сверка положит
	// на её место настоящий набор, и ядро подхватит его на ходу.
	Zagotovki []string
	// Pustye — чужие наборы, которых нет ни в кеше, ни в комплекте (только
	// когда подбор обязан закрыть все теги): пустая заглушка рядом с
	// комплектом. Лучше без одного списка, чем без связи.
	Pustye []string
	// Seti — наборы, которые остались ядру качать самому.
	Seti []string
}

// Podobrat решает для каждого тега из nuzhny, откуда ядру его взять.
//
// komplekt == nil — обычный подъём: чужой набор, которого нет в кеше, ядро
// качает само (как до кеша); если не сможет, служба повторит подъём уже с
// комплектом. komplekt != nil — тот самый повтор: закрыт должен быть КАЖДЫЙ
// тег, иначе ядро снова упадёт на «initialize rule-set»; чего нет нигде —
// пустая заглушка в papkaKomplekta.
//
// Порядок для каждого тега: кеш → комплект → (свой набор) копия группы или
// заготовка в кеше → (повтор) пустая заглушка → ядро качает само.
func Podobrat(papkaYadra string, nuzhny []string, komplekt map[string]string, papkaKomplekta string) (Podbor, error) {
	var p Podbor
	if len(nuzhny) == 0 {
		return p, nil
	}
	tegi := append([]string(nil), nuzhny...)
	sort.Strings(tegi)
	sved := ChitatSvedeniya(papkaYadra)
	p.Puti = make(map[string]string, len(tegi))
	dirKesha, err := filepath.Abs(PutKesha(papkaYadra))
	if err != nil {
		return p, err
	}
	var posledn error
	for _, teg := range tegi {
		if put := naDiske(papkaYadra, teg, sved); put != "" {
			if Svoy(teg) && !estSvedeniya(sved, teg, sved.Nabory[teg].Adres) {
				// Заготовка прошлого подъёма: та же группа с другой двери
				// могла с тех пор скачаться — берём её содержимое.
				if novyy, err := zagotovit(dirKesha, teg, soderzhimoeGruppy(papkaYadra, teg, tegi, sved)); err == nil {
					put = novyy
				}
				p.Puti[teg] = put
				p.Zagotovki = append(p.Zagotovki, teg)
				continue
			}
			p.Puti[teg] = put
			p.IzKesha = append(p.IzKesha, teg)
			continue
		}
		if put := komplekt[teg]; put != "" {
			p.Puti[teg] = put
			p.IzKomplekta = append(p.IzKomplekta, teg)
			continue
		}
		if Svoy(teg) {
			put, err := zagotovit(dirKesha, teg, soderzhimoeGruppy(papkaYadra, teg, tegi, sved))
			if err != nil {
				posledn = err
			} else {
				p.Puti[teg] = put
				p.Zagotovki = append(p.Zagotovki, teg)
				continue
			}
		}
		if komplekt != nil {
			dir := papkaKomplekta
			if dir == "" {
				dir = dirKesha
			}
			put, err := zapisatPustoy(dir, teg)
			if err != nil {
				posledn = err
			} else {
				p.Puti[teg] = put
				p.Pustye = append(p.Pustye, teg)
				continue
			}
		}
		p.Seti = append(p.Seti, teg)
	}
	if len(p.Puti) == 0 {
		p.Puti = nil
	}
	return p, posledn
}

// soderzhimoeGruppy — содержимое того же набора с другой двери (svoi-tunnel
// для svoi-tunnel-2), если оно уже скачано; иначе пустая заглушка.
func soderzhimoeGruppy(papkaYadra, teg string, tegi []string, sved Svedeniya) []byte {
	gruppa := Gruppa(teg)
	for _, drugoy := range tegi {
		if drugoy == teg || Gruppa(drugoy) != gruppa {
			continue
		}
		if !estSvedeniya(sved, drugoy, sved.Nabory[drugoy].Adres) {
			continue
		}
		put := naDiske(papkaYadra, drugoy, sved)
		if put == "" || FormatPoPuti(put) != FormatSource {
			continue
		}
		if b, err := os.ReadFile(put); err == nil && len(b) > 0 {
			return b
		}
	}
	return Zaglushka
}

// zagotovit кладёт заготовку своего набора в папку кеша — туда же и под тем
// же именем, куда сверка потом положит настоящий набор: ядро следит именно за
// этим файлом и подхватит подмену на ходу. Сведений о наборе не пишет, и
// сверка скачает его при первом же заходе.
func zagotovit(dirKesha, teg string, soderzhimoe []byte) (string, error) {
	if err := os.MkdirAll(dirKesha, 0o755); err != nil {
		return "", fmt.Errorf("папка кеша правил %q: %w", dirKesha, err)
	}
	put := filepath.Join(dirKesha, teg+rasshirenie(FormatSource))
	return put, zapisatCelikom(put, soderzhimoe)
}

// zapisatPustoy — пустая заглушка чужого набора рядом с комплектом (не в
// кеше: иначе следующий подъём принял бы её за скачанный набор).
func zapisatPustoy(dir, teg string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("папка под заглушку набора %q: %w", dir, err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	put := filepath.Join(abs, teg+rasshirenie(FormatSource))
	return put, zapisatCelikom(put, Zaglushka)
}

// zapisatCelikom пишет файл через временный и переименование; то же
// содержимое уже на месте — не трогает (ядро следит за файлом).
func zapisatCelikom(put string, soderzhimoe []byte) error {
	if sovpadaetSDiska(put, soderzhimoe) {
		return nil
	}
	vremenny := put + ".pishem"
	if err := os.WriteFile(vremenny, soderzhimoe, 0o644); err != nil {
		return err
	}
	if err := os.Rename(vremenny, put); err != nil {
		os.Remove(vremenny)
		return err
	}
	return nil
}

// SrokNabora — когда набор пора сверять: интервал из профиля
// (update_interval), но не реже SrokSvezhesti. Нет интервала — SrokSvezhesti.
func SrokNabora(interval time.Duration) time.Duration {
	if interval > 0 && interval < SrokSvezhesti {
		return interval
	}
	return SrokSvezhesti
}

// KSverke — какие наборы из adresa пора сверить с сервером прямо сейчас:
// файла нет, сведений нет (заготовка или новый адрес — например, другой ключ
// устройства), или прошло больше срока набора (sroki, тег -> интервал из
// профиля; см. SrokNabora).
//
// По наборам, а не всем кешем разом: свои наборы сверяются раз в две минуты,
// и тянуть с ними 23 чужих, которым хватает часа, незачем.
func KSverke(papkaYadra string, adresa map[string]string, sroki map[string]time.Duration) map[string]string {
	sved := ChitatSvedeniya(papkaYadra)
	itog := map[string]string{}
	for teg, adres := range adresa {
		if naDiske(papkaYadra, teg, sved) == "" || !estSvedeniya(sved, teg, adres) {
			itog[teg] = adres
			continue
		}
		if time.Since(sved.Nabory[teg].Svereno) > SrokNabora(sroki[teg]) {
			itog[teg] = adres
		}
	}
	return itog
}
