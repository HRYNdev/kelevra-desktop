// Пакет sluzhba: вся работа приложения, выставленная наружу как маленький
// локальный HTTP API, и интерфейс поверх него.
//
// Интерфейс — обычная страница в окне приложения. Такой стык даёт две вещи:
// окно можно проверить в браузере на сервере, где Windows нет, а логика
// проверяется без окна вовсе.
package sluzhba

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/avtozapusk"
	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/prava"
	"github.com/HRYNdev/kelevra-desktop/internal/pravila"
	"github.com/HRYNdev/kelevra-desktop/internal/proksi"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

//go:embed oblik/*
var oblik embed.FS

// Sluzhba — приложение целиком, без оболочки окна.
type Sluzhba struct {
	Nastroyki *hranenie.Nastroyki
	Yadro     *yadro.Yadro
	Podpiska  *podpiska.Klient

	// poprositPrava — точка подмены для стенда: настоящее окно UAC на linux
	// не показать, а то, что новая копия получает pid этой, ещё не повышенной
	// (передачу смены, а не гонку — см. polnayaZashchita), проверять надо
	// именно тут.
	poprositPrava func(smenaPID int) error
	// vyhod — как эта копия уходит после согласия на права. По умолчанию
	// os.Exit(0); на стенде подменяется, иначе тест убил бы сам себя.
	vyhod func()
	// zapustitYadro — точка подмены для тестов лестницы подстраховок в
	// PodnyatZashchitu (23-24.08): Yadro.Zapustit порождает настоящий
	// процесс, а тест должен управлять числом попыток и их исходом без
	// живого ядра. По умолчанию nil — PodnyatZashchitu зовёт s.Yadro.Zapustit.
	zapustitYadro func(context.Context) error
	// avtorezhimDlyaKnopki — точка подмены для тестов domaSeychas/podklyuchit:
	// собирает *avtorezhim.Avtorezhim для одиночного доверенного захода
	// кнопки «Подключиться» на подставных зондах, без настоящей сети. nil —
	// domaSeychas собирает боевой avtorezhim.Novyy() с s.tunnelPodnyat (см.
	// avtorezhimBoevoy).
	avtorezhimDlyaKnopki func() *avtorezhim.Avtorezhim
	// avtorezhimDnsAdres — из KELEVRA_AVTOREZHIM_DNS (Novaya): "ip:port"
	// резолвера, которого DNS-зонд обязан спрашивать НАПРЯМУЮ вместо
	// системного пути, и в domaSeychas, и в фоновом авторежиме
	// (ZapustitAvtorezhim). Пусто в бою — поведение прежнее (см.
	// avtorezhimBoevoy). Нужно исключительно площадке стендов: сам контейнер,
	// в котором они гоняются, живёт за резолвером, отвечающим на контрольные
	// домены fake-ip подменой (тот же диапазон 198.18.0.0/15, что и у
	// нашего ядра) — без этой настройки зонд честно, но ложно решает «дома»
	// и «Подключиться» молча не поднимает защиту (см. stend/zond_doma.sh).
	avtorezhimDnsAdres string
	// avtorezhimKnopkaTaimaut — таймаут одиночного захода кнопки
	// «Подключиться» (domaSeychas). <=0 значит 5 секунд — своё поле только
	// ради теста «заход завис», чтобы не ждать боевые 5с на каждый прогон.
	avtorezhimKnopkaTaimaut time.Duration
	// posleAvtozaprosaPrav — сигнал стенду, что zaprositPravaAvtomaticheskiEsliNado
	// дописала отметку на диск (успех или отказ — не важно). Фоновая
	// горутина не отдаёт своего результата вызвавшему, а тест не должен
	// узнавать о её завершении опросом незащищённого поля (так и была
	// поймана гонка 27.08 — busy-poll по s.Nastroyki.UzheSprosiliPrava() без
	// синхронизации). nil в бою — ничего не подмешивает.
	posleAvtozaprosaPrav func()
	// sohranitNastroyki и priUhode — две точки подмены ТОЛЬКО для доказательства
	// ПОРЯДКА в zaprositPravaAvtomaticheskiEsliNado (правка 27.08: сохранение
	// отметки обязано случиться раньше ухода). Временем этот порядок не
	// поймать: настоящий os.Exit откладывается на 300 мс в uydiPosleSoglasiyaNaPrava,
	// а Sohranit на t.TempDir() исполняется за микросекунды что до, что после
	// спавна той горутины — обе версии кода одинаково «успевают» с большим
	// запасом. Метки пишутся синхронно в момент вызова, отдельно от времени.
	// По умолчанию nil — sohranitNastroyki вызывает hranenie.Sohranit(s.Nastroyki)
	// напрямую, priUhode ничего не делает.
	sohranitNastroyki func() error
	priUhode          func()

	// OblachkoObnovleniya — как сказать о находке пузырём в трее (cmd/kelevra/
	// trey_windows.go: pokazatOblachkoObnovleniya), подключается снаружи из
	// main.go: пакет sluzhba про Windows-трей ничего не знает и знать не
	// должен (см. шапку файла — окно и логика разведены нарочно). nil —
	// сборка вызывающего кода не подключила хук (например, стенд-тесты
	// внутри этого пакета) — тогда находка всё равно видна в
	// /api/sostoyanie, просто без звука.
	OblachkoObnovleniya func(versiya string)

	// MetkaObnovleniya — как показать СОСТОЯНИЕ «обновление ждёт» на значке
	// трея (cmd/kelevra/metka_obnovleniya.go: pometitObnovlenie). Отдельный
	// хук от OblachkoObnovleniya нарочно, и вот почему: пузырь — СОБЫТИЕ,
	// про версию он звучит ровно один раз навсегда (povestitEsliNovaya,
	// отметка на диске), а метка — СОСТОЯНИЕ, она обязана держаться, пока
	// обновление не поставлено. Свяжи их вместе — и после перезапуска копии
	// (пузырь про эту версию уже сказан в прошлой жизни) значок снова
	// выглядел бы так, будто ставить нечего, и тыкать человеку было бы не
	// во что. Поэтому зовётся на КАЖДОЙ фоновой проверке: версия — есть
	// находка, "" — версия и так свежая, метку снять. nil — хук не
	// подключён (стенд-тесты внутри пакета).
	MetkaObnovleniya func(versiya string)

	// PerezapuskPosleObnovleniya — как поднять новую копию после того, как
	// PostavitNaydennoe заменила .exe на диске: та же передача смены, что уже
	// работает у prava.Poprosit после согласия на UAC (cmd/kelevra/main.go:
	// zapustitSmenuPosleObnovleniya, --smena, zhdatSmenu) — новая копия сама
	// дожидается смерти этого pid, а не гонка на фиксированной паузе.
	// Подключается снаружи из main.go по тому же принципу, что и
	// OblachkoObnovleniya выше: пакет sluzhba не порождает процессы сам. nil —
	// хук не подключён (стенд-тесты внутри этого пакета); тогда установка всё
	// равно считается удавшейся (файл на диске уже новый), но эта копия себя
	// НЕ гасит — иначе человек остался бы вообще без работающей копии.
	//
	// put — путь к новому .exe, УЖЕ разрешённый до Postavit(), а не то, что
	// хук получил бы, спроси он obnovlenie.PutSebya() заново сам: на Linux
	// os.Executable() у ещё живого (старого) процесса после Postavit()
	// возвращает путь ПЕРЕИМЕНОВАННОГО файла (readlink /proc/self/exe следует
	// за inode при rename) — замерено живьём при разборе stend/
	// obnovlenie_postavit.sh: вторая копия PutSebya() отдавала «...Kelevra.old»,
	// и новая копия лишний раз сама себя перекачивала поверх уже поставленной
	// версии, прежде чем встать правильно. Тот же put, что уже использован для
	// самого Postavit(), snимает вопрос совсем — его и передаём.
	PerezapuskPosleObnovleniya func(put string, pid int) error

	zamok      sync.Mutex
	svedeniya  *podpiska.Svedeniya
	klyuch     string
	kachaemBin bool // идёт скачивание ядра
	kartina    konfig.Kartina

	// naydennoeObnovlenie — находка ФОНОВОЙ проверки (SleditZaObnovleniem,
	// obnovlenieProveritRuchka), не ручного нажатия кнопки: та отвечает
	// прямо в HTTP-ответ и не запоминает себя. nil — фон ничего не нашёл
	// (или ещё не спрашивал). Видна человеку через /api/sostoyanie тем же
	// полем, что и остальная правда о приложении — молчащая в логе находка
	// человеку не видна вообще.
	naydennoeObnovlenie *obnovlenie.Novaya
	// idetProverkaObnovleniya — не даёт фоновому тику и толчку от открытия
	// окна другой копии (obnovlenieProveritRuchka) спросить GitHub разом.
	idetProverkaObnovleniya bool
	// idetUstanovkaObnovleniya — свой замок, отдельный от idetProverkaObnovleniya
	// выше: второй тычок в пузырь, пока первый ещё качает найденную сборку, не
	// должен звать obnovlenie.Postavit второй раз (см. PostavitNaydennoe).
	idetUstanovkaObnovleniya bool

	// Авторежим (переключение защиты по смене сети) живёт под своим замком,
	// отдельным от zamok выше: запуск/остановка служителя не должны ждать
	// того же замка, что держат при пересборке конфига или скачивании ядра.
	avtorezhimZamok  sync.Mutex
	avtorezhimOtmena context.CancelFunc     // не nil, пока служитель крутится
	avtorezhimEkz    *avtorezhim.Avtorezhim // тот же экземпляр — источник обстановки для /api/sostoyanie

	// avtorezhimKnopkaObstanovka — обстановка, увиденная последним заходом
	// domaSeychas (кнопка «Подключиться»). Отдельно от avtorezhimEkz: тот
	// живёт, только пока фоновый служитель поднят тумблером
	// Nastroyki.Avtorezhim, а кнопка обязана спрашивать обстановку всегда,
	// тумблера не касаясь (заказ Вовы 28.08). /api/sostoyanie берёт отсюда,
	// когда фонового служителя нет.
	avtorezhimKnopkaObstanovka avtorezhim.Sostoyanie
}

