package main

import (
	"context"
	"log"
	"runtime"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/kopiya"
	"github.com/HRYNdev/kelevra-desktop/internal/sluzhba"
	"github.com/HRYNdev/kelevra-desktop/internal/vinsluzhba"
)

// Служебное в сеансе человека уступает место службе Windows.
//
// Без службы окно работает через своё служебное (--sluzhba) — без прав, то
// есть в состоянии «Для работы Kelevra нужны права администратора». Службу
// ставит копия с правами (--postavit-sluzhbu): по кнопке «Дать права» или
// после обновления со старой версии. Поднявшись, служба кладёт свою метку в
// общую папку — и с этой секунды хозяев у связи два. Своё служебное обязано
// уйти: туннель держит служба, а окно сторож переведёт на её адрес сам
// (storozh_okna.go: zhdatZamenu смотрит и в общую папку).

// shagSlezhkiZaSluzhboy — как часто служебное смотрит, не встала ли служба.
// Секунда: человек только что согласился в окне Windows и ждёт результата.
const shagSlezhkiZaSluzhboy = time.Second

// sluzhbaWindowsVstala — служба Windows установлена и отвечает по метке в
// общей папке, и это не мы. Точка подмены для тестов.
var sluzhbaWindowsVstala = func(svoyAdres string) (string, bool) {
	if ust, err := vinsluzhba.Ustanovlena(); err != nil || !ust {
		return "", false
	}
	papka := hranenie.PapkaSluzhbyWindows()
	if papka == "" {
		return "", false
	}
	adres, est := kopiya.Nayti(papka)
	if !est || adres == svoyAdres {
		return "", false
	}
	return adres, true
}

// peredatMestoSluzhbeWindows ждёт службу Windows и, дождавшись, гасит свою
// связь, поднимает значок отдельным процессом (у службы значка нет) и
// уходит штатным путём — через uyti, тем же каналом, что «Выход» из значка.
func peredatMestoSluzhbeWindows(ctx context.Context, s *sluzhba.Sluzhba, svoyAdres string, uyti chan<- struct{}) {
	if runtime.GOOS != "windows" {
		return
	}
	t := time.NewTicker(shagSlezhkiZaSluzhboy)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		adres, est := sluzhbaWindowsVstala(svoyAdres)
		if !est {
			continue
		}
		log.Printf("служба Windows поставлена и работает (%s) — уступаю ей место", adres)
		_ = s.OpustitZashchitu()
		zapustitZnachok()
		select {
		case uyti <- struct{}{}:
		default:
		}
		return
	}
}
