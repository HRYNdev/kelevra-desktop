// Пакет konfig: превращает профиль, пришедший с сервера подписки, в конфиг,
// с которым ядро действительно поднимется на этом компьютере.
//
// Зачем он нужен. Сервер отдаёт один и тот же профиль всем клиентам, а писался
// он под телефон: там есть поля, которые ядро принимает ТОЛЬКО на Android
// (`route.override_android_vpn`, `exclude_package`, `platform.http_proxy`).
// Проверено настоящим ядром: с профилем как есть `sing-box check` падает
// строкой «`override_android_vpn` is only supported on Android» — то есть на
// компьютере ядро не стартует вообще, сколько на кнопку ни жми.
//
// Второе. Профиль поднимает два входа: туннель `tun-in` (весь трафик машины) и
// локальный прокси `mixed-in`. Туннелю на Windows нужны права администратора.
// Без прав остаётся прокси — но тогда система должна о нём знать, иначе ядро
// работает, а трафик идёт мимо. Поэтому в прокси-режиме мы просим ядро
// прописать себя системным прокси (`set_system_proxy`).
//
// Третье. Адрес Clash API у профиля свой (`experimental.clash_api`), и жёстко
// зашитый в приложении адрес его не угадает: приложение решит, что ядро мертво,
// и убьёт живой процесс. Адрес берём из профиля.
package konfig

import (
	"encoding/json"
	"fmt"
)

// Rezhim — как именно защищён трафик.
type Rezhim string

const (
	// Tunnel — весь трафик машины идёт через ядро. Нужны права администратора.
	Tunnel Rezhim = "tunnel"
	// Proksi — ядро стоит системным прокси. Прав не нужно, но защищены только
	// программы, которые системный прокси уважают (браузеры — да, часть игр — нет).
	Proksi Rezhim = "proksi"
)

// Kartina — что приложение знает о подготовленном конфиге.
type Kartina struct {
	Rezhim        Rezhim `json:"rezhim"`
	ClashAdres    string `json:"clash_adres"`    // где ядро поднимет Clash API
	ClashSekret   string `json:"clash_sekret"`   // пароль к нему, если задан
	ProksiAdres   string `json:"proksi_adres"`   // адрес локального прокси, если есть
	EstTunnel     bool   `json:"est_tunnel"`     // есть ли в профиле вход-туннель
	RuchnoyProksi bool   `json:"ruchnoy_proksi"` // прокси придётся прописать в системе руками
	Zametka       string `json:"zametka"`        // человеку: почему режим такой
}

// ClashPoUmolchaniyu — адрес, если профиль про Clash API молчит.
const ClashPoUmolchaniyu = "127.0.0.1:9090"

// Поля профиля, которые ядро принимает только на Android. На компьютере они
// либо валят старт, либо не значат ничего.
var androidPolyaTun = []string{"exclude_package", "include_package", "platform"}

// Vybor — то, что решает не профиль, а машина, на которой он запускается.
type Vybor struct {
	// Prava — есть ли у приложения права администратора. Нет прав — туннель не
	// поднять, и врать об этом нельзя: молча оставить туннель значит отдать
	// пользователю ядро, которое упадёт при старте.
	Prava bool
	// BezSistemnogoProksi — не просить ядро прописывать себя системным прокси.
	// Взводится после отказа системы: проверено живьём — ядро на такой отказ
	// не жалуется, а ПАДАЕТ («initialize system proxy: unsupported desktop
	// environment»), и человек остаётся вообще без связи вместо половинной защиты.
	BezSistemnogoProksi bool
}

// Prigotovit готовит профиль под эту машину.
func Prigotovit(syroy []byte, v Vybor) ([]byte, Kartina, error) {
	estPrava := v.Prava
	var d map[string]any
	if err := json.Unmarshal(syroy, &d); err != nil {
		return nil, Kartina{}, fmt.Errorf("профиль не разобрать: %w", err)
	}

	if r, ok := d["route"].(map[string]any); ok {
		delete(r, "override_android_vpn")
	}

	vhody, _ := d["inbounds"].([]any)
	k := Kartina{}
	var ostavshiesya []any
	var udalennyeTegi []string

	for _, v := range vhody {
		vh, ok := v.(map[string]any)
		if !ok {
			ostavshiesya = append(ostavshiesya, v)
			continue
		}
		tip, _ := vh["type"].(string)
		teg, _ := vh["tag"].(string)
		switch tip {
		case "tun":
			k.EstTunnel = true
			for _, p := range androidPolyaTun {
				delete(vh, p)
			}
			if !estPrava {
				udalennyeTegi = append(udalennyeTegi, teg)
				continue // вход выбрасываем целиком
			}
		case "mixed", "http", "socks":
			if k.ProksiAdres == "" {
				k.ProksiAdres = adresVhoda(vh)
			}
		}
		ostavshiesya = append(ostavshiesya, vh)
	}

	if estPrava && k.EstTunnel {
		k.Rezhim = Tunnel
		k.Zametka = "весь трафик компьютера идёт через Kelevra"
	} else {
		k.Rezhim = Proksi
		if k.ProksiAdres == "" {
			return nil, k, fmt.Errorf("в профиле нет ни туннеля с правами, ни локального прокси — подключаться нечем")
		}
		// Ядро само пропишет себя системным прокси: иначе оно работает,
		// а система об этом не знает и трафик идёт мимо.
		for _, vh := range ostavshiesya {
			m, ok := vh.(map[string]any)
			if !ok {
				continue
			}
			if tip, _ := m["type"].(string); tip == "mixed" || tip == "http" {
				m["set_system_proxy"] = !v.BezSistemnogoProksi
				break
			}
		}
		k.RuchnoyProksi = v.BezSistemnogoProksi
		if v.BezSistemnogoProksi {
			k.Zametka = "система не дала настроить прокси сама: пропишите в её настройках " + k.ProksiAdres
		} else if k.EstTunnel {
			k.Zametka = "прокси-режим: нет прав администратора, туннель не поднять"
		} else {
			k.Zametka = "прокси-режим: в профиле нет туннеля"
		}
	}

	d["inbounds"] = ostavshiesya
	if len(udalennyeTegi) > 0 {
		pochistitPravila(d, udalennyeTegi)
	}
	k.ClashAdres, k.ClashSekret = clash(d)
	zapomnitVybor(d)

	gotovyy, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, k, err
	}
	return gotovyy, k, nil
}

