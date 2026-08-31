// Пакет zhurnaly: суточная отправка собственных журналов разработчику.
//
// Зачем вообще. Kelevra.exe оконный, консоли у него нет, и единственный след
// отказа — файл kelevra.log в %LOCALAPPDATA%. Пока его не научились слать
// самим, разбор любой аварии начинался с «пришли мне журнал», то есть с
// похода человека в проводник — которого он не сделает. Раз в сутки вечером
// клиент отдаёт всё новое сам.
//
// Формат тела — ОДИНОЧНЫЙ gzip-поток, не tar и не zip: коллектор на той
// стороне уже написан под android-клиент и разбирает ровно это. Внутри куски
// файлов подряд, каждый предваряется строкой-разделителем
//
//	===== kelevra.log offset=0 bytes=12345 =====
//
// По ней коллектор и режет поток обратно на файлы.
package zhurnaly

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Potolok — больше сервер не примет (ответит 413). Режем по САМЫМ СВЕЖИМ
// логам: старое уже неинтересно, а разбирать надо то, что случилось сейчас.
const Potolok int64 = 25 << 20

// TipTela — тем же значением помечено тело у android-клиента.
const TipTela = "application/gzip"

// Srok — сколько ждём коллектор. 25 МБ по обычному каналу уходят с запасом.
const Srok = 3 * time.Minute

// Razdelitel — строка перед каждым куском. Формат дословный, менять его
// нельзя: коллектор ищет ровно эту форму.
const Razdelitel = "===== %s offset=%d bytes=%d ====="

// Kusok — один непрерывный кусок одного файла, попавший в посылку.
type Kusok struct {
	Put         string // полный путь на диске
	Imya        string // как назван в разделителе
	Smeshchenie int64  // с какого байта файла взят
	Bayt        int64  // сколько взято
	Razmer      int64  // размер файла на момент чтения
	Izmenen     int64  // время изменения файла, unixnano
}

// Otchet — что именно ушло на сервер.
type Otchet struct {
	SzhatoBayt int64 // размер тела запроса
	SyrykhBayt int64 // сколько сырых байт журнала внутри
	Kuski      []Kusok
	OtvetBayt  int64 // сколько байт насчитал сервер
}

// Metka — сколько байт одного файла уже улетело и как этот файл выглядел в
// тот момент.
type Metka struct {
	Otpravleno int64 `json:"otpravleno"`
	Razmer     int64 `json:"razmer"`
	Izmenen    int64 `json:"izmenen"` // unixnano
}

// Metki — весь этот учёт целиком, как он лежит на диске.
type Metki struct {
	Fayly map[string]Metka `json:"fayly"`
}

// ZagruzitMetki читает учёт. Отсутствие файла — не ошибка (первая отправка),
// битый файл — тоже: хуже повторной посылки только отсутствие посылок вовсе.
func ZagruzitMetki(put string) *Metki {
	m := &Metki{Fayly: map[string]Metka{}}
	b, err := os.ReadFile(put)
	if err != nil {
		return m
	}
	if err := json.Unmarshal(b, m); err != nil || m.Fayly == nil {
		return &Metki{Fayly: map[string]Metka{}}
	}
	return m
}

// SohranitMetki пишет учёт атомарно — тем же приёмом, что и hranenie.Sohranit:
// обрыв не должен оставить половину файла, иначе следующий запуск решит, что
// не отправлял ничего, и отправит всё заново.
func SohranitMetki(put string, m *Metki) error {
	if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	vremenny := put + ".tmp"
	if err := os.WriteFile(vremenny, b, 0o600); err != nil {
		return err
	}
	return os.Rename(vremenny, put)
}

