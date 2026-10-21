package obnovlenie

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// PredelStranicRelizov — сколько страниц списка релизов обходить.
//
// GitHub отдаёт не больше 100 релизов за страницу. В репозитории на
// 21.10.2026 их 92, и единственная сборка ядра (core-v1.14.0-beta.4-1) стоит
// на 91-м месте: ещё несколько выпусков приложения — и она ушла бы на вторую
// страницу, а новые установки остались бы без ядра. Десять страниц — тысяча
// релизов; дальше обход обрывается, а не ходит бесконечно.
const PredelStranicRelizov = 10

// SobratStranicy собирает список релизов постранично: следующая страница —
// по заголовку Link (rel="next"), который отдаёт GitHub, и только на тот же
// сервер, что первая. Список стенда без Link — одна страница.
//
// Ошибки — как у Proverit: сетевая возвращается как есть, ответ не 200 —
// *OshibkaStatusa, не тот JSON — *OshibkaRazbora. predelBayt — потолок
// одной страницы.
func SobratStranicy[T any](ctx context.Context, klient *http.Client, adres string, predelBayt int64) ([]T, error) {
	nachalo, err := url.Parse(adres)
	if err != nil {
		return nil, err
	}
	var vse []T
	for stranica := 0; stranica < PredelStranicRelizov && adres != ""; stranica++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, adres, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		otvet, err := klient.Do(req)
		if err != nil {
			return nil, err
		}
		if otvet.StatusCode != http.StatusOK {
			otvet.Body.Close()
			return nil, &OshibkaStatusa{Kod: otvet.StatusCode}
		}
		var chast []T
		err = json.NewDecoder(io.LimitReader(otvet.Body, predelBayt)).Decode(&chast)
		sleduyushchaya := SleduyushchayaStranica(otvet.Header.Get("Link"))
		otvet.Body.Close()
		if err != nil {
			return nil, &OshibkaRazbora{Prichina: err}
		}
		vse = append(vse, chast...)
		adres = ""
		if sleduyushchaya != "" && len(chast) > 0 {
			if u, err := url.Parse(sleduyushchaya); err == nil && u.Scheme == nachalo.Scheme && u.Host == nachalo.Host {
				adres = u.String()
			}
		}
	}
	return vse, nil
}

// SleduyushchayaStranica — адрес rel="next" из заголовка Link GitHub:
// <https://…&page=2>; rel="next", <…>; rel="last". Пусто — страница последняя.
func SleduyushchayaStranica(link string) string {
	for _, chast := range strings.Split(link, ",") {
		adres, parametry, ok := strings.Cut(strings.TrimSpace(chast), ";")
		if !ok || !strings.Contains(parametry, `rel="next"`) {
			continue
		}
		adres = strings.TrimSpace(adres)
		if strings.HasPrefix(adres, "<") && strings.HasSuffix(adres, ">") {
			return adres[1 : len(adres)-1]
		}
	}
	return ""
}
