//go:build !windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// pokazatOkno вне Windows окна не рисует: там приложение не живёт.
// Эта ветка нужна, чтобы всю работу приложения можно было проверить
// на сервере обычным браузером, без Windows.
func pokazatOkno(url string) {
	fmt.Println("окна тут нет; интерфейс приложения открыт по адресу:", url)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}

// podnyatChuzheeOkno вне Windows не ищет ничего: там нет ни WebView2, ни
// его окон. Вызывающий код (main.go) обязан откатиться на pokazatOkno.
func podnyatChuzheeOkno() bool {
	log.Printf("поднятие чужого окна не поддерживается вне Windows, открываю своё")
	return false
}
