package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/kopiya"
)

// Стенд беды «2 приложения»: окно осталось на экране после того, как его
// служба ушла (перезапуск с правами администратора либо «Выход» из трея).
// Без сторожа окно не закрывается никогда — этот тест краснеет.
func TestStorozhZakryvaetOknoKogdaSluzhbaUmerla(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	zakryto := make(chan struct{})
	go storozhitSluzhbu(server.URL, t.TempDir(), 20*time.Millisecond, 3, func() { close(zakryto) })

	// Фаза A: служба жива — окно закрывать не за что.
	select {
	case <-zakryto:
		t.Fatal("окно закрылось при живой службе")
	case <-time.After(200 * time.Millisecond):
	}

	// Фаза B: служба ушла. Окно обязано закрыться само.
	server.Close()
	select {
	case <-zakryto:
	case <-time.After(2 * time.Second):
		t.Fatal("служба мертва, а окно так и не закрылось — это и есть вторая копия на экране")
	}
}

// Один промах — не смерть: служба могла быть занята или ответ не успел за
// таймаут. Окно, закрывающееся от единственной заминки, — беда хуже исходной.
func TestStorozhTerpitOdinochnyyPromah(t *testing.T) {
	var zapros int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&zapros, 1) == 2 { // ровно один промах посередине
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	zakryto := make(chan struct{})
	go storozhitSluzhbu(server.URL, t.TempDir(), 20*time.Millisecond, 3, func() { close(zakryto) })

	select {
	case <-zakryto:
		t.Fatal("окно закрылось из-за одного промаха живой службы")
	case <-time.After(400 * time.Millisecond):
	}
	if atomic.LoadInt32(&zapros) < 3 {
		t.Fatalf("сторож не дошёл до промаха: запросов %d", atomic.LoadInt32(&zapros))
	}
}

// Служба не умерла, а ПЕРЕЕХАЛА: обновление подняло её заново на другом порту.
// Окно обязано открыться на новом адресе, а не остаться с надписью
// «Kelevra перезапускается…».
//
// Замер 08.09 с живой машины: служба слушала порт 61680, после обновления
// стала слушать 54520, окно стучалось в мёртвый и висело навсегда — при том,
// что само обновление прошло полностью и связь работала.
func TestStorozhVidyaPereezdOtkryvaetOknoZanovo(t *testing.T) {
	papka := t.TempDir()
	novaya := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer novaya.Close()
	// Метка на диске: служба уже живёт по НОВОМУ адресу.
	if err := kopiya.Zanyat(papka, novaya.URL, time.Now()); err != nil {
		t.Fatalf("не записал метку службы: %v", err)
	}

	vzyatNovyyAdres() // сбрасываем след прошлого теста
	zakryto := make(chan struct{})
	// Старый адрес мёртв: сервера на нём нет вовсе.
	go storozhitSluzhbu("http://127.0.0.1:1/", papka, 20*time.Millisecond, 3, func() { close(zakryto) })

	select {
	case <-zakryto:
	case <-time.After(3 * time.Second):
		t.Fatal("сторож не закрыл окно, хотя старая служба молчит")
	}
	if adres := vzyatNovyyAdres(); adres != novaya.URL {
		t.Fatalf("новый адрес службы %q, а служба живёт на %q — окно откроется в пустоту или не откроется вовсе",
			adres, novaya.URL)
	}
}

// Служба возвращается ПОЗЖЕ, чем сторож успевает разувериться.
//
// Это и есть настоящий порядок событий при обновлении, замеренный 08.09:
// служба уходит с отказом, диспетчер служб поднимает её через пять секунд, и
// только тогда новый процесс пишет метку. Сторож же принимал решение на
// шестой секунде и, не найдя живой метки, закрывал окно насовсем — человек
// оставался с надписью «Kelevra перезапускается…» и пустотой за ней.
//
// Без ожидания замены (zhdatZamenu) тест краснеет: адрес остаётся пустым.
func TestStorozhZhdyotSluzhbuKotorayaVernulasPozzhe(t *testing.T) {
	papka := t.TempDir()
	novaya := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer novaya.Close()

	vzyatNovyyAdres()
	zakryto := make(chan struct{})
	// Метки нет вовсе: старая служба ушла, новая ещё не поднялась.
	go storozhitSluzhbu("http://127.0.0.1:1/", papka, 20*time.Millisecond, 3, func() { close(zakryto) })

	// Замена появляется заметно позже порога молчания (3 × 20 мс).
	time.Sleep(300 * time.Millisecond)
	if err := kopiya.Zanyat(papka, novaya.URL, time.Now()); err != nil {
		t.Fatalf("не записал метку новой службы: %v", err)
	}

	select {
	case <-zakryto:
	case <-time.After(3 * time.Second):
		t.Fatal("сторож не закрыл окно, хотя служба переехала")
	}
	if adres := vzyatNovyyAdres(); adres != novaya.URL {
		t.Fatalf("сторож не дождался замены: адрес %q, а служба живёт на %q", adres, novaya.URL)
	}
}

// Служба ожила на ПРЕЖНЕМ адресе — окно закрывать не за что.
//
// Порт службы случайный, но случайный не значит непременно другой: тот же
// самый номер ей достаться может. Раньше эта ветка вела туда же, куда и
// смерть службы, — окно закрывалось у живой связи.
func TestStorozhNeZakryvaetOknoKogdaSluzhbaOzhilaNaTomZheAdrese(t *testing.T) {
	papka := t.TempDir()
	var otvechaet atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !otvechaet.Load() {
			// Молчим по-настоящему: обрываем соединение, как мёртвый порт.
			hj, ok := w.(http.Hijacker)
			if ok {
				c, _, err := hj.Hijack()
				if err == nil {
					c.Close()
					return
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	vzyatNovyyAdres()
	zakryto := make(chan struct{})
	go storozhitSluzhbu(server.URL, papka, 20*time.Millisecond, 3, func() { close(zakryto) })

	// Даём сторожу разувериться, потом оживляем службу на том же адресе.
	time.Sleep(200 * time.Millisecond)
	otvechaet.Store(true)

	select {
	case <-zakryto:
		t.Fatal("сторож закрыл окно, хотя служба ответила по прежнему адресу")
	case <-time.After(700 * time.Millisecond):
	}
}