// Novaya собирает службу на настоящих путях приложения.
// KELEVRA_PODPISKA и KELEVRA_SHEMA переопределяют сервер подписки, а
// KELEVRA_AVTOREZHIM_DNS — резолвер авторежима (см. avtorezhimDnsAdres) —
// это нужно только для проверки приложения на стенде, у пользователя они не заданы.
func Novaya() (*Sluzhba, error) {
	n, err := hranenie.Zagruzit()
	if err != nil {
		return nil, err
	}
	if err := hranenie.Sohranit(n); err != nil { // закрепляем device_id при первом запуске
		return nil, err
	}
	s := &Sluzhba{
		Nastroyki:          n,
		Yadro:              &yadro.Yadro{Bin: hranenie.PutYadra(), Papka: hranenie.PapkaYadra()},
		Podpiska:           &podpiska.Klient{DeviceID: n.DeviceID, Host: os.Getenv("KELEVRA_PODPISKA"), Shema: os.Getenv("KELEVRA_SHEMA")},
		klyuch:             sluchaynyy(),
		avtorezhimDnsAdres: os.Getenv("KELEVRA_AVTOREZHIM_DNS"),
	}
	// Профиль мог остаться с прошлого запуска: пересобираем его под нынешние
	// права, чтобы состояние в окне было правдой ещё до первого нажатия.
	_ = s.PerestroitKonfig()
	return s, nil
}

// PerestroitKonfig готовит рабочий конфиг ядра из профиля, который прислал
// сервер: убирает поля, работающие только на телефоне, и выбирает режим по
// правам. Без этого ядро на компьютере не стартует вообще.
func (s *Sluzhba) PerestroitKonfig() error { return s.perestroit(konfig.Vybor{}) }

// perestroit принимает отступления от обычной сборки конфига (dop.Prava
// подставляется тут же, по факту прав на машине — вызывающему коду задавать
// его незачем). BezSistemnogoProksi и BezSetevyhPravil в dop складываются, а
// не заменяют друг друга: PodnyatZashchitu может взвести оба по очереди на
// одном и том же подключении, и второй отказ не должен откатывать первую
// подстраховку.
func (s *Sluzhba) perestroit(dop konfig.Vybor) error {
	syroy, err := os.ReadFile(hranenie.PutProfilya())
	if err != nil {
		return err
	}
	dop.Prava = prava.Est()
	gotovyy, k, err := konfig.Prigotovit(syroy, dop)
	if err != nil {
		return err
	}
	if err := s.Yadro.ZapisatKonfig(gotovyy); err != nil {
		return err
	}
	s.Yadro.Api, s.Yadro.Sekret = k.ClashAdres, k.ClashSekret
	s.zamok.Lock()
	s.kartina = k
	s.zamok.Unlock()
	log.Printf("конфиг собран: режим %s, права %v, туннель в профиле %v, Clash API %s%s",
		k.Rezhim, prava.Est(), k.EstTunnel, k.ClashAdres, zametka(k.Zametka))
	return nil
}

// SohranitProfil кладёт присланный профиль на диск и пересобирает конфиг ядра.
func (s *Sluzhba) SohranitProfil(syroy []byte) error {
	if err := os.MkdirAll(hranenie.Papka(), 0o755); err != nil {
		return err
	}
	vremenny := hranenie.PutProfilya() + ".tmp"
	if err := os.WriteFile(vremenny, syroy, 0o600); err != nil {
		return err
	}
	if err := os.Rename(vremenny, hranenie.PutProfilya()); err != nil {
		return err
	}
	return s.PerestroitKonfig()
}

// Adres — на чём слушать: только петля, наружу приложение не смотрит.
func (s *Sluzhba) Slushat() (net.Listener, string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	url := fmt.Sprintf("http://%s/%s/", l.Addr().String(), s.klyuch)
	return l, url, nil
}

// Obsluzhit — маршруты приложения. Всё висит под случайным ключом в пути,
// чтобы чужая программа на этой же машине не дёргала наше API.
func (s *Sluzhba) Obsluzhit() http.Handler {
	m := http.NewServeMux()
	pref := "/" + s.klyuch
	stranicy, _ := fsPodpapki()
	m.Handle(pref+"/", http.StripPrefix(pref+"/", http.FileServer(http.FS(stranicy))))
	m.HandleFunc(pref+"/api/sostoyanie", s.sostoyanie)
	m.HandleFunc(pref+"/api/kod", s.kod)
	m.HandleFunc(pref+"/api/podklyuchit", s.podklyuchit)
	m.HandleFunc(pref+"/api/otklyuchit", s.otklyuchit)
	m.HandleFunc(pref+"/api/polnaya_zashchita", s.polnayaZashchita)
	m.HandleFunc(pref+"/api/avtozapusk", s.avtozapuskRuchka)
	m.HandleFunc(pref+"/api/avtorezhim", s.avtorezhimRuchka)
	m.HandleFunc(pref+"/api/uzly", s.uzly)
	m.HandleFunc(pref+"/api/vybrat", s.vybrat)
	m.HandleFunc(pref+"/api/zamerit", s.zamerit)
	m.HandleFunc(pref+"/api/zhurnal", s.zhurnal)
	m.HandleFunc(pref+"/api/obnovlenie", s.obnovlenieRuchka)
	m.HandleFunc(pref+"/api/obnovlenie_proverit", s.obnovlenieProveritRuchka)
	m.HandleFunc(pref+"/api/obnovlenie_postavit", s.obnovleniePostavitRuchka)
	return m
}

// srokProverkiObnovleniya — сколько ждём ответа GitHub на нажатие «Проверить
// обновление» в окне. Короче обычного (obnovlenie идёт в фоне при старте) —
// тут человек стоит и смотрит на подпись «Проверяем…».
// var, а не const: тест на таймаут (obnovlenie_test.go) сокращает срок,
// иначе пришлось бы ждать настоящие 6 секунд на каждый прогон.
var srokProverkiObnovleniya = 6 * time.Second

// srokUstanovkiObnovleniya — сколько ждём скачивание найденной сборки по
// тычку в пузырь (та же величина, что и у холодного обновления в
// cmd/kelevra/obnovlenie.go: srokZagruzki, ~8 МБ по обычной сети укладываются
// с большим запасом).
const srokUstanovkiObnovleniya = 3 * time.Minute

type otvetObnovleniya struct {
	Tekushchaya string `json:"tekushchaya"`
	Novaya      string `json:"novaya,omitempty"`
	Beda        string `json:"beda,omitempty"`
}

// prichinaBedyObnovleniya переводит ошибку obnovlenie.Proverit в русскую
// фразу без жаргона: человек за компьютером (Вова) не программист, «не
// удалось проверить» на любую беду молчит о причине — нет сети у него дома,
// GitHub лёг или просто долго думает, это три разных «что делать».
func prichinaBedyObnovleniya(err error) string {
	var oshibkaStatusa *obnovlenie.OshibkaStatusa
	if errors.As(err, &oshibkaStatusa) {
		return fmt.Sprintf("GitHub ответил ошибкой %d", oshibkaStatusa.Kod)
	}
	var oshibkaRazbora *obnovlenie.OshibkaRazbora
	if errors.As(err, &oshibkaRazbora) {
		return "GitHub ответил непонятным"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("GitHub не ответил за %d секунд", int(srokProverkiObnovleniya/time.Second))
	}
	var setevaya net.Error
	if errors.As(err, &setevaya) && setevaya.Timeout() {
		return fmt.Sprintf("GitHub не ответил за %d секунд", int(srokProverkiObnovleniya/time.Second))
	}
	return "нет интернета, проверить не у кого"
}

