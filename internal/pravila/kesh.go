package pravila

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Кеш свежих правил на диске: главное лекарство от «связь не поднялась».
//
// Беда, которую он лечит (замер 08.09 с живой машины, дважды за час):
//
//	ядро упало: initialize rule-set[0]: ads: lookup subkv... — не резолвится
//	источник правил недоступен, пробую встроенный комплект
//	...
//	поднимаю ядро совсем без правил (весь трафик через VPN)
//
// Домен при этом был жив: с той же машины он резолвился и файл скачивался за
// секунду — но ПОЗЖЕ, когда мы спросили руками. В момент старта ядра сеть
// перестраивается: Windows переключает DNS на туннельный, старый резолвер уже
// не отвечает, а новый ещё не работает, потому что ядро как раз и не
// поднялось. Курица и яйцо, и человек в нём остаётся без связи.
//
// Выход: не заставлять ядро ходить в сеть на старте вовсе. Правила берутся с
// диска (route.rule_set становится type:"local", тем же приёмом, что и
// встроенный комплект), а обновляются ФОНОМ, когда связь уже работает и
// торопиться некуда.
//
// Чем это отличается от встроенного комплекта. Комплект вшит в .exe, стареет
// от выпуска к выпуску и служит последней подстраховкой. Кеш — это свежие
// правила той же машины: их скачали при прошлом удачном подъёме, и они
// всегда новее комплекта.
//
// ЧЕГО ЗДЕСЬ НЕ ХВАТАЛО ДО 09.09, словами владельца: «списки кешируются и
// никогда не обновляются, это костыль». По делу — так и было:
//   - свежесть мерилась временем изменения ФАЙЛА, а оно врёт после переноса
//     папки данных (клиент переезжал из папки пользователя в общую 08.09);
//   - обновление тянуло все 23 набора целиком (≈1 МБ) даже когда на сервере
//     не поменялось ничего, потому что запрос шёл голым GET без единого
//     заголовка;
//   - двадцать три закачки шли одна за другой, и один сорвавшийся набор
//     оставлял кеш «вечно просроченным».
//
// Теперь у кеша есть свои сведения (svedeniya.json): по какому адресу взят
// набор, какой у него был ETag, когда мы последний раз СВЕРЯЛИСЬ с сервером.
// Сверка идёт условными запросами: сервер отвечает «не менялось» (304) без
// тела, и суточная проверка всех наборов стоит почти ничего.

// SrokSvezhesti — после какого возраста кеш стоит сверить с сервером.
//
// Час, а не сутки, как было. Сервер просит у ядра ровно этого
// (update_interval: "1h" у ads/main-domains/main-subnets/blocked-domains в
// rules.json), а цена сверки теперь — 23 условных запроса, на которые почти
// всегда приходит «не менялось» без тела.
const SrokSvezhesti = time.Hour

// imyaSvedeniy — файл со сведениями о наборах рядом с самими наборами.
const imyaSvedeniy = "svedeniya.json"

// PutKesha — папка кеша внутри папки ядра.
func PutKesha(papkaYadra string) string { return filepath.Join(papkaYadra, "pravila-kesh") }

// SvedeniyaNabora — что мы знаем про один набор в кеше.
//
// Adres хранится не для красоты: адрес набора может поменяться на сервере, и
// тогда старый ETag относится к другому файлу. Сверять надо адрес с адресом.
type SvedeniyaNabora struct {
	Adres   string    `json:"adres"`
	ETag    string    `json:"etag,omitempty"`
	Izmenen string    `json:"izmenen,omitempty"` // Last-Modified как его прислал сервер
	Razmer  int64     `json:"razmer,omitempty"`
	Svereno time.Time `json:"svereno"`
}

// Svedeniya — сведения обо всём кеше.
type Svedeniya struct {
	Nabory map[string]SvedeniyaNabora `json:"nabory"`
}

// ChitatSvedeniya — сведения кеша с диска. Отсутствие файла не ошибка: так
// выглядит кеш, собранный прошлыми версиями приложения.
func ChitatSvedeniya(papkaYadra string) Svedeniya {
	s := Svedeniya{Nabory: map[string]SvedeniyaNabora{}}
	b, err := os.ReadFile(filepath.Join(PutKesha(papkaYadra), imyaSvedeniy))
	if err != nil {
		return s
	}
	var prochli Svedeniya
	if err := json.Unmarshal(b, &prochli); err != nil || prochli.Nabory == nil {
		return s
	}
	return prochli
}

func zapisatSvedeniya(papkaYadra string, s Svedeniya) error {
	dir := PutKesha(papkaYadra)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	vremenny := filepath.Join(dir, imyaSvedeniy+".pishem")
	if err := os.WriteFile(vremenny, b, 0o644); err != nil {
		return err
	}
	return os.Rename(vremenny, filepath.Join(dir, imyaSvedeniy))
}

