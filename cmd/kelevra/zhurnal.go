package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// Путь запасной папки журнала знает hranenie (ZapasnayaPapkaZhurnala) — тот
// же путь обязана обойти суточная отправка журналов, и двух определений
// одного места быть не должно.

// predelZhurnala — при каком размере журнал уезжает в «прошлый». Приложение
// живёт месяцами, а строк пишет мало; полмегабайта — это надолго.
const predelZhurnala = 512 * 1024

// pisatelZhurnala — куда уходит вывод log.
//
// Здесь и была беда, из-за которой kelevra.log лежал РОВНО НОЛЬ БАЙТ во всех
// трёх прогонах стенда 31.08 (и на машине человека тоже). Раньше стояло
// io.MultiWriter(os.Stderr, f) — а io.MultiWriter останавливается на ПЕРВОЙ
// же ошибке и до следующего писателя не доходит:
//
//	for _, w := range t.writers {
//	    n, err = w.Write(p)
//	    if err != nil { return }
//	    if n != len(p) { err = ErrShortWrite; return }
//	}
//
// Kelevra.exe собирается оконным (-H=windowsgui). Когда его запускает
// проводник, ярлык, автозапуск или UAC (ShellExecuteW), никаких стандартных
// потоков процессу не достаётся: GetStdHandle отдаёт NULL, os.Stderr.Fd()
// равен 0, и первая же запись отвечает «The handle is invalid». Значит
// MultiWriter обрывался на stderr и до файла не доходил НИКОГДА — файл
// создавался (O_CREATE) и оставался пустым.
//
// Замерено опытом 31.08 на этой же машине: оконная сборка, запущенная так же,
// как её запускает проводник, — os.Stderr fd=0, «write /dev/stderr: The handle
// is invalid», файл через MultiWriter 0 байт, тот же файл напрямую 56 байт.
// Дочерний процесс (служба) при этом получает от os/exec настоящий NUL и
// пишет нормально — то есть половина событий терялась гарантированно, а
// вторая половина зависела от того, как процесс родился.
//
// Отсюда порядок здесь обратный: ФАЙЛ ПЕРВЫЙ и безусловный, stderr — эхо для
// стенда (stend/*.sh читают вывод exe), и его отказ не имеет права отнять
// журнал. Мёртвый stderr выключается после первой же ошибки, чтобы не
// платить системным вызовом за каждую строку.
type pisatelZhurnala struct {
	fayl io.Writer
	ekho io.Writer // stderr; nil, когда его нет или он уже отказал
}

func (p *pisatelZhurnala) Write(b []byte) (int, error) {
	n, err := p.fayl.Write(b)
	if p.ekho != nil {
		if _, e := p.ekho.Write(b); e != nil {
			p.ekho = nil // консоли нет — больше не пробуем
		}
	}
	return n, err
}

// rolProcessa — окно или служба. Окно и служба — РАЗНЫЕ процессы одного .exe
// (см. шапку main.go), и пишут они в ОДИН файл журнала. Без пометки строки
// двух процессов в нём перемешаны и неразличимы, а разбор аварии начинается
// ровно с вопроса «кто это сказал»: окно, которое не дождалось службы, или
// служба, у которой не встало ядро.
//
// Условие то же самое, что и в main() (rezhimSluzhby), и разъехаться им
// нельзя — сторожит TestRolProcessaSovpadaetSRezhimomSluzhby.
func rolProcessa() string {
	if os.Getenv("KELEVRA_BEZ_OKNA") == "1" || estArg(argSluzhba) {
		return "служба"
	}
	return "окно"
}

// otkrytZhurnal направляет весь вывод log в файл рядом с данными приложения.
//
// Зачем: Kelevra.exe собирается оконным (-H=windowsgui), консоли у него нет,
// и всё, что уходит в stderr, у пользователя не видит никто. Без файла
// единственным следом отказа остаётся то, что приложение «не запустилось»,
// а по такому описанию причину не найти.
//
// Файл открывается на ДОПИСЫВАНИЕ (O_APPEND, без O_TRUNC): накопленное за
// прошлые запуски — это и есть история, ради которой журнал заведён, и
// обнулять её новым стартом нельзя. Два процесса, дописывающие один файл
// одновременно, друг другу не мешают: O_APPEND на Windows — это
// FILE_APPEND_DATA, каждая запись атомарно уходит в конец (проверено опытом
// 31.08: окно и служба писали в один kelevra.log, обе строки на месте).
//
// Возвращает путь к журналу (пустой, если писать некуда) и функцию закрытия.
func otkrytZhurnal(papka string) (string, func()) {
	return otkrytZhurnalRolyu(papka, rolProcessa())
}

// otkrytZhurnalRolyu — то же самое, но роль задаётся снаружи: тесту нужно
// открыть журнал и от лица окна, и от лица службы в одном процессе.
func otkrytZhurnalRolyu(papka, rol string) (string, func()) {
	put, f := otkrytFayl(papka)
	if f == nil {
		// Запасной путь: если своя папка недоступна (нет прав, антивирус),
		// журнал всё равно нужен — именно этот случай и надо будет разбирать.
		put, f = otkrytFayl(hranenie.ZapasnayaPapkaZhurnala())
	}
	if f == nil {
		return "", func() {}
	}
	log.SetOutput(&pisatelZhurnala{fayl: f, ekho: os.Stderr})
	log.SetFlags(log.LstdFlags)
	log.SetPrefix(fmt.Sprintf("[%s %d] ", rol, os.Getpid()))
	return put, func() { _ = f.Close() }
}

func otkrytFayl(papka string) (string, *os.File) {
	if err := os.MkdirAll(papka, 0o755); err != nil {
		return "", nil
	}
	put := filepath.Join(papka, filepath.Base(hranenie.PutZhurnala()))
	if st, err := os.Stat(put); err == nil && st.Size() > predelZhurnala {
		_ = os.Rename(put, put+".proshlyy")
	}
	f, err := os.OpenFile(put, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", nil
	}
	return put, f
}
