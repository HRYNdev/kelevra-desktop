// Пакет zapisi: журнал не текстом, а данными.
//
// Зачем. Разбор жалобы на сервере вытаскивает из строк журнала отказы и сводки
// регулярными выражениями. Это работает, но ломается молча: ядро меняет
// формулировку — и разбор перестаёт находить события, а узнаётся это не сразу, а
// когда в очередной раз не получается поставить диагноз. Поля не ломаются.
//
// Почему отдельным файлом, а не переделкой журнала ядра. На текстовом журнале
// держится вся диагностика, и менять его вид значит остаться без разбора на те
// дни, пока обновление доезжает до людей. Здесь новый файл рядом: он уезжает той
// же суточной отправкой (она берёт весь список путей), разбирается отдельно, а
// если в нём что-то не так — текстовый журнал продолжает работать как раньше.
//
// Формат тот же, что у телефона: одна строка — одна запись,
// `{"v":1,"t":"otkaz","ts":…,…}`. Сервер разбирает обе платформы одним куском кода.
package zapisi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ImyaFayla — как называется файл записей. Отправка узнаёт его по этому имени.
const ImyaFayla = "zapisi.jsonl"

// Shema — версия формата. Растёт, когда меняется СМЫСЛ полей, а не когда
// добавилось новое поле: сервер обязан понимать, что читает.
const Shema = 1

// Potolok — больше файл не растёт: записи весят копейки против потока ядра.
const Potolok int64 = 512 * 1024

// Pisatel — куда и как складываем записи.
//
// Ошибка записи НИКОГДА не поднимается наверх: запись — это сведения о работе, а
// не работа. Уронить связь из-за журнала было бы обменом наоборот.
type Pisatel struct {
	zamok sync.Mutex
	put   string
}

// Novyy — писатель в папке журналов. Пустой путь = записи выключены.
func Novyy(papka string) *Pisatel {
	if papka == "" {
		return &Pisatel{}
	}
	return &Pisatel{put: filepath.Join(papka, ImyaFayla)}
}

// Put — где лежит файл записей (нужно отправке).
func (p *Pisatel) Put() string {
	if p == nil {
		return ""
	}
	return p.put
}

// zapisat — одна строка. Тип и поля решает вызывающий: пакет не знает смысла,
// он только складывает.
func (p *Pisatel) zapisat(tip string, polya map[string]any) {
	if p == nil || p.put == "" {
		return
	}
	zapis := map[string]any{
		"v":  Shema,
		"t":  tip,
		"ts": time.Now().UnixMilli(),
	}
	for k, v := range polya {
		if v != nil {
			zapis[k] = v
		}
	}
	stroka, err := json.Marshal(zapis)
	if err != nil {
		return
	}
	p.zamok.Lock()
	defer p.zamok.Unlock()
	p.obrezatEsliRazrossya()
	f, err := os.OpenFile(p.put, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(stroka, '\n'))
}

// obrezatEsliRazrossya — простая ротация в один файл-предшественник.
//
// Переименованием, а не копированием: файл маленький, но движение в таблице
// файлов всё равно дешевле и не зависит от свободного места. Предшественник
// уезжает той же отправкой, поэтому записи не теряются.
func (p *Pisatel) obrezatEsliRazrossya() {
	st, err := os.Stat(p.put)
	if err != nil || st.Size() < Potolok {
		return
	}
	_ = os.Remove(p.put + ".proshlyy")
	_ = os.Rename(p.put, p.put+".proshlyy")
}

// Otkaz — соединение не открылось. Главная запись, по которой ставится диагноз.
func (p *Pisatel) Otkaz(imya, adres string, port int, kod, vyhod string, ms int) {
	semeystvo := 4
	for _, c := range adres {
		if c == ':' {
			semeystvo = 6
			break
		}
	}
	polya := map[string]any{
		"adres": adres,
		"sem":   semeystvo,
		"kod":   kod,
	}
	if imya != "" {
		polya["imya"] = imya
	}
	if port > 0 {
		polya["port"] = port
	}
	if vyhod != "" {
		polya["vyhod"] = vyhod
	}
	if ms > 0 {
		polya["ms"] = ms
	}
	p.zapisat("otkaz", polya)
}

// Svodka — итог окна. Переживает ротацию потока ядра и потому остаётся
// единственным следом того, что творилось час назад.
func (p *Pisatel) Svodka(oknoSek, soed, otkazy int, kody, imena map[string]int) {
	p.zapisat("svodka", map[string]any{
		"okno":   oknoSek,
		"soed":   soed,
		"otkazy": otkazy,
		"kody":   kody,
		"imena":  imena,
	})
}

// Perehod — смена состояния: туннель, сеть, права, профиль, обновление.
func (p *Pisatel) Perehod(kod, podrobno string) {
	polya := map[string]any{"kod": kod}
	if podrobno != "" {
		polya["podrobno"] = podrobno
	}
	p.zapisat("perehod", polya)
}

// Sreda — обстановка: что за сеть и как настроено. Пишется на старте и при
// смене сети, десяток строк за сутки вместо тысяч.
func (p *Pisatel) Sreda(polya map[string]any) {
	p.zapisat("sreda", polya)
}
