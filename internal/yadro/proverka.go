package yadro

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Проверка нового конфига ДО того, как гасить работающее ядро.
//
// Зачем. Перезапуск ядра — это «погасить и поднять». Если новое ядро на новом
// конфиге не встаёт (сервер прислал поле, которого это ядро не знает; набор
// правил битый), человек остаётся без связи, а прежнее работавшее ядро уже
// погашено. sing-box сам при перечитке конфига (SIGHUP, cmd_run.go) сперва
// проверяет его и при ошибке остаётся на прежнем; у нас перечитка — новый
// процесс, значит проверку делаем сами: `sing-box check -c <конфиг> -D <папка>`
// тем же бинарём, той же папкой. Провал — рабочее ядро не трогаем.
//
// check собирает ядро из конфига (входы, выходы, правила, DNS), не запуская:
// адаптер туннеля не создаётся, порты не занимаются, сеть не трогается.

// ImyaProverochnogo — файл, куда кладётся проверяемый конфиг: рядом с
// рабочим (те же относительные пути), под своим именем, чтобы не задеть
// config.json живого ядра.
const ImyaProverochnogo = "config.proverka.json"

// srokProverki — сколько ждать check. Обычно доли секунды; дольше — ядро
// зависло на чём-то, что к смыслу конфига отношения не имеет.
var srokProverki = 20 * time.Second

// OshibkaProverki — ядро проверило конфиг и отвергло его. Вывод — текст
// ядра (последние строки), для журнала.
type OshibkaProverki struct{ Vyvod string }

func (o OshibkaProverki) Error() string {
	if o.Vyvod == "" {
		return "ядро не приняло конфиг"
	}
	return "ядро не приняло конфиг: " + o.Vyvod
}

// ErrProverkaNeSostoyalas — проверить не вышло (нет файла ядра, не записать
// конфиг, процесс не запустился, срок вышел). Это не приговор конфигу:
// вызывающий решает сам, идти ли дальше как раньше.
var ErrProverkaNeSostoyalas = errors.New("проверка конфига не состоялась")

// KonfigOtvergnut — ошибка значит «ядро отвергло конфиг» (а не «проверить не
// вышло»).
func KonfigOtvergnut(err error) bool {
	var o OshibkaProverki
	return errors.As(err, &o)
}

// Proverit — `<ядро> check` на конфиге telo. nil — ядро конфиг принимает.
func (y *Yadro) Proverit(ctx context.Context, telo []byte) error {
	if !y.EstBinar() {
		return fmt.Errorf("%w: нет файла ядра %s", ErrProverkaNeSostoyalas, y.Bin)
	}
	if err := os.MkdirAll(y.Papka, 0o755); err != nil {
		return fmt.Errorf("%w: %v", ErrProverkaNeSostoyalas, err)
	}
	put := filepath.Join(y.Papka, ImyaProverochnogo)
	// В конфиге пароль Clash API и ключи узлов: файл закрыт, как и рабочий.
	if err := zapisatZakryto(put, telo); err != nil {
		return fmt.Errorf("%w: конфиг не записан: %v", ErrProverkaNeSostoyalas, err)
	}
	defer os.Remove(put)

	pctx, otmena := context.WithTimeout(ctx, srokProverki)
	defer otmena()
	cmd := exec.CommandContext(pctx, y.Bin, "check", "-c", put, "-D", y.Papka, "--disable-color")
	cmd.Dir = y.Papka
	var vyvod bytes.Buffer
	cmd.Stdout, cmd.Stderr = &vyvod, &vyvod
	spryatatOkno(cmd)
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if pctx.Err() != nil {
		return fmt.Errorf("%w: ядро не ответило за %s", ErrProverkaNeSostoyalas, srokProverki)
	}
	var vyhod *exec.ExitError
	if !errors.As(err, &vyhod) {
		return fmt.Errorf("%w: %v", ErrProverkaNeSostoyalas, err)
	}
	return OshibkaProverki{Vyvod: hvostVyvoda(vyvod.String())}
}

// hvostVyvoda — строки ядра с причиной (FATAL/ERROR), иначе последние три.
func hvostVyvoda(s string) string {
	s = cveta.ReplaceAllString(strings.TrimSpace(s), "")
	if s == "" {
		return ""
	}
	stroki := strings.Split(s, "\n")
	var vazhnye []string
	for _, str := range stroki {
		if strings.Contains(str, "FATAL") || strings.Contains(str, "ERROR") {
			vazhnye = append(vazhnye, strings.TrimSpace(str))
		}
	}
	if len(vazhnye) == 0 {
		vazhnye = stroki
	}
	if len(vazhnye) > 3 {
		vazhnye = vazhnye[len(vazhnye)-3:]
	}
	itog := strings.Join(vazhnye, " | ")
	if len(itog) > 600 {
		itog = itog[:600] + "…"
	}
	return itog
}