// Razobrat сопоставляет файлы, лежащие на диске СЕЙЧАС, с отметками прошлой
// отправки и говорит про каждый, с какого байта его продолжать.
//
// Тут вся хитрость учёта. Журнал не просто растёт: при 512 КБ он
// ПЕРЕИМЕНОВЫВАЕТСЯ в kelevra.log.proshlyy (cmd/kelevra/zhurnal.go), и то же
// самое содержимое, которое вчера уже улетело под именем kelevra.log, сегодня
// лежит под другим именем. Отправить его второй раз — значит слать одно и то
// же.
//
// Поэтому файл узнаётся не по имени, а по паре РАЗМЕР+ВРЕМЯ ИЗМЕНЕНИЯ:
// отметка подходит файлу, если файл не стал МЕНЬШЕ и не стал СТАРШЕ
// записанного в ней — то есть перед нами тот же файл, просто доросший.
// Проверяется своя отметка (обычный рост на месте) и отметка
// «предшественника»: для kelevra.log.proshlyy это kelevra.log, откуда он и
// получился переименованием.
//
// И самое неочевидное — отметку предшественника забирает РОВНО ОДИН файл, и
// первым на неё смотрит ротация. Иначе так: журнал ушёл на 100 байтах,
// уехал в .proshlyy, новый kelevra.log дорос до 300 — и старая отметка
// «отправлено 100» подошла бы обоим сразу. Новый файл молча пропустил бы своё
// начало, а это ровно те строки, ради которых отправка и заведена (запуск,
// первая авария). Забрал ротацией — значит новому шлём с нуля.
func Razobrat(puti []string, m *Metki) []Kusok {
	type nayden struct {
		put             string
		razmer, izmenen int64
	}
	var est []nayden
	for _, put := range puti {
		st, err := os.Stat(put)
		if err != nil || st.IsDir() {
			continue
		}
		est = append(est, nayden{put, st.Size(), st.ModTime().UnixNano()})
	}
	sort.SliceStable(est, func(i, j int) bool {
		return predshestvennik(est[i].put) != "" && predshestvennik(est[j].put) == ""
	})
	zanyatye := map[string]bool{}
	var itog []Kusok
	for _, n := range est {
		var smeshchenie int64
		if o, e := m.Fayly[n.put]; e && !zanyatye[n.put] && podhodit(o, n.razmer, n.izmenen) {
			smeshchenie = o.Otpravleno
			zanyatye[n.put] = true
		} else if p := predshestvennik(n.put); p != "" {
			if o, e := m.Fayly[p]; e && !zanyatye[p] && podhodit(o, n.razmer, n.izmenen) {
				smeshchenie = o.Otpravleno
				zanyatye[p] = true
			}
		}
		if smeshchenie > n.razmer {
			smeshchenie = n.razmer
		}
		itog = append(itog, Kusok{
			Put: n.put, Imya: filepath.Base(n.put),
			Smeshchenie: smeshchenie, Bayt: n.razmer - smeshchenie,
			Razmer: n.razmer, Izmenen: n.izmenen,
		})
	}
	return itog
}

func podhodit(o Metka, razmer, izmenen int64) bool {
	return razmer >= o.Razmer && izmenen >= o.Izmenen
}

// predshestvennik — под каким именем этот файл жил до ротации.
func predshestvennik(put string) string {
	const hvost = ".proshlyy"
	if len(put) > len(hvost) && put[len(put)-len(hvost):] == hvost {
		return put[:len(put)-len(hvost)]
	}
	return ""
}

// Istochniki — все места, где приложение когда-либо держит свой журнал:
// рабочий файл, его ротация, и то же самое в запасной папке (%TEMP%\Kelevra),
// куда журнал уезжает, когда своя папка недоступна.
func Istochniki(putZhurnala, zapasnayaPapka string) []string {
	puti := []string{putZhurnala, putZhurnala + ".proshlyy"}
	if zapasnayaPapka != "" {
		zapasnyy := filepath.Join(zapasnayaPapka, filepath.Base(putZhurnala))
		if zapasnyy != putZhurnala {
			puti = append(puti, zapasnyy, zapasnyy+".proshlyy")
		}
	}
	return puti
}

