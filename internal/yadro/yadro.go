// Пакет yadro: запуск и остановка ядра sing-box и снятие с него показаний.
//
// Ядро — отдельный процесс. Приложение его порождает, следит за ним и убивает;
// состояние спрашивает у Clash API, который ядро поднимает по конфигу.
package yadro

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ApiAdres — куда стучаться, если конфиг про Clash API молчит. Настоящий адрес
// берётся из конфига: зашитый намертво не угадает чужой порт, и приложение
// решит, что живое ядро мертво (профиль с сервера поднимает API на 9090).
const ApiAdres = "127.0.0.1:9090"

// Sostoyanie — что приложение показывает пользователю.
type Sostoyanie string

const (
	Stoit     Sostoyanie = "stoit"     // ядро не запущено
	Podnimaem Sostoyanie = "podnimaem" // процесс порождён, API ещё молчит
	Rabotaet  Sostoyanie = "rabotaet"  // API отвечает
	Slomalos  Sostoyanie = "slomalos"  // процесс умер сам
)

// Yadro — один экземпляр ядра под управлением приложения.
type Yadro struct {
	Bin    string // путь к sing-box(.exe)
	Papka  string // рабочая папка ядра (в ней лежит config.json)
	Api    string // адрес Clash API, пусто = ApiAdres
	Sekret string // пароль Clash API, если конфиг его задаёт
	Spisok string // откуда брать сборки ядра, пусто = Relizy (подменяется на стенде)
	Klient *http.Client

	zamok   sync.Mutex
	derzh   *derzhatel
	umer    chan struct{}
	poslLog string
	sost    Sostoyanie
}

// derzhatel — процесс ядра, за которым следит эта копия приложения.
//
// Зачем отдельным типом, а не прежним *exec.Cmd. С 11.09.2026 ядро бывает двух
// родов: своё, запущенное этой копией, и ПРИНЯТОЕ — живое ядро прошлой копии,
// пережившее смену версии (см. peredacha.go). Управлять надо обоими одинаково,
// но узнаётся об их смерти по-разному: за своим можно ждать через Wait, за
// чужим нельзя вовсе — система не даёт дожидаться процесса, который тебе не
// ребёнок. Прятать эту разницу за одним типом честнее, чем размазывать её
// проверками на nil по всему файлу.
type derzhatel struct {
	pid int
	// cmd — не nil только у своего. Он же признак рода.
	cmd *exec.Cmd
	// proc — дескриптор принятого процесса. У своего не нужен: им командует cmd.
	proc *os.Process
}

// svoy — запускала ли ядро эта копия.
func (d *derzhatel) svoy() bool { return d != nil && d.cmd != nil }

// ubit гасит процесс независимо от рода.
func (d *derzhatel) ubit() error {
	if d == nil {
		return nil
	}
	if d.cmd != nil {
		if d.cmd.Process == nil {
			return nil
		}
		return zavershit(d.cmd)
	}
	if d.proc == nil {
		return nil
	}
	return d.proc.Kill()
}

func (y *Yadro) spisok() string {
	if y.Spisok != "" {
		return y.Spisok
	}
	return Relizy
}

func (y *Yadro) api() string {
	if y.Api != "" {
		return y.Api
	}
	return ApiAdres
}

func (y *Yadro) klient() *http.Client {
	if y.Klient != nil {
		return y.Klient
	}
	return &http.Client{Timeout: 3 * time.Second}
}

// PutKonfiga — конфиг, с которым запускается ядро.
func (y *Yadro) PutKonfiga() string { return filepath.Join(y.Papka, "config.json") }

// ZapisatKonfig кладёт свежий конфиг на диск рядом с ядром.
func (y *Yadro) ZapisatKonfig(telo []byte) error {
	if err := os.MkdirAll(y.Papka, 0o755); err != nil {
		return err
	}
	vremenny := y.PutKonfiga() + ".tmp"
	if err := os.WriteFile(vremenny, telo, 0o600); err != nil {
		return err
	}
	return os.Rename(vremenny, y.PutKonfiga())
}

