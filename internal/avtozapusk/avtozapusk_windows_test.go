//go:build windows

package avtozapusk

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

// Куст, внутри которого лежит ветка автозагрузки. Есть на любой Windows.
const kustVetki = `Software\Microsoft\Windows\CurrentVersion`

// Настоящую ветку автозагрузки тест только ЧИТАЕТ: недоступная ветка — НЕ
// особенность стенда, а ровно та беда, ради которой тест и написан
// (автозапуск молча перестаёт работать у человека). Пропуск оправдан только
// тогда, когда нет самого куста CurrentVersion — машина вообще не Windows.
//
// Пишет тест в свою ветку (vetka уводится в Software\KelevraProverka\…):
// до 22.10 он писал в настоящий HKCU\…\Run и возвращал запись после, и
// прогон на машине человека на время (а при падении посреди — насовсем)
// портил ему автозапуск. Своя ветка убирается за собой.
func vernutPosle(t *testing.T) {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, vetka, registry.QUERY_VALUE)
	if err != nil {
		if _, kustErr := registry.OpenKey(registry.CURRENT_USER, kustVetki, registry.QUERY_VALUE); kustErr != nil {
			t.Skipf("нет куста %s, машина не похожа на Windows: %v", kustVetki, kustErr)
		}
		// На раннере GitHub профиль пользователя искусственный: куст есть, а
		// ветки автозагрузки в нём нет — там никто никогда ничего не
		// запускал. У ЖИВОГО человека она есть всегда, и её отсутствие —
		// настоящая беда, поэтому вне CI проверка по-прежнему падает.
		if os.Getenv("GITHUB_ACTIONS") != "true" {
			t.Fatalf("куст %s на месте, а ветка автозагрузки %s недоступна (%v) — автозапуск у человека работать не будет", kustVetki, vetka, err)
		}
	} else {
		k.Close()
	}

	nastoyashchaya, bylaSluzhba := vetka, sluzhbaEst
	svoya := fmt.Sprintf(`%s\%d`, kornevayaProverki, time.Now().UnixNano())
	vetka = svoya + `\Run`
	sluzhbaEst = func() bool { return false }
	if kw, _, err := registry.CreateKey(registry.CURRENT_USER, vetka, registry.SET_VALUE); err != nil {
		t.Fatalf("своя ветка проверки не заводится: %v", err)
	} else {
		kw.Close()
	}
	t.Cleanup(func() {
		vetka, sluzhbaEst = nastoyashchaya, bylaSluzhba
		_ = registry.DeleteKey(registry.CURRENT_USER, svoya+`\Run`)
		_ = registry.DeleteKey(registry.CURRENT_USER, svoya)
		_ = registry.DeleteKey(registry.CURRENT_USER, kornevayaProverki) // пустая — уйдёт, нет — останется
	})
}

// kornevayaProverki — ветка, под которой проверки заводят свою «автозагрузку».
const kornevayaProverki = `Software\KelevraProverka`

func TestVklyuchitVyklyuchit(t *testing.T) {
	vernutPosle(t)

	if err := Vyklyuchit(); err != nil {
		t.Fatalf("выключить на чистом месте: %v", err)
	}
	est, err := Vklyuchen()
	if err != nil {
		t.Fatalf("проверить после выключения: %v", err)
	}
	if est {
		t.Fatalf("после выключения автозапуск считает себя включённым")
	}

	if err := Vklyuchit(); err != nil {
		t.Fatalf("включить: %v", err)
	}
	est, err = Vklyuchen()
	if err != nil {
		t.Fatalf("проверить после включения: %v", err)
	}
	if !est {
		t.Fatalf("включили, а Vklyuchen говорит «нет»")
	}

	// Главное, ради чего это вообще пишется: Windows должна выполнить не просто
	// путь, а путь В КАВЫЧКАХ и с режимом --tiho. Без кавычек папка с пробелом
	// рвёт команду, без --tiho при каждом включении компьютера вылезет окно.
	k, err := registry.OpenKey(registry.CURRENT_USER, vetka, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("открыть ветку: %v", err)
	}
	defer k.Close()
	zapis, _, err := k.GetStringValue(Imya)
	if err != nil {
		t.Fatalf("прочитать запись: %v", err)
	}
	if !strings.HasPrefix(zapis, `"`) || !strings.Contains(zapis, `.exe" `) {
		t.Fatalf("путь не в кавычках: %q", zapis)
	}
	if !strings.HasSuffix(zapis, " "+Argument) {
		t.Fatalf("запись без режима %s: %q", Argument, zapis)
	}

	// Повторное включение не должно ни ломаться, ни плодить вторую запись.
	if err := Vklyuchit(); err != nil {
		t.Fatalf("повторное включение: %v", err)
	}
	ust, err := Ustarela()
	if err != nil {
		t.Fatalf("проверить свежесть: %v", err)
	}
	if ust {
		t.Fatalf("только что переписали запись, а она считается устаревшей")
	}

	if err := Vyklyuchit(); err != nil {
		t.Fatalf("выключить: %v", err)
	}
	if err := Vyklyuchit(); err != nil {
		t.Fatalf("повторное выключение обязано быть тихим: %v", err)
	}
}

func TestUstarelaVidnaChuzhayaZapis(t *testing.T) {
	vernutPosle(t)

	k, _, err := registry.CreateKey(registry.CURRENT_USER, vetka, registry.SET_VALUE)
	if err != nil {
		t.Fatalf("открыть ветку на запись: %v", err)
	}
	// Так выглядит запись, оставшаяся от установки в другую папку.
	if err := k.SetStringValue(Imya, `"C:\Staraya\Kelevra.exe" --tiho`); err != nil {
		k.Close()
		t.Fatalf("записать чужой путь: %v", err)
	}
	k.Close()

	est, err := Vklyuchen()
	if err != nil || !est {
		t.Fatalf("чужая запись обязана читаться как «включено»: %v %v", est, err)
	}
	ust, err := Ustarela()
	if err != nil {
		t.Fatalf("проверить свежесть: %v", err)
	}
	if !ust {
		t.Fatalf("запись ведёт на другой файл, а Ustarela молчит")
	}
}

// Окно пишет автозапуск на копию в Program Files, а не на себя: окно бывает
// запущено из «Загрузок», и запись на него умирает с уборкой папки (беда ПК
// 18–21.10). Свежесть сверяется с тем же путём.
func TestVklyuchitNaChuzhoyPut(t *testing.T) {
	vernutPosle(t)
	sluzhbaEst = func() bool { return true }
	const pf = `C:\Program Files\Kelevra\Kelevra.exe`
	if err := VklyuchitNa(pf); err != nil {
		t.Fatalf("включить на Program Files: %v", err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, vetka, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("открыть ветку: %v", err)
	}
	zapis, _, err := k.GetStringValue(Imya)
	k.Close()
	if err != nil {
		t.Fatalf("прочитать запись: %v", err)
	}
	if zapis != `"`+pf+`" `+ArgumentSoSluzhboy {
		t.Fatalf("запись %q, ждали путь Program Files с %s", zapis, ArgumentSoSluzhboy)
	}
	if ust, err := UstarelaDlya(pf); err != nil || ust {
		t.Fatalf("запись на Program Files сверена с ним же как устаревшая: %v %v", ust, err)
	}
	if ust, err := Ustarela(); err != nil || !ust {
		t.Fatalf("с exe теста запись на Program Files обязана читаться устаревшей: %v %v", ust, err)
	}
}
