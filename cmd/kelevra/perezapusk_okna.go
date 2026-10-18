package main

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/vinsluzhba"
)

// Окно старой версии после обновления службы — перезапускается само (17.10).
//
// Под службой Windows обновление меняет .exe на диске и перезапускает только
// службу. Окно — отдельный процесс в сессии человека — сторож переводил на
// новый адрес (storozh_okna.go), и оно жило дальше: страница уже новой версии
// (её отдаёт служба), а привязки Go — старые. 17.10 так «Панель сети» в окне
// 0.6.73 после обновления до 0.6.74 открылась в системном браузере: у старого
// процесса нет kelevraPanel, а страница тогда уходила в window.open.
//
// Теперь, когда служба вернулась после молчания, окно спрашивает её версию и,
// если служба НОВЕЕ, закрывается и запускает себя заново тем же .exe (на диске
// он уже новый). Только «новее», а не «другая»: окно новее службы (человек
// запустил свежий файл из другой папки рядом со старой службой) перезапуском
// ничего не получит, а крутиться так могло бы бесконечно.

// srokVoprosaVersii — локальный HTTP; не ответила служба за это время — окно
// просто остаётся как есть, как и было до 17.10.
const srokVoprosaVersii = 3 * time.Second

// versiyaSluzhby — версия службы по её /api/sostoyanie (поле versiya). Пусто —
// не ответила или не сказала.
func versiyaSluzhby(adres string) string {
	klient := &http.Client{Timeout: srokVoprosaVersii}
	otvet, err := klient.Get(adres + "api/sostoyanie")
	if err != nil {
		return ""
	}
	defer otvet.Body.Close()
	if otvet.StatusCode != http.StatusOK {
		return ""
	}
	var telo struct {
		Versiya string `json:"versiya"`
	}
	if err := json.NewDecoder(otvet.Body).Decode(&telo); err != nil {
		return ""
	}
	return telo.Versiya
}

// nuzhenPerezapuskOkna — служба новее окна. Неизвестная версия службы — не
// повод: окно остаётся.
func nuzhenPerezapuskOkna(versiyaOkna, versiyaSluzhby string) bool {
	return versiyaSluzhby != "" && obnovlenie.Sravnit(versiyaSluzhby, versiyaOkna) > 0
}

// hvostObnovleniya — «.old», «.old1», «.old2»… в конце пути: так обновление
// отодвигает прежний файл (internal/obnovlenie: Postavit, svobodnoyeImyaHvosta).
var hvostObnovleniya = regexp.MustCompile(`(?i)\.old\d*$`)

// putUstanovlennoy — exe службы в Program Files ("" — его там нет). Подменяется
// в тестах: на машине разработчика служба может стоять по-настоящему.
var putUstanovlennoy = vinsluzhba.PutUstanovlennoy

// putDlyaNovoyKopii — какой файл запускать, когда окно или значок
// перезапускаются из-за того, что служба стала новее них.
//
// Служба живёт в Program Files и обновляет там свой exe; окно и значок могут
// быть запущены из Загрузок или другой папки, где лежит прежняя версия.
// Перезапуск «тем же файлом» дал бы ту же старую версию, служба снова была
// бы новее, и так по кругу. Поэтому, когда файл службы есть, берём его: он
// той же версии, что и служба. Нет его (служба не в Program Files, другая
// система) — прежнее правило putDlyaPerezapuska.
func putDlyaNovoyKopii(svoy string) string {
	if p := putUstanovlennoy(); p != "" {
		return p
	}
	return putDlyaPerezapuska(svoy)
}

// putDlyaPerezapuska — какой файл запускать взамен себя. Свой путь процесса
// после обновления может указывать уже на отодвинутый хвост (так ведёт себя
// Linux; на Windows путь обычно прежний, а под ним уже новый файл) — тогда
// берём путь без хвоста, где лежит новая версия. Хвост вида «Kelevra.old»
// (без .exe) — пробуем и «Kelevra.exe».
func putDlyaPerezapuska(svoy string) string {
	if !hvostObnovleniya.MatchString(svoy) {
		return svoy
	}
	bez := hvostObnovleniya.ReplaceAllString(svoy, "")
	for _, p := range []string{bez, bez + ".exe"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return svoy
}
