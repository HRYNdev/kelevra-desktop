package konfig

import (
	"net/netip"
	"strings"
)

// Узлы своей сети — без DNS: резолвер выхода узла — hosts профиля.
//
// Живой случай 07.11.2026 (ноут, после выключения и включения Wi-Fi):
// полторы минуты «lookup node2kv…: context deadline exceeded», пока ядро не
// перезапустили. Имена узлов ядро разрешало сервером "local" (type local,
// системный список DNS): у выходов узлов своего domain_resolver нет, и набор
// берёт route.default_domain_resolver — с явным транспортом, мимо dns.rules
// (sing-box 1.14.1: common/dialer/dialer.go:110-115, dns/router.go:1291-1296).
// Поэтому сервер "svoi-imena" (type hosts, в predefined — адреса узлов),
// который сервер кладёт в профиль, и правило dns.rules «имена узлов →
// svoi-imena» самому ядру при наборе узла не помогали: они работают только
// для запросов программ через hijack-dns. А список DNS у "local" на Windows —
// снимок, который после смены сети может застрять на 127.0.0.1:53
// (dns/transport/local/systemconfig/source_windows.go).
//
// Здесь каждому выходу, у которого server — имя, а адрес этого имени есть в
// hosts-сервере профиля, ставится "domain_resolver": "<тег hosts>". Сервер
// hosts отвечает из памяти (dns/transport/hosts/hosts.go: Exchange →
// FixedResponse по predefined), без сети и без системного DNS: набор узла
// больше не зависит ни от состояния резолвера после смены сети, ни от того,
// режут ли DNS. Адреса — «одна истина» сервера и приезжают с профилем.
//
// Не трогаем: выход со своим domain_resolver (решение сервера), выход с
// detour (имя тогда разрешает выход-посредник), группы и direct/block/dns,
// эндпоинты («Дом» идёт через detour). Имени нет в hosts или hosts-сервера
// нет вовсе — выход как был: профили без svoi-imena не ломаются.

// TegSvoihImen — тег hosts-сервера с адресами своих имён в профиле.
const TegSvoihImen = "svoi-imena"

// neVyhodUzla — выходы, у которых нет своего набора до сервера.
var neVyhodUzla = map[string]bool{
	"direct": true, "block": true, "dns": true, "selector": true, "urltest": true,
}

// uzlamResolverIzProfilya — см. шапку файла. Возвращает, скольким выходам
// поставлен резолвер.
func uzlamResolverIzProfilya(d map[string]any) int {
	imena := imenaHosts(d)
	if len(imena) == 0 {
		return 0
	}
	vyhody, _ := d["outbounds"].([]any)
	n := 0
	for _, v := range vyhody {
		vh, ok := v.(map[string]any)
		if !ok {
			continue
		}
		tip, _ := vh["type"].(string)
		if neVyhodUzla[strings.ToLower(tip)] {
			continue
		}
		if _, est := vh["domain_resolver"]; est {
			continue
		}
		if detour, _ := vh["detour"].(string); detour != "" {
			continue
		}
		server, _ := vh["server"].(string)
		imya := kanonImeni(server)
		if imya == "" {
			continue
		}
		if _, err := netip.ParseAddr(imya); err == nil {
			continue // уже адрес
		}
		teg, est := imena[imya]
		if !est {
			continue
		}
		vh["domain_resolver"] = teg
		n++
	}
	return n
}

// imenaHosts — имя → тег hosts-сервера, у которого для имени есть хоть один
// годный адрес. Несколько hosts-серверов знают одно имя — берётся
// TegSvoihImen, иначе первый по порядку.
func imenaHosts(d map[string]any) map[string]string {
	dn, ok := d["dns"].(map[string]any)
	if !ok {
		return nil
	}
	servery, _ := dn["servers"].([]any)
	imena := map[string]string{}
	for _, sv := range servery {
		m, ok := sv.(map[string]any)
		if !ok {
			continue
		}
		if tip, _ := m["type"].(string); tip != "hosts" {
			continue
		}
		teg, _ := m["tag"].(string)
		if teg == "" {
			continue
		}
		pred, _ := m["predefined"].(map[string]any)
		for imya, adresa := range pred {
			k := kanonImeni(imya)
			if k == "" || !estGodnyyAdres(adresa) {
				continue
			}
			if byl, est := imena[k]; est && byl == TegSvoihImen {
				continue
			} else if est && teg != TegSvoihImen {
				continue
			}
			imena[k] = teg
		}
	}
	return imena
}

// estGodnyyAdres — в значении predefined (строка или список строк) есть
// разбираемый ненулевой адрес. Нулевой (0.0.0.0, ::) — запись-блокировка, не
// адрес узла.
func estGodnyyAdres(v any) bool {
	var spisok []string
	switch a := v.(type) {
	case string:
		spisok = []string{a}
	case []any:
		for _, x := range a {
			if s, ok := x.(string); ok {
				spisok = append(spisok, s)
			}
		}
	}
	for _, s := range spisok {
		if ip, err := netip.ParseAddr(strings.TrimSpace(s)); err == nil && !ip.IsUnspecified() {
			return true
		}
	}
	return false
}

func kanonImeni(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
}