// obnovlenieRuchka — «Проверить обновление» в настройках. Обновление и так
// ставится само и молча при каждом запуске (cmd/kelevra/obnovlenie.go), но
// человеку негде спросить прямо сейчас, свежая ли у него версия (эталон
// телефона: SimpleSettingsScreen.kt, пункт «Проверить обновление»).
// Ошибка сети — обычное дело для приложения, которое чинит сеть: отдаём её
// строкой, а не роняем ручку, чтобы окно не повисло (стенд гоняется без
// сети нарочно, см. stend/oblik_snimok.py).
func (s *Sluzhba) obnovlenieRuchka(w http.ResponseWriter, r *http.Request) {
	adres := obnovlenie.SpisokReliza
	if svoy := os.Getenv("KELEVRA_RELIZY"); svoy != "" {
		adres = svoy // стенд
	}
	ctx, otmena := context.WithTimeout(r.Context(), srokProverkiObnovleniya)
	defer otmena()
	o := otvetObnovleniya{Tekushchaya: podpiska.Versiya}
	n, err := obnovlenie.Proverit(ctx, &http.Client{Timeout: srokProverkiObnovleniya}, adres, podpiska.Versiya)
	if err != nil {
		log.Printf("проверка обновления по нажатию: %v", err)
		o.Beda = prichinaBedyObnovleniya(err)
	} else if n != nil {
		o.Novaya = n.Versiya
	}
	otdat(w, o, nil)
}

// obnovlenieProveritRuchka — тихий толчок фоновой проверке от ДРУГОЙ копии
// (cmd/kelevra/main.go: chuzhaya в adresKopii). Человек кликнул значок в
// трее, пока Kelevra уже работала где-то в фоне неделями, — момент не хуже
// холодного старта, чтобы спросить GitHub. Сам ответ HTTP ждать нечего:
// проверка идёт в своей горутине, а ручка отвечает сразу, чтобы открытие
// чужого окна не задержалось на сетевой таймаут.
func (s *Sluzhba) obnovlenieProveritRuchka(w http.ResponseWriter, r *http.Request) {
	go s.ProveritObnovlenieFonom()
	otdat(w, map[string]any{"zapushcheno": true}, nil)
}

// ProveritObnovlenieFonom — тело фоновой проверки, общее для тикера
// (SleditZaObnovleniem) и толчка от другой копии (obnovlenieProveritRuchka).
//
// Не ставит найденную сборку сама (obnovlenie.Postavit тут не звучит): это
// значило бы менять .exe и просить человека на перезапуск прямо у него под
// руками, без предупреждения, посреди рабочего сеанса с поднятой защитой —
// то, что задача запрещает напрямую. Только запоминает находку в
// naydennoeObnovlenie, чтобы её увидел /api/sostoyanie — тем же путём, каким
// человек уже видит остальную правду о приложении (кнопка «Проверить
// обновление» ставит её так же, только сразу в HTTP-ответ и без памяти).
//
// idetProverkaObnovleniya не даёт наложиться двум одновременным проверкам
// (тик и толчок совпали): вторая тихо выходит, не трогая сеть повторно.
func (s *Sluzhba) ProveritObnovlenieFonom() {
	s.zamok.Lock()
	if s.idetProverkaObnovleniya {
		s.zamok.Unlock()
		return
	}
	s.idetProverkaObnovleniya = true
	s.zamok.Unlock()
	defer func() {
		s.zamok.Lock()
		s.idetProverkaObnovleniya = false
		s.zamok.Unlock()
	}()

	adres := obnovlenie.SpisokReliza
	if svoy := os.Getenv("KELEVRA_RELIZY"); svoy != "" {
		adres = svoy // стенд
	}
	ctx, otmena := context.WithTimeout(context.Background(), srokProverkiObnovleniya)
	defer otmena()
	n, err := obnovlenie.Proverit(ctx, &http.Client{Timeout: srokProverkiObnovleniya}, adres, podpiska.Versiya)
	if err != nil {
		// Нет сети — обычное дело для приложения, которое чинит сеть: тихо,
		// без паники и без падения процесса. Следующий тик попробует снова.
		log.Printf("фоновая проверка обновления: не вышло (%v), работаю как есть", err)
		return
	}
	s.zamok.Lock()
	s.naydennoeObnovlenie = n // может стать снова nil — «версия и так свежая», это тоже правда
	s.zamok.Unlock()
	if s.MetkaObnovleniya != nil {
		// СОСТОЯНИЕ, а не событие: и находку, и её исчезновение (версия и
		// так свежая) значок обязан отразить — см. поле MetkaObnovleniya.
		if n != nil {
			s.MetkaObnovleniya(n.Versiya)
		} else {
			s.MetkaObnovleniya("")
		}
	}
	if n != nil {
		log.Printf("фоновая проверка обновления: найдена версия %s", n.Versiya)
		s.povestitEsliNovaya(n.Versiya)
	}
}

// povestitEsliNovaya решает, стоит ли беспокоить человека пузырём в трее.
//
// ГОВОРИМ РОВНО ОДИН РАЗ НА ВЕРСИЮ. Тик крутится раз в несколько часов
// (SleditZaObnovleniem) и без этой проверки копия, которая молча висит в
// трее сутками, повторяла бы один и тот же пузырь на каждом тике — хозяин
// продукта 26.08 ругался матом именно на повторяющиеся уведомления.
//
// Отметка хранится на диске (hranenie.Nastroyki.ObyavlennoeObnovlenie), не
// только в памяти процесса: перезапуск НЕ повод сказать заново про версию,
// про которую уже сказали в прошлом запуске — само обновление умеет
// перезапускать себя (cmd/kelevra/obnovlenie.go), а человек может закрыть и
// снова открыть приложение вручную; ни то ни другое не значит, что он забыл
// уже увиденный пузырь. Вышла версия ЕЩЁ новее — строка не совпадёт, и
// объявление придёт снова: каждая новая версия имеет право прозвучать один раз.
func (s *Sluzhba) povestitEsliNovaya(versiya string) {
	if s.Nastroyki.ObyavlennoeObnovlenie == versiya {
		return
	}
	s.Nastroyki.ObyavlennoeObnovlenie = versiya
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		log.Printf("не сохранил отметку об объявленном обновлении: %v", err)
	}
	if s.OblachkoObnovleniya != nil {
		s.OblachkoObnovleniya(versiya)
	}
}

// obnovleniePostavitRuchka — тычок в пузырь трея (cmd/kelevra/trey_windows.go:
// tychokVPuzyr) либо ручной путь к тому же самому: заказ человека 26.08 —
// «приходит обновление и ты тыкаешь, и всё», а не открывать окно и жать
// «Проверить обновление» самому. Только POST: это действие, а не чтение.
func (s *Sluzhba) obnovleniePostavitRuchka(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		otdat(w, nil, fmt.Errorf("только POST"))
		return
	}
	versiya, err := s.PostavitNaydennoe()
	if err != nil {
		otdat(w, nil, err)
		return
	}
	otdat(w, map[string]any{"gotovo": true, "versiya": versiya}, nil)
}

// PostavitNaydennoe скачивает и ставит НАЙДЕННУЮ фоновой проверкой сборку
// (naydennoeObnovlenie) — в отличие от ProveritObnovlenieFonom, который
// только запоминает находку и никогда сам не качает и не ставит (см. её
// комментарий). Тычок человека — вот то согласие, которого не хватало.
//
// idetUstanovkaObnovleniya под тем же zamok, что и naydennoeObnovlenie: второй
// тычок, пока первый ещё качает, тихо получает «установка уже идёт» вместо
// того, чтобы звать obnovlenie.Postavit второй раз (риск наложения записи —
// см. комментарий Postavit про CreateTemp на процесс).
//
// Любая неудача — ничего не гасим и не трогаем: эта копия продолжает жить
// старой версией, человек может нажать «Проверить обновление» и попробовать
// снова. Успех — уходим тем же путём, каким уходит polnayaZashchita после
// согласия на UAC: гасим своё ядро, поднимаем смену (PerezapuskPosleObnovleniya)
// и завершаем процесс, — с той же паузой ~300мс, чтобы HTTP-ответ успел уйти.
func (s *Sluzhba) PostavitNaydennoe() (string, error) {
	s.zamok.Lock()
	if s.naydennoeObnovlenie == nil {
		s.zamok.Unlock()
		return "", fmt.Errorf("обновления нет: сперва проверка")
	}
	if s.idetUstanovkaObnovleniya {
		s.zamok.Unlock()
		return "", fmt.Errorf("установка уже идёт")
	}
	n := *s.naydennoeObnovlenie
	s.idetUstanovkaObnovleniya = true
	s.zamok.Unlock()
	defer func() {
		s.zamok.Lock()
		s.idetUstanovkaObnovleniya = false
		s.zamok.Unlock()
	}()

	put, err := obnovlenie.PutSebya()
	if err != nil {
		log.Printf("установка обновления по тычку: не знаю, где лежу: %v", err)
		return "", fmt.Errorf("не знаю, где лежу: %w", err)
	}
	ctx, otmena := context.WithTimeout(context.Background(), srokUstanovkiObnovleniya)
	defer otmena()
	if err := obnovlenie.Postavit(ctx, &http.Client{Timeout: srokUstanovkiObnovleniya}, n, put); err != nil {
		log.Printf("установка обновления по тычку: не вышло (%v), работаю старой версией", err)
		return "", fmt.Errorf("не поставилось: %w", err)
	}
	log.Printf("установка обновления по тычку: версия %s встала на диск", n.Versiya)

	pid := os.Getpid()
	go func() {
		time.Sleep(300 * time.Millisecond) // дать HTTP-ответу уйти в окно/пузырь
		if s.PerezapuskPosleObnovleniya == nil {
			// Не гасим себя: без хука новую копию поднять некому, и человек
			// остался бы вообще без работающей защиты. Файл на диске уже
			// новый — свежая версия встанет со следующего обычного запуска.
			log.Printf("установка обновления: хук перезапуска не подключён, эта копия продолжает работать старым процессом")
			return
		}
		_ = s.Yadro.Ostanovit() // ядро старой копии гасим сами, как и polnayaZashchita
		if err := s.PerezapuskPosleObnovleniya(put, pid); err != nil {
			log.Printf("установка обновления: не поднял новую копию: %v", err)
			return
		}
		if s.vyhod != nil {
			s.vyhod()
			return
		}
		os.Exit(0)
	}()
	return n.Versiya, nil
}

