// Команда podpisat: ставит подпись выпуска. Запускается только в сборке выпуска.
//
//	KELEVRA_SIGN_KEY=<seed base64> podpisat <файл> [имя в релизе]
//
// Пишет <файл>.sig рядом и сразу проверяет его вшитым в приложение ключом: если
// секрет не от той пары, выпуск падает здесь, а не у людей. Ключ берётся только
// из переменной окружения, в аргументы командной строки он не попадает.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/HRYNdev/kelevra-desktop/internal/podpis"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "использование: podpisat <файл> [имя в релизе]")
		os.Exit(2)
	}
	put := os.Args[1]
	imya := filepath.Base(put)
	if len(os.Args) > 2 {
		imya = os.Args[2]
	}
	klyuch := os.Getenv("KELEVRA_SIGN_KEY")
	if klyuch == "" {
		fmt.Fprintln(os.Stderr, "нет KELEVRA_SIGN_KEY: подписать нечем")
		os.Exit(1)
	}
	tekst, err := podpis.Podpisat(klyuch, imya, put)
	if err != nil {
		fmt.Fprintln(os.Stderr, "не подписалось:", err)
		os.Exit(1)
	}
	if err := podpis.Proverit(put, imya, []byte(tekst)); err != nil {
		fmt.Fprintln(os.Stderr, "подпись не сошлась со вшитым открытым ключом (секрет не от этой пары?):", err)
		os.Exit(1)
	}
	if err := os.WriteFile(put+podpis.SufiksFayla, []byte(tekst), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("подписано:", imya)
}
