// Подбор имени для сетевого адаптера туннеля.
//
// Зачем. Замер с машины человека 01.09 (его же журнал приложения):
//
//	22:07:42 поднят туннель (адаптер "tun125")
//	22:07:45 человек нажал «Отключить» / останавливаю ядро
//	22:07:58 ядро запущено: pid 29604
//	22:08:14 ядро упало само: FATAL start service: start inbound/tun[tun-in]:
//	         configure tun interface: (create adapter: Cannot create a file when
//	         that file already exists. | open existing adapter: Element not found.)
//	22:08:14 полный режим не поднялся — работаю частично
//
// Читается однозначно: имя «tun125» после остановки ядра осталось занятым.
// Создать адаптер по этому имени нельзя («файл уже существует»), открыть
// существующий тоже нельзя («элемент не найден») — устройство висит в
// полумёртвом состоянии, из которого выходит не мгновенно. Повторилось в тот
// вечер дважды подряд, итог для человека каждый раз один: половинная защита
// вместо полной, хотя права он выдал и туннель ему положен.
//
// Отсюда два решения, и оба тут.
//
// Первое: занятость имени проверяется ДО запуска ядра. Проверка стоит
// миллисекунды (net.Interfaces()), а незнание стоит пятнадцати секунд, которые
// человек смотрит на «Подключаюсь», чтобы получить в конце откат.
//
// Второе: занятое имя — не приговор. interface_name это просто строка в
// конфиге, ядру всё равно, tun125 там или tun126. Поэтому мы НЕ сносим чужое
// сетевое устройство (снос требует прав, возни с PnP-стеком Windows и вполне
// может задеть не наше — а необратимое в чужой системе делается только по
// прямой просьбе), а берём соседнее свободное имя. Туннель поднимается с
// первой попытки, в системе не ломается ничего.
//
// Ждать перед подменой всё же стоит: чаще всего висит адаптер нашей же
// прошлой попытки, который драйвер вот-вот уберёт сам, и вернуть человеку его
// собственное имя чище, чем плодить tun126, tun127, tun128 на каждое нажатие.

package tunnel

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// SrokOsvobozhdeniya — сколько ждём, что занятое имя освободится само.
	// Три секунды: столько человек терпит на кнопке, не думая, что она
	// сломалась, и этого хватает драйверу убрать адаптер умершего процесса.
	// Платится этот срок ТОЛЬКО когда имя действительно занято — свободное
	// имя не стоит ничего (см. SvobodnoeImya: первый же вопрос системе).
	SrokOsvobozhdeniya = 3 * time.Second

	// shagOprosa — как часто переспрашиваем систему, пока ждём.
	shagOprosa = 200 * time.Millisecond

	// zapasnyhImyon — сколько соседних имён пробуем, прежде чем сдаться.
	// Восемь: если заняты и tun125, и семь соседей, дело не в нашем остатке,
	// а в системе, которую надо перезагружать, — и врать человеку про это
	// нельзя (см. konfig.ZametkaAdapterZanyat).
	zapasnyhImyon = 8
)

// Podbor — что вышло у подбора имени. Возвращается целиком, а не одной
// строкой: журналу нужно знать, ждали ли мы и что оказалось занято, а
// вызывающему — сказать ли человеку про занятое устройство словами.
type Podbor struct {
	// Imya — какое имя брать. Пусто — свободного не нашлось вовсе.
	Imya string
	// Ishodnoe — какое имя стояло в профиле.
	Ishodnoe string
	// Zhdali — сколько прождали освобождения. Ноль — имя было свободно сразу.
	Zhdali time.Duration
	// Zamena — пришлось взять другое имя, не то, что в профиле.
	Zamena bool
	// Zanyaty — имена, которые оказались заняты. Для журнала: по нему видно,
	// один остаток висит или система забита ими насовсем.
	Zanyaty []string
}

// Vyshlo — нашлось ли имя, с которым можно запускать ядро.
func (p Podbor) Vyshlo() bool { return p.Imya != "" }

// Slovami — одна строка для журнала. Молчит, когда рассказывать нечего:
// обычный запуск на свободном имени не должен засорять журнал человека.
func (p Podbor) Slovami() string {
	switch {
	case !p.Vyshlo():
		return fmt.Sprintf("имя сетевого адаптера %q занято, и все %d соседних тоже (%s) — "+
			"полный режим сейчас не поднять", p.Ishodnoe, zapasnyhImyon, strings.Join(p.Zanyaty, ", "))
	case p.Zamena:
		return fmt.Sprintf("имя сетевого адаптера %q занято остатком прошлой попытки и за %s не освободилось — "+
			"беру свободное %q", p.Ishodnoe, p.Zhdali.Round(100*time.Millisecond), p.Imya)
	case p.Zhdali > 0:
		return fmt.Sprintf("имя сетевого адаптера %q было занято, освободилось за %s — запускаюсь на нём",
			p.Imya, p.Zhdali.Round(100*time.Millisecond))
	default:
		return ""
	}
}