// EstBinar — стоит ли ядро на месте. Без него подключаться нечем.
func (y *Yadro) EstBinar() bool {
	st, err := os.Stat(y.Bin)
	return err == nil && !st.IsDir()
}

// Zapustit порождает ядро и ждёт, пока его API отзовётся.
// Ошибка возвращается вместе с последними строками лога ядра — иначе
// пользователю нечего сказать, кроме «не работает».
func (y *Yadro) Zapustit(ctx context.Context) error {
	y.zamok.Lock()
	if y.derzh != nil {
		// Два окна опрашивают /api/sostoyanie независимо и оба могут решить
		// «надо подключиться» одновременно (см. internal/sluzhba/oblik). Цель
		// вызова — «ядро работает» — уже достигнута, это не повод на ошибку:
		// симметрично с Ostanovit, для которого повторный вызов на уже
		// остановленном ядре тоже не ошибка.
		y.zamok.Unlock()
		log.Printf("ядро уже запущено: повторный Zapustit — не ошибка")
		return nil
	}
	if !y.EstBinar() {
		y.zamok.Unlock()
		return fmt.Errorf("ядро не найдено: %s", y.Bin)
	}
	if _, err := os.Stat(y.PutKonfiga()); err != nil {
		y.zamok.Unlock()
		return fmt.Errorf("нет конфига: сначала введите код доступа")
	}

	cmd := exec.Command(y.Bin, "run", "-c", y.PutKonfiga(), "-D", y.Papka)
	cmd.Dir = y.Papka
	zhurnalYadra, err := sozdatZhurnalYadra(y.Papka)
	if err != nil {
		y.zamok.Unlock()
		return err
	}
	cmd.Stdout, cmd.Stderr = zhurnalYadra, zhurnalYadra
	spryatatOkno(cmd) // на Windows у ядра не должно мигать чёрное окно
	if err := cmd.Start(); err != nil {
		zhurnalYadra.Close()
		y.zamok.Unlock()
		log.Printf("ядро не запустилось: %v", err)
		return fmt.Errorf("ядро не запустилось: %w", err)
	}
	log.Printf("ядро запущено: pid %d, конфиг %s, Clash API %s", cmd.Process.Pid, y.PutKonfiga(), y.api())
	svoy := &derzhatel{pid: cmd.Process.Pid, cmd: cmd}
	y.derzh, y.sost, y.umer = svoy, Podnimaem, make(chan struct{})
	umer := y.umer
	y.zamok.Unlock()

	go func() {
		_ = cmd.Wait()
		zhurnalYadra.Close()
		y.zamok.Lock()
		if y.derzh == svoy { // не мы его остановили — значит упал
			y.derzh, y.sost = nil, Slomalos
			y.poslLog = hvostLoga(filepath.Join(y.Papka, "yadro.log"))
			log.Printf("ядро упало само: %s", y.poslLog)
		}
		y.zamok.Unlock()
		close(umer)
	}()

	// Ждём API: раньше него ядро ещё ничего не проксирует.
	srok, otmena := context.WithTimeout(ctx, 45*time.Second)
	defer otmena()
	for {
		select {
		case <-umer:
			hvost := hvostLoga(filepath.Join(y.Papka, "yadro.log"))
			log.Printf("ядро упало при старте: %s", hvost)
			return fmt.Errorf("ядро упало при старте: %s", hvost)
		case <-srok.Done():
			_ = y.Ostanovit()
			hvost := hvostLoga(filepath.Join(y.Papka, "yadro.log"))
			log.Printf("ядро не ответило за 45 секунд: %s", hvost)
			return fmt.Errorf("ядро не ответило за 45 секунд: %s", hvost)
		case <-time.After(300 * time.Millisecond):
			if y.Zhivo() {
				y.zamok.Lock()
				// Ядро поднялось: беда прошлой попытки больше не беда,
				// иначе окно показывает красную строку поверх работающей связи.
				y.sost, y.poslLog = Rabotaet, ""
				y.zamok.Unlock()
				// Записка пишется только теперь, когда ядро ДЕЙСТВИТЕЛЬНО
				// работает. Записать её при старте процесса значило бы
				// оставить преемнице приглашение принять ядро, которое ещё
				// может упасть на подъёме.
				ZapisatPeredachu(y.Papka, Peredacha{
					PID:        cmd.Process.Pid,
					Bin:        y.Bin,
					Api:        y.api(),
					Otpechatok: OtpechatokKonfiga(y.PutKonfiga()),
					Adapter:    ImyaAdapteraIzKonfiga(y.PutKonfiga()),
					Kogda:      time.Now(),
				})
				log.Printf("ядро ответило: связь работает")
				return nil
			}
		}
	}
}

