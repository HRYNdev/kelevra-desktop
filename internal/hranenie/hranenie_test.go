package hranenie

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// KELEVRA_DIR обязан перекрывать %LOCALAPPDATA% именно на Windows — там, где
// живёт продукт. До 22.08 порядок был обратный, и любой windows-тест,
// поставивший себе t.TempDir(), молча писал в живую папку приложения: настройки
// одного теста доставались другому (стенд краснел на выборе узла), а на машине
// человека прогон тестов затёр бы ему его собственный профиль.
//
// Тест сторожит ПОРЯДОК, поэтому обе переменные ставим одновременно: проверка
// «KELEVRA_DIR работает» без заданного LOCALAPPDATA зеленела бы и на старом
// коде, то есть не проверяла бы ничего.
func TestKelevraDirSilneeLocalAppData(t *testing.T) {
	svoya := t.TempDir()
	chuzhaya := t.TempDir()
	t.Setenv("KELEVRA_DIR", svoya)
	t.Setenv("LOCALAPPDATA", chuzhaya)

	if p := Papka(); p != svoya {
		t.Fatalf("папка %q вместо заданной KELEVRA_DIR %q (LOCALAPPDATA=%q)", p, svoya, chuzhaya)
	}
	// Всё остальное считается от Papka(): если корень уехал, уедет и профиль,
	// и журнал, и конфиг ядра. Щупаем один производный путь, чтобы поймать
	// случай, когда Papka() починили, а кто-то рядом склеивает путь сам.
	if got, want := PutProfilya(), filepath.Join(svoya, "profil.json"); got != want {
		t.Fatalf("профиль лёг в %q, ждали %q", got, want)
	}
}

// Без KELEVRA_DIR на Windows остаётся %LOCALAPPDATA%\Kelevra — это боевой путь,
// и переставленное условие не должно было его сломать.
func TestBezPeremennoyWindowsBeretLocalAppData(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("проверяет ветку Windows")
	}
	chuzhaya := t.TempDir()
	t.Setenv("KELEVRA_DIR", "")
	t.Setenv("LOCALAPPDATA", chuzhaya)

	if got, want := Papka(), filepath.Join(chuzhaya, "Kelevra"); got != want {
		t.Fatalf("папка %q вместо %q", got, want)
	}
}

// TestZagruzitStaryyFayBezAvtorezhimaDayotVyklyucheno: файл настроек, записанный
// до появления поля Avtorezhim, не должен ронять Zagruzit — и авторежим на нём
// обязан читаться выключенным (он сам дёргает VPN пользователя, включать это
// молча за него нельзя).
func TestZagruzitStaryyFaylBezAvtorezhimaDayotVyklyucheno(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())
	staryy := `{"kod":"abc","device_id":"xyz","avtopodklyuch":true,"obnovlyat_min":30}`
	if err := os.MkdirAll(Papka(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(putNastroek(), []byte(staryy), 0o600); err != nil {
		t.Fatal(err)
	}

	n, err := Zagruzit()
	if err != nil {
		t.Fatalf("Zagruzit на старом файле без поля avtorezhim упал: %v", err)
	}
	if n.Avtorezhim {
		t.Fatal("Avtorezhim прочитан включённым на файле, где его вообще не было")
	}
	if n.Kod != "abc" || n.ObnovlyatMin != 30 {
		t.Fatalf("остальные поля старого файла не сохранились: %+v", n)
	}
}

// TestZagruzitStaryyFaylBezPravaZaprosheny — критичный риск миграции: у
// человека, поставившего приложение ЛЮБОЙ версией до появления автозапроса
// прав, файл настроек уже существует, а поля prava_zaprosheny в нём нет.
// Считаем это «уже спрашивали» — иначе первый же коннект на новой версии
// выкинет незваный UAC-попап, который выглядит как malware (см. диагноз).
func TestZagruzitStaryyFaylBezPravaZaprosheny(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())
	staryy := `{"kod":"abc","device_id":"xyz","avtopodklyuch":true,"obnovlyat_min":30}`
	if err := os.MkdirAll(Papka(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(putNastroek(), []byte(staryy), 0o600); err != nil {
		t.Fatal(err)
	}

	n, err := Zagruzit()
	if err != nil {
		t.Fatalf("Zagruzit на старом файле без поля prava_zaprosheny упал: %v", err)
	}
	if !n.UzheSprosiliPrava() {
		t.Fatal("старый файл без prava_zaprosheny обязан читаться как «уже спрашивали» — иначе незваный UAC-попап на существующей установке")
	}
}

// TestZagruzitBezFaylaDayotEshcheNeSprosheno — противоположный случай: файла
// нет вовсе, значит инсталл подлинно чистый, и права ПОКА не спрашивали —
// первое успешное подключение обязано спросить их само.
func TestZagruzitBezFaylaDayotEshcheNeSprosheno(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())

	n, err := Zagruzit()
	if err != nil {
		t.Fatalf("Zagruzit на чистом инсталле упал: %v", err)
	}
	if n.UzheSprosiliPrava() {
		t.Fatal("на чистом инсталле (файла не было) UzheSprosiliPrava не должен быть true — иначе автозапрос прав никогда не сработает")
	}
}

// Общая папка нужна службе Windows: её собственный профиль человеку не виден,
// а окну из чужого сеанса недоступен. Как только общая папка появилась, в неё
// обязаны смотреть ОБА процесса — иначе служба поднимет ядро по одному
// конфигу, а окно покажет другой.
func TestObshchayaPapkaPeretyagivaetOba(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("общая папка есть только в Windows")
	}
	obshchaya := t.TempDir()
	t.Setenv("KELEVRA_DIR", "")
	t.Setenv("PROGRAMDATA", obshchaya)
	t.Setenv("LOCALAPPDATA", t.TempDir())

	// Пока общей папки нет — работаем по-старому, в профиле человека.
	if p := Papka(); p == filepath.Join(obshchaya, "Kelevra") {
		t.Fatalf("общей папки ещё нет, а Papka() уже показывает на неё: %q", p)
	}

	// Появилась — оба процесса обязаны переехать.
	svoya := filepath.Join(obshchaya, "Kelevra")
	if err := os.MkdirAll(svoya, 0o755); err != nil {
		t.Fatalf("не создать общую папку: %v", err)
	}
	if p := Papka(); p != svoya {
		t.Fatalf("Papka() = %q, хочу общую %q", p, svoya)
	}
}

// KELEVRA_DIR старше всего: на нём стоят стенд и тесты, и общая папка не имеет
// права его перебить — иначе прогон тестов пишет в живые данные машины.
func TestPeremennayaStarsheObshchey(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("общая папка есть только в Windows")
	}
	svoya := t.TempDir()
	obshchaya := t.TempDir()
	if err := os.MkdirAll(filepath.Join(obshchaya, "Kelevra"), 0o755); err != nil {
		t.Fatalf("не создать общую папку: %v", err)
	}
	t.Setenv("PROGRAMDATA", obshchaya)
	t.Setenv("KELEVRA_DIR", svoya)
	if p := Papka(); p != svoya {
		t.Fatalf("Papka() = %q, а KELEVRA_DIR = %q — переменную перебили", p, svoya)
	}
}
