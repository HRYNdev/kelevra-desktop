package proksi

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// imyaMetki — файл-метка «системный прокси на этот адрес поставили мы».
//
// Зачем он нужен. Snyat() убирает прокси только при мягком выходе: штатном
// завершении службы, панике, «Отключить», неудачном подключении. Жёсткая
// смерть процесса — Диспетчер задач, выключение или перезагрузка Windows
// (оконная сборка без консоли не получает SIGTERM), пропадание питания —
// ни один из этих путей не проходит: реестр остаётся с ProxyEnable=1 и нашим
// адресом, а снять его больше некому. Метка на диске переживает такую
// смерть и даёт следующему запуску окна (cmd/kelevra/main.go) увидеть,
// что прокси в системе — доказанно наш, и снять его самому.
const imyaMetki = "proksi.json"

func putMetki() string { return filepath.Join(hranenie.Papka(), imyaMetki) }

type metka struct {
	Adres string `json:"adres"`
}

// Otmetit запоминает, что системный прокси на adres поставили мы — своей
// рукой (Postavit) или рукой ядра, но с нашим подтверждением (Stoit). Ошибка
// записи не должна мешать защите: метка — это только подстраховка на случай
// жёсткой смерти процесса, не сама защита.
func Otmetit(adres string) {
	b, err := json.Marshal(metka{Adres: adres})
	if err != nil {
		return
	}
	if err := os.MkdirAll(hranenie.Papka(), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(putMetki(), b, 0o600)
}

// ProchestMetku читает метку, оставленную прошлым (возможно, уже мёртвым)
// запуском. Пустой адрес и false — метки нет.
func ProchestMetku() (string, bool) {
	b, err := os.ReadFile(putMetki())
	if err != nil {
		return "", false
	}
	var m metka
	if err := json.Unmarshal(b, &m); err != nil || m.Adres == "" {
		return "", false
	}
	return m.Adres, true
}

// UbratMetku удаляет метку. Вызывается из каждого Snyat(): после снятия
// прокси (или попытки снять то, чего уже нет) метке взяться неоткуда.
func UbratMetku() { _ = os.Remove(putMetki()) }
