package sluzhba

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"sort"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
	"github.com/HRYNdev/kelevra-desktop/internal/zapisi"
)

// Записи с полями на компьютере: читаем СВОЙ журнал ядра и складываем из него
// события данными, а не текстом.
//
// Зачем читать файл, а не спрашивать ядро. Ядро пишет журнал само, в свой файл;
// его служебный порт отдаёт тот же поток, но подписка на него — это ещё одно
// соединение и ещё одна причина сломаться, тогда как файл уже есть и уже
// ротируется. Разбор строк живёт в internal/zapisi и проверяется без ядра.
//
// Зачем вообще. До 10.09.2026 сервер вытаскивал отказы из строк журнала
// регулярными выражениями. Это работает, но ломается молча: ядро меняет
// формулировку — и разбор перестаёт находить события, а узнаётся это тогда, когда
// в очередной раз не выходит поставить диагноз. Телефон с этого дня пишет поля,
// компьютер теперь тоже, и сервер разбирает обе платформы одинаково.

// ShagChteniyaYadra — как часто заглядываем в хвост журнала ядра.
//
// Десять секунд, а не постоянное слежение: поток ядра идёт пачками, а нам нужны
// не строки, а события и сводки. Пропустить ничего нельзя — читаем от
// запомненного смещения, и всё, что накопилось, разбирается целиком.
var ShagChteniyaYadra = 10 * time.Second

// OknoSvodkiYadra — за какое окно считается сводка.
var OknoSvodkiYadra = 10 * time.Minute

// nablyudatelYadra — состояние чтения между заходами.
type nablyudatelYadra struct {
	pisatel   *zapisi.Pisatel
	put       string
	smeshenie int64
	razmer    int64

	oknoNachalo time.Time
	soed        int
	otkazy      int
	kody        map[string]int
	imena       map[string]int

	// spravochnik — «адрес → имя сайта» у служебного порта ядра. nil, если
	// ядра нет: тогда отказы пишутся по адресу, как было до 11.09.2026.
	spravochnik *yadro.Imena
}

// SleditZaYadrom крутит чтение журнала ядра, пока живёт служба.
//
// KELEVRA_BEZ_ZAPISEY=1 глушит слежку целиком: на нём стоят стенды, которым
// лишний файл в папке журналов только мешает сверять снимки.
func (s *Sluzhba) SleditZaYadrom(ctx context.Context, shag time.Duration) {
	if os.Getenv("KELEVRA_BEZ_ZAPISEY") == "1" {
		return
	}
	if shag <= 0 {
		shag = ShagChteniyaYadra
	}
	n := &nablyudatelYadra{
		pisatel: zapisi.Novyy(hranenie.Papka()),
		put:     yadro.PutZhurnalaVPapke(hranenie.PapkaYadra()),
		kody:    map[string]int{},
		imena:   map[string]int{},
	}
	if s.Yadro != nil {
		n.spravochnik = yadro.NovyeImena(s.Yadro)
	}
	// Обстановка одной записью на старте: по ней видно, с какими правами и в
	// каком режиме работала машина, когда случилось остальное.
	n.pisatel.Sreda(map[string]any{"rezhim": s.rezhimKartiny(), "prava": s.estPrava()})
	t := time.NewTicker(shag)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.zahod(time.Now())
		}
	}
}

// zahod — один проход по новому хвосту журнала ядра.
func (n *nablyudatelYadra) zahod(seychas time.Time) {
	if n.oknoNachalo.IsZero() {
		n.oknoNachalo = seychas
	}
	stroki, err := n.prochitatNovoe()
	if err == nil {
		for _, stroka := range stroki {
			n.uchest(stroka)
		}
	}
	if seychas.Sub(n.oknoNachalo) >= OknoSvodkiYadra {
		n.zakrytOkno(seychas)
	}
}

