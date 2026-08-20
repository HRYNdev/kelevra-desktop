//go:build windows

package avtozapusk

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// Тест трогает НАСТОЯЩУЮ пользовательскую ветку реестра — под wine это
// песочница стенда, у человека этот тест никогда не выполняется. Чтобы не
// оставить за собой чужой автозапуск, исходное состояние снимается и
// возвращается назад.
func vernutPosle(t *testing.T) {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, vetka, registry.QUERY_VALUE)
	if err != nil {
		t.Skipf("ветка автозагрузки недоступна: %v", err)
	}
	bylo, _, errCht := k.GetStringValue(Imya)
	k.Close()
	t.Cleanup(func() {
		if errCht == registry.ErrNotExist {
			_ = Vyklyuchit()
			return
		}
		kw, _, err := registry.CreateKey(registry.CURRENT_USER, vetka, registry.SET_VALUE)
		if err != nil {
			return
		}
		defer kw.Close()
		_ = kw.SetStringValue(Imya, bylo)
	})
}

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