// IzKesha — что лежит в кеше прямо сейчас: тег -> путь к файлу.
//
// Возвращает ТОЛЬКО полные наборы: если хоть одного тега из nuzhny не
// хватает, кеш не годится и возвращается nil. Половина правил хуже целого
// комплекта — ядро с недостающим набором просто не поднимется, а мы об этом
// узнаем позже всех.
//
// Пустой список нужных наборов — это не «кеш подошёл», а «спрашивать было
// нечего»: профиль не прочитан. Раньше отсюда возвращалась пустая, но не
// nil-карта, и в журнал уходило бодрое «правила беру из кеша (0 наборов)».
func IzKesha(papkaYadra string, nuzhny []string) map[string]string {
	if len(nuzhny) == 0 {
		return nil
	}
	dir := PutKesha(papkaYadra)
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	itog := make(map[string]string, len(nuzhny))
	for _, teg := range nuzhny {
		put := filepath.Join(abs, teg+".srs")
		st, err := os.Stat(put)
		if err != nil || st.Size() == 0 {
			return nil
		}
		itog[teg] = put
	}
	return itog
}

// Vozrast — сколько прошло с последней УДАЧНОЙ сверки кеша с сервером.
//
// Меряется по сведениям, а не по времени файла: файл не меняется, когда
// сервер отвечает «не менялось», и не переживает переноса папки данных.
// Кеша нет или сведений нет — вернём очень большой возраст, это честнее нуля.
func Vozrast(papkaYadra string, nuzhny []string) time.Duration {
	if len(nuzhny) == 0 {
		return 100 * 365 * 24 * time.Hour
	}
	sved := ChitatSvedeniya(papkaYadra)
	staraya := time.Time{}
	for _, teg := range nuzhny {
		n, est := sved.Nabory[teg]
		if !est || n.Svereno.IsZero() {
			// Про этот набор сведений нет — падаем на время файла: кеш мог
			// быть собран прошлой версией приложения, и выбрасывать его
			// из-за отсутствия сведений было бы расточительством.
			st, err := os.Stat(filepath.Join(PutKesha(papkaYadra), teg+".srs"))
			if err != nil {
				return 100 * 365 * 24 * time.Hour
			}
			if staraya.IsZero() || st.ModTime().Before(staraya) {
				staraya = st.ModTime()
			}
			continue
		}
		if staraya.IsZero() || n.Svereno.Before(staraya) {
			staraya = n.Svereno
		}
	}
	if staraya.IsZero() {
		return 100 * 365 * 24 * time.Hour
	}
	return time.Since(staraya)
}

// Ustarel — пора ли сверить кеш с сервером. Пустой или неполный кеш — тоже «пора».
func Ustarel(papkaYadra string, nuzhny []string) bool {
	if IzKesha(papkaYadra, nuzhny) == nil {
		return true
	}
	return Vozrast(papkaYadra, nuzhny) > SrokSvezhesti
}

// Itog — чем закончилась сверка кеша с сервером.
//
// Разница между «сверено» и «обновлено» здесь главная: первое означает, что
// мы спросили сервер и он ответил, второе — что набор на диске реально
// поменялся. Именно Obnovleno решает, есть ли смысл беспокоить работающее
// ядро: если на сервере ничего не менялось, беспокоить его незачем.
type Itog struct {
	Svereno   int
	Obnovleno int
	Otkazy    int
}

// odnovremenno — сколько наборов качаем разом.
//
// Их 23, и почти на все приходит «не менялось» без тела. Четыре — чтобы
// сверка занимала секунды, а не полминуты, и при этом не выглядела для
// сервера налётом.
const odnovremenno = 4

