// Пакет yadro: запуск и остановка ядра sing-box и снятие с него показаний.
//
// Ядро — отдельный процесс. Приложение его порождает, следит за ним и убивает;
// состояние спрашивает у Clash API, который ядро поднимает по конфигу.
package yadro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
	Klient *http.Client

	zamok   sync.Mutex
	process *exec.Cmd
	umer    chan struct{}
	poslLog string
	sost    Sostoyanie
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
	if y.process != nil {
		y.zamok.Unlock()
		return fmt.Errorf("ядро уже запущено")
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
	log, err := os.Create(filepath.Join(y.Papka, "yadro.log"))
	if err != nil {
		y.zamok.Unlock()
		return err
	}
	cmd.Stdout, cmd.Stderr = log, log
	spryatatOkno(cmd) // на Windows у ядра не должно мигать чёрное окно
	if err := cmd.Start(); err != nil {
		log.Close()
		y.zamok.Unlock()
		return fmt.Errorf("ядро не запустилось: %w", err)
	}
	y.process, y.sost, y.umer = cmd, Podnimaem, make(chan struct{})
	umer := y.umer
	y.zamok.Unlock()

	go func() {
		_ = cmd.Wait()
		log.Close()
		y.zamok.Lock()
		if y.process == cmd { // не мы его остановили — значит упал
			y.process, y.sost = nil, Slomalos
			y.poslLog = hvostLoga(filepath.Join(y.Papka, "yadro.log"))
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
			return fmt.Errorf("ядро упало при старте: %s", hvostLoga(filepath.Join(y.Papka, "yadro.log")))
		case <-srok.Done():
			_ = y.Ostanovit()
			return fmt.Errorf("ядро не ответило за 45 секунд: %s", hvostLoga(filepath.Join(y.Papka, "yadro.log")))
		case <-time.After(300 * time.Millisecond):
			if y.Zhivo() {
				y.zamok.Lock()
				// Ядро поднялось: беда прошлой попытки больше не беда,
				// иначе окно показывает красную строку поверх работающей связи.
				y.sost, y.poslLog = Rabotaet, ""
				y.zamok.Unlock()
				return nil
			}
		}
	}
}

// Ostanovit гасит ядро. Повторный вызов на остановленном — не ошибка.
func (y *Yadro) Ostanovit() error {
	y.zamok.Lock()
	cmd, umer := y.process, y.umer
	y.process, y.sost = nil, Stoit
	y.zamok.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := zavershit(cmd); err != nil {
		return err
	}
	select {
	case <-umer:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
	}
	return nil
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

// hvostLoga — последние строки лога ядра, чтобы показать причину, а не «ошибку».
func hvostLoga(put string) string {
	b, err := os.ReadFile(put)
	if err != nil {
		return ""
	}
	if len(b) > 4096 {
		b = b[len(b)-4096:]
	}
	stroki := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(stroki) > 6 {
		stroki = stroki[len(stroki)-6:]
	}
	return strings.Join(stroki, " | ")
}

// PID запущенного ядра — для окна диагностики.
func (y *Yadro) PID() string {
	y.zamok.Lock()
	defer y.zamok.Unlock()
	if y.process == nil || y.process.Process == nil {
		return ""
	}
	return strconv.Itoa(y.process.Process.Pid)
}
