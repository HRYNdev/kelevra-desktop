// Пакет pravila: встроенный в исполняемый файл слепок правил маршрутизации
// (route.rule_set) — на случай, если их живой источник недоступен.
//
// Зачем он нужен. Боевой профиль несёт 22 route.rule_set (тип remote), все
// качаются с https://subkv.chickenkiller.com/rules/<tag>.srs мимо туннеля
// (http_client.detour:"direct"). Если источник недоступен и локальный кеш
// пуст, ядро падает целиком (см. internal/sluzhba.go, ветка «initialize
// rule-set»). До 0.6.11 подстраховкой было internal/konfig.Vybor.BezSetevyhPravil
// — трафик шёл в VPN вообще без разбора, человек терял умную маршрутизацию.
//
// Замер живьём 23.08: сумма всех 22 файлов — 495 039 байт (0.47 МБ), из них
// ads.srs — 468 924 Б (94.7%). По Last-Modified 18 из 22 не менялись с 3
// августа, ежедневно меняется только ads.srs. Значит вшитый в .exe комплект
// стареет медленно и стоит копейки — можно возить его В САМОМ приложении и
// подставлять вместо remote, когда источник недостижим (internal/konfig
// Vybor.PravilaIzKomplekta).
package pravila

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed komplekt/*.srs komplekt/data.txt
var komplekt embed.FS

// Data — дата снимка встроенного комплекта (komplekt/data.txt), человеку в
// окне: «работают встроенные правила от <дата>».
func Data() string {
	b, err := komplekt.ReadFile("komplekt/data.txt")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Tegi — теги всех правил в комплекте (имена файлов komplekt/*.srs без
// расширения), по алфавиту.
func Tegi() []string {
	zapisi, err := fs.ReadDir(komplekt, "komplekt")
	if err != nil {
		return nil
	}
	var tegi []string
	for _, z := range zapisi {
		if z.IsDir() || !strings.HasSuffix(z.Name(), ".srs") {
			continue
		}
		tegi = append(tegi, strings.TrimSuffix(z.Name(), ".srs"))
	}
	sort.Strings(tegi)
	return tegi
}

// Razlozhit раскладывает встроенный комплект в dir (создаёт папку при нужде)
// и возвращает карту тег→абсолютный путь к разложенному файлу.
//
// Идемпотентно: если на месте уже лежит файл того же размера и содержимого —
// не переписывает его (не насилует диск при каждом перезапуске ядра и не
// сбрасывает mtime зря).
func Razlozhit(dir string) (map[string]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("папка под встроенный комплект правил %q: %w", dir, err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("путь до комплекта правил: %w", err)
	}
	itog := map[string]string{}
	for _, teg := range Tegi() {
		soderzhimoe, err := komplekt.ReadFile("komplekt/" + teg + ".srs")
		if err != nil {
			return nil, fmt.Errorf("встроенный файл %s.srs: %w", teg, err)
		}
		put := filepath.Join(abs, teg+".srs")
		if sovpadaetSDiska(put, soderzhimoe) {
			itog[teg] = put
			continue
		}
		if err := os.WriteFile(put, soderzhimoe, 0o644); err != nil {
			return nil, fmt.Errorf("записать %s: %w", put, err)
		}
		itog[teg] = put
	}
	return itog, nil
}

// sovpadaetSDiska — правда, если по пути put уже лежит ровно soderzhimoe.
func sovpadaetSDiska(put string, soderzhimoe []byte) bool {
	svedeniya, err := os.Stat(put)
	if err != nil || svedeniya.Size() != int64(len(soderzhimoe)) {
		return false
	}
	naDiske, err := os.ReadFile(put)
	if err != nil {
		return false
	}
	return string(naDiske) == string(soderzhimoe)
}