// SvobodnoeImya — под каким именем поднимать туннель прямо сейчас.
//
// Порядок ровно такой и не другой:
//
//  1. Спросить систему про имя из профиля. Свободно — берём его и уходим, не
//     потратив ничего: это обычный случай, и замедлять его нельзя.
//  2. Занято — подождать до srok, переспрашивая: почти всегда это адаптер
//     нашей же прошлой попытки, и своё имя лучше чужого.
//  3. Так и не освободилось — взять соседнее свободное.
//  4. Заняты и соседние — честно вернуть пустое имя. Вызывающий на этом не
//     падает, а опускается на ступень ниже и НАЗЫВАЕТ причину человеку.
//
// zhiv — проверка «есть ли в системе адаптер с таким именем», nil = боевая
// [Zhivoy]. Параметром, а не вызовом изнутри: настоящих сетевых адаптеров в
// тесте быть не должно вовсе.
func SvobodnoeImya(zhelaemoe string, srok time.Duration, zhiv Adapter) Podbor {
	zhelaemoe = strings.TrimSpace(zhelaemoe)
	if zhelaemoe == "" {
		// Профиль не задал interface_name — подбирать нечего, ядро назовёт
		// адаптер само. Пустое имя тут не беда, а «вопрос не ко мне».
		return Podbor{}
	}
	if zhiv == nil {
		zhiv = Zhivoy
	}
	p := Podbor{Ishodnoe: zhelaemoe}
	if !zhiv(zhelaemoe) {
		p.Imya = zhelaemoe
		return p
	}

	nachalo := time.Now()
	shag := shagOprosa
	if srok < shag {
		shag = srok
	}
	for shag > 0 && time.Since(nachalo) < srok {
		time.Sleep(shag)
		if !zhiv(zhelaemoe) {
			p.Imya, p.Zhdali = zhelaemoe, time.Since(nachalo)
			return p
		}
	}
	p.Zhdali = time.Since(nachalo)

	imya, zanyaty := OboytiZanyatoe(zhelaemoe, zhiv)
	p.Imya, p.Zanyaty, p.Zamena = imya, zanyaty, imya != ""
	return p
}

// ZanyatoePoOshibke — говорит ли падение ядра о занятом имени адаптера.
//
// Дословно с машины человека 01.09:
//
//	start inbound/tun[tun-in]: configure tun interface: (create adapter: Cannot
//	create a file when that file already exists. | open existing adapter:
//	Element not found.)
//
// Опознаём по «configure tun interface» и «create adapter» — это СВОИ строки
// ядра, а не системные. Текст самих ошибок Windows («Cannot create a file…»)
// переведён на язык системы и на другой машине придёт по-русски: ловить беду
// по нему значит поймать её только у тех, у кого Windows английская.
//
// От соседней беды («configure tun interface: permission denied» — прав нет)
// отличается именно «create adapter»: там адаптер даже не начинали создавать.
func ZanyatoePoOshibke(err error) bool {
	if err == nil {
		return false
	}
	tekst := strings.ToLower(err.Error())
	return strings.Contains(tekst, "configure tun interface") && strings.Contains(tekst, "create adapter")
}

// OboytiZanyatoe подбирает СОСЕДНЕЕ свободное имя, а само занятое не
// проверяет и не возвращает никогда.
//
// Отдельно от [SvobodnoeImya] ради второго случая, который проверкой заранее
// не ловится вовсе. Ядро сказало «create adapter: файл уже существует | open
// existing adapter: элемент не найден» — значит устройство есть на уровне
// драйвера, но в списке сетевых интерфейсов Windows его может уже не быть
// («элемент не найден» — ровно про это). Тогда наш вопрос системе честно
// отвечает «свободно», ядро всё равно падает, и переспрашивать бессмысленно:
// на повтор надо идти с ДРУГИМ именем, не проверяя старое.
func OboytiZanyatoe(zanyatoe string, zhiv Adapter) (imya string, zanyaty []string) {
	if zhiv == nil {
		zhiv = Zhivoy
	}
	zanyaty = []string{zanyatoe}
	for _, k := range kandidaty(zanyatoe) {
		if !zhiv(k) {
			return k, zanyaty
		}
		zanyaty = append(zanyaty, k)
	}
	return "", zanyaty
}

// kandidaty — соседние имена по порядку. «tun125» → tun126…tun133: имя
// остаётся узнаваемым и в окне диагностики, и в журнале, и в чужих сетевых
// настройках, куда человек может заглянуть.
func kandidaty(imya string) []string {
	osnova, nomer, estNomer := razobratImya(imya)
	spisok := make([]string, 0, zapasnyhImyon)
	for i := 1; i <= zapasnyhImyon; i++ {
		if estNomer {
			spisok = append(spisok, fmt.Sprintf("%s%d", osnova, nomer+i))
			continue
		}
		// Имя без числового хвоста («kelevra») — приписываем номер к нему
		// целиком, а не выдумываем своё имя: чужую строку из профиля мы не
		// переписываем, только дополняем.
		spisok = append(spisok, fmt.Sprintf("%s%d", imya, i))
	}
	return spisok
}

// razobratImya делит имя на основу и хвостовое число: «tun125» → («tun», 125).
func razobratImya(imya string) (osnova string, nomer int, est bool) {
	i := len(imya)
	for i > 0 && imya[i-1] >= '0' && imya[i-1] <= '9' {
		i--
	}
	if i == len(imya) {
		return imya, 0, false
	}
	n, err := strconv.Atoi(imya[i:])
	if err != nil {
		// Хвост длиннее int — считаем, что числа нет: выдуманное переполнение
		// хуже честного «допишу номер в конец».
		return imya, 0, false
	}
	return imya[:i], n, true
}
