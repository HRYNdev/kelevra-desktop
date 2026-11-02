package sluzhba

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stendZamka — стенд, где «запуск ядра» ждёт отпускания и считает, сколько
// запусков идёт одновременно.
type stendZamka struct {
	s        *Sluzhba
	vhodov   atomic.Int32
	seychas  atomic.Int32
	naibolsh atomic.Int32
	vshli    chan struct{}
	otpustit chan struct{}
}

func novyyStendZamka(t *testing.T) *stendZamka {
	t.Helper()
	st := &stendZamka{s: gotovStendLestnicy(t), vshli: make(chan struct{}, 8), otpustit: make(chan struct{})}
	st.s.zashchitaPodnyataDlyaStenda = func() bool { return true }
	st.s.zapustitYadro = func(ctx context.Context) error {
		st.vhodov.Add(1)
		n := st.seychas.Add(1)
		for {
			m := st.naibolsh.Load()
			if n <= m || st.naibolsh.CompareAndSwap(m, n) {
				break
			}
		}
		st.vshli <- struct{}{}
		<-st.otpustit
		st.seychas.Add(-1)
		return nil
	}
	return st
}

func (st *stendZamka) zhdatVhoda(t *testing.T) {
	t.Helper()
	select {
	case <-st.vshli:
	case <-time.After(5 * time.Second):
		t.Fatal("запуск ядра не начался")
	}
}

// Гонка 02.11: самоизлечение гасит ядро, и в этот момент авторежим («вне
// дома, туннеля нет — поднимаю») зовёт PodnyatZashchitu. Раньше оба шли
// вместе: второй запуск возвращал «ядро уже запущено» до готовности API и
// выбранный узел не применялся. Теперь второй ждёт очереди и, увидев
// поднятое ядро, ничего не запускает.
func TestPerezapuskIPodyomNeIdutVmeste(t *testing.T) {
	st := novyyStendZamka(t)
	pokolenie := st.s.pokolenieAvtorezhima()
	var vse sync.WaitGroup
	vse.Add(1)
	go func() {
		defer vse.Done()
		st.s.perezapustitDlyaDoma("проверка", pokolenie)
	}()
	st.zhdatVhoda(t)

	vse.Add(1)
	go func() {
		defer vse.Done()
		if err := st.s.PodnyatZashchitu(context.Background()); err != nil {
			t.Errorf("второй подъём: %v", err)
		}
	}()
	// Второй подъём должен встать в очередь, а не запустить ядро рядом.
	select {
	case <-st.vshli:
		t.Fatal("второй запуск ядра начался, пока шёл перезапуск")
	case <-time.After(300 * time.Millisecond):
	}
	close(st.otpustit)
	vse.Wait()
	if st.naibolsh.Load() != 1 {
		t.Fatalf("одновременных запусков ядра %d, ждали 1", st.naibolsh.Load())
	}
	if st.vhodov.Load() != 1 {
		t.Fatalf("запусков ядра %d, ждали 1: второй подъём должен увидеть поднятое ядро", st.vhodov.Load())
	}
}

// Смена блока ждёт идущий подъём и перезапускает ядро после него (конфиг
// занятого подъёма мог быть собран без нового блока); самоизлечение не ждёт.
func TestPerezapuskPriZanyatoyOcheredi(t *testing.T) {
	st := novyyStendZamka(t)
	close(st.otpustit)
	pokolenie := st.s.pokolenieAvtorezhima()
	st.s.zamokPodyoma.Lock() // идёт чужой подъём

	st.s.perezapustitDlyaDoma("самоизлечение", pokolenie)
	if st.vhodov.Load() != 0 {
		t.Fatal("самоизлечение при занятой очереди не должно запускать ядро")
	}

	gotovo := make(chan struct{})
	go func() {
		st.s.perezapustit("смена блока", pokolenie, true)
		close(gotovo)
	}()
	select {
	case <-gotovo:
		t.Fatal("смена блока не дождалась очереди")
	case <-time.After(300 * time.Millisecond):
	}
	st.s.zamokPodyoma.Unlock()
	select {
	case <-gotovo:
	case <-time.After(5 * time.Second):
		t.Fatal("смена блока не перезапустила ядро после очереди")
	}
	if st.vhodov.Load() != 1 {
		t.Fatalf("запусков ядра %d, ждали 1 (перезапуск после подъёма)", st.vhodov.Load())
	}
}
