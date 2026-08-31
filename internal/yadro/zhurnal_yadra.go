package yadro

import (
	"os"
	"path/filepath"
)

// imyaZhurnala / hvostProshlogo — журнал ядра и его ротация. Хвост тот же,
// что у журнала приложения (cmd/kelevra/zhurnal.go): одно имя для одного
// понятия, и суточная отправка журналов (internal/zhurnaly) узнаёт по нему,
// что перед ней предшественник, а не отдельный файл.
const (
	imyaZhurnala   = "yadro.log"
	hvostProshlogo = ".proshlyy"
)

// PutZhurnala — журнал ядра этого экземпляра.
func (y *Yadro) PutZhurnala() string { return filepath.Join(y.Papka, imyaZhurnala) }

// PutProshlogoZhurnala — журнал ядра ПРОШЛОГО запуска.
func (y *Yadro) PutProshlogoZhurnala() string { return y.PutZhurnala() + hvostProshlogo }

// sozdatZhurnalYadra готовит yadro.log под новый запуск ядра, СОХРАНИВ
// предыдущий.
//
// Беда 31.08, приборно: было 129 822 байта, стало 6 445 — os.Create обнулял
// журнал ядра на каждом старте. То есть посмертной истории не оставалось
// вовсе, а именно она и нужна: «подключено, но не грузится» разбирается
// строками ТОГО запуска, который сломался, а человек к этому моменту уже
// нажал «Отключить» и «Подключить» ещё раз — и стёр всё, что могло объяснить.
// Теперь предыдущий запуск лежит рядом под .proshlyy.
//
// Ротация делается переименованием, а не копированием: файл может быть в
// сотни килобайт, а переименование — одно движение в файловой таблице и не
// зависит от свободного места. Пустой журнал не ротируем: сохранять ноль
// байт поверх настоящей истории прошлого запуска — это её потерять (так
// бывает, когда ядро упало, не написав ни строки).
//
// Ошибки ротации намеренно проглатываются: не удалось сохранить прошлый
// журнал — это досадно, но запускать ядро всё равно надо, а os.Create ниже
// либо отработает, либо честно вернёт ошибку вызывающему.
func sozdatZhurnalYadra(papka string) (*os.File, error) {
	put := filepath.Join(papka, imyaZhurnala)
	if st, err := os.Stat(put); err == nil && !st.IsDir() && st.Size() > 0 {
		_ = os.Remove(put + hvostProshlogo)
		_ = os.Rename(put, put+hvostProshlogo)
	}
	return os.Create(put)
}
