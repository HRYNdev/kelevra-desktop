// Пакет kopiya: приложение на компьютере должно быть одно.
//
// Пользователь запускает .exe двойным щелчком и не видит, работает ли уже
// первая копия: окна может не быть на переднем плане. Вторая копия подняла бы
// второе ядро, оно взяло бы те же порты (Clash API, прокси) и упало бы —
// человек увидел бы «ошибка» на ровном месте. Поэтому вторая копия не
// поднимает ничего своего, а открывает окно уже работающей.
//
// Признак живой копии — не PID: номера процессов после перезагрузки
// переиспользуются, и метка от давно умершей копии указала бы на чужой
// процесс. Живость проверяется тем же способом, каким её видит пользователь:
// отвечает ли адрес окна. Адрес содержит случайный ключ этого запуска,
// поэтому подделать его посторонним процессом нельзя.
package kopiya

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Skolko ждём ответа от уже запущенной копии. Это петля 127.0.0.1: живая
// копия отвечает за миллисекунды, а мёртвый порт сразу даёт отказ соединения.
const Skolko = 1500 * time.Millisecond

type zapis struct {
	URL   string `json:"url"`
	PID   int    `json:"pid"`
	Kogda string `json:"kogda"`
}

// Metka — файл, которым запущенная копия отмечает себя.
func Metka(papka string) string { return filepath.Join(papka, "zapushcheno.json") }

// Nayti возвращает адрес окна уже работающей копии, если она жива.
func Nayti(papka string) (string, bool) {
	b, err := os.ReadFile(Metka(papka))
	if err != nil {
		return "", false
	}
	var z zapis
	if err := json.Unmarshal(b, &z); err != nil || z.URL == "" {
		return "", false
	}
	if !otvechaet(z.URL) {
		return "", false
	}
	return z.URL, true
}

// otvechaet — жив ли адрес. Любая ошибка и любой код, кроме 2xx, считаются
// «копии нет»: лучше поднять свою, чем открыть окно в никуда.
func otvechaet(url string) bool {
	klient := &http.Client{Timeout: Skolko}
	otvet, err := klient.Get(url)
	if err != nil {
		return false
	}
	defer otvet.Body.Close()
	return otvet.StatusCode >= 200 && otvet.StatusCode < 300
}

// Zanyat отмечает эту копию как работающую. Ошибка записи не должна мешать
// приложению: без метки оно просто теряет защиту от второго запуска.
func Zanyat(papka, url string, teper time.Time) error {
	if err := os.MkdirAll(papka, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(zapis{URL: url, PID: os.Getpid(), Kogda: teper.Format(time.RFC3339)})
	if err != nil {
		return err
	}
	vremenny := Metka(papka) + ".tmp"
	if err := os.WriteFile(vremenny, b, 0o600); err != nil {
		return err
	}
	return os.Rename(vremenny, Metka(papka))
}

// Osvobodit убирает метку при нормальном выходе. Если копия упала и метка
// осталась, беды нет: следующий запуск увидит, что адрес не отвечает.
func Osvobodit(papka string) { _ = os.Remove(Metka(papka)) }
