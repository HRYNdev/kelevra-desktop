// Команда podpisat: ставит подпись выпуска. Запускается только в сборке выпуска.
//
//	KELEVRA_SIGN_KEY=<seed base64> podpisat <файл> [имя в релизе] [тег релиза]
//
// С тегом подпись второго формата (podpis.PodpisatSTegom): тег входит в
// подписанную строку. Так подписываются архивы ядра core-*: замена ядра у
// стоящих установок принимает только такую подпись с совпадающим тегом.
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
	teg := ""
	if len(os.Args) > 3 {
		teg = os.Args[3]
	}
	klyuch := os.Getenv("KELEVRA_SIGN_KEY")
	if klyuch == "" {
		fmt.Fprintln(os.Stderr, "нет KELEVRA_SIGN_KEY: подписать нечем")
		os.Exit(1)
	}
	var tekst string
	var err error
	if teg != "" {
		tekst, err = podpis.PodpisatSTegom(klyuch, imya, teg, put)
	} else {
		tekst, err = podpis.Podpisat(klyuch, imya, put)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "не подписалось:", err)
		os.Exit(1)
	}
	if teg != "" {
		err = podpis.ProveritSTegom(put, imya, teg, []byte(tekst))
	} else {
		err = podpis.Proverit(put, imya, []byte(tekst))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "подпись не сошлась со вшитым открытым ключом (секрет не от этой пары?):", err)
		os.Exit(1)
	}
	if err := os.WriteFile(put+podpis.SufiksFayla, []byte(tekst), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if teg != "" {
		fmt.Println("подписано:", imya, "тег", teg)
		return
	}
	fmt.Println("подписано:", imya)
}
