// Лже-ядро для stend/proksi.sh (сценарий 5): разыгрывает единственный кусок
// поведения настоящего sing-box, который под этим wine никогда не удаётся
// поймать живьём.
//
// Диагноз 23.08. Настоящее ядро на первой попытке («система не дала
// настроить прокси») успевает ЗАПИСАТЬ реестр (ProxyServer, ProxyEnable) ДО
// того, как упасть на notify-вызове (winapi error #12009) — см. sluzhba.go,
// комментарий у strings.Contains(err.Error(), "system proxy"). Из-за этого
// stend/proksi.sh сценарий 4 всегда застаёт internal/proksi.Stoit() уже
// true, а ветку Postavit() (реестр прописывает САМО приложение, а не ядро)
// живьём не проверяет ни разу. Это лже-ядро играет противоположный, тоже
// возможный на живой Windows случай: первая попытка падает строкой
// «system proxy», НЕ тронув реестр вовсе. Тогда после восстановления
// (BezSistemnogoProksi=true) реестр обязано прописать само приложение.
//
// Поведение, управляемое маркер-файлом popytka.marker в рабочей папке
// (та же -D, что дают настоящему ядру):
//
//	маркера нет  — создать его, написать в лог строку с «system proxy» и
//	               упасть кодом 1, реестра не касаясь;
//	маркер есть  — поднять на 127.0.0.1:9090 (тот же адрес Clash API, что
//	               несёт internal/konfig/testdata/profil_stend_bez_seti.json)
//	               http-сервер, отвечающий 200 на /version, и висеть, пока
//	               стенд его не убьёт.
//
// Второй маркер, tiho.marker (сценарий 8), играет ТИХИЙ отказ: ядро стартует
// с первого раза и без единой ошибки, но системный прокси в реестр не пишет —
// ни строки в лог, ни ненулевого кода выхода. Так выглядит любой отказ
// set_system_proxy, о котором ядро решило не кричать; приложению не на что
// опереться, кроме самого реестра.
//
// Третье применение — stend/zamer_skorosti.sh (кнопка «Проверить скорость»,
// ручка /api/zamerit, internal/sluzhba/sluzhba.go:zamerit). Ей нужен не
// системный прокси, а Clash API узлов: список групп (GET /proxies, читает
// его internal/yadro/uzly.go:Gruppy) и сам замер задержки конкретного узла
// (GET /proxies/<имя>/delay, uzly.go:Zamerit). Список строится прямо из
// config.json, который приложение кладёт рядом с ядром (тот же -D) — лже-ядро
// его не выдумывает, а честно разбирает outbounds, как это делает
// internal/yadro/uzly.go:GruppyStatik. Поведение замера каждого узла
// управляется файлом zamer_scen.json в той же папке (см. zamerScen ниже) —
// стенд переписывает его между сценами без перезапуска процесса.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const marker = "popytka.marker"
const markerTiho = "tiho.marker"
const clashAdres = "127.0.0.1:9090"

// zamerScen — сценарий ответа на замер задержки конкретного узла, читается
// заново на каждый запрос (не кешируется), чтобы стенд мог сменить поведение
// между вызовами /api/zamerit без перезапуска лже-ядра. Три поля — три формы
// ответа, которые обязан пережить хендлер zamerit (задача stend/zamer_skorosti.sh):
// узел ответил, узел явно отказал (400 + message), узел завис.
type zamerScen struct {
	Beda     map[string]string `json:"beda"`      // узел -> причина отказа (400 + {"message": ...})
	ZavisSek map[string]int    `json:"zavis_sek"` // узел -> сколько секунд молчать, прежде чем всё-таки ответить
	DelayMs  map[string]int    `json:"delay_ms"`  // узел -> задержка успешного ответа ({"delay": N})
}

func chitatZamerScen() zamerScen {
	var s zamerScen
	b, err := os.ReadFile("zamer_scen.json")
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	return s
}

