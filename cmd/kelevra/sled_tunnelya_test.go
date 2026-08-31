package main

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/tunnel"
)

// Уборка следа туннеля на холодном старте — рядом со snyatOsirotevshiySled,
// который чинит ту же аварию для системного прокси (31.08: прошлый запуск
// умер жёстко, запись в реестре осталась, у человека пропал интернет).
//
// Разница в вопросе. У прокси надо СНЯТЬ то, что осталось. У туннеля снимать
// нечего: адаптер wintun принадлежит процессу ядра и уничтожается драйвером
// вместе с ним, а маршруты auto_route прописаны на LUID этого адаптера —
// Windows снимает их вместе с интерфейсом. Значит задача другая: УБЕДИТЬСЯ,
// что так и есть, и не молчать, если разбор не сошёлся.

// TestUborkaSledaTunnelyaPosleAvarii — след прошлой копии есть, живой копии
// нет, адаптера с таким именем в системе нет. След обязан уйти: он описывает
// намерение прошлой копии, а не состояние системы, и хранить его дальше
// значит тревожить человека на каждом следующем запуске.
//
// Имя адаптера заведомо несуществующее: настоящих сетевых адаптеров тест
// трогать не должен вовсе (запрет менять сеть на машине человека), а
// net.Interfaces() под ним — чтение и ничего больше.
func TestUborkaSledaTunnelyaPosleAvarii(t *testing.T) {
	papka := t.TempDir()
	t.Setenv("KELEVRA_DIR", papka)

	tunnel.Otmetit("kelevra-takogo-adaptera-net-0000", 4242)
	snyatOsirotevshiySledTunnelya(papka)

	if _, _, est := tunnel.ProchestMetku(); est {
		t.Fatal("след прошлой (мёртвой) копии пережил уборку — тревога повторится на каждом запуске")
	}
}

// Следа нет — обычный холодный старт. Уборка обязана промолчать и ничего не
// создать: лишний файл в папке данных человека тут не нужен.
func TestUborkaSledaTunnelyaBezSledaMolchit(t *testing.T) {
	papka := t.TempDir()
	t.Setenv("KELEVRA_DIR", papka)

	snyatOsirotevshiySledTunnelya(papka)

	if _, _, est := tunnel.ProchestMetku(); est {
		t.Fatal("следа не было, а после уборки он появился")
	}
}

// Адаптер ПЕРЕЖИЛ смерть процесса. Разбор ядра обещает, что так не бывает
// (шапка internal/tunnel), но разбор — это чтение чужого кода, а не замер.
// Приложение не имеет права притвориться, что всё чисто: оно обязано сказать
// вслух в журнал — журнал уезжает разработчику суточной отправкой, и это
// единственный способ узнать правду с машины человека.
//
// Проверяем не текст строки, а сам факт, что про живой адаптер сказано и что
// сказано ИНАЧЕ, чем про чистый уход: молчание тут и есть беда.
func TestUborkaSledaTunnelyaGovoritProZhivoyAdapter(t *testing.T) {
	papka := t.TempDir()
	t.Setenv("KELEVRA_DIR", papka)

	imya := "kelevra-zhivoy-adapter-0000"
	tunnel.Otmetit(imya, 4242)

	skazano := perehvatZhurnala(t, func() {
		uborkaSledaTunnelya(papka, func(string) bool { return true })
	})

	if !strings.Contains(skazano, imya) {
		t.Fatalf("про живой адаптер в журнале ни слова (%q) — беду увидит только человек, и то не сразу", skazano)
	}
	if !strings.Contains(skazano, "маршрутизацию") && !strings.Contains(skazano, "ЖИВ") {
		t.Fatalf("сказано так, будто всё чисто: %q", skazano)
	}
	if _, _, est := tunnel.ProchestMetku(); est {
		t.Fatal("след остался: он описывает намерение мёртвой копии, а не систему, и будет тревожить каждый запуск")
	}
}

// Уборка после аварии обязана попасть в журнал и в обычном случае тоже:
// прогон 31.08 нашёл ровно обратное — файл остался лежать, «и в журнал об
// этом ничего не написано».
func TestUborkaSledaTunnelyaPishetVZhurnal(t *testing.T) {
	papka := t.TempDir()
	t.Setenv("KELEVRA_DIR", papka)
	tunnel.Otmetit("kelevra-takogo-adaptera-net-0000", 1288)

	skazano := perehvatZhurnala(t, func() { snyatOsirotevshiySledTunnelya(papka) })

	if !strings.Contains(skazano, "kelevra-takogo-adaptera-net-0000") {
		t.Fatalf("уборка прошла молча: %q", skazano)
	}
}

// Холодный старт СЛУЖЕБНОГО режима (--sluzhba, он же KELEVRA_BEZ_OKNA=1)
// обязан прибрать осиротевший след.
//
// Это и есть находка прогона 31.08: подняли туннель, сняли питание — файл
// tunnel.json остался лежать и следующим запуском убран не был, просто
// перезаписался новым pid при следующем подключении. Причина: уборка стояла
// НИЖЕ развилки на служебный режим, а туннель поднимает и след пишет именно
// служба — до уборки она не доходила никогда.
//
// Проверяем порядок в самом main(): поднять весь запуск целиком в проверке
// нельзя (он поднимает ядро, окно и слушает порт), а порядок этих двух строк
// — ровно то, что сломалось, и ровно то, что молча сломается снова.
func TestUborkaSledaTunnelyaStoitDoRazvilkiSluzhby(t *testing.T) {
	ishodnik, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("не прочитал main.go: %v", err)
	}
	tekst := string(ishodnik)
	uborka := strings.Index(tekst, "\n\tsnyatOsirotevshiySledTunnelya(papka)")
	razvilka := strings.Index(tekst, "\n\tif rezhimSluzhby {")
	if uborka < 0 || razvilka < 0 {
		t.Fatalf("не нашёл в main.go ни уборку (%d), ни развилку служебного режима (%d)", uborka, razvilka)
	}
	if uborka > razvilka {
		t.Fatal("уборка следа туннеля стоит ПОСЛЕ развилки служебного режима — " +
			"служба (а туннель поднимает именно она) до уборки не дойдёт никогда")
	}
}

// perehvatZhurnala ловит то, что код сказал через log, — журнал приложения и
// есть тот единственный способ узнать правду с машины человека.
func perehvatZhurnala(t *testing.T, delo func()) string {
	t.Helper()
	var buf bytes.Buffer
	preg := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(preg)
	delo()
	return buf.String()
}
