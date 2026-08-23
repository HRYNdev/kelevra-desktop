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
package main

import (
	"fmt"
	"net/http"
	"os"
)

const marker = "popytka.marker"
const clashAdres = "127.0.0.1:9090"

func main() {
	if _, err := os.Stat(marker); err != nil {
		if err := os.WriteFile(marker, []byte("1"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "лже-ядро: не записал маркер:", err)
		}
		fmt.Fprintln(os.Stderr, "start inbound/mixed[mixed-in]: set system proxy: "+
			"InternetSetOption(ProxySettingsChanged): winapi error #12009 (лже-ядро, попытка 1)")
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "лже-ядро: попытка 2, поднимаю Clash API на "+clashAdres)
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"version":"lzhe-1.0"}`)
	})
	if err := http.ListenAndServe(clashAdres, mux); err != nil {
		fmt.Fprintln(os.Stderr, "лже-ядро: не поднял Clash API:", err)
		os.Exit(1)
	}
}