// Razobrat — только прочитать профиль, ничего не меняя: нужно, когда конфиг
// уже лежит на диске и надо знать, куда стучаться за состоянием.
func Razobrat(telo []byte) (Kartina, error) {
	var d map[string]any
	if err := json.Unmarshal(telo, &d); err != nil {
		return Kartina{}, err
	}
	k := Kartina{}
	vhody, _ := d["inbounds"].([]any)
	for _, v := range vhody {
		vh, ok := v.(map[string]any)
		if !ok {
			continue
		}
		switch tip, _ := vh["type"].(string); tip {
		case "tun":
			k.EstTunnel, k.Rezhim = true, Tunnel
		case "mixed", "http", "socks":
			if k.ProksiAdres == "" {
				k.ProksiAdres = adresVhoda(vh)
			}
		}
	}
	if k.Rezhim == "" {
		k.Rezhim = Proksi
	}
	k.ClashAdres, k.ClashSekret = clash(d)
	return k, nil
}

// pochistitPravila убирает выброшенные входы из правил маршрутизации: правило,
// которое ссылается на несуществующий вход, ядро не примет.
func pochistitPravila(d map[string]any, tegi []string) {
	r, ok := d["route"].(map[string]any)
	if !ok {
		return
	}
	pravila, ok := r["rules"].([]any)
	if !ok {
		return
	}
	udaleno := map[string]bool{}
	for _, t := range tegi {
		if t != "" {
			udaleno[t] = true
		}
	}
	var ostatok []any
	for _, p := range pravila {
		pr, ok := p.(map[string]any)
		if !ok {
			ostatok = append(ostatok, p)
			continue
		}
		spisok, est := pr["inbound"]
		if !est {
			ostatok = append(ostatok, pr)
			continue
		}
		var ostalis []any
		switch v := spisok.(type) {
		case string:
			if !udaleno[v] {
				ostalis = append(ostalis, v)
			}
		case []any:
			for _, s := range v {
				if str, ok := s.(string); ok && udaleno[str] {
					continue
				}
				ostalis = append(ostalis, s)
			}
		}
		if len(ostalis) == 0 {
			continue // правило было только про выброшенный вход — выбрасываем и его
		}
		pr["inbound"] = ostalis
		ostatok = append(ostatok, pr)
	}
	r["rules"] = ostatok
}

func clash(d map[string]any) (adres, sekret string) {
	adres = ClashPoUmolchaniyu
	e, ok := d["experimental"].(map[string]any)
	if !ok {
		return adres, ""
	}
	c, ok := e["clash_api"].(map[string]any)
	if !ok {
		return adres, ""
	}
	if s, _ := c["external_controller"].(string); s != "" {
		adres = s
	}
	sekret, _ = c["secret"].(string)
	return adres, sekret
}

func adresVhoda(vh map[string]any) string {
	host, _ := vh["listen"].(string)
	if host == "" {
		host = "127.0.0.1"
	}
	port, _ := vh["listen_port"].(float64)
	if port == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", host, int(port))
}

// zapomnitVybor велит ядру хранить выбранный человеком узел между запусками.
// Хранилище — cache_file; в профиле хозяина оно есть, но у чужого профиля может
// не быть, а без него выбор из окна сбрасывается на серверный по умолчанию при
// каждом перезапуске ядра — а ядро мы перезапускаем сами при смене режима.
//
// Отдельного выключателя `store_selected` тут БОЛЬШЕ НЕТ, и это не забывчивость:
// ядро (sing-box 1.14) на такое поле не ругается, а ПАДАЕТ строкой
// «unknown field "store_selected"» — поймано живым прогоном, не тестом.
// В этой версии выбор хранится сам, как только включён cache_file.
func zapomnitVybor(d map[string]any) {
	e, ok := d["experimental"].(map[string]any)
	if !ok {
		e = map[string]any{}
		d["experimental"] = e
	}
	c, ok := e["cache_file"].(map[string]any)
	if !ok {
		c = map[string]any{"path": "cache.db"}
		e["cache_file"] = c
	}
	c["enabled"] = true
}
