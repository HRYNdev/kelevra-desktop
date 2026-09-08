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

	novyyAdresSluzhby = ""
	zakryto := make(chan struct{})
	// Старый адрес мёртв: сервера на нём нет вовсе.
	go storozhitSluzhbu("http://127.0.0.1:1/", papka, 20*time.Millisecond, 3, func() { close(zakryto) })

	select {
	case <-zakryto:
	case <-time.After(3 * time.Second):
		t.Fatal("сторож не закрыл окно, хотя старая служба молчит")
	}
	if novyyAdresSluzhby != novaya.URL {
		t.Fatalf("новый адрес службы %q, а служба живёт на %q — окно откроется в пустоту или не откроется вовсе",
			novyyAdresSluzhby, novaya.URL)
	}
}
