//go:build windows

package main

import (
	"log"

	"github.com/jchv/go-webview2"
)

// pokazatOkno открывает окно приложения на встроенном WebView2 (он есть в
// Windows 10/11 из коробки) и не возвращается, пока пользователь его не закроет.
func pokazatOkno(url string) {
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "Kelevra",
			Width:  420,
			Height: 660,
			Center: true,
		},
	})
	if w == nil {
		log.Fatal("не удалось открыть окно: нужен компонент WebView2")
	}
	defer w.Destroy()
	w.Navigate(url)
	w.Run()
}
