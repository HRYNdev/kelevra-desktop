package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Стенд беды «2 приложения»: окно осталось на экране после того, как его
// служба ушла (перезапуск с правами администратора либо «Выход» из трея).
// Без сторожа окно не закрывается никогда — этот тест краснеет.
func TestStorozhZakryvaetOknoKogdaSluzhbaUmerla(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	zakryto := make(chan struct{})
	go storozhitSluzhbu(server.URL, 20*time.Millisecond, 3, func() { close(zakryto) })

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
	go storozhitSluzhbu(server.URL, 20*time.Millisecond, 3, func() { close(zakryto) })

	select {
	case <-zakryto:
		t.Fatal("окно закрылось из-за одного промаха живой службы")
	case <-time.After(400 * time.Millisecond):
	}
	if atomic.LoadInt32(&zapros) < 3 {
		t.Fatalf("сторож не дошёл до промаха: запросов %d", atomic.LoadInt32(&zapros))
	}
}
