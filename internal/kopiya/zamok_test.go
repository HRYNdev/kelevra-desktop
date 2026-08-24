package kopiya

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestZamokVzaimoisklyuchayushchiy доказывает саму суть починки: замок —
// настоящий атомарный примитив ОС, а не ещё один файл, который можно
// прочитать в чужом промежутке между «прочитал» и «записал». Много
// горутин ломятся за ним одновременно; ни разу больше одной не должно
// решить, что держит его прямо сейчас.
//
// Владение Windows-мьютексом привязано к ОС-потоку, а не к горутине: без
// runtime.LockOSThread планировщик Go может пересадить горутину-владельца
// на другой поток и отдать её поток другой горутине — та бесплатно
// "наследует" чужое владение, и тест видит несуществующую гонку (или,
// наоборот, не видит настоящую). LockOSThread прибивает каждого гонщика к
// своему потоку на время удержания замка. Настоящую межпроцессную гонку
// (два разных .exe) этот тест не судит вовсе — за это отвечает
// stend/dvoynoy_zapusk.sh.
func TestZamokVzaimoisklyuchayushchiy(t *testing.T) {
	const gonshchikov = 40
	var odnovremenno int32
	var maxOdnovremenno int32
	var pobediteley int32
	var wg sync.WaitGroup

	for i := 0; i < gonshchikov; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			z, oderzhal := Vzyat(2 * time.Second)
			if !oderzhal {
				return
			}
			defer z.Otdat()
			atomic.AddInt32(&pobediteley, 1)
			teper := atomic.AddInt32(&odnovremenno, 1)
			for {
				stary := atomic.LoadInt32(&maxOdnovremenno)
				if teper <= stary || atomic.CompareAndSwapInt32(&maxOdnovremenno, stary, teper) {
					break
				}
			}
			// Держим замок заметное время — без него гонка внутри теста
			// сама себя не покажет: горутины могли бы просто не столкнуться
			// по времени, а не потому что замок их развёл.
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&odnovremenno, -1)
		}()
	}
	wg.Wait()

	if max := atomic.LoadInt32(&maxOdnovremenno); max > 1 {
		t.Fatalf("замок пустил %d держателей одновременно, должен быть 1", max)
	}
	if pobediteley != gonshchikov {
		t.Fatalf("не все %d горутин получили замок в пределах таймаута: получили %d", gonshchikov, pobediteley)
	}
}

// TestZamokPovtornyyVzyatPosleOtdat — после Otdat замок снова свободен:
// вторая волна не должна упереться в объект, который "навсегда" занят
// первой.
func TestZamokPovtornyyVzyatPosleOtdat(t *testing.T) {
	z1, ok := Vzyat(time.Second)
	if !ok {
		t.Fatal("первый Vzyat должен пройти сразу")
	}
	z1.Otdat()

	z2, ok := Vzyat(time.Second)
	if !ok {
		t.Fatal("после Otdat замок должен снова браться")
	}
	z2.Otdat()

	// Повторный Otdat не должен падать (nil-получатель и двойной вызов).
	var nilZamok *Zamok
	nilZamok.Otdat()
	z2.Otdat()
}

// TestGonkaNaytiZanyatBezZamkaIZZamkom — прямая имитация исходной беды:
// N "процессов" делают ровно то, что main.go делает вокруг Nayti/Zanyat.
// Без замка вокруг решения "я первый" несколько горутин совпадают в
// промежутке между чтением метки и её записью и все считают себя первыми
// (красный по старой схеме). С замком побеждает ровно одна.
func TestGonkaNaytiZanyatSZamkom(t *testing.T) {
	papka := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	const gonshchikov = 20
	var podnyaliSluzhbu int32
	var wg sync.WaitGroup
	for i := 0; i < gonshchikov; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			z, oderzhal := Vzyat(3 * time.Second)
			if oderzhal {
				defer z.Otdat()
			}
			if _, est := Nayti(papka); est {
				return // копия уже есть — открыли бы её окно, не своё ядро
			}
			// "Поднимаем службу": имитируем задержку старта ядра ДО того,
			// как метка появится на диске — именно это окно и было дырой.
			time.Sleep(3 * time.Millisecond)
			atomic.AddInt32(&podnyaliSluzhbu, 1)
			_ = Zanyat(papka, server.URL+"/", time.Now())
		}()
	}
	wg.Wait()

	if podnyaliSluzhbu != 1 {
		t.Fatalf("с замком должна была подняться ровно 1 служба, поднялось %d", podnyaliSluzhbu)
	}
}