// Obnovit сверяет наборы с сервером и обновляет те, что изменились.
//
// Зовётся ФОНОМ и только когда связь уже есть: на старте ядра сеть занята
// собой (см. шапку файла).
//
// Пишем через временный файл и переименование: оборванная закачка не должна
// оставить в кеше половину набора — с ней ядро не поднимется, а выглядеть
// это будет как «правила есть».
func Obnovit(ctx context.Context, klient *http.Client, adresa map[string]string, papkaYadra string) (Itog, error) {
	var itog Itog
	dir := PutKesha(papkaYadra)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return itog, fmt.Errorf("папка кеша правил %q: %w", dir, err)
	}
	if klient == nil {
		klient = &http.Client{Timeout: 2 * time.Minute}
	}
	sved := ChitatSvedeniya(papkaYadra)

	tegi := make([]string, 0, len(adresa))
	for teg := range adresa {
		tegi = append(tegi, teg)
	}
	// Порядок постоянный: журнал одной машины должен читаться одинаково изо
	// дня в день, иначе разбор превращается в сравнение случайных перечней.
	sort.Strings(tegi)

	var zamok sync.Mutex
	var posledn error
	rabota := make(chan string)
	var gruppa sync.WaitGroup
	for i := 0; i < odnovremenno; i++ {
		gruppa.Add(1)
		go func() {
			defer gruppa.Done()
			for teg := range rabota {
				zamok.Lock()
				bylo := sved.Nabory[teg]
				zamok.Unlock()

				stalo, izmenilsya, err := sverit(ctx, klient, adresa[teg], filepath.Join(dir, teg+".srs"), bylo)

				zamok.Lock()
				switch {
				case err != nil:
					itog.Otkazy++
					posledn = fmt.Errorf("%s: %w", teg, err)
				default:
					itog.Svereno++
					if izmenilsya {
						itog.Obnovleno++
					}
					sved.Nabory[teg] = stalo
				}
				zamok.Unlock()
			}
		}()
	}
	for _, teg := range tegi {
		if err := ctx.Err(); err != nil {
			break
		}
		rabota <- teg
	}
	close(rabota)
	gruppa.Wait()

	if itog.Svereno > 0 {
		if err := zapisatSvedeniya(papkaYadra, sved); err != nil && posledn == nil {
			posledn = fmt.Errorf("сведения кеша: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return itog, err
	}
	return itog, posledn
}

// sverit спрашивает сервер про один набор и, если тот изменился, кладёт его в кеш.
//
// Условный запрос — весь смысл: с ETag и Last-Modified сервер отвечает 304 без
// тела, и сверка всех 23 наборов не стоит почти ничего. Файлы раздаёт nginx
// (KelevraServer/nginx/nginx.conf), оба заголовка он отдаёт сам, а мы их до
// 09.09 просто выбрасывали и качали мегабайт заново.
func sverit(ctx context.Context, klient *http.Client, adres, cel string, bylo SvedeniyaNabora) (SvedeniyaNabora, bool, error) {
	stalo := SvedeniyaNabora{Adres: adres}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adres, nil)
	if err != nil {
		return stalo, false, err
	}
	// Условия ставим, только если файл на диске ЦЕЛ и взят с ТОГО ЖЕ адреса.
	// Иначе 304 оставит нас с пустотой вместо набора: сервер скажет «не
	// менялось», а у нас на диске нечего показывать ядру.
	if bylo.Adres == adres && estFayl(cel) {
		if bylo.ETag != "" {
			req.Header.Set("If-None-Match", bylo.ETag)
		}
		if bylo.Izmenen != "" {
			req.Header.Set("If-Modified-Since", bylo.Izmenen)
		}
	}
	otvet, err := klient.Do(req)
	if err != nil {
		return stalo, false, err
	}
	defer otvet.Body.Close()

	if otvet.StatusCode == http.StatusNotModified {
		stalo = bylo
		stalo.Adres = adres
		stalo.Svereno = time.Now()
		return stalo, false, nil
	}
	if otvet.StatusCode != http.StatusOK {
		return stalo, false, fmt.Errorf("ответ %d", otvet.StatusCode)
	}
	razmer, err := polozhit(otvet.Body, cel)
	if err != nil {
		return stalo, false, err
	}
	stalo.ETag = otvet.Header.Get("ETag")
	stalo.Izmenen = otvet.Header.Get("Last-Modified")
	stalo.Razmer = razmer
	stalo.Svereno = time.Now()
	return stalo, true, nil
}

func estFayl(put string) bool {
	st, err := os.Stat(put)
	return err == nil && st.Size() > 0
}

// polozhit пишет тело в кеш через временный файл. Возвращает размер набора.
func polozhit(telo io.Reader, cel string) (int64, error) {
	vremenny := cel + ".kachaem"
	f, err := os.Create(vremenny)
	if err != nil {
		return 0, err
	}
	razmer, err := io.Copy(f, telo)
	zakryt := f.Close()
	if err == nil {
		err = zakryt
	}
	if err != nil {
		os.Remove(vremenny)
		return 0, err
	}
	// Заголовок SRS — дешёвая проверка, что нам отдали набор правил, а не
	// страницу с ошибкой от чужого прокси или заглушку провайдера.
	if !pohozheNaSRS(vremenny) {
		os.Remove(vremenny)
		return 0, fmt.Errorf("файл не похож на набор правил")
	}
	if err := os.Rename(vremenny, cel); err != nil {
		return 0, err
	}
	return razmer, nil
}

func pohozheNaSRS(put string) bool {
	f, err := os.Open(put)
	if err != nil {
		return false
	}
	defer f.Close()
	shapka := make([]byte, 3)
	if _, err := io.ReadFull(f, shapka); err != nil {
		return false
	}
	return strings.EqualFold(string(shapka), "SRS")
}
