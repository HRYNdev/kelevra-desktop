package yadro

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"
)

// podstrokaShum — обрывок строки, которую golang.org/x/net/http2 печатает
// при обычном закрытии тела ответа: «http2: response body closed». sing-box
// сам считает это штатным закрытием — baderror.WrapH2 переводит её в
// net.ErrClosed, — но x/net успевает напечатать её раньше. Чистый шум:
// handoff 23.10 насчитал ~1000 строк в час в yadro.log.
const podstrokaShum = "response body closed"

// maxStrokaLoga — верхний предел одной строки лога, которую фильтр готов
// принять целиком. Обычные строки sing-box короткие; предел нужен, чтобы
// редкая длинная строка (например, дамп конфига в одну строку) не рвала
// bufio.Scanner по умолчанию (64 КиБ) на несколько кусков.
const maxStrokaLoga = 1 << 20 // 1 МиБ

// filtrLogYadra построчно переносит вывод ядра из r в w, отбрасывая строки
// с podstrokaShum и считая их вместо записи. Раз в period (если накопилось
// хоть что-то) и при выходе (r отдал EOF — процесс закрыл свой конец пайпа)
// пишет в w одну итоговую строку с числом пропущенных строк. Остальные
// строки идут в w как есть, порядок не меняется.
//
// period<=0 отключает промежуточные сбросы: тогда итог пишется только один
// раз, перед возвратом.
func filtrLogYadra(r io.Reader, w io.Writer, period time.Duration) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxStrokaLoga)

	var propushcheno int
	posledniySbros := time.Now()

	sbros := func() {
		if propushcheno > 0 {
			fmt.Fprintf(w, "%s [фильтр] пропущено %d строк «http2: response body closed» (обычное закрытие http2)\n",
				time.Now().Format("2006-01-02 15:04:05"), propushcheno)
			propushcheno = 0
		}
		posledniySbros = time.Now()
	}

	for sc.Scan() {
		stroka := sc.Text()
		if strings.Contains(stroka, podstrokaShum) {
			propushcheno++
		} else {
			fmt.Fprintln(w, stroka)
		}
		if period > 0 && time.Since(posledniySbros) >= period {
			sbros()
		}
	}
	sbros()
}