// prochitatNovoe — только то, что дописалось с прошлого захода.
//
// Ротация узнаётся по размеру: файл стал меньше запомненного — значит ядро
// перезапустилось и журнал начат заново, читаем с начала. Иначе после каждого
// перезапуска ядра мы читали бы мимо файла и молча ничего не находили.
func (n *nablyudatelYadra) prochitatNovoe() ([]string, error) {
	st, err := os.Stat(n.put)
	if err != nil {
		return nil, err
	}
	if st.Size() < n.razmer {
		n.smeshenie = 0
	}
	n.razmer = st.Size()
	if n.smeshenie >= st.Size() {
		return nil, nil
	}
	f, err := os.Open(n.put)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(n.smeshenie, io.SeekStart); err != nil {
		return nil, err
	}
	// Потолок на один заход: если ядро вылило мегабайты, разберём их частями,
	// а не съедим память разом.
	const potolokZaZahod = 4 << 20
	dlina := st.Size() - n.smeshenie
	if dlina > potolokZaZahod {
		dlina = potolokZaZahod
	}
	bufer := make([]byte, dlina)
	prochitano, err := io.ReadFull(f, bufer)
	if err != nil && prochitano == 0 {
		return nil, err
	}
	bufer = bufer[:prochitano]
	// Смещение двигаем только до последнего перевода строки в прочитанном
	// куске: недописанный ядром хвост (без \n) остаётся на месте и будет
	// прочитан заново, вместе со своим продолжением, на следующем заходе.
	// Если куска целиком нет — оставляем и смещение, и хвост нетронутыми,
	// иначе следующий заход снова упрётся в тот же неполный кусок и будет
	// бесконечно его перечитывать, не продвигаясь.
	poslednPerevod := bytes.LastIndexByte(bufer, '\n')
	if poslednPerevod < 0 {
		return nil, nil
	}
	n.smeshenie += int64(poslednPerevod + 1)
	return razbitNaStroki(string(bufer[:poslednPerevod+1])), nil
}

// uchest — одна строка журнала ядра в счётчики и, если это отказ, в запись.
func (n *nablyudatelYadra) uchest(stroka string) {
	s := zapisi.Razobrat(stroka)
	switch {
	case s.Otkaz:
		n.otkazy++
		n.kody[s.Kod]++
		// Ядро печатает в строке отказа адрес, а имя знает, но не пишет.
		// Спрашиваем его у служебного порта — иначе ось «сайт» в журнале
		// компьютера отсутствует вовсе и диагноз ставить нечем.
		imya := s.Imya
		if imya == "" {
			imya = n.spravochnik.Imya(s.Adres)
		}
		if imya != "" {
			n.imena[imya]++
		}
		n.pisatel.Otkaz(imya, s.Adres, s.Port, s.Kod, s.Vyhod, 0)
	case s.Vhod:
		n.soed++
	}
}

// zakrytOkno — сводка за окно и обнуление счётчиков.
func (n *nablyudatelYadra) zakrytOkno(seychas time.Time) {
	soed, otkazy := n.soed, n.otkazy
	kody, imena := n.kody, n.imena
	n.oknoNachalo = seychas
	n.soed, n.otkazy = 0, 0
	n.kody, n.imena = map[string]int{}, map[string]int{}
	if soed == 0 && otkazy == 0 {
		return
	}
	n.pisatel.Svodka(int(OknoSvodkiYadra.Seconds()), soed, otkazy, kody, topImen(imena, 5))
	if otkazy > 0 && soed > 0 && otkazy*100/soed >= 15 {
		log.Printf("записи: за окно отказов %d из %d соединений — это уже беда", otkazy, soed)
	}
}

// topImen — пять самых частых имён: этого хватает, чтобы увидеть «ломается
// именно такой-то сайт», и сводка не растёт вместе с их числом.
func topImen(imena map[string]int, skolko int) map[string]int {
	if len(imena) <= skolko {
		return imena
	}
	type para struct {
		imya  string
		raz   int
	}
	spisok := make([]para, 0, len(imena))
	for i, r := range imena {
		spisok = append(spisok, para{i, r})
	}
	sort.Slice(spisok, func(a, b int) bool { return spisok[a].raz > spisok[b].raz })
	itog := make(map[string]int, skolko)
	for _, p := range spisok[:skolko] {
		itog[p.imya] = p.raz
	}
	return itog
}

// razbitNaStroki — строки без пустых. Отдельной функцией, чтобы её можно было
// проверить на кусках с оборванной последней строкой.
func razbitNaStroki(tekst string) []string {
	var itog []string
	nachalo := 0
	for i := 0; i < len(tekst); i++ {
		if tekst[i] == '\n' {
			if stroka := tekst[nachalo:i]; len(stroka) > 0 {
				itog = append(itog, stroka)
			}
			nachalo = i + 1
		}
	}
	// Хвост без перевода строки не берём: ядро допишет его в следующий раз, а
	// разобранная половина строки дала бы кривое событие.
	return itog
}
