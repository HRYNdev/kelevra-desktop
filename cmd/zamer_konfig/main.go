// Замерочный вход: гоняет konfig.Prigotovit НАСТОЯЩИМ кодом приложения на
// профиле, который дают снаружи, и печатает готовый конфиг в stdout.
//
// Зачем он есть. stend/pravila_nedostupny.sh (и любой другой стенд, который
// хочет скормить настоящему ядру честный конфиг) не имеет права собирать его
// сам самодельным JSON — тогда стенд проверял бы свою фантазию о формате
// конфига, а не то, что реально уходит в sing-box из internal/konfig. Этот
// вход — единственная разница между «стенд щупает продукт» и «стенд щупает
// себя»: он вызывает ровно ту же функцию Prigotovit, что и internal/sluzhba.
//
// Диагностика (режим, адрес прокси, заметка человеку) уходит в stderr, чтобы
// stdout можно было напрямую перенаправить в файл конфига.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/pravila"
)

func main() {
	profil := flag.String("profil", "", "путь к профилю (обязателен)")
	prava := flag.Bool("prava", false, "как Vybor.Prava — есть ли права администратора")
	bezProksi := flag.Bool("bez-proksi", false, "как Vybor.BezSistemnogoProksi")
	bezPravil := flag.Bool("bez-pravil", false, "как Vybor.BezSetevyhPravil — источник правил недоступен")
	komplektDir := flag.String("komplekt-dir", "", "как Vybor.PravilaIzKomplekta: разложить internal/pravila в эту папку и подставить пути (главнее -bez-pravil)")
	flag.Parse()

	if *profil == "" {
		fmt.Fprintln(os.Stderr, "нужен -profil <путь>")
		os.Exit(2)
	}
	syroy, err := os.ReadFile(*profil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	v := konfig.Vybor{
		Prava:               *prava,
		BezSistemnogoProksi: *bezProksi,
		BezSetevyhPravil:    *bezPravil,
	}
	if *komplektDir != "" {
		komplekt, err := pravila.Razlozhit(*komplektDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "pravila.Razlozhit:", err)
			os.Exit(1)
		}
		v.PravilaIzKomplekta = komplekt
		v.PravilaKomplektData = pravila.Data()
	}
	gotovyy, k, err := konfig.Prigotovit(syroy, v)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Prigotovit:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "режим=%v прокси=%s заметка=%q\n", k.Rezhim, k.ProksiAdres, k.Zametka)
	os.Stdout.Write(gotovyy)
}