// Upakovat пакует всё новое из разобранных кандидатов в ОДИН gzip-поток.
//
// Пустой результат (nil, nil, nil) — законный и частый исход: нового с
// прошлого вечера могло не появиться вовсе, и слать пустое незачем.
func Upakovat(kandidaty []Kusok, potolok int64) ([]byte, []Kusok, error) {
	var vybor []Kusok
	for _, k := range kandidaty {
		if k.Bayt <= 0 {
			continue // всё это уже отправлено, либо файл пуст
		}
		vybor = append(vybor, k)
	}
	if len(vybor) == 0 {
		return nil, nil, nil
	}
	// Бюджет раздаётся от САМОГО СВЕЖЕГО файла: упрёмся в потолок — обрежется
	// старьё, а не сегодняшняя авария. Внутри одного файла по той же причине
	// берётся ХВОСТ (смещение уезжает вперёд), а не начало.
	sort.SliceStable(vybor, func(i, j int) bool { return vybor[i].Izmenen > vybor[j].Izmenen })
	var vzyato int64
	otobrannye := make([]Kusok, 0, len(vybor))
	for _, k := range vybor {
		ostatok := potolok - vzyato
		if ostatok <= 0 {
			break
		}
		if k.Bayt > ostatok {
			k.Smeshchenie = k.Razmer - ostatok
			k.Bayt = ostatok
		}
		vzyato += k.Bayt
		otobrannye = append(otobrannye, k)
	}
	// В поток пишем по времени вперёд: читать склеенный журнал сверху вниз
	// должно быть так же, как читать его в папке.
	sort.SliceStable(otobrannye, func(i, j int) bool { return otobrannye[i].Izmenen < otobrannye[j].Izmenen })

	var telo bytes.Buffer
	gz := gzip.NewWriter(&telo)
	var ushli []Kusok
	for _, k := range otobrannye {
		f, err := os.Open(k.Put)
		if err != nil {
			continue // файл мог уехать в ротацию прямо сейчас — не повод рушить посылку
		}
		if _, err := f.Seek(k.Smeshchenie, io.SeekStart); err != nil {
			_ = f.Close()
			continue
		}
		// Читаем ДО того, как написать разделитель: в разделителе стоит длина
		// куска, и коллектор режет поток именно по ней. Соври в ней хоть на
		// байт (файл успел уехать в ротацию между Stat и чтением) — и всё,
		// что идёт дальше по потоку, разберётся не туда.
		soderzhimoe, err := io.ReadAll(io.LimitReader(f, k.Bayt))
		_ = f.Close()
		if err != nil {
			continue
		}
		if len(soderzhimoe) == 0 {
			continue
		}
		k.Bayt = int64(len(soderzhimoe))
		if _, err := fmt.Fprintf(gz, Razdelitel+"\n", k.Imya, k.Smeshchenie, k.Bayt); err != nil {
			return nil, nil, err
		}
		if _, err := gz.Write(soderzhimoe); err != nil {
			return nil, nil, err
		}
		if _, err := gz.Write([]byte("\n")); err != nil {
			return nil, nil, err
		}
		ushli = append(ushli, k)
	}
	if err := gz.Close(); err != nil {
		return nil, nil, err
	}
	if len(ushli) == 0 {
		return nil, nil, nil
	}
	return telo.Bytes(), ushli, nil
}

// otvetServera — что говорит коллектор. Успех: {"ok":true,"bytes":N}.
type otvetServera struct {
	Ok   bool   `json:"ok"`
	Bayt int64  `json:"bytes"`
	Beda string `json:"error"`
}

// Otpravshchik — вся суточная отправка целиком. Поля-пути внедряемые, чтобы
// стенд гонял её на своих файлах и своём сервере, а не на живой папке.
type Otpravshchik struct {
	Adres    string // куда POST-ить, например https://subkv.chickenkiller.com/logs
	HTTP     *http.Client
	DeviceID string
	Versiya  string
	Puti     []string // что слать; пусто — Istochniki на настоящих путях
	PutMetok string   // где учёт; пусто — hranenie.PutOtmetokZhurnalov()
	Potolok  int64    // 0 — Potolok
	// Zagolovki ставит на запрос заголовки устройства. Внедряемая, а не
	// прямой вызов ustroystvo.Zagolovki: так пакет не тянет за собой ничего
	// ради одного вызова, и стенд может посмотреть, что именно уехало.
	Zagolovki func(h http.Header, deviceID, versiya string)
}

func (o *Otpravshchik) klient() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return &http.Client{Timeout: Srok}
}

func (o *Otpravshchik) potolok() int64 {
	if o.Potolok > 0 {
		return o.Potolok
	}
	return Potolok
}

