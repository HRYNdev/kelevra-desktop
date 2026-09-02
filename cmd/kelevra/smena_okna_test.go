package main

import "testing"

// TestPokazatLiOkno держит починку жалобы 02.09: после нажатия «Обновить»
// человек остался без окна вовсе (разбор целиком — в шапке smena_okna.go).
//
// Место опасное ровно тем же, чем и tihiyZapusk рядом: его уже дважды чинили
// так, что починка становилась новой болезнью — сперва «новая копия молчит
// ВСЕГДА» (25.08, два окна), потом обратно (27.08, окно не работает до
// перезапуска). Поэтому таблица покрывает все наборы аргументов, какие
// приложение шлёт себе само, и оба исхода замера окна — чтобы третьего раза
// не было и здесь.
func TestPokazatLiOkno(t *testing.T) {
	byloIUshlo := itogSmenyOkna{BylOkno: true, Ushlo: true}
	byloNeUshlo := itogSmenyOkna{BylOkno: true}
	nikakogo := itogSmenyOkna{}

	sluchai := []struct {
		imya     string
		tiho     bool
		smenaPID int
		itog     itogSmenyOkna
		hochet   bool
		pochemu  string
	}{
		{
			imya: "обновление нажали в открытом окне", tiho: true, smenaPID: 1234,
			itog: byloIUshlo, hochet: true,
			pochemu: "человек нажал «Обновить», глядя в окно; молчание в ответ — и есть жалоба 02.09",
		},
		{
			imya: "обновление нажали в трее, окна не было", tiho: true, smenaPID: 1234,
			itog: nikakogo, hochet: false,
			pochemu: "тычок в пузырь при закрытом окне: окна не было — открывать его не просили",
		},
		{
			imya: "старое окно не закрылось", tiho: true, smenaPID: 1234,
			itog: byloNeUshlo, hochet: true,
			pochemu: "окно было, значит человек ждёт своё; пустой экран после нажатия хуже двух окон",
		},
		{
			imya: "автозапуск с Windows", tiho: true, smenaPID: 0,
			itog: nikakogo, hochet: false,
			pochemu: "вход в систему: служба поднимается молча, значок в трее — окна тут никто не просил",
		},
		{
			imya: "смена прав после UAC", tiho: false, smenaPID: 1234,
			itog: nikakogo, hochet: true,
			pochemu: "человек нажал «Включить для всех программ» и согласился в UAC — он ждёт окно (беда 27.08)",
		},
		{
			imya: "человек щёлкнул по значку", tiho: false, smenaPID: 0,
			itog: nikakogo, hochet: true,
			pochemu: "обычный запуск руками — окно обязано открыться",
		},
	}
	for _, s := range sluchai {
		t.Run(s.imya, func(t *testing.T) {
			got := pokazatLiOkno(s.tiho, s.smenaPID, s.itog)
			if got != s.hochet {
				t.Errorf("pokazatLiOkno(tiho=%v, smenaPID=%d, %+v) = %v, ждали %v: %s",
					s.tiho, s.smenaPID, s.itog, got, s.hochet, s.pochemu)
			}
		})
	}
}

// TestPokazatLiOknoNeZavisitOtUshlo — Ushlo решает НЕ судьбу нашего окна, а
// только то, увидит ли человек рядом чужое (и попадёт ли об этом строка в
// журнал, см. zakrytStaroeOkno). Своё окно открывается в обоих случаях: копия,
// которая молчит, потому что не смогла закрыть чужое окно, оставила бы
// человека вовсе без работающего приложения — ровно с тем, на что он и
// жаловался.
func TestPokazatLiOknoNeZavisitOtUshlo(t *testing.T) {
	ushlo := pokazatLiOkno(true, 1234, itogSmenyOkna{BylOkno: true, Ushlo: true})
	neUshlo := pokazatLiOkno(true, 1234, itogSmenyOkna{BylOkno: true})
	if ushlo != neUshlo {
		t.Fatalf("решение про своё окно зависит от чужого: ушло=%v, не ушло=%v", ushlo, neUshlo)
	}
	if !ushlo {
		t.Fatal("окно было открыто, а новая копия его не вернула — жалоба 02.09 не починена")
	}
}
