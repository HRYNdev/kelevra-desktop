package zhurnaly

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// polozhit кладёт файл с заданным содержимым и временем изменения.
func polozhit(t *testing.T, put, soderzhimoe string, kogda time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(put, []byte(soderzhimoe), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(put, kogda, kogda); err != nil {
		t.Fatal(err)
	}
}

// raspakovat разжимает тело посылки. Оно обязано быть ОДИНОЧНЫМ gzip-потоком:
// коллектор на той стороне не умеет ни tar, ни zip, ни склейку потоков.
func raspakovat(t *testing.T, telo []byte) string {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(telo))
	if err != nil {
		t.Fatalf("тело — не gzip: %v", err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("gzip не дочитался: %v", err)
	}
	return string(b)
}

// server — подставной коллектор: запоминает последнюю посылку и отвечает так,
// как договорено ({"ok":true,"bytes":N}).
type server struct {
	*httptest.Server
	telo      []byte
	zagolovki http.Header
	kod       int
	otvet     string
}

func podnyatServer(t *testing.T) *server {
	t.Helper()
	s := &server{kod: http.StatusOK}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.telo = b
		s.zagolovki = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.kod)
		if s.otvet != "" {
			_, _ = io.WriteString(w, s.otvet)
			return
		}
		_, _ = fmt.Fprintf(w, `{"ok":true,"bytes":%d}`, len(b))
	}))
	t.Cleanup(s.Close)
	return s
}

func otpravshchik(t *testing.T, s *server, papka string, puti []string) *Otpravshchik {
	t.Helper()
	return &Otpravshchik{
		Adres:    s.URL + "/logs",
		DeviceID: "0123456789abcdef",
		Versiya:  "0.6.30",
		Puti:     puti,
		PutMetok: filepath.Join(papka, "otpravka_zhurnalov.json"),
		Zagolovki: func(h http.Header, id, v string) {
			h.Set("X-Device-Id", id)
			h.Set("X-Device-Model", "ASUSTeK TUF GAMING B550-PLUS")
			h.Set("X-Device-Platform", "Windows 10 Pro 22H2 (19045)")
			h.Set("X-App-Version", v)
		},
	}
}

// Формат тела — договор с коллектором, а не наше внутреннее дело: один
// gzip-поток, куски подряд, перед каждым дословный разделитель.
func TestTeloOdinGzipSRazdelitelyami(t *testing.T) {
	papka := t.TempDir()
	zhurnal := filepath.Join(papka, "kelevra.log")
	polozhit(t, zhurnal, "старая запись\n", time.Now().Add(-2*time.Hour))
	polozhit(t, zhurnal+".proshlyy", "совсем старая\n", time.Now().Add(-5*time.Hour))

	m := &Metki{Fayly: map[string]Metka{}}
	telo, kuski, err := Upakovat(Razobrat([]string{zhurnal, zhurnal + ".proshlyy"}, m), Potolok)
	if err != nil {
		t.Fatal(err)
	}
	if len(kuski) != 2 {
		t.Fatalf("кусков %d, ждали 2", len(kuski))
	}
	raspakovano := raspakovat(t, telo)
	for _, k := range kuski {
		zhdem := fmt.Sprintf(Razdelitel, k.Imya, k.Smeshchenie, k.Bayt)
		if !strings.Contains(raspakovano, zhdem) {
			t.Errorf("в потоке нет разделителя %q\n--- поток ---\n%s", zhdem, raspakovano)
		}
	}
	if !strings.Contains(raspakovano, "старая запись") || !strings.Contains(raspakovano, "совсем старая") {
		t.Errorf("в потоке нет содержимого файлов:\n%s", raspakovano)
	}
	// Порядок — по времени вперёд: склеенный журнал читается сверху вниз так
	// же, как он писался.
	if strings.Index(raspakovano, "совсем старая") > strings.Index(raspakovano, "старая запись") {
		t.Errorf("куски в потоке идут не по времени:\n%s", raspakovano)
	}
}