// Otpravit собирает и отдаёт всё новое. Пустой Otchet без ошибки — «нового
// нет», это нормальный исход, а не отказ.
//
// Локально НИЧЕГО не удаляется: журнал нужен человеку в окне («Журнал
// работы») ровно так же, как разработчику на сервере.
func (o *Otpravshchik) Otpravit(ctx context.Context) (Otchet, error) {
	m := ZagruzitMetki(o.PutMetok)
	vse := Razobrat(o.Puti, m)
	telo, kuski, err := Upakovat(vse, o.potolok())
	if err != nil {
		return Otchet{}, err
	}
	if len(kuski) == 0 {
		return Otchet{}, nil
	}
	zapros, err := http.NewRequestWithContext(ctx, http.MethodPost, o.Adres, bytes.NewReader(telo))
	if err != nil {
		return Otchet{}, err
	}
	zapros.Header.Set("Content-Type", TipTela)
	if o.Zagolovki != nil {
		o.Zagolovki(zapros.Header, o.DeviceID, o.Versiya)
	}
	otvet, err := o.klient().Do(zapros)
	if err != nil {
		return Otchet{}, err
	}
	defer otvet.Body.Close()
	syroy, _ := io.ReadAll(io.LimitReader(otvet.Body, 64<<10))
	if otvet.StatusCode != http.StatusOK {
		return Otchet{}, oshibkaOtveta(otvet.StatusCode, syroy)
	}
	var razobrano otvetServera
	if err := json.Unmarshal(syroy, &razobrano); err != nil {
		return Otchet{}, fmt.Errorf("сервер ответил не по форме: %w", err)
	}
	if !razobrano.Ok {
		if razobrano.Beda != "" {
			return Otchet{}, fmt.Errorf("сервер не принял журналы: %s", razobrano.Beda)
		}
		return Otchet{}, fmt.Errorf("сервер не принял журналы")
	}
	// Отметки пишем ТОЛЬКО после подтверждения: не подтвердил — в следующий
	// раз те же байты уйдут заново, и это правильнее, чем потерять их молча.
	//
	// Учёт ПЕРЕСОБИРАЕТСЯ под то, что лежит на диске сейчас, а не дописывается
	// поверх старого. Так из него сами собой уходят отметки исчезнувших файлов
	// и — главное — отметка журнала, который только что уехал в ротацию: она
	// уже переписана на своё новое имя (см. Razobrat), и оставь её на старом
	// месте, новый журнал завтра пропустил бы своё начало.
	novye := &Metki{Fayly: make(map[string]Metka, len(vse))}
	for _, k := range vse {
		novye.Fayly[k.Put] = Metka{Otpravleno: k.Smeshchenie, Razmer: k.Razmer, Izmenen: k.Izmenen}
	}
	var syrykh int64
	for _, k := range kuski {
		syrykh += k.Bayt
		novye.Fayly[k.Put] = Metka{
			// Otpravleno = размер файла, а не смещение+длина: когда кусок
			// обрезан потолком, середина пропущена СОЗНАТЕЛЬНО, и гоняться
			// за ней завтра не надо — свежее важнее полного.
			Otpravleno: k.Razmer,
			Razmer:     k.Razmer,
			Izmenen:    k.Izmenen,
		}
	}
	if err := SohranitMetki(o.PutMetok, novye); err != nil {
		return Otchet{}, fmt.Errorf("журналы ушли, но учёт не сохранился: %w", err)
	}
	return Otchet{
		SzhatoBayt: int64(len(telo)),
		SyrykhBayt: syrykh,
		Kuski:      kuski,
		OtvetBayt:  razobrano.Bayt,
	}, nil
}

// oshibkaOtveta переводит коды коллектора на человеческий: у каждого своя
// причина и свой смысл для следующей попытки.
func oshibkaOtveta(kod int, telo []byte) error {
	var razobrano otvetServera
	_ = json.Unmarshal(telo, &razobrano)
	prichina := razobrano.Beda
	switch kod {
	case http.StatusRequestEntityTooLarge: // 413
		if prichina == "" {
			prichina = "посылка больше, чем сервер принимает"
		}
		return fmt.Errorf("сервер не принял журналы (413): %s", prichina)
	case http.StatusInsufficientStorage: // 507
		if prichina == "" {
			prichina = "на сервере кончилось место"
		}
		return fmt.Errorf("сервер не принял журналы (507): %s", prichina)
	default:
		if prichina == "" {
			prichina = "без объяснения"
		}
		return fmt.Errorf("сервер не принял журналы (%d): %s", kod, prichina)
	}
}
