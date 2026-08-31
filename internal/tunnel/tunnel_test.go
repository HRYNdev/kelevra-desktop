package tunnel

import (
	"os"
	"testing"
)

// papka изолирует след на диске: тесты не должны трогать %LOCALAPPDATA% того,
// кто их запускает (тот же приём, что у стенда службы).
func papka(t *testing.T) {
	t.Helper()
	t.Setenv("KELEVRA_DIR", t.TempDir())
}

// nikogdaZhivoy/vsegdaZhivoy — подменные проверки адаптера. Настоящий Zhivoy
// ходит в net.Interfaces(); в тесте настоящих сетевых адаптеров быть не должно
// вовсе (запрет трогать сеть на машине человека), поэтому проверка передаётся
// параметром, а не зовётся изнутри.
func nikogdaZhivoy(string) bool { return false }
func vsegdaZhivoy(string) bool  { return true }

func TestSledZapominaetsyaIChitaetsya(t *testing.T) {
	papka(t)
	Otmetit("tun125", 4242)
	adapter, pid, est := ProchestMetku()
	if !est {
		t.Fatal("след только что записали, а его не видно")
	}
	if adapter != "tun125" || pid != 4242 {
		t.Fatalf("след = (%q, %d), ждали (\"tun125\", 4242)", adapter, pid)
	}
}

// Штатный выход снимает след сам — ровно по этому признаку следующий запуск
// отличает «ушли аккуратно» от «умерли жёстко». Если UbratMetku промолчит,
// каждый обычный запуск будет выглядеть аварийным и тревожить человека.
func TestShtatnyyVyhodSnimaetSled(t *testing.T) {
	papka(t)
	Otmetit("tun125", 4242)
	UbratMetku()
	if _, _, est := ProchestMetku(); est {
		t.Fatal("след пережил штатное снятие — следующий запуск сочтёт выход аварийным")
	}
	// Второе снятие подряд (выход после «Отключить», где след уже сняли) не
	// должно быть отказом: OpustitZashchitu зовёт UbratMetku безусловно.
	UbratMetku()
}

// Главный сценарий задачи: аварийное завершение процесса. След на диске
// остался, живой копии нет, адаптер смерть процесса не пережил — система
// вернулась в исходное состояние сама, убирать нечего, кроме самого следа.
func TestAvariynayaSmertAdapterUshyolSSoboy(t *testing.T) {
	papka(t)
	Otmetit("tun125", 4242)

	itog := UbratOsirotevshiy(false, nikogdaZhivoy)
	if !itog.BylSled {
		t.Fatal("след был, а уборка его не увидела — авария осталась бы незамеченной")
	}
	if itog.Adapter != "tun125" {
		t.Fatalf("имя адаптера из следа = %q, ждали \"tun125\"", itog.Adapter)
	}
	if itog.AdapterVisit {
		t.Fatal("адаптера нет, а уборка говорит, что он висит — ложная тревога заставит чинить несломанное")
	}
	if _, _, est := ProchestMetku(); est {
		t.Fatal("след пережил уборку — тревога повторится на каждом следующем запуске")
	}
}

// Тот же сценарий, но вывод разбора не сошёлся: адаптер ПЕРЕЖИЛ процесс.
// Молчать об этом нельзя — это и есть та авария, ради которой след заведён.
// След при этом всё равно снимается: он описывает намерение прошлой копии, а
// не состояние системы, и хранить его дальше значит копить ложную тревогу.
func TestAvariynayaSmertAdapterPerezhilProcess(t *testing.T) {
	papka(t)
	Otmetit("tun125", 4242)

	itog := UbratOsirotevshiy(false, vsegdaZhivoy)
	if !itog.BylSled || !itog.AdapterVisit {
		t.Fatalf("адаптер жив после смерти процесса, а уборка отвечает %+v — человек об этом не узнает", itog)
	}
	if _, _, est := ProchestMetku(); est {
		t.Fatal("след остался на диске — следующий запуск повторит ту же тревогу")
	}
}

// Чужая живая копия держит туннель прямо сейчас. Трогать её след — то же
// самое, что окну снимать системный прокси у живой службы (беда 20.08).
func TestZhivayaKopiyaSvoySledNeOtdayot(t *testing.T) {
	papka(t)
	Otmetit("tun125", 4242)

	itog := UbratOsirotevshiy(true, vsegdaZhivoy)
	if itog.BylSled {
		t.Fatalf("копия жива, а уборка всё равно отчиталась о находке: %+v", itog)
	}
	adapter, _, est := ProchestMetku()
	if !est || adapter != "tun125" {
		t.Fatal("уборка сняла след живой копии — та останется без своей страховки на случай аварии")
	}
}

// Штатный запуск без всякого следа: уборке нечего делать и не о чем говорить.
func TestBezSledaUborkaMolchit(t *testing.T) {
	papka(t)
	itog := UbratOsirotevshiy(false, vsegdaZhivoy)
	if itog.BylSled || itog.AdapterVisit || itog.Adapter != "" {
		t.Fatalf("следа не было, а уборка отвечает %+v", itog)
	}
}

// Битый след (файл есть, JSON не разбирается) не должен ни падать, ни
// притворяться находкой: на диске у человека бывает всё, включая обрезанный
// файл после выключения питания на самой записи.
func TestBityySledNePadaetINeVydumyvaet(t *testing.T) {
	papka(t)
	Otmetit("tun125", 4242)
	if err := os.WriteFile(putMetki(), []byte("{это не json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, est := ProchestMetku(); est {
		t.Fatal("битый след прочитался как настоящий")
	}
	if itog := UbratOsirotevshiy(false, vsegdaZhivoy); itog.BylSled {
		t.Fatalf("битый след стал находкой: %+v", itog)
	}
}

// Zhivoy — боевая проверка. Трогать настоящие адаптеры машины нельзя, но два
// её свойства проверяются безопасно, только чтением: пустое имя — всегда
// «нет» (иначе любой профиль без interface_name дал бы вечную тревогу), и
// заведомо несуществующее имя — тоже «нет».
func TestZhivoyNaPustomIVydumannomImeni(t *testing.T) {
	if Zhivoy("") {
		t.Fatal("пустое имя адаптера сочтено живым адаптером")
	}
	if Zhivoy("kelevra-takogo-adaptera-net-0000") {
		t.Fatal("выдуманное имя адаптера сочтено живым")
	}
}
