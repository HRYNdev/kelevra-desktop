// Пакет obnovlenie держит приложение свежим без участия человека.
//
// Зачем. Пока обновления нет, каждая новая сборка — это моё письмо со ссылкой и
// его ручное «скачать заново». Ровно так 20.08 у него на руках оказалась
// вчерашняя нерабочая сборка: ссылка была старая, а новую надо было принести
// отдельно. Приложение обязано забирать себя само.
package obnovlenie

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SpisokReliza — откуда берём список релизов.
//
// Именно СПИСОК, а не /latest: в этом же репозитории лежат релизы ядра
// (core-*), и «latest» легко окажется ядром, а не приложением.
//
// per_page=100 (не 20): порядок в ответе GitHub — по дате коммита тега, а
// НЕ по версии (см. Sravnit и TestSpisokNeUporyadochenPoVersii). Замерено
// живьём 25.08.2026: в репозитории 32 релиза, 31 из них app-v, и релиз со
// старым коммитом (app-v0.6.9) лежит в ответе ВЫШЕ более новой app-v0.6.15.
// При окне в 20 и 31 app-релизе самый свежий рискует вовсе выпасть за
// границу страницы — тогда клиент слышит «обновлений нет» уже навсегда,
// сколько бы ни спрашивал.
const SpisokReliza = "https://api.github.com/repos/HRYNdev/kelevra-desktop/releases?per_page=100"

// PrefiksPrilozheniya — метка релизов самого приложения.
const PrefiksPrilozheniya = "app-v"

// PeriodFonovoyProverki — как часто уже работающая (не только что стартовавшая)
// копия сама спрашивает GitHub о новой версии.
//
// obnovitsya() (cmd/kelevra/obnovlenie.go) зовётся РОВНО ОДИН РАЗ — на
// холодном старте переднего процесса. Копия, которую человек не закрывал
// днями (обычный режим работы: свернул в трей и забыл), эту проверку не
// проходит больше никогда — 0.6.23 и 0.6.24 разошлись 0 раз именно поэтому.
// Раз в несколько часов достаточно: обновления выходят не каждый день, а
// более частый опрос не даёт ничего, кроме лишней нагрузки на GitHub API.
const PeriodFonovoyProverki = 4 * time.Hour

// ImyaFayla — как называется наша сборка внутри релиза.
const ImyaFayla = "Kelevra.exe"

// Novaya — найденная свежая сборка.
type Novaya struct {
	Versiya string
	Ssylka  string
	Razmer  int64
}

type relizJSON struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

// OshibkaStatusa — GitHub ответил, но не 200 (сервер жив, но отказал: лимит
// запросов, авария, битый адрес). Код нужен отдельным полем, а не только в
// тексте: вызывающему (кнопка «Проверить обновление» в internal/sluzhba) он
// нужен, чтобы показать человеку «GitHub ответил ошибкой 503», а не парсить
// строку регуляркой.
type OshibkaStatusa struct {
	Kod int
}

func (o *OshibkaStatusa) Error() string {
	return fmt.Sprintf("список релизов: ответ %d", o.Kod)
}

// OshibkaRazbora — тело ответа получено целиком, но это не тот JSON, который
// мы ждём (прокси подсунул страницу-заглушку, GitHub вернул html и т.п.).
type OshibkaRazbora struct {
	Prichina error
}

func (o *OshibkaRazbora) Error() string {
	return fmt.Sprintf("список релизов не разобрать: %v", o.Prichina)
}

func (o *OshibkaRazbora) Unwrap() error { return o.Prichina }

