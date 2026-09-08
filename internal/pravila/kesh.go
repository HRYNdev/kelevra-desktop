package pravila

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Кеш свежих правил на диске: главное лекарство от «связь не поднялась».
//
// Беда, которую он лечит (замер 08.09 с живой машины, дважды за час):
//
//	ядро упало: initialize rule-set[0]: ads: lookup subkv... — не резолвится
//	источник правил недоступен, пробую встроенный комплект
//	...
//	поднимаю ядро совсем без правил (весь трафик через VPN)
//
// Домен при этом был жив: с той же машины он резолвился и файл скачивался за
// секунду — но ПОЗЖЕ, когда мы спросили руками. В момент старта ядра сеть
// перестраивается: Windows переключает DNS на туннельный, старый резолвер уже
// не отвечает, а новый ещё не работает, потому что ядро как раз и не
// поднялось. Курица и яйцо, и человек в нём остаётся без связи.
//
// Выход: не заставлять ядро ходить в сеть на старте вовсе. Правила берутся с
// диска (route.rule_set становится type:"local", тем же приёмом, что и
// встроенный комплект), а обновляются ФОНОМ, когда связь уже работает и
// торопиться некуда.
//
// Чем это отличается от встроенного комплекта. Комплект вшит в .exe, стареет
// от выпуска к выпуску и служит последней подстраховкой. Кеш — это свежие
// правила той же машины: их скачали при прошлом удачном подъёме, и они
// всегда новее комплекта.

// SrokSvezhesti — после какого возраста кеш стоит обновить фоном.
// Сутки: по замеру 23.08 ежедневно меняется только ads.srs, остальные файлы
// не менялись неделями.
const SrokSvezhesti = 24 * time.Hour

// PutKesha — папка кеша внутри папки ядра.
func PutKesha(papkaYadra string) string { return filepath.Join(papkaYadra, "pravila-kesh") }

// IzKesha — что лежит в кеше прямо сейчас: тег -> путь к файлу.
//
// Возвращает ТОЛЬКО полные наборы: если хоть одного тега из nuzhny не
// хватает, кеш не годится и возвращается nil. Половина правил хуже целого
// комплекта — ядро с недостающим набором просто не поднимется, а мы об этом
// узнаем позже всех.
func IzKesha(papkaYadra string, nuzhny []string) map[string]string {
	dir := PutKesha(papkaYadra)
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	itog := make(map[string]string, len(nuzhny))
	for _, teg := range nuzhny {
		put := filepath.Join(abs, teg+".srs")
		st, err := os.Stat(put)
		if err != nil || st.Size() == 0 {
			return nil
		}
		itog[teg] = put
	}
	return itog
}

// Ustarel — пора ли обновить кеш фоном. Пустой или неполный кеш — тоже «пора».
func Ustarel(papkaYadra string, nuzhny []string) bool {
	m := IzKesha(papkaYadra, nuzhny)
	if m == nil {
		return true
	}
	for _, put := range m {
		st, err := os.Stat(put)
		if err != nil || time.Since(st.ModTime()) > SrokSvezhesti {
			return true
		}
	}
	return false
}

// Obnovit качает наборы в кеш. Зовётся ФОНОМ и только когда связь уже есть:
// на старте ядра сеть занята собой (см. шапку файла).
//
// Пишем через временный файл и переименование: оборванная закачка не должна
// оставить в кеше половину набора — с ней ядро не поднимется, а выглядеть
// это будет как «правила есть».
func Obnovit(ctx context.Context, klient *http.Client, adresa map[string]string, papkaYadra string) (int, error) {
	dir := PutKesha(papkaYadra)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("папка кеша правил %q: %w", dir, err)
	}
	if klient == nil {
		klient = &http.Client{Timeout: 2 * time.Minute}
	}
	skachano := 0
	var posledn error
	for teg, adres := range adresa {
		if err := ctx.Err(); err != nil {
			return skachano, err
		}
		if err := odin(ctx, klient, adres, filepath.Join(dir, teg+".srs")); err != nil {
			posledn = fmt.Errorf("%s: %w", teg, err)
			continue
		}
		skachano++
	}
	return skachano, posledn
}

func odin(ctx context.Context, klient *http.Client, adres, cel string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adres, nil)
	if err != nil {
		return err
	}
	otvet, err := klient.Do(req)
	if err != nil {
		return err
	}
	defer otvet.Body.Close()
	if otvet.StatusCode != http.StatusOK {
		return fmt.Errorf("ответ %d", otvet.StatusCode)
	}
	vremenny := cel + ".kachaem"
	f, err := os.Create(vremenny)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, otvet.Body)
	zakryt := f.Close()
	if err == nil {
		err = zakryt
	}
	if err != nil {
		os.Remove(vremenny)
		return err
	}
	// Заголовок SRS — дешёвая проверка, что нам отдали набор правил, а не
	// страницу с ошибкой от чужого прокси или заглушку провайдера.
	if !pohozheNaSRS(vremenny) {
		os.Remove(vremenny)
		return fmt.Errorf("файл не похож на набор правил")
	}
	return os.Rename(vremenny, cel)
}

func pohozheNaSRS(put string) bool {
	f, err := os.Open(put)
	if err != nil {
		return false
	}
	defer f.Close()
	shapka := make([]byte, 3)
	if _, err := io.ReadFull(f, shapka); err != nil {
		return false
	}
	return strings.EqualFold(string(shapka), "SRS")
}