// SleditZaObnovleniem крутит ProveritObnovlenieFonom по расписанию, пока
// живёт служба (тот же ctx, что и ObnovlyatProfil — оба гасит один и тот же
// defer otmena() в zapustitSluzhbu).
//
// Первая проверка — СРАЗУ при старте, до входа в цикл тикера (заказ Вовы
// 26.08: «просто приходит обновление и ты тыкаешь, а не автоматом» — раньше
// единственным моментом узнать о новой версии был холодный старт, где
// cmd/kelevra/obnovlenie.go молча ставил её сам; теперь установка требует
// тычка человека, а найти обновление служба обязана сама и сразу, не через
// period часов — иначе «приходит само» не выполняется, просто откладывается
// на 4 часа). Отменённый ДО первого вызова ctx уважаем и в сеть не ходим
// вовсе. KELEVRA_BEZ_OBNOVLENIYA=1 — тот же переключатель, что раньше глушил
// obnovitsya() на холодном старте, — глушит и эту немедленную проверку: на
// нём стоит масса стендов (stend/*.sh), которым сеть тут не нужна и не
// должна понадобиться неожиданно. Сам тикер эта переменная не трогает, как и
// раньше.
// period внедряемый (не голая obnovlenie.PeriodFonovoyProverki внутри
// функции): стенд гоняет его в миллисекундах, а не ждёт настоящих часов.
func (s *Sluzhba) SleditZaObnovleniem(ctx context.Context, period time.Duration) {
	if os.Getenv("KELEVRA_BEZ_OBNOVLENIYA") != "1" {
		select {
		case <-ctx.Done():
			return
		default:
			s.ProveritObnovlenieFonom()
		}
	}
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.ProveritObnovlenieFonom()
		}
	}
}

// zhurnal отдаёт хвост журнала прямо в окно.
//
// Иначе единственный способ прислать причину — идти в %LOCALAPPDATA% через
// проводник, а человек, у которого «не работает», этого делать не будет.
// Отдаём текстом, чтобы его можно было выделить и скопировать целиком.
func (s *Sluzhba) zhurnal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	b, err := os.ReadFile(hranenie.PutZhurnala())
	if err != nil {
		fmt.Fprintf(w, "журнал не читается: %v", err)
		return
	}
	const predel = 40 * 1024 // больше в окно всё равно не влезет
	if len(b) > predel {
		b = b[len(b)-predel:]
	}
	_, _ = w.Write(b)
}

// uzly — какие группы выходов есть у ядра и что в них выбрано.
//
// Пока ядро работает, спрашиваем сам Clash API — это правда с точностью до
// секунды. Пока оно стоит, спросить некого, но список узлов — не тайна: он
// часть конфига, который уже лежит на диске (PerestroitKonfig пишет его при
// каждом сохранении кода доступа, до всякого «Подключить»). Раньше в этом
// состоянии окно отдавало пустой список и человек видел 300px пустоты вместо
// списка (Вова, снимок 21.08) — здесь показываем ту же конфигурацию, только
// без задержек: их взять неоткуда без живого ядра.
func (s *Sluzhba) uzly(w http.ResponseWriter, r *http.Request) {
	if s.Yadro.Sost() == yadro.Rabotaet {
		g, err := s.Yadro.Gruppy()
		if err != nil {
			otdat(w, nil, err)
			return
		}
		otdat(w, map[string]any{"gruppy": g}, nil)
		return
	}
	syroy, err := os.ReadFile(s.Yadro.PutKonfiga())
	if err != nil {
		otdat(w, map[string]any{"gruppy": []any{}}, nil) // код ещё не введён — конфига нет
		return
	}
	g, err := yadro.GruppyStatik(syroy, s.Nastroyki.Uzly)
	if err != nil {
		otdat(w, map[string]any{"gruppy": []any{}}, nil) // конфиг битый — не валим окно из-за списка узлов
		return
	}
	otdat(w, map[string]any{"gruppy": g}, nil)
}

func (s *Sluzhba) vybrat(w http.ResponseWriter, r *http.Request) {
	var vhod struct {
		Gruppa string `json:"gruppa"`
		Uzel   string `json:"uzel"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&vhod); err != nil {
		otdat(w, nil, fmt.Errorf("не разобрал запрос"))
		return
	}
	if vhod.Gruppa == "" || vhod.Uzel == "" {
		otdat(w, nil, fmt.Errorf("не сказано, что и на что переключить"))
		return
	}
	var err error
	// Ядро работает — переключаем прямо сейчас через Clash API. Ядро стоит —
	// переключать пока нечего, но выбор всё равно обязан запомниться: человек
	// выбирает узел ДО «Подключить», а не только когда защита уже включена.
	// podklyuchit() применит сохранённое, как только ядро поднимется.
	if s.Yadro.Sost() == yadro.Rabotaet {
		err = s.Yadro.Vybrat(vhod.Gruppa, vhod.Uzel)
	}
	if err == nil {
		s.zapomnitUzel(vhod.Gruppa, vhod.Uzel)
	}
	otdat(w, map[string]any{"gotovo": true}, err)
}

// zapomnitUzel сохраняет выбор человека на диске — единое хранилище что для
// «выбрал до подключения», что для «выбрал во время работы» (второе тоже
// стоит запомнить: свежий выбор обязан пережить следующий холодный старт,
// даже если cache_file ядра почему-то не поднимется).
func (s *Sluzhba) zapomnitUzel(gruppa, uzel string) {
	if s.Nastroyki.Uzly == nil {
		s.Nastroyki.Uzly = map[string]string{}
	}
	s.Nastroyki.Uzly[gruppa] = uzel
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		log.Printf("не сохранил выбор узла: %v", err)
	}
}

// primenitSohranennyeUzly переносит выбор узла, сделанный раньше (в том числе
// до самого первого подключения), на только что поднятое ядро: у него своя
// Clash API появляется именно сейчас, а не раньше. Ошибка одной группы — не
// повод рушить подключение целиком: профиль мог обновиться, и сохранённое имя
// узла — устареть.
func (s *Sluzhba) primenitSohranennyeUzly() {
	for gruppa, uzel := range s.Nastroyki.Uzly {
		if err := s.Yadro.Vybrat(gruppa, uzel); err != nil {
			log.Printf("не применил сохранённый узел %q для группы %q: %v", uzel, gruppa, err)
		}
	}
}

// zamerit гоняет пробу через каждый узел группы. Ошибка одного узла — это его
// ответ, а не отказ замера: узел, который не отвечает, человеку тоже надо видеть.
func (s *Sluzhba) zamerit(w http.ResponseWriter, r *http.Request) {
	var vhod struct {
		Uzly []string `json:"uzly"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&vhod); err != nil {
		otdat(w, nil, fmt.Errorf("не разобрал запрос"))
		return
	}
	if len(vhod.Uzly) > 32 {
		vhod.Uzly = vhod.Uzly[:32]
	}
	ctx, otmena := context.WithTimeout(r.Context(), 20*time.Second)
	defer otmena()
	itog := make([]map[string]any, len(vhod.Uzly))
	var gr sync.WaitGroup
	for i, u := range vhod.Uzly {
		gr.Add(1)
		go func(i int, u string) {
			defer gr.Done()
			ms, err := s.Yadro.Zamerit(ctx, u)
			z := map[string]any{"imya": u, "zaderzhka": ms}
			if err != nil {
				z["beda"] = err.Error()
			}
			itog[i] = z
		}(i, u)
	}
	gr.Wait()
	otdat(w, map[string]any{"zamer": itog}, nil)
}