// Ostanovit гасит ядро. Повторный вызов на остановленном — не ошибка.
func (y *Yadro) Ostanovit() error {
	y.zamok.Lock()
	d, umer := y.derzh, y.umer
	y.derzh, y.sost = nil, Stoit
	y.zamok.Unlock()
	if d == nil {
		return nil
	}
	log.Printf("останавливаю ядро (pid %d, %s)", d.pid, rodYadra(d))
	// Записку снимаем ДО того, как гасим: если нас убьют посреди остановки,
	// преемница не должна получить приглашение принять ядро, которого уже нет.
	UbratPeredachu(y.Papka)
	if err := d.ubit(); err != nil {
		log.Printf("не смог остановить ядро: %v", err)
		return err
	}
	// За своим ядром ждём по каналу, который закрывает Wait. За принятым
	// ждать нечему: система не даёт дожидаться чужого процесса, поэтому
	// спрашиваем его служебный порт, пока он не замолчит.
	if d.svoy() {
		select {
		case <-umer:
		case <-time.After(5 * time.Second):
			_ = d.ubit()
		}
		return nil
	}
	srok := time.After(5 * time.Second)
	for {
		select {
		case <-srok:
			return nil
		case <-time.After(150 * time.Millisecond):
			if !y.Zhivo() {
				return nil
			}
		}
	}
}

// rodYadra — «своё» или «принятое», одним словом для журнала. Разбирая
// журнал человека, это первое, что надо знать про ядро.
func rodYadra(d *derzhatel) string {
	if d.svoy() {
		return "своё"
	}
	return "принятое"
}

// ItogPriyoma — чем кончилась попытка принять ядро прошлой копии.
type ItogPriyoma struct {
	// Prinyato — ядро теперь под управлением этой копии.
	Prinyato bool
	// KonfigUstarel — ядро живо и наше, но работает по другому конфигу.
	// Принимать такое нельзя: человек получил бы старые правила молча. Зато
	// и рвать связь незачем — вызывающий поднимет новое ядро рядом и погасит
	// это, когда новое будет готово (см. sluzhba.SmenitYadroPlavno).
	KonfigUstarel bool
	// Pochemu — для журнала. Пусто при успехе.
	Pochemu string
}

