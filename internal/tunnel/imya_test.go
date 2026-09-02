package tunnel

import (
	"fmt"
	"testing"
	"time"
)

// Проверки подбора имени сетевого адаптера — та самая беда с машины человека
// 01.09: после «Отключить» имя tun125 осталось занято, следующий запуск не смог
// ни создать адаптер, ни открыть существующий, и ядро упало через 15 секунд.
// Дважды за вечер. Итог человеку: половинная защита вместо полной.
//
// Проверки написаны на СЦЕНАРИЙ, а не на устройство подбора: какое имя
// приложение возьмёт и сколько на это потратит.

// zanyaty — подменная проверка адаптера по списку занятых имён. Настоящих
// сетевых адаптеров в проверках быть не должно вовсе (запрет трогать сеть на
// машине человека), поэтому проверка передаётся параметром.
func zanyaty(imena ...string) Adapter {
	nabor := map[string]bool{}
	for _, i := range imena {
		nabor[i] = true
	}
	return func(imya string) bool { return nabor[imya] }
}

// Обычный запуск: имя из профиля свободно. Он не имеет права ничего стоить —
// иначе подбор, заведённый ради редкой беды, замедлит каждое нажатие.
func TestSvobodnoeImyaBeretsyaSrazuIBezOzhidaniya(t *testing.T) {
	nachalo := time.Now()
	p := SvobodnoeImya("tun125", SrokOsvobozhdeniya, zanyaty())
	proshlo := time.Since(nachalo)

	if p.Imya != "tun125" {
		t.Fatalf("имя свободно, а взято %q — приложение переименовало адаптер на ровном месте", p.Imya)
	}
	if p.Zamena || p.Zhdali != 0 {
		t.Fatalf("свободное имя обошлось в %+v — обычное нажатие стало дороже", p)
	}
	if proshlo > time.Second {
		t.Fatalf("подбор свободного имени занял %s — человек ждёт кнопку зря", proshlo)
	}
	if p.Slovami() != "" {
		t.Fatalf("обычный запуск наговорил в журнал %q", p.Slovami())
	}
}

// Главный сценарий: адаптер прошлой попытки ещё висит, но вот-вот уйдёт.
// Своё имя лучше чужого — ждём его, а не плодим tun126 на каждое нажатие.
func TestZanyatoeImyaOsvobodilosVoVremyaOzhidaniya(t *testing.T) {
	zaprosov := 0
	zhiv := func(imya string) bool {
		zaprosov++
		return zaprosov < 3 // третий вопрос системе застаёт имя свободным
	}

	p := SvobodnoeImya("tun125", 2*time.Second, zhiv)
	if p.Imya != "tun125" {
		t.Fatalf("имя освободилось, а приложение всё равно взяло %q вместо своего", p.Imya)
	}
	if p.Zamena {
		t.Fatal("подмена имени там, где хватило подождать — в системе останется лишний адаптер")
	}
	if p.Zhdali == 0 {
		t.Fatal("имя было занято, а ожидание не отмечено — журнал соврёт, что всё прошло гладко")
	}
}

// Имя так и не освободилось. Сносить чужое сетевое устройство мы не будем
// (необратимое в системе — только по прямой просьбе), а вот взять соседнее
// свободное можно: ядру всё равно, tun125 или tun126.
func TestNeOsvobodilosBeryomSosedneeSvobodnoe(t *testing.T) {
	p := SvobodnoeImya("tun125", 0, zanyaty("tun125"))
	if !p.Vyshlo() {
		t.Fatal("имя занято одно, а свободного не нашлось — полный режим потерян без причины")
	}
	if p.Imya == "tun125" {
		t.Fatal("взято то же занятое имя — ядро упадёт ровно там же, где падало")
	}
	if p.Imya != "tun126" {
		t.Fatalf("взято %q, ждали соседнее tun126: имя должно оставаться узнаваемым", p.Imya)
	}
	if !p.Zamena || p.Ishodnoe != "tun125" {
		t.Fatalf("подмена не отмечена: %+v", p)
	}
	if p.Slovami() == "" {
		t.Fatal("приложение подменило имя адаптера и промолчало в журнал")
	}
}

