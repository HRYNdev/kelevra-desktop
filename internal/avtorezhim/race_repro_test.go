package avtorezhim

import (
	"context"
	"sync"
	"testing"
)

// TestZahodIPrichinaSlepotyBezGonki — регрессия на боевую гонку данных между
// Avtorezhim.Zahod (пишет slepyhPodryad и poslednyayaPrichinaSlepoty) и
// Avtorezhim.PrichinaSlepoty (читает их же) на одном экземпляре.
//
// Почему это беда у человека, а не в тесте: в проде Zahod серийно, по кругу,
// зовёт Sluzhitel.Krutit из своей горутины (internal/avtorezhim/sluzhitel.go,
// см. цикл вокруг zahod), а PrichinaSlepoty зовёт HTTP-ручка
// /api/sostoyanie (internal/sluzhba/sluzhba.go) на КАЖДЫЙ запрос окна к
// службе — то есть ровно тогда, когда пользователь открыл или обновил окно
// авторежима, пока фоновый цикл продолжает крутиться. s.avtorezhimZamok в
// sluzhba.go защищает только подмену указателя *Avtorezhim, а не поля этого
// же экземпляра — окно и фон читают и пишут одну и ту же память без
// синхронизации. Без замка внутри Avtorezhim это данные-гонка (доказано
// -race, см. коммит), которая на живой машине способна отдать человеку
// повреждённое значение причины слепоты или зависнуть под race-детектором
// в его собственной сборке.
func TestZahodIPrichinaSlepotyBezGonki(t *testing.T) {
	a := &Avtorezhim{
		Dns:           fakeDns{doma: false},
		Trafik:        &fakeTrafik{},
		Zadvizhka:     NovayaZadvizhka(Neizvestno),
		TunnelPodnyat: func() bool { return true },
		SetevoyAdres: func() (string, string, error) {
			return "", "", context.DeadlineExceeded
		},
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			a.Zahod(context.Background(), true, false)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = a.PrichinaSlepoty()
		}
	}()
	wg.Wait()
}