// Дважды одно и то же не уходит — ни при повторной отправке, ни после
// ротации, когда те же байты лежат уже под другим именем.
func TestOdnoITozheNeUhoditDvazhdy(t *testing.T) {
	papka := t.TempDir()
	zhurnal := filepath.Join(papka, "kelevra.log")
	s := podnyatServer(t)
	o := otpravshchik(t, s, papka, []string{zhurnal, zhurnal + ".proshlyy"})

	polozhit(t, zhurnal, "первый день\n", time.Now().Add(-24*time.Hour))
	otchet, err := o.Otpravit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if otchet.SyrykhBayt == 0 {
		t.Fatal("первая отправка ушла пустой")
	}

	// Второй заход подряд: нового нет — посылки быть не должно.
	s.telo = nil
	otchet, err = o.Otpravit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(otchet.Kuski) != 0 || s.telo != nil {
		t.Fatalf("отправили то же самое второй раз: %+v", otchet)
	}

	// Журнал дорос — уходит ТОЛЬКО хвост.
	polozhit(t, zhurnal, "первый день\nвторой день\n", time.Now())
	otchet, err = o.Otpravit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raspakovano := raspakovat(t, s.telo)
	if strings.Contains(raspakovano, "первый день") {
		t.Errorf("хвост поехал вместе с уже отправленным началом:\n%s", raspakovano)
	}
	if !strings.Contains(raspakovano, "второй день") {
		t.Errorf("хвоста нет в посылке:\n%s", raspakovano)
	}

	// Ротация: тот же файл переехал в .proshlyy, на его месте новый журнал.
	// Ни один байт из .proshlyy не должен уехать повторно, а НАЧАЛО нового
	// журнала — обязано.
	st, err := os.Stat(zhurnal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(zhurnal, zhurnal+".proshlyy"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(zhurnal+".proshlyy", st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	polozhit(t, zhurnal, "третий день после ротации\n", time.Now().Add(time.Minute))
	s.telo = nil
	if _, err := o.Otpravit(context.Background()); err != nil {
		t.Fatal(err)
	}
	raspakovano = raspakovat(t, s.telo)
	if strings.Contains(raspakovano, "первый день") || strings.Contains(raspakovano, "второй день") {
		t.Errorf("после ротации уехало уже отправленное:\n%s", raspakovano)
	}
	if !strings.Contains(raspakovano, "третий день после ротации") {
		t.Errorf("после ротации начало нового журнала потерялось:\n%s", raspakovano)
	}
}

// Потолок: больше 25 МБ сервер не примет (413), поэтому режем сами — и режем
// по САМЫМ СВЕЖИМ логам, а не по первым попавшимся.
func TestPotolokRezhetPoSvezhim(t *testing.T) {
	papka := t.TempDir()
	staryy := filepath.Join(papka, "kelevra.log.proshlyy")
	svezhiy := filepath.Join(papka, "kelevra.log")
	polozhit(t, staryy, strings.Repeat("s", 1000), time.Now().Add(-10*time.Hour))
	polozhit(t, svezhiy, strings.Repeat("n", 1000), time.Now())

	m := &Metki{Fayly: map[string]Metka{}}
	telo, kuski, err := Upakovat(Razobrat([]string{svezhiy, staryy}, m), 600)
	if err != nil {
		t.Fatal(err)
	}
	var vsego int64
	for _, k := range kuski {
		vsego += k.Bayt
	}
	if vsego > 600 {
		t.Errorf("влезло %d байт при потолке 600", vsego)
	}
	raspakovano := raspakovat(t, telo)
	if !strings.Contains(raspakovano, "nnn") {
		t.Errorf("свежий журнал не попал в посылку вовсе:\n%.200s", raspakovano)
	}
	// Обрезанный кусок берётся ХВОСТОМ: смещение уезжает к концу файла.
	for _, k := range kuski {
		if k.Put == svezhiy && k.Bayt < 1000 && k.Smeshchenie+k.Bayt != k.Razmer {
			t.Errorf("обрезали не хвост: смещение %d + %d != %d", k.Smeshchenie, k.Bayt, k.Razmer)
		}
	}
}

// Отказ сервера — это не «ушло». Отметки обязаны остаться нетронутыми, иначе
// те же байты потеряются молча.
func TestOtkazServeraNeSchitaetsyaOtpravkoy(t *testing.T) {
	for _, sluchay := range []struct {
		kod   int
		otvet string
		slovo string
	}{
		{http.StatusRequestEntityTooLarge, `{"ok":false,"error":"too big"}`, "413"},
		{http.StatusInsufficientStorage, `{"ok":false,"error":"no space"}`, "507"},
		{http.StatusInternalServerError, `{"ok":false,"error":"boom"}`, "500"},
	} {
		papka := t.TempDir()
		zhurnal := filepath.Join(papka, "kelevra.log")
		polozhit(t, zhurnal, "запись\n", time.Now())
		s := podnyatServer(t)
		s.kod, s.otvet = sluchay.kod, sluchay.otvet
		o := otpravshchik(t, s, papka, []string{zhurnal})
		if _, err := o.Otpravit(context.Background()); err == nil {
			t.Fatalf("%d: отказ сервера принят за успех", sluchay.kod)
		} else if !strings.Contains(err.Error(), sluchay.slovo) {
			t.Errorf("%d: в ошибке нет кода: %v", sluchay.kod, err)
		}
		if _, err := os.Stat(o.PutMetok); err == nil {
			t.Errorf("%d: отметки записаны, хотя ничего не ушло", sluchay.kod)
		}
	}
}

// Заголовки устройства и тип тела — то, по чему коллектор понимает, чья это
// посылка. Без них она безымянная.
func TestPosylkaPodpisanaUstroystvom(t *testing.T) {
	papka := t.TempDir()
	zhurnal := filepath.Join(papka, "kelevra.log")
	polozhit(t, zhurnal, "запись\n", time.Now())
	s := podnyatServer(t)
	o := otpravshchik(t, s, papka, []string{zhurnal})
	if _, err := o.Otpravit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.zagolovki.Get("Content-Type"); got != TipTela {
		t.Errorf("Content-Type = %q, ждали %q", got, TipTela)
	}
	for _, z := range []string{"X-Device-Id", "X-Device-Model", "X-Device-Platform", "X-App-Version"} {
		if s.zagolovki.Get(z) == "" {
			t.Errorf("заголовок %s не доехал", z)
		}
	}
	if u, _ := url.Parse(o.Adres); u.Path != "/logs" {
		t.Errorf("шлём не на /logs, а на %q", u.Path)
	}
}

// Пустое не шлём вовсе: ни пустой файл, ни отсутствующий не повод дёргать
// сервер.
func TestPustoeNeShlyom(t *testing.T) {
	papka := t.TempDir()
	pustoy := filepath.Join(papka, "kelevra.log")
	polozhit(t, pustoy, "", time.Now())
	s := podnyatServer(t)
	o := otpravshchik(t, s, papka, []string{pustoy, filepath.Join(papka, "net-takogo.log")})
	otchet, err := o.Otpravit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(otchet.Kuski) != 0 || s.telo != nil {
		t.Fatalf("отправили пустоту: %+v", otchet)
	}
}

// Istochniki обязаны накрывать и запасной путь (%TEMP%\Kelevra): именно туда
// уезжает журнал, когда своя папка недоступна — то есть ровно в том случае,
// который и надо разбирать.
func TestIstochnikiNakryvayutZapasnoyPut(t *testing.T) {
	puti := Istochniki(`C:\Users\x\AppData\Local\Kelevra\kelevra.log`, `C:\Temp\Kelevra`)
	if len(puti) != 4 {
		t.Fatalf("путей %d, ждали 4: %v", len(puti), puti)
	}
	var proshlyh, zapasnyh int
	for _, p := range puti {
		if strings.HasSuffix(p, ".proshlyy") {
			proshlyh++
		}
		if strings.Contains(p, "Temp") {
			zapasnyh++
		}
	}
	if proshlyh != 2 || zapasnyh != 2 {
		t.Errorf("ротаций %d, запасных %d в %v", proshlyh, zapasnyh, puti)
	}
}
