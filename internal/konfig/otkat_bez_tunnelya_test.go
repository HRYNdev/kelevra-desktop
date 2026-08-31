package konfig

import "testing"

// Vybor.BezTunnelya — последняя ступень лестницы деградации режимов: права
// есть, полный режим в ключе есть, а поднять его сейчас не вышло (ядро упало
// на создании сетевого адаптера). Приложение опускается на ступень ниже
// (sluzhba.PodnyatZashchitu), и сборка конфига обязана и собрать половинный
// режим, и назвать его человеку ПРАВДИВО.
//
// Проверка на сценарий: что получил человек, а не как это сделано. До 31.08
// этой ступени не было вовсе — человек оставался с «нет связи» и без всякой
// защиты.
func TestOtkatBezTunnelyaGovoritPravdu(t *testing.T) {
	gotovyy, k, err := Prigotovit(profil(t), Vybor{Prava: true, BezTunnelya: true})
	if err != nil {
		t.Fatalf("конфиг отката не собрался — падать тут значит оставить человека вовсе без связи: %v", err)
	}
	if k.Rezhim != Proksi {
		t.Fatalf("режим %q, ждали режим браузеров: полный только что не поднялся", k.Rezhim)
	}
	if !k.Chastichnaya {
		t.Fatal("защита половинная, а конфиг об этом молчит — окно нарисует обычное «подключено»")
	}
	if k.PochemuChastichnaya != PrichinaTunnelNePodnyalsya {
		t.Fatalf("причина %q: про права врать нельзя, они есть, и кнопки UAC человеку никто не покажет", k.PochemuChastichnaya)
	}
	if k.Zametka != ZametkaTunnelNePodnyalsya {
		t.Fatalf("заметка %q — она обязана сказать, что полный режим не вышел, а не что так задумано", k.Zametka)
	}
	// Вход-туннель обязан уйти из конфига: оставь его — и ядро упадёт на том
	// же месте, а «откат» будет повтором той же неудачной попытки.
	estTun, estMixed := false, false
	for _, vh := range vhody(razobrat(t, gotovyy)) {
		switch vh["type"] {
		case "tun":
			estTun = true
		case "mixed":
			estMixed = true
		}
	}
	if estTun {
		t.Fatal("в конфиге отката снова вход-туннель: ядро упадёт там же, где упало")
	}
	if !estMixed {
		t.Fatal("в конфиге отката не осталось локального входа — подключаться нечем")
	}
	if k.ProksiAdres == "" {
		t.Fatal("адрес локального прокси пуст — системе нечего прописать, трафик пойдёт мимо")
	}
}

// Слова отката не должны подменять собой обычный половинный режим: когда прав
// просто нет, человеку по-прежнему говорят про права и показывают кнопку.
func TestBezPravSlovaNePodmenyayutsyaSlovamiOtkata(t *testing.T) {
	_, k, err := Prigotovit(profil(t), Vybor{Prava: false})
	if err != nil {
		t.Fatal(err)
	}
	if k.PochemuChastichnaya != PrichinaBezPrav || k.Zametka != ZametkaBezPrav {
		t.Fatalf("без прав человеку сказали %q / %q — а надо про права и про кнопку", k.Zametka, k.PochemuChastichnaya)
	}
}
