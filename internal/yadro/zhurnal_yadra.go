package yadro

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// imyaZhurnala / hvostProshlogo — журнал ядра и его ротация. Хвост тот же,
// что у журнала приложения (cmd/kelevra/zhurnal.go): одно имя для одного
// понятия, и суточная отправка журналов (internal/zhurnaly) узнаёт по нему,
// что перед ней предшественник, а не отдельный файл.
const (
	imyaZhurnala   = "yadro.log"
	hvostProshlogo = ".proshlyy"
)

// skolkoProshlyh — сколько прошлых журналов ядра лежит рядом: .proshlyy
// (самый свежий из прошлых), .proshlyy.2 … .proshlyy.N.
//
// Одного .proshlyy мало: «Дом» лечится перезапуском ядра, сторож поднимает
// упавшее ядро — и каждый перезапуск сдвигал ротацию. Журнал того запуска, в
// котором случилась беда, затирался за часы (31.10: почему «Дом» не ожил,
// доказать было нечем), а при падениях подряд — за минуты.
const skolkoProshlyh = 4

// PredelZhurnala — выше этого размера рабочий журнал ядра уезжает в
// .proshlyy прямо на ходу (ObrezatZhurnal). Ядро пишет в файл само, сутками
// и неделями без перезапуска; без предела файл рос бы без конца.
const PredelZhurnala = 16 << 20

// PutZhurnala — журнал ядра этого экземпляра.
func (y *Yadro) PutZhurnala() string { return filepath.Join(y.Papka, imyaZhurnala) }

// PutZhurnalaVPapke — где лежал бы журнал ядра, работающего в этой папке.
//
// Нужна там, где экземпляра ядра под рукой нет, а путь знать надо: суточная
// отправка журналов (internal/zhurnaly) собирает список файлов до и независимо
// от того, поднято ли ядро вообще. Имя файла живёт здесь в одном месте, чтобы
// отправка и запись не разъехались молча.
func PutZhurnalaVPapke(papka string) string { return filepath.Join(papka, imyaZhurnala) }

// PutProshlogoZhurnala — журнал ядра ПРОШЛОГО запуска.
func (y *Yadro) PutProshlogoZhurnala() string { return y.PutZhurnala() + hvostProshlogo }

// putProshlogo — n-й прошлый журнал: 1 — .proshlyy, дальше .proshlyy.n.
func putProshlogo(put string, n int) string {
	if n <= 1 {
		return put + hvostProshlogo
	}
	return fmt.Sprintf("%s%s.%d", put, hvostProshlogo, n)
}

// sdvinutProshlye освобождает место .proshlyy: самый старый уходит, остальные
// сдвигаются на одну ступень. Ошибки молча — см. sozdatZhurnalYadra.
func sdvinutProshlye(put string) {
	_ = os.Remove(putProshlogo(put, skolkoProshlyh))
	for n := skolkoProshlyh - 1; n >= 1; n-- {
		if _, err := os.Stat(putProshlogo(put, n)); err == nil {
			_ = os.Rename(putProshlogo(put, n), putProshlogo(put, n+1))
		}
	}
}

// sozdatZhurnalYadra готовит yadro.log под новый запуск ядра, СОХРАНИВ
// предыдущие.
//
// Беда 31.08, приборно: было 129 822 байта, стало 6 445 — os.Create обнулял
// журнал ядра на каждом старте. То есть посмертной истории не оставалось
// вовсе, а именно она и нужна: «подключено, но не грузится» разбирается
// строками ТОГО запуска, который сломался, а человек к этому моменту уже
// нажал «Отключить» и «Подключить» ещё раз — и стёр всё, что могло объяснить.
// Теперь прошлые запуски лежат рядом: .proshlyy, .proshlyy.2 … (skolkoProshlyh).
//
// Ротация делается переименованием, а не копированием: файл может быть в
// сотни килобайт, а переименование — одно движение в файловой таблице и не
// зависит от свободного места. Пустой журнал не ротируем: сохранять ноль
// байт поверх настоящей истории прошлого запуска — это её потерять (так
// бывает, когда ядро упало, не написав ни строки).
//
// Файл открывается на ДОЗАПИСЬ и отдаётся ядру напрямую (Zapustit): ядро
// пишет журнал само, без трубы через процесс службы. Раньше вывод шёл через
// io.Pipe службы, и после её обновления принятое ядро писало в трубу умершего
// процесса — yadro.log молчал до следующего перезапуска (handoff 06.11).
// Дозапись (O_APPEND) нужна ещё и для ObrezatZhurnal: файл, усечённый службой
// на ходу, ядро продолжает с его нового конца, а не с прежнего смещения.
// Не удалось переименовать (файл держит чужой процесс) — новый запуск пишет в
// конец прежнего, но не стирает его.
//
// Ошибки ротации намеренно проглатываются: не удалось сохранить прошлый
// журнал — это досадно, но запускать ядро всё равно надо, а открытие ниже
// либо отработает, либо честно вернёт ошибку вызывающему.
func sozdatZhurnalYadra(papka string) (*os.File, error) {
	put := filepath.Join(papka, imyaZhurnala)
	if st, err := os.Stat(put); err == nil && !st.IsDir() && st.Size() > 0 {
		sdvinutProshlye(put)
		_ = os.Rename(put, putProshlogo(put, 1))
	}
	return os.OpenFile(put, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
}

// ObrezatZhurnal — рабочий журнал ядра больше PredelZhurnala: его содержимое
// уезжает в .proshlyy (ротация сдвигается), сам файл усекается до нуля, а
// живое ядро продолжает писать в него же (дозапись). Переименовать файл под
// живым ядром нельзя — на Windows ядро держит его открытым, — поэтому копия и
// усечение. Строки, дописанные ядром между копией и усечением, теряются: это
// миллисекунды раз в много часов.
//
// true — журнал обрезан.
func ObrezatZhurnal(papka string, predel int64) (bool, error) {
	put := filepath.Join(papka, imyaZhurnala)
	st, err := os.Stat(put)
	if err != nil || st.IsDir() || st.Size() <= predel {
		return false, nil
	}
	sdvinutProshlye(put)
	if err := skopirovatZhurnal(put, putProshlogo(put, 1)); err != nil {
		return false, err
	}
	if err := os.Truncate(put, 0); err != nil {
		return false, err
	}
	return true, nil
}

func skopirovatZhurnal(otkuda, kuda string) error {
	src, err := os.Open(otkuda)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(kuda, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// pripisatVZhurnal дописывает строку службы в журнал ядра (запуск, выход с
// кодом): ядро пишет без времени (log.timestamp выключен), и без этих строк
// по файлу не понять, где кончился один запуск и начался другой.
func pripisatVZhurnal(papka, tekst string) {
	f, err := os.OpenFile(filepath.Join(papka, imyaZhurnala), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "--- %s kelevra: %s ---\n", time.Now().Format("2006-01-02 15:04:05"), tekst)
}

// ShumVZhurnale — строка журнала ядра, которую разбор пропускает: обычное
// закрытие http2 (podstrokaShum, ~1000 строк в час). Раньше её вырезал фильтр
// на трубе службы (filtr.go); теперь ядро пишет в файл само, и шум отсеивается
// при чтении: в причине падения (hvostLoga) и в разборе отказов (sluzhba).
func ShumVZhurnale(stroka string) bool { return strings.Contains(stroka, podstrokaShum) }