type otvetSostoyaniya struct {
	// Versiya — обновляется приложение само, и версия обязана быть видна
	// человеку (Вова, 20.08). До 23.08 её печатала шапка; на телефоне
	// значка версии в шапке нет вовсе (эталон SimpleSettingsScreen.kt),
	// поэтому версия переехала в подвал вкладки «Настройки» (index.html).
	Versiya    string `json:"versiya"`
	Sost       string `json:"sost"`
	KodEst     bool   `json:"kod_est"`
	YadroEst   bool   `json:"yadro_est"`
	KachaemBin bool   `json:"kachaem_yadro"`
	Beda       string `json:"beda,omitempty"`
	VverhBayt  int64  `json:"vverh_bayt"`
	VnizBayt   int64  `json:"vniz_bayt"`
	Imya       string `json:"imya,omitempty"`
	DoUnix     int64  `json:"do_unix,omitempty"`
	PID        string `json:"pid,omitempty"`
	Rezhim     string `json:"rezhim,omitempty"`     // tunnel | proksi
	Zametka    string `json:"zametka,omitempty"`    // почему режим такой
	MozhnoTun  bool   `json:"mozhno_tun,omitempty"` // туннель в профиле есть, а прав нет
	Prava      bool   `json:"prava"`                // запущены ли мы администратором
	// RuchnoyProksi — система отказалась настроить прокси сама, адрес придётся вписать руками.
	RuchnoyProksi bool `json:"ruchnoy_proksi,omitempty"`
	// Автозапуск с Windows. Podderzhivaetsya — ложь на не-Windows сборке (тумблер
	// там нечестно показывать хоть включённым, хоть выключенным: он ничего не
	// делает). Ustarela — запись есть, но ведёт на другой .exe (переустановка в
	// другую папку) — человеку это тоже нужно видеть, а не молчаливое «выключено».
	AvtozapuskPodderzhivaetsya bool   `json:"avtozapusk_podderzhivaetsya"`
	AvtozapuskVklyuchen        bool   `json:"avtozapusk_vklyuchen,omitempty"`
	AvtozapuskUstarela         bool   `json:"avtozapusk_ustarela,omitempty"`
	AvtozapuskBeda             string `json:"avtozapusk_beda,omitempty"`
	// Avtorezhim — переключение защиты по смене сети (дома/не дома).
	// Obstanovka пустая, пока служитель не крутится (Vklyuchen == false).
	AvtorezhimVklyuchen  bool   `json:"avtorezhim_vklyuchen"`
	AvtorezhimObstanovka string `json:"avtorezhim_obstanovka,omitempty"`
	// AvtorezhimSlepPrichina — почему авторежим PodryadDoPrichiny заходов
	// подряд не может понять, дома ли ноутбук (см. avtorezhim.Avtorezhim.
	// PrichinaSlepoty). Пусто — либо слепоты нет, либо она ещё не длится
	// достаточно, чтобы тревожить человека.
	AvtorezhimSlepPrichina string `json:"avtorezhim_slep_prichina,omitempty"`
	// NovayaVersiyaDostupna — находка ФОНОВОЙ проверки (SleditZaObnovleniem,
	// obnovlenieProveritRuchka), а не ручного нажатия кнопки «Проверить
	// обновление» — та отвечает своим отдельным otvetObnovleniya.Novaya.
	// Пусто — фон ничего не нашёл или ещё не спрашивал.
	NovayaVersiyaDostupna string `json:"novaya_versiya_dostupna,omitempty"`
	// PravaUzheSprosheny — приложение уже (само или кнопкой) спрашивало права
	// администратора хоть раз, отдельно от Prava (есть ли они СЕЙЧАС): вместе
	// эти два поля различают «ещё не спрашивали» и «спрашивали и отказали».
	PravaUzheSprosheny bool `json:"prava_uzhe_sprosheny"`
}

func (s *Sluzhba) sostoyanie(w http.ResponseWriter, r *http.Request) {
	o := otvetSostoyaniya{
		Versiya:  podpiska.Versiya,
		Sost:     string(s.Yadro.Sost()),
		KodEst:   s.Nastroyki.Kod != "",
		YadroEst: s.Yadro.EstBinar(),
		Beda:     s.Yadro.PoslednyayaBeda(),
		PID:      s.Yadro.PID(),
	}
	if t, err := s.Yadro.Trafik(); err == nil {
		o.VverhBayt, o.VnizBayt = t.VverhBayt, t.VnizBayt
	}
	s.zamok.Lock()
	o.KachaemBin = s.kachaemBin
	if s.svedeniya != nil {
		o.Imya, o.DoUnix = s.svedeniya.Imya, s.svedeniya.Do
	}
	k := s.kartina
	if s.naydennoeObnovlenie != nil {
		o.NovayaVersiyaDostupna = s.naydennoeObnovlenie.Versiya
	}
	s.zamok.Unlock()
	o.Rezhim = string(k.Rezhim)
	// Zametka описывает ОБЪЁМ защиты («защищены только браузеры» и соседи,
	// internal/konfig/konfig.go) — правда, только пока защита реально
	// поднята (API ядра отвечает). k.kartina не чистится, когда защита
	// опущена — ни ручным тумблером (OpustitZashchitu), ни авторежимом
	// (avtorezhimKolbek), — так что без этого условия окно продолжало бы
	// врать про объём защиты, которой в этот момент нет вовсе. Заметка
	// авторежима (зачем защита опущена) — отдельный текст, живёт в JS
	// (zametkaAvtorezhima, oblik/index.html) и этой правки не касается.
	if o.Sost == string(yadro.Rabotaet) {
		o.Zametka = k.Zametka
	}
	o.Prava = prava.Est()
	o.MozhnoTun = k.EstTunnel && !o.Prava
	o.PravaUzheSprosheny = s.Nastroyki.UzheSprosiliPrava()
	o.RuchnoyProksi = k.RuchnoyProksi
	// runtime.GOOS, а не сам факт ошибки: на не-Windows avtozapusk всегда
	// отвечает своей заглушкой avtozapusk.Ne, и эту ошибку незачем нести в
	// окно текстом — там и так спрятан весь блок.
	o.AvtozapuskPodderzhivaetsya = runtime.GOOS == "windows"
	if o.AvtozapuskPodderzhivaetsya {
		vkl, err := avtozapusk.Vklyuchen()
		if err != nil {
			o.AvtozapuskBeda = err.Error()
		} else {
			o.AvtozapuskVklyuchen = vkl
			if vkl {
				if ust, err := avtozapusk.Ustarela(); err == nil {
					o.AvtozapuskUstarela = ust
				}
			}
		}
	}
	s.avtorezhimZamok.Lock()
	o.AvtorezhimVklyuchen = s.Nastroyki.Avtorezhim
	if s.avtorezhimEkz != nil {
		o.AvtorezhimObstanovka = s.avtorezhimEkz.Zadvizhka.Tekushcheye().String()
		o.AvtorezhimSlepPrichina = s.avtorezhimEkz.PrichinaSlepoty()
	} else if s.avtorezhimKnopkaObstanovka != avtorezhim.Neizvestno {
		// Фоновый служитель не крутится (тумблер выключен) — обстановка,
		// увиденная последним нажатием «Подключиться» (domaSeychas), это
		// единственное, что вообще известно; человек обязан её видеть (см.
		// zametkaAvtorezhima, oblik/index.html — Правка 2 сняла там условие
		// на тумблер).
		o.AvtorezhimObstanovka = s.avtorezhimKnopkaObstanovka.String()
	}
	s.avtorezhimZamok.Unlock()
	otdat(w, o, nil)
}

// avtozapuskRuchka включает или выключает запуск Kelevra вместе с Windows.
// Повторное «включить» на устаревшей записи — это и есть починка: Vklyuchit
// перезаписывает путь на нынешний .exe.
func (s *Sluzhba) avtozapuskRuchka(w http.ResponseWriter, r *http.Request) {
	var vhod struct {
		Vklyuchit bool `json:"vklyuchit"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&vhod); err != nil {
		otdat(w, nil, fmt.Errorf("не разобрал запрос"))
		return
	}
	var err error
	if vhod.Vklyuchit {
		err = avtozapusk.Vklyuchit()
	} else {
		err = avtozapusk.Vyklyuchit()
	}
	otdat(w, map[string]any{"gotovo": true}, err)
}

// avtorezhimRuchka включает или выключает автоматическое переключение
// защиты по смене сети (дома/не дома) — по образцу avtozapuskRuchka выше.
func (s *Sluzhba) avtorezhimRuchka(w http.ResponseWriter, r *http.Request) {
	var vhod struct {
		Vklyuchit bool `json:"vklyuchit"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&vhod); err != nil {
		otdat(w, nil, fmt.Errorf("не разобрал запрос"))
		return
	}
	s.Nastroyki.Avtorezhim = vhod.Vklyuchit
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		otdat(w, nil, err)
		return
	}
	if vhod.Vklyuchit {
		s.ZapustitAvtorezhim(context.Background())
	} else {
		s.OstanovitAvtorezhim()
	}
	otdat(w, map[string]any{"gotovo": true}, nil)
}

// ZapustitAvtorezhim поднимает фонового служителя авторежима (see
// internal/avtorezhim.Sluzhitel), если он ещё не крутится. Идемпотентно:
// повторный вызов, пока служитель уже работает, ничего не делает — так
// вызвать можно и из ручки /api/avtorezhim, и один раз при старте службы
// (cmd/kelevra/main.go), не заботясь, кто из них подоспел первым.
func (s *Sluzhba) ZapustitAvtorezhim(roditelskiy context.Context) {
	s.avtorezhimZamok.Lock()
	defer s.avtorezhimZamok.Unlock()
	if s.avtorezhimOtmena != nil {
		return
	}
	ctx, otmena := context.WithCancel(roditelskiy)
	s.avtorezhimOtmena = otmena
	s.avtorezhimEkz = s.avtorezhimBoevoy()
	sluzh := &avtorezhim.Sluzhitel{
		Avtorezhim: s.avtorezhimEkz,
		Sledchik:   avtorezhim.NovySledchik(),
		Kolbek:     s.avtorezhimKolbek,
	}
	log.Printf("авторежим: включаю слежение за сетью")
	go sluzh.Krutit(ctx)
}

