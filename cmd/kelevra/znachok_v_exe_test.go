package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

// Значок САМОГО .exe (тот, что видно в Проводнике, на панели задач и на
// закреплённой кнопке) берётся не из go:embed znachok.ico, а из PE-ресурса —
// COFF-объекта cmd/kelevra/znachok_windows.syso, который go build линкует
// молча, просто потому что файл лежит в пакете main.
//
// Беда, которую этот тест ловит. Файлов ДВА, а рисуется значок ОДНИМ
// (cmd/kelevra/znachok_iz_kanona.py переписывает только znachok.ico), и
// пересборка .syso делается руками внешним инструментом (см. «пересборка
// .syso» в stend/znachok_exe.sh). Забыть её нечем не наказывается: сборка
// проходит, стенд znachok_exe.sh тоже зелен — он проверяет, что ресурс ЕСТЬ,
// а не что он СВЕЖИЙ. Ровно так и вышло: 30.08 значок перерисовали дважды
// (круглая подложка внутри кольца, потом хинтинг тонкой линии), .syso не
// тронули ни разу, и в Проводнике с панелью задач у человека остался значок
// двух поколений давности — при том что трей и заголовок окна показывали
// свежий (они читают znachok.ico напрямую). хозяин про это писал дважды:
// 23.08 «сама аватарка приложения на панеле задач не сделана», 30.08 «не
// хочешь иконку нормально круглой сделать, че это за квадрат».
//
// Проверка простая и не зависит от инструмента, которым собран .syso: каждый
// образ из .ico обязан лежать в .syso байт в байт. RT_ICON хранит ровно те же
// байты, что и запись .ico (спецификация PE/COFF), поэтому поиск подстрокой
// честен и не требует разбора COFF.
func TestZnachokVExeSvezhiy(t *testing.T) {
	ico, err := os.ReadFile("znachok.ico")
	if err != nil {
		t.Fatalf("не прочитал znachok.ico: %v", err)
	}
	syso, err := os.ReadFile("znachok_windows.syso")
	if err != nil {
		t.Fatalf("не прочитал znachok_windows.syso: %v", err)
	}

	obrazy, err := razobratKatalogIco(ico)
	if err != nil {
		t.Fatalf("znachok.ico не разобрать: %v", err)
	}
	if len(obrazy) == 0 {
		t.Fatal("в znachok.ico нет ни одного образа")
	}

	for _, o := range obrazy {
		if !bytes.Contains(syso, o.dannye) {
			t.Errorf("образ %dx%d (%d байт) из znachok.ico не найден в znachok_windows.syso — "+
				"значок .exe отстал от значка трея. Пересобрать: "+
				"go run github.com/akavel/rsrc@latest -arch amd64 "+
				"-ico cmd/kelevra/znachok.ico -o cmd/kelevra/znachok_windows.syso",
				o.shirina, o.vysota, len(o.dannye))
		}
	}
}

// zapisKatalogaIco — одна запись каталога .ico вместе со своими байтами.
type zapisKatalogaIco struct {
	shirina, vysota int
	dannye          []byte
}

// razobratKatalogIco читает каталог .ico: заголовок ICONDIR (6 байт) и по 16 байт на
// ICONDIRENTRY. Нулевая ширина/высота в формате означает 256 — так .ico
// умещает крупный размер в один байт.
func razobratKatalogIco(b []byte) ([]zapisKatalogaIco, error) {
	if len(b) < 6 {
		return nil, errKatalogIcoOborvan
	}
	if binary.LittleEndian.Uint16(b[2:]) != 1 {
		return nil, errNeIcoVovse
	}
	kolichestvo := int(binary.LittleEndian.Uint16(b[4:]))
	if len(b) < 6+16*kolichestvo {
		return nil, errKatalogIcoOborvan
	}
	var obrazy []zapisKatalogaIco
	for i := 0; i < kolichestvo; i++ {
		z := b[6+16*i:]
		shirina, vysota := int(z[0]), int(z[1])
		if shirina == 0 {
			shirina = 256
		}
		if vysota == 0 {
			vysota = 256
		}
		razmer := int(binary.LittleEndian.Uint32(z[8:]))
		nachalo := int(binary.LittleEndian.Uint32(z[12:]))
		if nachalo < 0 || razmer <= 0 || nachalo+razmer > len(b) {
			return nil, errKatalogIcoOborvan
		}
		obrazy = append(obrazy, zapisKatalogaIco{shirina, vysota, b[nachalo : nachalo+razmer]})
	}
	return obrazy, nil
}

var (
	errKatalogIcoOborvan = oshibkaKatalogaIco("файл .ico обрывается посреди каталога образов")
	errNeIcoVovse        = oshibkaKatalogaIco("это не .ico: в заголовке не тот тип")
)

type oshibkaKatalogaIco string

func (o oshibkaKatalogaIco) Error() string { return string(o) }
