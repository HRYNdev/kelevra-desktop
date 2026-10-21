package sluzhba

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
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
	pisatel *zapisi.Pisatel
	put     string
	// mesto — файл, где смещение переживает перезапуск службы
	// (chtenie_yadra.go). Пусто — смещение живёт только в памяти.
	mesto     string
	smeshenie int64
	razmer    int64
	// fayl — какой именно файл читаем. Ядро на каждом своём старте
	// откладывает прошлый журнал в .prev и заводит новый (yadro.sozdatZhurnalYadra):
	// другой файл — читаем с начала, даже если он уже дорос до старого смещения.
	fayl os.FileInfo
	// seredinaStroki — смещение поставлено на конец куска, написанного до
	// старта службы, и он мог оборваться посреди строки: её остаток при
	// первом чтении пропускается.
	seredinaStroki bool
	// mestoSdvinulos — с последней записи места смещение ушло или файл
	// сменился: место пора записать на диск.
	mestoSdvinulos bool

	oknoNachalo time.Time
	soed        int
	otkazy      int
	shumV6      int
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
		mesto:   filepath.Join(hranenie.Papka(), ImyaMestaChteniya),
		kody:    map[string]int{},
		imena:   map[string]int{},
	}
	if s.Yadro != nil {
		n.spravochnik = yadro.NovyeImena(s.Yadro)
	}
	n.nachat(time.Now())
	// Обстановка одной записью на старте: по ней видно, с какими правами и в
	// каком режиме работала машина, когда случилось остальное.
	n.pisatel.Sreda(map[string]any{"rezhim": s.rezhimKartiny(), "prava": s.estPrava()})
	t := time.NewTicker(shag)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			// Служба уходит штатно (обновление, остановка): место чтения —
			// с нынешним временем, чтобы следующая копия продолжила с него.
			n.zapomnitMesto(time.Now())
			return
		case <-t.C:
			if s.vykl(vyklZapisiYadra) {
				continue
			}
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
	if n.mestoSdvinulos {
		n.zapomnitMesto(seychas)
	}
	if seychas.Sub(n.oknoNachalo) >= OknoSvodkiYadra {
		n.zakrytOkno(seychas)
	}
}

// nachatSKonca ставит смещение на нынешний конец журнала ядра.
//
// Запасной путь старта, когда прошлая копия службы не оставила места чтения
// (первый запуск этой версии) или оставила устаревшее (chtenie_yadra.go).
// Раньше смещение после перезапуска службы начиналось с нуля, и журнал, уже
// разобранный прошлой копией, разбирался заново: старые отказы ложились в
// одно свежее окно и давали ложную «беду» (ноутбук 07–08.10: «отказов 1023
// из 1466» при выключенном ядре — ровно сумма трёх настоящих окон утра).
// Цена конца файла — строки, которые ядро дописало, пока службы не было:
// они не учитываются. Ложная тревога хуже.
func (n *nablyudatelYadra) nachatSKonca() {
	f, err := os.Open(n.put)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return
	}
	n.fayl = st
	n.razmer = st.Size()
	n.smeshenie = st.Size()
	b := make([]byte, 1)
	_, err = f.ReadAt(b, st.Size()-1)
	n.seredinaStroki = err != nil || b[0] != '\n'
}

// prochitatNovoe — только то, что дописалось с прошлого захода.
//
// Ротация узнаётся по размеру: файл стал меньше запомненного — значит ядро
// перезапустилось и журнал начат заново, читаем с начала. Иначе после каждого
// перезапуска ядра мы читали бы мимо файла и молча ничего не находили.
func (n *nablyudatelYadra) prochitatNovoe() ([]string, error) {
	// Сведения о файле — через открытый файл, а не os.Stat по пути: на Windows
	// os.SameFile у сведений по пути узнаёт файл по пути же в момент сравнения
	// и принял бы новый журнал за прежний.
	f, err := os.Open(n.put)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() < n.razmer || (n.fayl != nil && !os.SameFile(n.fayl, st)) {
		n.smeshenie = 0
		n.seredinaStroki = false
		n.mestoSdvinulos = true
	}
	n.fayl = st
	n.razmer = st.Size()
	if n.smeshenie >= st.Size() {
		return nil, nil
	}
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
	if n.seredinaStroki {
		// Остаток строки, начатой до старта службы, — не наш: пропускаем до
		// первого перевода строки. Его ещё нет — ждём следующего захода.
		perviy := bytes.IndexByte(bufer, '\n')
		if perviy < 0 {
			return nil, nil
		}
		n.smeshenie += int64(perviy + 1)
		bufer = bufer[perviy+1:]
		n.seredinaStroki = false
		n.mestoSdvinulos = true
	}
	poslednPerevod := bytes.LastIndexByte(bufer, '\n')
	if poslednPerevod < 0 {
		return nil, nil
	}
	n.smeshenie += int64(poslednPerevod + 1)
	n.mestoSdvinulos = true
	return razbitNaStroki(string(bufer[:poslednPerevod+1])), nil
}

// uchest — одна строка журнала ядра в счётчики и, если это отказ, в запись.
func (n *nablyudatelYadra) uchest(stroka string) {
	s := zapisi.Razobrat(stroka)
	switch {
	case s.Otkaz:
		n.otkazy++
		if s.ShumV6() {
			n.shumV6++
		}
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
	soed, otkazy, shum := n.soed, n.otkazy, n.shumV6
	kody, imena := n.kody, n.imena
	n.oknoNachalo = seychas
	n.soed, n.otkazy, n.shumV6 = 0, 0, 0
	n.kody, n.imena = map[string]int{}, map[string]int{}
	if soed == 0 && otkazy == 0 {
		return
	}
	n.pisatel.Svodka(int(OknoSvodkiYadra.Seconds()), soed, otkazy, shum, kody, topImen(imena, 5))
	if bedaZaOkno(soed, otkazy, shum) {
		if shum > 0 {
			log.Printf("записи: за окно отказов %d из %d соединений — это уже беда (ещё %d отказов по IPv6 без пути не в счёт)",
				otkazy-shum, soed-shum, shum)
		} else {
			log.Printf("записи: за окно отказов %d из %d соединений — это уже беда", otkazy, soed)
		}
	}
}

// bedaZaOkno — порог «беды»: 15% отказов от соединений окна.
//
// Шум шестой версии (zapisi.Sobytie.ShumV6) вычитается с обеих сторон: каждая
// такая попытка — соединение, которое программа тут же повторила по четвёртой
// версии, то есть ни отказом, ни отдельным соединением для человека она не
// была. Без вычитания порог пробивался этим шумом на каждом старте у машин без
// IPv6 (ноутбук 07.10: 1023 из 1031 отказа — шум, по IPv4 ни одного).
func bedaZaOkno(soed, otkazy, shumV6 int) bool {
	otkazy -= shumV6
	soed -= shumV6
	return otkazy > 0 && soed > 0 && otkazy*100/soed >= 15
}

// topImen — пять самых частых имён: этого хватает, чтобы увидеть «ломается
// именно такой-то сайт», и сводка не растёт вместе с их числом.
func topImen(imena map[string]int, skolko int) map[string]int {
	if len(imena) <= skolko {
		return imena
	}
	type para struct {
		imya string
		raz  int
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
