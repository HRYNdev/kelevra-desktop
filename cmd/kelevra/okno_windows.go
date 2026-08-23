//go:build windows

package main

import (
	"log"
	"os"

	"github.com/jchv/go-webview2"
)

// gdeWebView2 — официальный установщик компонента от Microsoft.
const gdeWebView2 = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

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
		// Не log.Fatal: в оконной сборке это молчание, а человек видит,
		// что двойной щелчок не сделал ничего. Компонент ставится бесплатно
		// и в одну кнопку, поэтому говорим прямо, что именно поставить.
		log.Printf("ОТКАЗ: нет компонента WebView2, окно открыть нечем")
		skazat("Kelevra не запустилась",
			"В этой Windows нет компонента WebView2 — окно приложения рисовать нечем.\n\n"+
				"Он бесплатный, от Microsoft. Поставьте его отсюда и запустите Kelevra снова:\n"+
				gdeWebView2+"\n\n(этот текст можно скопировать целиком: Ctrl+C)")
		os.Exit(1)
	}
	defer w.Destroy()
	// Окно живёт ровно столько, сколько живёт его служба: без неё показывать
	// нечего, а осиротевшее окно человек принимает за вторую копию приложения
	// (см. storozh_okna.go). Terminate по документации go-webview2 можно звать
	// из чужого потока, поэтому сторож работает своей горутиной.
	go storozhitSluzhbu(url, shagStorozha, molchaniyDoZakrytiya, w.Terminate)
	w.Navigate(url)
	w.Run()
}
