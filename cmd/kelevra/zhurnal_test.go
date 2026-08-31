package main

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Сторожа журнала приложения.
//
// Беда 31.08, приборно: %LOCALAPPDATA%\Kelevra\kelevra.log = РОВНО НОЛЬ БАЙТ
// во всех трёх прогонах стенда (с правами, без прав, после аварийного
// выключения) и на машине человека тоже. То есть перехвата вывода в файл не
// было вовсе: суточной отправке журналов (internal/zhurnaly) нечего слать,
// сообщениям об уборке следа туннеля некуда писаться, разбирать аварии нечем.
//
// Причина — io.MultiWriter(os.Stderr, f) в otkrytZhurnal: MultiWriter встаёт
// на первой ошибке и до второго писателя не идёт, а у оконной сборки
// (-H=windowsgui), запущенной проводником, ярлыком, автозапуском или UAC,
// стандартных потоков нет вовсе — os.Stderr.Fd() = 0 и «The handle is
// invalid» на первой же строке. Подробности и замер — в шапке zhurnal.go.

// mertvyyPisatel — stderr, которого нет: ровно так ведёт себя os.Stderr у
// оконного процесса без консоли.
type mertvyyPisatel struct{ zvali int }

func (m *mertvyyPisatel) Write(b []byte) (int, error) {
	m.zvali++
	return 0, errors.New("write /dev/stderr: The handle is invalid.")
}

// vernutZhurnalNaMesto — log глобален на процесс, и тест обязан вернуть его
// как было, иначе следующий тест пакета будет писать в чужой временный файл.
func vernutZhurnalNaMesto(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
		log.SetPrefix("")
	})
}

// TestZhurnalPishetKogdaStderrMertv — сама починка: отказ stderr не имеет
// права отнять у нас файл.
func TestZhurnalPishetKogdaStderrMertv(t *testing.T) {
	put := filepath.Join(t.TempDir(), "kelevra.log")
	f, err := os.OpenFile(put, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	mertvyy := &mertvyyPisatel{}
	p := &pisatelZhurnala{fayl: f, ekho: mertvyy}
	if _, err := p.Write([]byte("строка при мёртвом stderr\n")); err != nil {
		t.Fatalf("запись в файл вернула ошибку: %v", err)
	}
	// Вторая строка: мёртвый stderr больше не дёргается — иначе каждая
	// строка журнала стоила бы отказавшего системного вызова.
	if _, err := p.Write([]byte("вторая строка\n")); err != nil {
		t.Fatal(err)
	}
	if mertvyy.zvali != 1 {
		t.Fatalf("в мёртвый stderr стучались %d раз, а надо один раз и больше не пробовать", mertvyy.zvali)
	}
	b, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "строка при мёртвом stderr") || !strings.Contains(string(b), "вторая строка") {
		t.Fatalf("в журнале не обе строки: %q", b)
	}
}

// TestStaryyMultiWriterTeryalZhurnal — сторож самого диагноза. Если однажды
// кто-то вернёт io.MultiWriter «чтобы и в консоль тоже», этот тест напомнит,
// чем это кончилось: файл 0 байт и разбирать аварию нечем.
func TestStaryyMultiWriterTeryalZhurnal(t *testing.T) {
	put := filepath.Join(t.TempDir(), "kelevra.log")
	f, err := os.OpenFile(put, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, _ = io.MultiWriter(&mertvyyPisatel{}, f).Write([]byte("это не доедет\n"))
	st, err := os.Stat(put)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 0 {
		t.Fatalf("io.MultiWriter вдруг дописал %d байт — диагноз 31.08 больше не воспроизводится, "+
			"проверь, не изменилось ли поведение io.MultiWriter", st.Size())
	}
}

// TestZhurnalDerzhitSobytiyaOboihProcessov — окно и служба это РАЗНЫЕ процессы
// одного .exe, и в журнале обязаны быть события обоих, различимые глазом.
func TestZhurnalDerzhitSobytiyaOboihProcessov(t *testing.T) {
	vernutZhurnalNaMesto(t)
	papka := t.TempDir()

	put, zakryt := otkrytZhurnalRolyu(papka, "окно")
	if put == "" {
		t.Fatal("журнал не открылся")
	}
	log.Printf("окно поднимает службу")
	zakryt()

	put2, zakryt2 := otkrytZhurnalRolyu(papka, "служба")
	if put2 != put {
		t.Fatalf("служба пишет в %q, а окно писало в %q — это разные файлы", put2, put)
	}
	log.Printf("служба слушает")
	zakryt2()

	b, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	tekst := string(b)
	for _, nado := range []string{"окно поднимает службу", "служба слушает", "[окно ", "[служба "} {
		if !strings.Contains(tekst, nado) {
			t.Fatalf("в журнале нет %q:\n%s", nado, tekst)
		}
	}
}

// TestPovtornyyStartNeObnulyaetZhurnal — накопленное за прошлые запуски это и
// есть история, ради которой журнал заведён.
func TestPovtornyyStartNeObnulyaetZhurnal(t *testing.T) {
	vernutZhurnalNaMesto(t)
	papka := t.TempDir()

	put, zakryt := otkrytZhurnalRolyu(papka, "окно")
	log.Printf("первый запуск")
	zakryt()
	do, err := os.Stat(put)
	if err != nil {
		t.Fatal(err)
	}
	if do.Size() == 0 {
		t.Fatal("после первого запуска журнал пуст — ровно беда 31.08")
	}

	_, zakryt2 := otkrytZhurnalRolyu(papka, "окно")
	log.Printf("второй запуск")
	zakryt2()

	b, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "первый запуск") {
		t.Fatalf("второй запуск затёр первый:\n%s", b)
	}
	if !strings.Contains(string(b), "второй запуск") {
		t.Fatalf("второго запуска в журнале нет:\n%s", b)
	}
	if posle, _ := os.Stat(put); posle.Size() <= do.Size() {
		t.Fatalf("журнал не вырос: было %d, стало %d", do.Size(), posle.Size())
	}
}

// TestRolProcessaSovpadaetSRezhimomSluzhby — пометка строки обязана значить то
// же самое, что и развилка в main() (rezhimSluzhby). Разъедутся — и в журнале
// будет написано «окно» там, где на самом деле служба, а по такому следу
// аварию не разобрать.
func TestRolProcessaSovpadaetSRezhimomSluzhby(t *testing.T) {
	t.Setenv("KELEVRA_BEZ_OKNA", "1")
	if rol := rolProcessa(); rol != "служба" {
		t.Fatalf("при KELEVRA_BEZ_OKNA=1 роль %q, а main() в этом случае идёт в zapustitSluzhbu", rol)
	}
	t.Setenv("KELEVRA_BEZ_OKNA", "")
	if rol := rolProcessa(); rol != "окно" {
		t.Fatalf("без KELEVRA_BEZ_OKNA и без --sluzhba роль %q, а это обычный запуск окна", rol)
	}
}