// OstanovitAvtorezhim гасит служителя авторежима, если он крутится.
// Идемпотентно так же, как ZapustitAvtorezhim.
func (s *Sluzhba) OstanovitAvtorezhim() {
	s.avtorezhimZamok.Lock()
	defer s.avtorezhimZamok.Unlock()
	if s.avtorezhimOtmena == nil {
		return
	}
	log.Printf("авторежим: выключаю слежение за сетью")
	s.avtorezhimOtmena()
	s.avtorezhimOtmena = nil
	s.avtorezhimEkz = nil
}

// tunnelPodnyat — стоит ли сейчас НАШ туннель на пути зондов авторежима.
// Именно туннель, а не «защита вообще»: в прокси-режиме ядро прописано
// системным прокси, а зонды авторежима ходят мимо системного прокси
// (net.Resolver и net.Dialer его не читают) — там они мерят настоящую сеть
// и слепыми не становятся.
// Зачем признак нужен — см. avtorezhim.Nablyudeniye.ZondSlep: в туннеле
// зонды видят подмену нашего же fakeip и решают «дома» где угодно.
func (s *Sluzhba) tunnelPodnyat() bool {
	if s.Yadro == nil || s.Yadro.Sost() != yadro.Rabotaet {
		return false
	}
	s.zamok.Lock()
	rezhim := s.kartina.Rezhim
	s.zamok.Unlock()
	return rezhim == konfig.Tunnel
}

// avtorezhimKolbek — что делать при реальной смене обстановки: дома —
// опустить защиту (обход уже делает роутер), вне дома — поднять (нужен
// полный туннель). Neizvestno нарочно не делает ничего — неизвестность не
// повод дёргать чужой туннель.
func (s *Sluzhba) avtorezhimKolbek(ctx context.Context, sost avtorezhim.Sostoyanie) {
	switch sost {
	case avtorezhim.Doma:
		log.Printf("авторежим: обстановка «дома» — опускаю защиту")
		if err := s.OpustitZashchitu(); err != nil {
			log.Printf("авторежим: не опустил защиту: %v", err)
		}
	case avtorezhim.VneDoma:
		log.Printf("авторежим: обстановка «вне дома» — поднимаю защиту")
		if err := s.PodnyatZashchitu(ctx); err != nil {
			log.Printf("авторежим: не поднял защиту: %v", err)
		}
	default:
		// Neizvestno — ничего не делаем нарочно, см. комментарий выше.
	}
}