// Заняты и соседи: дело не в остатке прошлой попытки, а в самой системе.
// Врать про это нельзя — пустое имя означает «полный режим сейчас не поднять»,
// и вызывающий по нему опустится на ступень ниже, назвав человеку причину.
func TestVseImenaZanyatyPodborChestnoSdayotsya(t *testing.T) {
	var vse []string
	for i := 125; i <= 200; i++ {
		vse = append(vse, fmt.Sprintf("tun%d", i))
	}

	p := SvobodnoeImya("tun125", 0, zanyaty(vse...))
	if p.Vyshlo() {
		t.Fatalf("все имена заняты, а подбор выдал %q — ядро упадёт на нём через 15 секунд", p.Imya)
	}
	if len(p.Zanyaty) != zapasnyhImyon+1 {
		t.Fatalf("занятыми названы %v — в журнале должно быть видно, сколько имён перебрали", p.Zanyaty)
	}
	if p.Slovami() == "" {
		t.Fatal("подбор сдался молча — по журналу с машины человека беду не опознать")
	}
}

// Профиль без interface_name: имя выберет само ядро, подбирать нечего.
// Пустое имя тут не беда, а «вопрос не ко мне» — падать и тревожить незачем.
func TestPustoeImyaNeBeda(t *testing.T) {
	p := SvobodnoeImya("", SrokOsvobozhdeniya, zanyaty())
	if p.Imya != "" || p.Ishodnoe != "" || p.Zamena {
		t.Fatalf("на пустом имени подбор что-то выдумал: %+v", p)
	}
	p = SvobodnoeImya("   ", SrokOsvobozhdeniya, zanyaty())
	if p.Imya != "" || p.Zamena {
		t.Fatalf("на пробельном имени подбор что-то выдумал: %+v", p)
	}
}

// Второй случай, ради которого OboytiZanyatoe отделён от SvobodnoeImya: ядро
// сказало «open existing adapter: элемент не найден», то есть в списке
// интерфейсов Windows адаптера уже нет и наша проверка честно ответит
// «свободно». Переспрашивать бессмысленно — на повтор идём с ДРУГИМ именем.
func TestOboytiZanyatoeNikogdaNeVozvrashchaetToZheImya(t *testing.T) {
	imya, _ := OboytiZanyatoe("tun125", zanyaty()) // система говорит: всё свободно
	if imya == "tun125" {
		t.Fatal("повтор пошёл на том же имени, на котором ядро только что упало")
	}
	if imya != "tun126" {
		t.Fatalf("для повтора взято %q, ждали tun126", imya)
	}
}

// Имя без числового хвоста тоже должно уметь подмениться: interface_name в
// профиле пишем не мы, и завтра там может стоять что угодно.
func TestImyaBezNomeraTozhePodbiraetsya(t *testing.T) {
	p := SvobodnoeImya("kelevra", 0, zanyaty("kelevra"))
	if p.Imya != "kelevra1" {
		t.Fatalf("для имени без номера взято %q, ждали kelevra1", p.Imya)
	}
}

// Опознание беды по строке ядра. Ловим по СВОИМ строкам ядра, а не по тексту
// системной ошибки Windows: он переведён на язык системы, и на русской машине
// придёт по-русски.
func TestZanyatoePoOshibkeOtlichaetSvoyuBeduOtChuzhoy(t *testing.T) {
	zanyato := fmt.Errorf("ядро упало при старте: FATAL[0015] start service: start inbound/tun[tun-in]: " +
		"configure tun interface: (create adapter: Cannot create a file when that file already exists. " +
		"| open existing adapter: Element not found.)")
	if !ZanyatoePoOshibke(zanyato) {
		t.Fatal("дословная строка с машины человека не опознана — починка не включится там, где нужна")
	}
	// Та же строка, но система говорит по-русски: беда та же, ответ тот же.
	poRusski := fmt.Errorf("start inbound/tun[tun-in]: configure tun interface: " +
		"(create adapter: Невозможно создать файл, так как он уже существует. | " +
		"open existing adapter: Элемент не найден.)")
	if !ZanyatoePoOshibke(poRusski) {
		t.Fatal("на русской Windows беда не опознаётся — опознание завязано на перевод, а не на строки ядра")
	}

	netPrav := fmt.Errorf("FATAL[0000] start service: initialize inbound/tun[0]: " +
		"configure tun interface: permission denied")
	if ZanyatoePoOshibke(netPrav) {
		t.Fatal("отказ по правам принят за занятое имя — приложение начнёт менять имена вместо просьбы прав")
	}
	if ZanyatoePoOshibke(nil) {
		t.Fatal("nil принят за беду")
	}
}