// Prinyat берёт под управление живое ядро прошлой копии.
//
// Опознание тройное, и все три проверки обязательны. Номера процессов система
// переиспользует, поэтому одного номера мало: под ним может оказаться что
// угодно, занявшее освободившееся место. Живой служебный порт говорит, что
// процесс не просто существует, а работает ядром. Совпадение бинаря отсекает
// чужую копию sing-box, поставленную человеком отдельно, — командовать чужим
// процессом мы не вправе.
// zhelaemyyOtpechatok — отпечаток конфига, по которому ПРИНИМАЮЩАЯ копия
// подняла бы ядро сама. Параметром, а не чтением файла: к моменту приёма
// конфиг на диске уже перезаписан черновой сборкой стартующей копии (замер
// 11.09.2026: «правила ядро качает само» вместо «правила из кеша»), и сверка
// с ним объявляла бы сменившимся конфиг, который на самом деле совпадает.
// Пустая строка — посчитать не вышло, тогда конфиг не сверяем вовсе: принять
// работающее ядро всё равно лучше, чем оборвать связь из-за неизвестности.
func (y *Yadro) Prinyat(p Peredacha, zhelaemyyOtpechatok string) ItogPriyoma {
	y.zamok.Lock()
	zanyato := y.derzh != nil
	y.zamok.Unlock()
	if zanyato {
		return ItogPriyoma{Pochemu: "эта копия уже держит ядро"}
	}
	if p.PID <= 0 {
		return ItogPriyoma{Pochemu: "в записке нет номера процесса"}
	}
	if p.Bin != "" && !odinItotZhe(p.Bin, y.Bin) {
		return ItogPriyoma{Pochemu: "ядро в записке — не наш бинарь"}
	}
	proc, err := os.FindProcess(p.PID)
	if err != nil {
		return ItogPriyoma{Pochemu: fmt.Sprintf("процесса %d уже нет", p.PID)}
	}
	if !y.Zhivo() {
		return ItogPriyoma{Pochemu: "служебный порт ядра молчит"}
	}
	// Конфиг сверяем последним: две проверки выше отвечают «наше ли это
	// ядро», а эта — «годится ли оно как есть». Разные вопросы и разные
	// ответы вызывающему.
	if p.Otpechatok != "" && zhelaemyyOtpechatok != "" && p.Otpechatok != zhelaemyyOtpechatok {
		return ItogPriyoma{KonfigUstarel: true,
			Pochemu: "ядро живо, но работает по прежнему конфигу"}
	}

	y.zamok.Lock()
	if y.derzh != nil { // кто-то успел между проверкой и взятием замка
		y.zamok.Unlock()
		return ItogPriyoma{Pochemu: "эта копия уже держит ядро"}
	}
	prinyatoe := &derzhatel{pid: p.PID, proc: proc}
	y.derzh, y.sost, y.umer = prinyatoe, Rabotaet, make(chan struct{})
	umer := y.umer
	y.zamok.Unlock()

	log.Printf("принял живое ядро прошлой копии: pid %d, адаптер %q — туннель не прерывался",
		p.PID, p.Adapter)
	go y.sleditZaPrinyatym(prinyatoe, umer)
	return ItogPriyoma{Prinyato: true}
}

// sleditZaPrinyatym замечает смерть принятого ядра.
//
// Опросом, а не ожиданием: дожидаться можно только своего ребёнка, а принятое
// ядро этой копии не ребёнок. Спрашиваем служебный порт, а не наличие процесса:
// живой процесс с мёртвым портом для человека — то же самое, что мёртвое ядро,
// трафик через него всё равно не идёт.
func (y *Yadro) sleditZaPrinyatym(d *derzhatel, umer chan struct{}) {
	const shag = 2 * time.Second
	for {
		time.Sleep(shag)
		y.zamok.Lock()
		nashe := y.derzh == d
		y.zamok.Unlock()
		if !nashe {
			return // сами остановили или сменили — следить больше не за чем
		}
		if y.Zhivo() {
			continue
		}
		y.zamok.Lock()
		if y.derzh != d { // умерло ровно в тот миг, когда его меняли
			y.zamok.Unlock()
			return
		}
		y.derzh, y.sost = nil, Slomalos
		y.poslLog = hvostLoga(filepath.Join(y.Papka, "yadro.log"))
		y.zamok.Unlock()
		log.Printf("принятое ядро (pid %d) перестало отвечать: %s", d.pid, y.poslLog)
		close(umer)
		return
	}
}