// kod принимает код доступа, качает по нему конфиг и запоминает код,
// только если конфиг настоящий: иначе приложение запомнит нерабочий код.
func (s *Sluzhba) kod(w http.ResponseWriter, r *http.Request) {
	var vhod struct {
		Kod string `json:"kod"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&vhod); err != nil {
		otdat(w, nil, fmt.Errorf("не разобрал запрос"))
		return
	}
	kod := strings.TrimSpace(vhod.Kod)
	ctx, otmena := context.WithTimeout(r.Context(), 30*time.Second)
	defer otmena()
	profil, err := s.Podpiska.Konfig(ctx, kod)
	if err != nil {
		log.Printf("сервер подписки не дал профиль: %v", err)
		otdat(w, nil, err)
		return
	}
	log.Printf("профиль получен с сервера подписки: %d байт", len(profil))
	if err := s.SohranitProfil(profil); err != nil {
		otdat(w, nil, err)
		return
	}
	s.Nastroyki.Kod = kod
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		otdat(w, nil, err)
		return
	}
	if sv, err := s.Podpiska.Svedeniya(ctx, kod); err == nil {
		s.zamok.Lock()
		s.svedeniya = sv
		s.zamok.Unlock()
	}
	otdat(w, map[string]any{"gotovo": true}, nil)
}

// PodnyatZashchitu поднимает ядро, а если ядра на машине ещё нет — сначала
// приносит его сам. Общее тело для ручки «Подключить» (podklyuchit) и для
// авторежима (вызывается из avtorezhimKolbek, когда обстановка стала «вне
// дома») — раньше это было только внутри HTTP-обработчика, и авторежиму
// подключиться было некуда.
//
// ctx — основа для собственных таймаутов метода (15 минут на скачивание
// ядра, 70 секунд на его старт); HTTP-обработчик передаёт context.Background()
// — так же, как было устроено до выноса метода, чтобы поведение ручки не
// изменилось ни на йоту.
func (s *Sluzhba) PodnyatZashchitu(ctx context.Context) error {
	if !s.Yadro.EstBinar() {
		s.zamok.Lock()
		uzhe := s.kachaemBin
		s.kachaemBin = true
		s.zamok.Unlock()
		if uzhe {
			return fmt.Errorf("ядро уже качается")
		}
		log.Printf("ядра на машине нет, качаю")
		zctx, otmena := context.WithTimeout(ctx, 15*time.Minute)
		nachalo := time.Now()
		err := s.Yadro.Zagruzit(zctx)
		otmena()
		if err == nil {
			log.Printf("ядро скачано за %s", time.Since(nachalo).Round(time.Second))
		} else {
			log.Printf("не смог скачать ядро: %v", err)
		}
		s.zamok.Lock()
		s.kachaemBin = false
		s.zamok.Unlock()
		if err != nil {
			return err
		}
	}
	// Права могли появиться (человек перезапустил приложение администратором) —
	// пересобираем конфиг перед стартом, иначе режим останется вчерашним.
	vybor := konfig.Vybor{}
	if err := s.perestroit(vybor); err != nil {
		log.Printf("не подготовил конфиг: %v", err)
		return fmt.Errorf("не подготовил конфиг: %w", err)
	}
	zapustit := s.Yadro.Zapustit
	if s.zapustitYadro != nil {
		zapustit = s.zapustitYadro
	}
	zctx, otmena := context.WithTimeout(ctx, 70*time.Second)
	defer otmena()
	err := zapustit(zctx)
	// Отказ системы настроить прокси ядро считает поводом упасть. Человеку от
	// этого одна беда: связи нет вообще. Поднимаем ядро без просьбы к системе
	// и говорим адрес прокси прямо в окне.
	//
	// 23.08: сеть подстраховки не ловила ровно тот случай, ради которого её
	// сделали. На Linux ядро падает строкой «initialize system proxy», а на
	// WINDOWS — «start inbound/mixed[mixed-in]: set system proxy:
	// InternetSetOption(ProxySettingsChanged): winapi error #12009» (замер под
	// wine, stend/proksi.sh сценарий 4). Слова «initialize» там нет, и вся
	// подстраховка на Windows молча простаивала: вместо половинной защиты с
	// запиской «пропишите прокси руками» человек получал «ядро упало при
	// старте» и связь никакую. Сверяем по общей части — «system proxy».
	//
	// bezProksiEsliNado — та же проверка, но названная функцией, а не
	// разовым if: 28.08 приёмка выпуска 0.6.32 нашла, что подстраховка
	// молчала, когда «system proxy» падает НЕ на первой попытке, а внутри
	// лестницы правил ниже (embed или BezSetevyhPravil) — тогда err на
	// входе в лестницу был «initialize rule-set», разовая проверка это
	// пропускала, а после лестницы её никто не повторял. Человек получал
	// «ядро упало при старте» и не подключённую защиту целиком, хотя обе
	// подстраховки по отдельности рабочие — их просто не сложили. Зовём
	// после КАЖДОЙ попытки поднять ядро (исходной и обеих ступеней
	// лестницы), а не только после первой.
	bezProksiEsliNado := func(v *konfig.Vybor, popytkaErr error) error {
		if popytkaErr == nil || v.BezSistemnogoProksi || !strings.Contains(popytkaErr.Error(), "system proxy") {
			return popytkaErr
		}
		log.Printf("система не дала настроить прокси, поднимаю ядро без этой просьбы")
		v.BezSistemnogoProksi = true
		if e := s.perestroit(*v); e != nil {
			return popytkaErr
		}
		zctxSP, otmenaSP := context.WithTimeout(ctx, 70*time.Second)
		defer otmenaSP()
		return zapustit(zctxSP)
	}
	err = bezProksiEsliNado(&vybor, err)
	// Второй такой же отказ, найден 23.08 замером настоящего ядра
	// (.stend/sing-box-linux) на боевом профиле (22 route.rule_set, качаются
	// с subkv.chickenkiller.com detour:"direct" — мимо туннеля). Источник
	// правил жив, кеш пуст → 3.3с и порт открыт. Источник мёртв (connection
	// refused) или молчит (i/o timeout), кеш пуст → ядро НЕ открывает порт
	// вовсе, а падает целиком за 0.4с и за 5.2с соответственно строкой
	// «initialize rule-set[N]: initial rule-set: ...». Наполненный кеш беду
	// прячет (0.04с) — значит бьёт она по первому запуску и по слабой сети,
	// проваленному DNS или провайдеру, который режет домен правил.
	//
	// Лестница из двух ступеней вместо одной (23.08→24.08). Первая версия
	// подстраховки (BezSetevyhPravil) чинит связь, но выбрасывает разбор
	// трафика целиком — человек теряет умную маршрутизацию (что через VPN, а
	// что напрямую) насовсем, пока источник правил не отдышится. Замер
	// живьём: все 22 файла вместе весят 495 039 байт (0.47 МБ, из них
	// ads.srs — 94.7%), 18 из 22 не менялись с 3 августа — комплект стареет
	// медленно, его можно возить прямо в приложении (internal/pravila) и
	// подставлять вместо remote, когда сеть подвела. Поэтому сперва пробуем
	// встроенный комплект (konfig.Vybor.PravilaIzKomplekta) — умная
	// маршрутизация выживает, — и только если он не поднял ядро (комплекта
	// нет на диске не разложить, или сам профиль не совпадает с комплектом),
	// откатываемся к прежнему BezSetevyhPravil.
	if err != nil && strings.Contains(err.Error(), "initialize rule-set") {
		log.Printf("источник правил маршрутизации недоступен, пробую встроенный комплект правил (умная маршрутизация сохранится)")
		podnyalsyaKomplektom := false
		if komplekt, kErr := pravila.Razlozhit(filepath.Join(hranenie.PapkaYadra(), "pravila")); kErr != nil {
			log.Printf("не разложил встроенный комплект правил: %v", kErr)
		} else {
			vyborKomplekt := vybor
			vyborKomplekt.PravilaIzKomplekta = komplekt
			vyborKomplekt.PravilaKomplektData = pravila.Data()
			if e := s.perestroit(vyborKomplekt); e != nil {
				log.Printf("встроенный комплект правил не применился к профилю: %v", e)
			} else {
				zctx3, otmena3 := context.WithTimeout(ctx, 70*time.Second)
				defer otmena3()
				errKomplekt := zapustit(zctx3)
				errKomplekt = bezProksiEsliNado(&vyborKomplekt, errKomplekt)
				if errKomplekt == nil {
					err = nil
					vybor = vyborKomplekt
					podnyalsyaKomplektom = true
					log.Printf("поднялся на встроенном комплекте правил (снимок от %s) — умная маршрутизация жива", pravila.Data())
				} else {
					log.Printf("встроенный комплект не поднял ядро: %v", errKomplekt)
					err = errKomplekt
				}
			}
		}
		if !podnyalsyaKomplektom {
			log.Printf("встроенный комплект не помог, поднимаю ядро совсем без правил (весь трафик через VPN)")
			vybor.BezSetevyhPravil = true
			if e := s.perestroit(vybor); e == nil {
				zctx4, otmena4 := context.WithTimeout(ctx, 70*time.Second)
				defer otmena4()
				err = zapustit(zctx4)
				err = bezProksiEsliNado(&vybor, err)
			}
		}
	}
	// Зелёный поверх пустоты. До 23.08 «ядро поднялось без ошибки» само по
	// себе считалось доказательством, что системный прокси в реестре стоит:
	// проверка Stoit/Postavit висела ТОЛЬКО внутри ветки-подстраховки, то
	// есть срабатывала лишь когда ядро упало ГРОМКО, строкой «system proxy».
	// Молчаливый отказ — ядро стартовало, порт слушает, а записи в реестре
	// нет или она не наша — не ловил никто: окно показывало зелёную защиту,
	// метка «прокси поставили мы» писалась по предположению, а трафик шёл
	// мимо. Проверяем не молчание ядра, а сам реестр, и на любом успешном
	// подъёме, а не только после известной поломки.
	if err == nil {
		s.zamok.Lock()
		proksiRezhim := s.kartina.Rezhim == konfig.Proksi && s.kartina.ProksiAdres != ""
		adres, estTunnel := s.kartina.ProksiAdres, s.kartina.EstTunnel
		s.zamok.Unlock()
		if proksiRezhim {
			stoit := proksi.Stoit(adres)
			if !stoit {
				// Ядро либо не просили (BezSistemnogoProksi), либо оно
				// промолчало. Тот же ключ реестра от лица своего процесса
				// пишут рабочие VPN-клиенты — пробуем сами.
				stoit = proksi.Postavit(adres)
			}
			s.zamok.Lock()
			s.kartina.RuchnoyProksi = !stoit
			if stoit {
				s.kartina.Zametka = konfig.ZametkaProksiRezhima(estTunnel)
			} else {
				s.kartina.Zametka = fmt.Sprintf(konfig.ZametkaRuchnoyProksi, adres)
			}
			s.zamok.Unlock()
			log.Printf("системный прокси в реестре: стоит=%v (адрес %s)", stoit, adres)
		}
	}
	if err != nil {
		// Ядро могло прописать системный прокси ещё до того, как упало или
		// не ответило за 70 секунд (падение при старте, таймаут API) — запись
		// в реестре остаётся висеть, а Ostanovit() её не снимает. Тот же баг,
		// что был на «Отключить» и закрытии окна (Вова, 20.08), только на
		// неудачном подключении.
		proksi.Snyat()
	} else {
		// Узел, выбранный в окне ДО этого нажатия (или на прошлом сеансе),
		// применяем прямо сейчас: раньше выбор до подключения был декорацией —
		// список стоял пустым, а нажать было нечего.
		s.primenitSohranennyeUzly()

		// Метка на диске: успешный подъём защиты означает, что системный
		// прокси на s.kartina.ProksiAdres реально стоит в реестре — его
		// поставило либо само ядро (обычный путь), либо мы сами чуть выше
		// (проверка Stoit/Postavit). Жёсткая смерть процесса (Диспетчер
		// задач, выключение питания) не даёт службе снять его штатно —
		// метка переживает эту смерть и даёт следующему запуску окна
		// (cmd/kelevra/main.go) снять чужой для него, но доказанно наш
		// прокси самому. RuchnoyProksi=true — противоположный случай: адрес
		// в реестре не стоит, человеку показана записка «впишите руками»,
		// снимать нечего.
		s.zamok.Lock()
		adres, pishemMetku := s.kartina.ProksiAdres, s.kartina.Rezhim == konfig.Proksi && !s.kartina.RuchnoyProksi && s.kartina.ProksiAdres != ""
		s.zamok.Unlock()
		if pishemMetku {
			proksi.Otmetit(adres)
		}
	}
	return err
}

// domaSeychas — один доверенный заход авторежима перед подъёмом защиты
// (podklyuchit): кнопка «Подключиться» обязана сама решить, дома человек
// или нет, а не поднимать VPN безусловно (Вова, 28.08: «нажимаю
// подключиться, он не определяет дома или нет, а когда выключен —
// определяет ИНОГДА»). Нарочно НЕ зависит от тумблера Nastroyki.Avtorezhim —
// тот управляет только фоновым автопереключением (avtorezhimKolbek); кнопка
// спрашивает обстановку всегда, своим собственным заходом.
//
// dovereno=true у Zahod: решение принимается по ПЕРВОМУ наблюдению, не
// дожидаясь Podtverzhdeniy заходов подряд (см. Zadvizhka.Predlozhit) — кнопку
// нажали один раз, набирать гистерезис для неё бессмысленно.
//
// Заход ограничен собственным таймаутом (по умолчанию 5с,
// avtorezhimKnopkaTaimaut): не ответили зонды — ctx истекает, DomaPoDns
// вернёт ошибку, а Avtorezhim.Zahod уже трактует её как «не дома»
// (безопасный дефолт) — неизвестность не должна оставлять человека без VPN,
// это дороже лишнего VPN дома.
//
// Увиденная обстановка запоминается в avtorezhimKnopkaObstanovka —
// /api/sostoyanie показывает её человеку тем же полем, что и фоновый
// авторежим, даже если тумблер выключен (zametkaAvtorezhima, oblik/index.html).
// avtorezhimBoevoy собирает боевой avtorezhim.Novyy() с s.tunnelPodnyat —
// общая точка для domaSeychas (кнопка «Подключиться») и ZapustitAvtorezhim
// (фоновое слежение), чтобы KELEVRA_AVTOREZHIM_DNS (см. avtorezhimDnsAdres)
// подменяла резолвер одинаково в обоих путях, а не только в одном из них.
func (s *Sluzhba) avtorezhimBoevoy() *avtorezhim.Avtorezhim {
	a := avtorezhim.Novyy()
	a.TunnelPodnyat = s.tunnelPodnyat
	if s.avtorezhimDnsAdres != "" {
		podmena := func() avtorezhim.DnsProver {
			return &avtorezhim.DnsZond{AdresResolvera: s.avtorezhimDnsAdres}
		}
		a.Dns = podmena()
		a.DnsPryamoy = func(_, _ string) avtorezhim.DnsProver { return podmena() }
	}
	return a
}

func (s *Sluzhba) domaSeychas(roditelskiy context.Context) avtorezhim.Sostoyanie {
	sobrat := s.avtorezhimDlyaKnopki
	if sobrat == nil {
		sobrat = s.avtorezhimBoevoy
	}
	taimaut := s.avtorezhimKnopkaTaimaut
	if taimaut <= 0 {
		taimaut = 5 * time.Second
	}
	ctx, otmena := context.WithTimeout(roditelskiy, taimaut)
	defer otmena()
	_, _, tekushcheye := sobrat().Zahod(ctx, true, true)
	s.avtorezhimZamok.Lock()
	s.avtorezhimKnopkaObstanovka = tekushcheye
	s.avtorezhimZamok.Unlock()
	return tekushcheye
}

func (s *Sluzhba) podklyuchit(w http.ResponseWriter, r *http.Request) {
	if s.domaSeychas(context.Background()) == avtorezhim.Doma {
		log.Printf("«Подключиться»: обстановка «дома» — защиту не поднимаю, обход блокировок уже делает роутер")
		otdat(w, map[string]any{"gotovo": true}, nil)
		return
	}
	err := s.PodnyatZashchitu(context.Background())
	if err == nil {
		// В фоне и после ответа: сам запрос прав (окно UAC) не должен
		// задержать «gotovo» в окне — человек уже подключён, а вопрос про
		// права идёт следом, а не вместо ответа.
		go s.zaprositPravaAvtomaticheskiEsliNado()
	}
	otdat(w, map[string]any{"gotovo": true}, err)
}

// OpustitZashchitu останавливает ядро и снимает системный прокси. Общее тело
// для ручки «Отключить» (otklyuchit) и для авторежима (когда обстановка
// стала «дома»). Снимаем прокси безусловно: ядро могло умереть само ещё до
// вызова, запись в реестре всё равно висит.
func (s *Sluzhba) OpustitZashchitu() error {
	err := s.Yadro.Ostanovit()
	proksi.Snyat()
	return err
}

func (s *Sluzhba) otklyuchit(w http.ResponseWriter, r *http.Request) {
	log.Printf("человек нажал «Отключить»")
	err := s.OpustitZashchitu()
	otdat(w, map[string]any{"gotovo": true}, err)
}

// ObnovlyatProfil перекачивает конфиг по расписанию, как это делает мобильный клиент.
func (s *Sluzhba) ObnovlyatProfil(ctx context.Context) {
	shag := time.Duration(s.Nastroyki.ObnovlyatMin) * time.Minute
	if shag < 15*time.Minute {
		shag = 15 * time.Minute
	}
	t := time.NewTicker(shag)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.Nastroyki.Kod == "" {
				continue
			}
			k, err := s.Podpiska.Konfig(ctx, s.Nastroyki.Kod)
			if err != nil {
				log.Printf("плановое обновление профиля не удалось: %v", err)
				continue // сеть могла лечь; работаем на прежнем конфиге
			}
			if err := s.SohranitProfil(k); err != nil {
				log.Printf("плановое обновление профиля не сохранилось: %v", err)
			}
		}
	}
}

// polnayaZashchita просит у Windows права администратора: без них ядро не
// поднимет туннель. При согласии человека приложение перезапускается уже с
// правами, а эта копия уходит — две копии на машине не нужны.
func (s *Sluzhba) polnayaZashchita(w http.ResponseWriter, r *http.Request) {
	if prava.Est() {
		otdat(w, map[string]any{"gotovo": true}, nil)
		return
	}
	// Метка копии раньше снималась ДО окна UAC. ShellExecuteW возвращается
	// уже с запущенной копией, а та копия первым делом смотрит метку: не
	// увидев её, она решала «приложения ещё нет» и стартовала как первая —
	// поверх ещё живой старой (беда 25.08, «2 нахуй открыто»: гонка между
	// «метка снята» и «старая копия реально умерла» проигрывалась). Метка
	// теперь живёт у этой копии до самой её смерти; вместо гонки — явная
	// передача: новая копия получает наш pid аргументом --smena и сама ждёт
	// нашу смерть, прежде чем снять метку и занять её место (см.
	// cmd/kelevra/main.go: zhdatSmenu).
	if err := s.sprositPrava(); err != nil {
		// Человек нажал «Нет»: метку никто не трогал — эта копия остаётся
		// работать как ни в чём не бывало, второй запуск по-прежнему увидит
		// её живой.
		otdat(w, nil, err)
		return
	}
	otdat(w, map[string]any{"gotovo": true, "perezapusk": true}, nil)
	log.Printf("человек согласился на права администратора, перезапускаюсь")
	s.uydiPosleSoglasiyaNaPrava()
}

// sprositPrava — настоящее окно UAC либо подмена со стенда. Передаёт свой
// pid, чтобы новая копия знала, чьей смерти ей ждать (см. Poprosit).
func (s *Sluzhba) sprositPrava() error {
	if s.poprositPrava != nil {
		return s.poprositPrava(os.Getpid())
	}
	return prava.Poprosit(os.Getpid())
}

// uydiPosleSoglasiyaNaPrava — общий хвост согласия на права: и кнопка
// «Включить для всех программ» (polnayaZashchita), и автозапрос после первого
// успешного подключения (zaprositPravaAvtomaticheskiEsliNado) заканчиваются
// одинаково — повышенная копия уже запущена (ShellExecuteW внутри sprositPrava
// это сделал), а эта, старая, обязана погасить своё ядро и уйти, иначе на
// машине останутся две живые копии (беда 25.08).
func (s *Sluzhba) uydiPosleSoglasiyaNaPrava() {
	if s.priUhode != nil {
		s.priUhode() // синхронная метка для теста: момент решения уйти, ДО 300-мс паузы
	}
	go func() {
		time.Sleep(300 * time.Millisecond) // дать ответу/сохранению уйти
		_ = s.Yadro.Ostanovit()            // ядро старой копии гасим сами
		if s.vyhod != nil {
			s.vyhod()
			return
		}
		os.Exit(0)
	}()
}

// zaprositPravaAvtomaticheskiEsliNado — первый успешный коннект с профилем,
// которому нужен туннель (EstTunnel), сам спрашивает права администратора
// ОДИН раз, тем же путём, что и кнопка «Включить для всех программ»: человек
// уже впустил приложение (ввёл код доступа), и запрос прав — тот же шаг,
// который он иначе сделал бы сам следующим кликом. Кнопка остаётся на месте
// как запасной путь — этот автозапрос её не заменяет, а опережает.
//
// Не критический путь для podklyuchit: вызывается уже ПОСЛЕ того, как
// PodnyatZashchitu вернула успех и ответ ушёл в окно, поэтому отказ или
// ошибка тут не портят сам факт подключения — только пишутся в журнал.
//
// UzheSprosiliPrava() — единственный сторож повторного вопроса и, что
// важнее, сторож против незваного попапа на СУЩЕСТВУЮЩЕЙ установке: миграция
// в hranenie.Zagruzit уже отметила такие файлы как «спрошено», сюда эта
// функция для них вообще не доходит.
func (s *Sluzhba) zaprositPravaAvtomaticheskiEsliNado() {
	s.zamok.Lock()
	estTunnel := s.kartina.EstTunnel
	s.zamok.Unlock()
	if !estTunnel || s.Nastroyki.UzheSprosiliPrava() || prava.Est() {
		return
	}
	oshibka := s.sprositPrava()
	if oshibka != nil {
		log.Printf("автозапрос прав администратора при первом подключении: отказ или ошибка: %v", oshibka)
	} else {
		log.Printf("человек согласился на права администратора при первом подключении, перезапускаюсь")
	}
	// Отметку пишем ДО ухода, а не после (как было раньше): при согласии
	// uydiPosleSoglasiyaNaPrava гасит эту копию из фоновой горутины через
	// 300 мс, и если Sohranit не успеет долететь до диска раньше выхода
	// процесса, следующий (уже обычный, НЕ повышенный) запуск увидит файл
	// без отметки и спросит права ЕЩЁ РАЗ — а человек просил ровно один раз
	// при старте. Флаг ставим и при отказе тоже: он значит «спрашивали», а
	// не «дали» — Prava/prava.Est() отдельно отвечает на «есть ли они».
	s.Nastroyki.OtmetitPravaZaprosheny()
	sohranit := s.sohranitNastroyki
	if sohranit == nil {
		sohranit = func() error { return hranenie.Sohranit(s.Nastroyki) }
	}
	if err := sohranit(); err != nil {
		log.Printf("не сохранил отметку об автозапросе прав администратора: %v", err)
	}
	if oshibka == nil {
		s.uydiPosleSoglasiyaNaPrava()
	}
	if s.posleAvtozaprosaPrav != nil {
		s.posleAvtozaprosaPrav()
	}
}

func otdat(w http.ResponseWriter, telo any, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"beda": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(telo)
}

func zametka(z string) string {
	if z == "" {
		return ""
	}
	return ", " + z
}

func sluchaynyy() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// fsPodpapki отдаёт встроенные страницы так, будто они лежат в корне.
func fsPodpapki() (fs.FS, error) { return fs.Sub(oblik, "oblik") }