func main() {
	if _, err := os.Stat(markerTiho); err == nil {
		fmt.Fprintln(os.Stderr, "лже-ядро: тихий успех — стартую с первой попытки, реестра не касаюсь")
		sluzhit()
		return
	}
	if _, err := os.Stat(marker); err != nil {
		if err := os.WriteFile(marker, []byte("1"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "лже-ядро: не записал маркер:", err)
		}
		fmt.Fprintln(os.Stderr, "start inbound/mixed[mixed-in]: set system proxy: "+
			"InternetSetOption(ProxySettingsChanged): winapi error #12009 (лже-ядро, попытка 1)")
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "лже-ядро: попытка 2, поднимаю Clash API на "+clashAdres)
	sluzhit()
}

// sluzhit поднимает то единственное, по чему приложение считает ядро живым, —
// Clash API на том же адресе, что несёт профиль стенда.
func sluzhit() {
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"version":"lzhe-1.0"}`)
	})
	mux.HandleFunc("GET /proxies", proxiesSpisok)
	mux.HandleFunc("GET /proxies/{name}/delay", proxiesDelay)
	if err := http.ListenAndServe(clashAdres, mux); err != nil {
		fmt.Fprintln(os.Stderr, "лже-ядро: не поднял Clash API:", err)
		os.Exit(1)
	}
}

// outboundCfg — тот же кусок формы конфига sing-box, что разбирает
// internal/yadro/uzly.go:GruppyStatik: тип узла, его имя, его варианты (для
// групп-переключателей) и узел по умолчанию.
type outboundCfg struct {
	Type      string   `json:"type"`
	Tag       string   `json:"tag"`
	Outbounds []string `json:"outbounds"`
	Default   string   `json:"default"`
}

// proxiesSpisok отвечает на GET /proxies — то же самое, что у настоящего
// Clash API (internal/yadro/uzly.go:Gruppy это парсит): карта всех outbound
// из config.json, который приложение кладёт рядом с ядром до его запуска.
func proxiesSpisok(w http.ResponseWriter, r *http.Request) {
	b, err := os.ReadFile("config.json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "лже-ядро: не прочитал config.json: " + err.Error()})
		return
	}
	var d struct {
		Outbounds []outboundCfg `json:"outbounds"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "лже-ядро: config.json битый: " + err.Error()})
		return
	}
	proxies := make(map[string]any, len(d.Outbounds))
	for _, o := range d.Outbounds {
		seychas := o.Default
		if seychas == "" && len(o.Outbounds) > 0 {
			seychas = o.Outbounds[0]
		}
		vse := o.Outbounds
		if vse == nil {
			vse = []string{}
		}
		proxies[o.Tag] = map[string]any{
			"type":    o.Type,
			"now":     seychas,
			"all":     vse,
			"history": []any{},
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"proxies": proxies})
}

// proxiesDelay отвечает на GET /proxies/<имя>/delay — сам замер (Zamerit,
// internal/yadro/uzly.go:136-165). Три формы ответа управляются
// zamer_scen.json (см. zamerScen выше): беда с причиной, зависание без
// ответа (пока не отвалится сам клиент — Zamerit поднимает http.Client.Timeout
// до 6с, uzly.go:146) или обычный успешный замер.
func proxiesDelay(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	scen := chitatZamerScen()
	if msg, mertv := scen.Beda[name]; mertv {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": msg})
		return
	}
	if sek, visit := scen.ZavisSek[name]; visit {
		select {
		case <-time.After(time.Duration(sek) * time.Second):
			// Отвечаем поздно — приложение почти наверняка уже отвалилось
			// по своему клиентскому таймауту (6с), но дожить до конца
			// стоит: реальный узел так же может ответить позже, чем клиент
			// готов ждать, и мы не хотим течь горутинами лже-ядра.
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]int{"delay": 1})
		case <-r.Context().Done():
			// Клиент (или сам стенд через --max-time) ушёл раньше — отвечать некому.
		}
		return
	}
	// scen.DelayMs[name] явно нулём (сцена «а», порча --kontrol=a) — не то же
	// самое, что имя вообще не упомянуто: значение по умолчанию (35) нужно
	// только во втором случае, а карта map[string]int не различает их без ", ok".
	d, zadano := scen.DelayMs[name]
	if !zadano {
		d = 35
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"delay": d})
}