// Proverit возвращает сборку новее текущей или nil, если обновляться не на что.
// Ошибка сети — это не беда приложения: обновление не состоялось, работаем дальше.
// Ошибка сети/DNS/таймаута возвращается КАК ЕСТЬ от http.Client (не
// оборачивается): она уже несёт нужный тип (net.Error/url.Error), и
// вызывающий различает её причину через errors.As/errors.Is, не теряя
// исходную беду в тексте.
func Proverit(ctx context.Context, klient *http.Client, adres, tekushchaya string) (*Novaya, error) {
	if klient == nil {
		klient = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adres, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	otvet, err := klient.Do(req)
	if err != nil {
		return nil, err
	}
	defer otvet.Body.Close()
	if otvet.StatusCode != http.StatusOK {
		return nil, &OshibkaStatusa{Kod: otvet.StatusCode}
	}
	var relizy []relizJSON
	if err := json.NewDecoder(io.LimitReader(otvet.Body, 1<<20)).Decode(&relizy); err != nil {
		return nil, &OshibkaRazbora{Prichina: err}
	}
	// Берём МАКСИМУМ по версии, а не первый попавшийся в ответе.
	// Порядок в ответе GitHub по версиям НЕ упорядочен: 24.08 в этом же
	// репозитории app-v0.6.9 стоял в списке ВЫШЕ, чем app-v0.6.15. Прежний код
	// выходил на первом же app-релизе, и клиент, у которого версия новее этого
	// первого, слышал «обновлений нет» — навсегда, хотя ниже по списку лежали
	// более свежие сборки.
	var luchshiy *relizJSON
	var luchshayaV string
	for i := range relizy {
		r := &relizy[i]
		if r.Draft || r.Prerelease || !strings.HasPrefix(r.TagName, PrefiksPrilozheniya) {
			continue
		}
		versiya := strings.TrimPrefix(r.TagName, PrefiksPrilozheniya)
		if luchshiy == nil || Sravnit(versiya, luchshayaV) > 0 {
			luchshiy, luchshayaV = r, versiya
		}
	}
	if luchshiy == nil || Sravnit(luchshayaV, tekushchaya) <= 0 {
		return nil, nil // самого свежего релиза нет или он не новее нас
	}
	for _, a := range luchshiy.Assets {
		if a.Name == ImyaFayla && a.Size > 0 {
			return &Novaya{Versiya: luchshayaV, Ssylka: a.URL, Razmer: a.Size}, nil
		}
	}
	// Релиз есть, а файла в нём нет: это моя недосборка, а не повод
	// откатываться на более старую версию.
	return nil, fmt.Errorf("в релизе %s нет %s", luchshiy.TagName, ImyaFayla)
}

// Sravnit сравнивает версии вида 1.2.3 (хвост «-rabota» и подобный не мешает):
// 1 — a новее b, -1 — старее, 0 — то же самое.
func Sravnit(a, b string) int {
	ra, rb := razobrat(a), razobrat(b)
	for i := 0; i < 3; i++ {
		if ra[i] != rb[i] {
			if ra[i] > rb[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func razobrat(v string) [3]int {
	var r [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	for i, kusok := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}
		n, err := strconv.Atoi(strings.TrimSpace(kusok))
		if err != nil {
			return r
		}
		r[i] = n
	}
	return r
}

// popytokPereimenovaniya — сколько раз пробуем переименовать файл, если он
// занят: на Windows свежескачанный .exe (или отодвигаемый .old) секунду-другую
// может держать антивирус, и первая попытка штатно проваливается.
const popytokPereimenovaniya = 5

// pauzaPereimenovaniya — шаг паузы между попытками; растёт линейно
// (200,400,600,800 мс), суммарно укладываясь в ~2 секунды при 5 попытках —
// замок антивируса живёт секунды, не часы.
const pauzaPereimenovaniya = 200 * time.Millisecond

// pereimenovat и spat — переменные уровня пакета, а не прямые вызовы
// os.Rename/time.Sleep: тесты пакета подменяют их, чтобы на линуксе
// воспроизвести windows-специфичный отказ переименования занятого файла без
// настоящего антивируса, и чтобы не ждать реальные секунды пауз.
var (
	pereimenovat = os.Rename
	spat         = time.Sleep
)

// pereimenovatSPovtorami пробует переименовать файл несколько раз подряд —
// см. popytokPereimenovaniya.
func pereimenovatSPovtorami(ot, kuda string) error {
	var poslednyaya error
	for i := 0; i < popytokPereimenovaniya; i++ {
		if i > 0 {
			spat(time.Duration(i) * pauzaPereimenovaniya)
		}
		if err := pereimenovat(ot, kuda); err != nil {
			poslednyaya = err
			continue
		}
		return nil
	}
	return poslednyaya
}

// kopirovatFayl копирует содержимое (не переименовывает — на переименование
// уже не хватило попыток дважды) как последний шанс вернуть putExe в рабочее
// состояние, если ни новый файл, ни отодвинутая копия старого не встают на
// место через os.Rename.
func kopirovatFayl(otkuda, kuda string) error {
	dannye, err := os.ReadFile(otkuda)
	if err != nil {
		return err
	}
	return os.WriteFile(kuda, dannye, 0o755)
}

// Hod — сколько байт новой сборки уже у нас (skachano) и сколько их всего
// (vsego). vsego == 0 значит «сколько всего, неизвестно»: сервер не назвал
// длины ответа (ContentLength == -1). Это законный случай, а не беда: окно
// тогда рисует неопределённую полосу и пишет «Скачиваю…» без процента —
// выдуманный процент хуже честного «идёт».
//
// Знаменатель берём именно из ответа ТОГО сервера, который прямо сейчас шлёт
// байты, а не из Novaya.Razmer: размер из списка релизов описывает файл на
// GitHub, а не это соединение (редирект, прокси-заглушка, обрезанный ответ),
// и подставлять его значило бы считать долю от чужого числа.
type Hod func(skachano, vsego int64)

// schetchik — читатель поверх тела ответа, который считает прошедшие мимо
// байты. Обёртка, а не ручной цикл копирования: io.Copy сам подбирает буфер
// и быстрые пути, а всё, что нужно от нас, — знать, сколько уже прошло.
type schetchik struct {
	iz      io.Reader
	vsego   int64
	proshlo int64
	hod     Hod
}

func (s *schetchik) Read(b []byte) (int, error) {
	n, err := s.iz.Read(b)
	if n > 0 {
		s.proshlo += int64(n)
		if s.hod != nil {
			s.hod(s.proshlo, s.vsego)
		}
	}
	return n, err
}

// Postavit кладёт новую сборку на место текущей и возвращает путь к ней.
//
// Windows не даёт затереть запущенный .exe, но даёт его ПЕРЕИМЕНОВАТЬ: старый
// файл уезжает в <имя>.old, новый встаёт на его место. Хвост убирается при
// следующем запуске (UbratHvost), потому что прямо сейчас он ещё занят.
//
// ЖЕЛЕЗНОЕ ОБЕЩАНИЕ: после любого исхода (nil или ошибка) по пути putExe
// обязан лежать РАБОТАЮЩИЙ файл — либо новая версия, либо вернувшаяся
// старая. 28.08.2026 живьём случился ровно провал этого обещания: второе
// переименование упало (антивирус держал свежий .exe), а неприкрытая ошибка
// отката оставила владельца вовсе без .exe — на диске лежали только
// два файла с хвостом .old.
func Postavit(ctx context.Context, klient *http.Client, n Novaya, putExe string) error {
	return PostavitSHodom(ctx, klient, n, putExe, nil)
}

// PostavitSHodom — та же Postavit, но с докладом о ходе скачивания (см. Hod).
// Отдельная дверь, а не шестой параметр у Postavit: звонящих у неё пять, и
// ход нужен ровно одному — окну (internal/sluzhba: PostavitNaydennoe), а
// остальным пришлось бы дописывать nil без всякой на то причины.
//
// hod == nil — обычное дело: считать байты всё равно приходится (обёртка
// стоит на пути), но рассказывать о них некому.
func PostavitSHodom(ctx context.Context, klient *http.Client, n Novaya, putExe string, hod Hod) error {
	if klient == nil {
		klient = &http.Client{Timeout: 5 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.Ssylka, nil)
	if err != nil {
		return err
	}
	otvet, err := klient.Do(req)
	if err != nil {
		return err
	}
	defer otvet.Body.Close()
	if otvet.StatusCode != http.StatusOK {
		return fmt.Errorf("скачивание: ответ %d", otvet.StatusCode)
	}

	// Рядом с самим приложением: переименование поверх дисков не работает, а
	// нам нужна именно замена файла на своём месте.
	//
	// Имя — СВОЁ у каждого вызова (os.CreateTemp), а не жёсткое putExe+".new".
	// Обновление идёт ДО одиночного замка приложения (см. cmd/kelevra), а
	// значит два одновременно запущенных .exe вполне могут звать Postavit
	// разом. При общем имени оба открывали один и тот же файл с O_TRUNC —
	// один процесс обнулял уже записанные байты другого, а проверка размера
	// сверяла СВОИ байты из io.Copy, а не то, что реально лежит на диске:
	// оба проходили проверку, хотя файл на диске был перемешан. Отдельный
	// файл на процесс убирает наложение записей в принципе.
	f, err := os.CreateTemp(filepath.Dir(putExe), ".kelevra-*.new")
	if err != nil {
		return fmt.Errorf("не могу записать рядом с приложением: %w", err)
	}
	vremennyy := f.Name()
	// Уборка мусора на любом выходе с ошибкой: uspeshno=true выставляется
	// только прямо перед тем, как файл встаёт на постоянное место.
	uspeshno := false
	defer func() {
		if !uspeshno {
			os.Remove(vremennyy)
		}
	}()
	// os.CreateTemp создаёт файл с 0600 — нам нужен запускаемый .exe.
	if err := f.Chmod(0o755); err != nil {
		f.Close()
		return fmt.Errorf("не могу выставить права на новый файл: %w", err)
	}

	// Знаменатель — длина ЭТОГО ответа. Её может не быть вовсе
	// (ContentLength == -1 у ответа без Content-Length, например chunked):
	// тогда vsego == 0 и доля неизвестна — см. Hod.
	vsego := otvet.ContentLength
	if vsego < 0 {
		vsego = 0
	}
	// Первый доклад — ДО чтения тела: он говорит окну «загрузка началась» и
	// сразу называет знаменатель. Без него окно узнало бы о начале только с
	// первым прочитанным куском, а на медленной сети это заметная пауза.
	if hod != nil {
		hod(0, vsego)
	}
	_, err = io.Copy(f, &schetchik{iz: otvet.Body, vsego: vsego, hod: hod})
	if err != nil {
		f.Close()
		return err
	}
	// Размер сверяем по ФАКТУ на диске, а не по счётчику io.Copy: счётчик —
	// это сколько байт записал именно я, а на диске могло оказаться другое,
	// если файл написан не тем.
	svedeniya, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("не могу проверить записанный файл: %w", err)
	}
	skachano := svedeniya.Size()
	if err := f.Close(); err != nil {
		return err
	}
	// Оборванная закачка не должна встать на место рабочего приложения.
	if n.Razmer > 0 && skachano != n.Razmer {
		return fmt.Errorf("скачано %d байт вместо %d", skachano, n.Razmer)
	}

	// ПРОВЕРКА ДО того, как тронут putExe. Самый дешёвый способ сдержать
	// железное обещание — не начинать замену, если менять не на что: пока
	// putExe цел и на месте, откатывать нечего и терять нечего. Сюда ловится
	// ровно та беда, из-за которой 28.08 у человека «остались 2 .old и
	// нихуя»: скачался не .exe, а страница-заглушка прокси или обрезок,
	// который запуститься не может.
	if err := proveritNovyy(vremennyy, putExe); err != nil {
		return fmt.Errorf("%w — прежнее приложение не тронуто", err)
	}

	staryy := putExe + ".old"
	os.Remove(staryy)
	// pereimenovatSPovtorami, а не голый os.Rename: первый шаг отодвигает
	// ЗАПУЩЕННЫЙ .exe, и его на секунду может держать антивирус ровно так же,
	// как держит второй (см. popytokPereimenovaniya). Одна попытка тут — это
	// «обновление не встало» на пустом месте.
	if err := pereimenovatSPovtorami(putExe, staryy); err != nil {
		return fmt.Errorf("не могу отодвинуть текущее приложение: %w", err)
	}
	if err := pereimenovatSPovtorami(vremennyy, putExe); err != nil {
		// Второе переименование не встало (обычно антивирус ещё держит
		// свежий .exe) — обязаны вернуть putExe в рабочее состояние.
		// Ошибку отката ПРОВЕРЯЕМ: непроверенный откат ровно и есть причина
		// беды 28.08 (человек остался без .exe).
		return bedaSOtkatom("не могу поставить новое приложение", err, staryy, putExe)
	}
	// ПРОВЕРКА ПОСЛЕ замены. Файл лёг на место — но лёг ли он целым? Между
	// проверкой выше и этой строкой стоят два переименования, и на Windows
	// в них вмешивается антивирус: он может отобрать, обрезать или подменить
	// свежий .exe уже на его постоянном месте. Обещание «после любого исхода
	// по пути putExe лежит РАБОТАЮЩИЙ файл» проверяется здесь, а не
	// предполагается: не сошлось — откатываемся на прежнюю сборку, которая
	// точно работала.
	if err := proveritNovyy(putExe, staryy); err != nil {
		return bedaSOtkatom("новое приложение встало нерабочим", err, staryy, putExe)
	}
	uspeshno = true
	return nil
}

// bedaSOtkatom — общий хвост всех неудач замены: вернуть putExe в рабочее
// состояние и рассказать человеку, чем всё кончилось.
//
// Порядок попыток: переименование отодвинутого файла обратно (дёшево и
// мгновенно), затем его копия (os.Rename не встал — но записать байты можно
// и в занятый по имени путь). Хвост .old после удавшегося отката убирается
// ТУТ ЖЕ, а не откладывается до следующего запуска: уборка (UbratHvost)
// живёт внутри самого приложения, и 28.08 она не сработала ровно потому,
// что приложения к тому моменту уже не существовало. Теперь она не зависит
// от того, встал новый файл или нет.
func bedaSOtkatom(chto string, prichina error, staryy, putExe string) error {
	if otkat := pereimenovatSPovtorami(staryy, putExe); otkat == nil {
		// Переименование само убрало .old (он и стал putExe) — хвоста нет.
		return fmt.Errorf("%s (%v), вернул прежнее", chto, prichina)
	} else if kop := kopirovatFayl(staryy, putExe); kop == nil {
		// putExe снова рабочий, значит .old — уже просто мусор. Удаляем сразу;
		// если файл занят (на Windows это ЗАПУЩЕННАЯ копия), отказ не беда —
		// доберёт UbratHvost на следующем запуске, и добирать будет чему,
		// потому что .exe на месте.
		os.Remove(staryy)
		return fmt.Errorf("%s (%v), откат переименованием не удался (%v), вернул прежнее копией из %s", chto, prichina, otkat, staryy)
	} else {
		return fmt.Errorf("%s (%v), откат тоже не удался (%v), и копия не удалась (%v): приложение осталось в файле %s, переименуйте его вручную обратно в %s", chto, prichina, otkat, kop, staryy, putExe)
	}
}

// proveritNovyy — годится ли скачанный файл на роль приложения. Переменная
// уровня пакета, а не прямой вызов: тесты подменяют её, чтобы разыграть
// «новый файл не запустился» без настоящего битого .exe, и чтобы будущая,
// более строгая проверка (например, пробный запуск) вставала сюда одной
// строкой, а не переписыванием Postavit.
var proveritNovyy = pohozhNaObrazets

// dlinaPodpisi — сколько первых байт файла читаем, чтобы узнать его род.
// Четырёх хватает на обе подписи, которые нас касаются: "MZ" у Windows-PE и
// "ELF" у линуксовых сборок стенда.
const dlinaPodpisi = 4

// pohozhNaObrazets — новый файл того же РОДА, что и тот, который он меняет?
//
// Сравниваем не байты и не размер, а род: исполняемый файл Windows (PE, "MZ")
// или линукса (ELF). Образцом служит файл, который прямо сейчас работает у
// человека, — он и есть эталон правильного рода.
//
// Почему сравнение с образцом, а не жёсткое «обязан быть PE»: под линуксом
// (стенд, CI) обе стороны — обычные файлы без всякой подписи, и жёсткое
// правило объявило бы там сломанным любое обновление. Образец делает проверку
// одинаково честной в обоих мирах: на боевой Windows образец — настоящий .exe,
// и страница-заглушка прокси, HTML-ошибка GitHub или обрезок закачки эту
// проверку не пройдут; на стенде подписи нет ни у кого, и проверка молчит.
//
// Род образца не опознан или образец не читается — проверка МОЛЧИТ и
// пропускает: сравнивать не с чем, а запрещать обновление из-за собственной
// слепоты хуже, чем разрешить его. Обещание про целость putExe от этого не
// страдает: его держат откаты в bedaSOtkatom, а не эта проверка.
func pohozhNaObrazets(novyy, obrazets string) error {
	rodObraztsa := rodFayla(obrazets)
	if rodObraztsa == "" {
		return nil
	}
	rodNovogo := rodFayla(novyy)
	if rodNovogo != rodObraztsa {
		if rodNovogo == "" {
			return fmt.Errorf("скачано не приложение: файл не похож на исполняемый (%s), а прежний — %s", rodNeizvestnyy, rodObraztsa)
		}
		return fmt.Errorf("скачано не приложение: файл рода %s, а прежний — %s", rodNovogo, rodObraztsa)
	}
	return nil
}

// rodNeizvestnyy — как называем в тексте ошибки род, который не опознали.
const rodNeizvestnyy = "неопознанный"

// rodFayla определяет род файла по первым байтам: "PE" (Windows .exe),
// "ELF" (линукс) или "" — ни то, ни другое.
func rodFayla(put string) string {
	f, err := os.Open(put)
	if err != nil {
		return ""
	}
	defer f.Close()
	b := make([]byte, dlinaPodpisi)
	n, _ := io.ReadFull(f, b)
	b = b[:n]
	switch {
	case len(b) >= 2 && b[0] == 'M' && b[1] == 'Z':
		return "PE"
	case len(b) >= 4 && b[0] == 0x7f && b[1] == 'E' && b[2] == 'L' && b[3] == 'F':
		return "ELF"
	}
	return ""
}

// UbratHvost удаляет мусор прошлых обновлений рядом с putExe: сам putExe и
// прочие файлы в папке не трогает.
//   - <имя>.old — штатный хвост честной замены (см. Postavit);
//   - <имя без .exe>.old — след уже исправленного бага, когда хвост получал
//     имя без расширения .exe;
//   - .kelevra-*.new — забытые временные файлы недокачанных или не вставших
//     на место попыток обновления.
func UbratHvost(putExe string) {
	os.Remove(putExe + ".old")
	bezRasshireniya := strings.TrimSuffix(putExe, filepath.Ext(putExe))
	os.Remove(bezRasshireniya + ".old")
	if musor, err := filepath.Glob(filepath.Join(filepath.Dir(putExe), ".kelevra-*.new")); err == nil {
		for _, m := range musor {
			os.Remove(m)
		}
	}
}

// PutSebya — путь к самому себе, разрешённый до настоящего файла.
func PutSebya() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if nastoyashchiy, err := filepath.EvalSymlinks(p); err == nil {
		return nastoyashchiy, nil
	}
	return p, nil
}
