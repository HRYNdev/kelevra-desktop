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
// Диагностика (режим, заметка человеку) уходит в stderr, чтобы stdout можно
// было напрямую перенаправить в файл конфига.
//
// -vhod-shchupa: приложение с 0.6.77 собирает только туннель, а стенду без
// root туннель не поднять. Флаг меняет в ГОТОВОМ конфиге вход tun на mixed
// (без системного прокси) на порту из mixed-входа профиля — решение
// маршрутизатора у обоих входов одно и то же. Это вход стенда, а не режим
// приложения: в бою такого конфига не бывает.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/pravila"
)

func main() {
	profil := flag.String("profil", "", "путь к профилю (обязателен)")
	// -prava и -bez-proksi остались ради старых вызовов стендов: режима, который
	// они выбирали (прокси), больше нет, и сборка от них не зависит.
	_ = flag.Bool("prava", false, "не действует с 0.6.77 (режим один — туннель)")
	_ = flag.Bool("bez-proksi", false, "не действует с 0.6.77 (системный прокси не ставится)")
	vhodShchupa := flag.Bool("vhod-shchupa", false, "заменить в готовом конфиге tun на mixed (порт из mixed-входа профиля) — вход стенда без root")
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
		BezSetevyhPravil: *bezPravil,
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
	if *vhodShchupa {
		if gotovyy, err = vstavitVhodShchupa(syroy, gotovyy); err != nil {
			fmt.Fprintln(os.Stderr, "вход щупа:", err)
			os.Exit(1)
		}
	}
	fmt.Fprintf(os.Stderr, "режим=%v вход щупа=%v заметка=%q\n", k.Rezhim, *vhodShchupa, k.Zametka)
	os.Stdout.Write(gotovyy)
}

// vstavitVhodShchupa — см. шапку файла (-vhod-shchupa).
func vstavitVhodShchupa(syroy, gotovyy []byte) ([]byte, error) {
	var profil, d map[string]any
	if err := json.Unmarshal(syroy, &profil); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(gotovyy, &d); err != nil {
		return nil, err
	}
	var mixed map[string]any
	vhody, _ := profil["inbounds"].([]any)
	for _, v := range vhody {
		if vh, ok := v.(map[string]any); ok && vh["type"] == "mixed" {
			mixed = vh
			break
		}
	}
	if mixed == nil {
		return nil, fmt.Errorf("в профиле нет mixed-входа — порт щупа взять неоткуда")
	}
	mixed["set_system_proxy"] = false
	teg, _ := mixed["tag"].(string)
	d["inbounds"] = []any{mixed}
	if r, ok := d["route"].(map[string]any); ok {
		pravila, _ := r["rules"].([]any)
		for _, p := range pravila {
			pr, ok := p.(map[string]any)
			if !ok {
				continue
			}
			spisok, _ := pr["inbound"].([]any)
			for _, x := range spisok {
				if x == "tun-in" {
					pr["inbound"] = append(spisok, teg)
					break
				}
			}
		}
	}
	return json.MarshalIndent(d, "", "  ")
}