// odinItotZhe — один ли это файл. Сравнение по чистому пути, а не по строке:
// записка могла быть написана копией, которая шла к тому же бинарю другой
// дорогой (через служебные пути Windows, например).
func odinItotZhe(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(ra), filepath.Clean(rb))
}

// zapros — обращение к Clash API ядра с паролем, если он задан конфигом.
func (y *Yadro) zapros(put string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, "http://"+y.api()+put, nil)
	if err != nil {
		return nil, err
	}
	if y.Sekret != "" {
		req.Header.Set("Authorization", "Bearer "+y.Sekret)
	}
	return y.klient().Do(req)
}

// Zhivo — отвечает ли Clash API ядра.
func (y *Yadro) Zhivo() bool {
	otvet, err := y.zapros("/version")
	if err != nil {
		return false
	}
	defer otvet.Body.Close()
	return otvet.StatusCode == http.StatusOK
}

// Sost — текущее состояние глазами приложения.
func (y *Yadro) Sost() Sostoyanie {
	y.zamok.Lock()
	defer y.zamok.Unlock()
	if y.sost == "" {
		return Stoit
	}
	return y.sost
}

// PoslednyayaBeda — хвост лога, если ядро упало само.
func (y *Yadro) PoslednyayaBeda() string {
	y.zamok.Lock()
	defer y.zamok.Unlock()
	return y.poslLog
}

// Trafik — сколько прошло через ядро с его запуска.
type Trafik struct {
	VverhBayt int64 `json:"up"`
	VnizBayt  int64 `json:"down"`
}

// Trafik снимает счётчики с Clash API ядра.
func (y *Yadro) Trafik() (*Trafik, error) {
	otvet, err := y.zapros("/connections")
	if err != nil {
		return nil, err
	}
	defer otvet.Body.Close()
	var v struct {
		UploadTotal   int64 `json:"uploadTotal"`
		DownloadTotal int64 `json:"downloadTotal"`
	}
	if err := json.NewDecoder(otvet.Body).Decode(&v); err != nil {
		return nil, err
	}
	return &Trafik{VverhBayt: v.UploadTotal, VnizBayt: v.DownloadTotal}, nil
}

// cveta — раскраска вывода ядра. В файле и в окне она выглядит абракадаброй
// вида «[36mINFO[0m»: терминала, который её понимает, у приложения нет.
var cveta = regexp.MustCompile("\x1b\\[[0-9;]*m")

// hvostLoga — последние строки лога ядра, чтобы показать причину, а не «ошибку».
//
// Если ядро само назвало причину падения строкой FATAL или ERROR — показываются
// именно они: остальное в его логе INFO-шум про интерфейсы и порты, а человеку
// в окне нужна одна строка, по которой видно, что случилось.
func hvostLoga(put string) string {
	b, err := os.ReadFile(put)
	if err != nil {
		return ""
	}
	if len(b) > 8192 {
		b = b[len(b)-8192:]
	}
	chistyy := cveta.ReplaceAllString(strings.TrimSpace(string(b)), "")
	stroki := strings.Split(chistyy, "\n")
	var vazhnye []string
	for _, str := range stroki {
		if strings.Contains(str, "FATAL") || strings.Contains(str, "ERROR") {
			vazhnye = append(vazhnye, strings.TrimSpace(str))
		}
	}
	if len(vazhnye) > 0 {
		if len(vazhnye) > 3 {
			vazhnye = vazhnye[len(vazhnye)-3:]
		}
		return strings.Join(vazhnye, " | ")
	}
	if len(stroki) > 6 {
		stroki = stroki[len(stroki)-6:]
	}
	return strings.Join(stroki, " | ")
}

// PID запущенного ядра — для окна диагностики.
func (y *Yadro) PID() string {
	y.zamok.Lock()
	defer y.zamok.Unlock()
	if y.derzh == nil {
		return ""
	}
	return strconv.Itoa(y.derzh.pid)
}
