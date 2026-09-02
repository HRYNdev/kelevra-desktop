// DNS-часть подготовки конфига: fakeip и резолвер прямого выхода.
//
// Почему это отдельный файл, а не ещё сто строк в konfig.go. Всё остальное в
// пакете решает один вопрос — какие ВХОДЫ останутся в конфиге. Здесь решается
// другой: как в каждом из двух режимов должен вести себя DNS, чтобы конфиг был
// согласован сам с собой. Ошибка тут не роняет ядро громко, она даёт человеку
// «ядро работает, а сайты не открываются» — самую дорогую породу беды, потому
// что искать её человек будет где угодно, только не в DNS.
//
// Разбор 31.08, две находки на боевом профиле.
//
// Первая — fakeip. В профиле есть сервер {"type":"fakeip"} и правило
// dns.rules, посылающее к нему два десятка rule_set (youtube, telegram,
// meta…). Смысл fakeip такой: на запрос про youtube.com ядро отвечает
// выдуманным адресом из 198.18.0.0/15, дожидается, когда программа на него
// постучится, узнаёт по адресу исходный домен и уводит соединение в туннель,
// не резолвя домен по-настоящему нигде. Работает это ровно при одном
// условии: и запрос, и последующее соединение приходят В ЯДРО. С туннелем так
// и есть. В режиме системного прокси — нет: DNS Windows делает мимо ядра
// (прокси за резолв не отвечает), и в базе ядра боевой машины не нашлось ни
// одной записи 198.18 — fakeip там не срабатывал вовсе. Мина в том, что он
// может сработать НАПОЛОВИНУ: программа, спросившая DNS через сам прокси
// (socks5 udp), получает 198.18.x.x, а потом идёт по этому адресу напрямую,
// мимо прокси, — и попадает в никуда. Поэтому в режиме прокси fakeip
// обезвреживается, а не оставляется «на всякий случай».
//
// Вторая — резолвер прямого выхода. route.final в профиле "direct", а
// route.default_domain_resolver указывает на сервер "local", который на самом
// деле {"type":"tcp","server":"1.1.1.1"}. Всё, что не попало ни в один
// rule_set, идёт напрямую, и чтобы туда пойти, ядру надо сперва отрезолвить
// домен ЭТИМ сервером. В журнале ядра на боевой машине резолвер умирает:
// «lookup www.bing.com: exchange4: use of closed network connection». Одна
// мёртвая TCP-сессия к 1.1.1.1 — и половина интернета перестаёт открываться,
// хотя «подключено» горит зелёным.
package konfig

// soglasovatFakeip — режим туннеля: fakeip тут ОЖИВАЕТ, и связка обязана быть
// целой с обеих сторон.
//
// Целая связка — это две вещи разом: (1) сервер fakeip есть и на него кто-то
// ссылается из dns.rules, (2) DNS-запросы программ вообще доходят до ядра, то
// есть в route.rules стоит правило hijack-dns. Без второго половина связки
// висит вхолостую: Windows спрашивает свой резолвер, ядро об этом не знает,
// а выдуманные адреса не выдаются никогда.
//
// Если fakeip в профиле есть, а ссылаться на него уже некому (так бывает
// после BezSetevyhPravil — та выбрасывает из dns.rules всё с rule_set),
// сервер убирается совсем. Это не уборка ради красоты: оставленный без
// ссылок fakeip — заряженная мина, которая оживёт от любого будущего правила,
// написанного не глядя.
func soglasovatFakeip(d map[string]any) {
	teg := tegFakeip(d)
	if teg == "" {
		return
	}
	if !naFakeipSsylayutsya(d, teg) {
		ubratServerDns(d, teg)
		// Сервера больше нет — хранить его карту между запусками не для кого.
		// Половина связки, оставленная включённой, это ровно та мина, ради
		// которой вся функция и написана: cache_file продолжит переживать
		// между запусками адреса 198.18, которые теперь никто не выдаёт и
		// никто не разворачивает обратно в домен.
		pogasitHranenieFakeip(d)
		return
	}
	if !estHijackDns(d) {
		dobavitHijackDns(d)
	}
}

// pogasitHranenieFakeip выключает experimental.cache_file.store_fakeip.
// Общая для обоих режимов: в прокси-режиме fakeip обезврежен целиком
// (obezvreditFakeip), в туннельном — выброшен за ненадобностью, когда на
// него перестали ссылаться; в обоих случаях хранить его карту нечему.
func pogasitHranenieFakeip(d map[string]any) {
	e, ok := d["experimental"].(map[string]any)
	if !ok {
		return
	}
	c, ok := e["cache_file"].(map[string]any)
	if !ok {
		return
	}
	c["store_fakeip"] = false
}

// obezvreditFakeip — режим прокси: fakeip обязан не сработать ВООБЩЕ, ни
// целиком, ни наполовину (почему — см. шапку файла).
//
// Не «удалить сервер и всё»: правила dns.rules ссылаются на него по тегу, и
// ядро на ссылку в никуда не запустится. Поэтому сперва каждая такая ссылка
// переводится на обычный резолвер (тот, что и так стоит в dns.final), и
// только потом сам сервер уходит. Домены, которые в туннельном режиме
// получали выдуманный адрес, в прокси-режиме резолвятся как все остальные —
// это ровно то, что и должно быть: маршрутизация в прокси-режиме решается по
// домену из CONNECT, а не по подменённому адресу.
//
// store_fakeip у cache_file гасится тем же движением: без него ядро продолжит
// копить и переживать между запусками карту выдуманных адресов, которой в
// этом режиме неоткуда взяться правильно.
func obezvreditFakeip(d map[string]any) {
	teg := tegFakeip(d)
	if teg == "" {
		return
	}
	zamena := obychnyyResolver(d, teg)
	if zamena == "" {
		// Переводить ссылки не на что: трогать конфиг опаснее, чем оставить
		// его как есть — ядро хотя бы запустится. Такого профиля у нас нет,
		// но молча сломать чужой мы права не имеем.
		return
	}
	perebratDnsPravila(d, func(pr map[string]any) {
		if srv, _ := pr["server"].(string); srv == teg {
			pr["server"] = zamena
		}
	})
	ubratServerDns(d, teg)
	pogasitHranenieFakeip(d)
}

// pryamoyResolverCherezSistemu — режим прокси: домены прямого выхода резолвит
// САМА WINDOWS, а не зашитый в профиль 1.1.1.1 по TCP.
//
// Почему именно так, а не «переставить route.final в туннель». Оба варианта
// чинят симптом, но стоят разного. Перевод final в туннель отправил бы туда
// весь неопознанный трафик, включая российские сайты и банки, которые профиль
// нарочно держит прямыми (route.rules с ip_is_private и rule_set
// russia_inside — не случайность), а от главной беды прокси-режима — что UDP
// идёт мимо — не спас бы всё равно. Замена резолвера чинит именно то, что
// сломано: в прокси-режиме туннеля нет, системный резолвер НЕ перехвачен, и
// это по определению тот самый резолвер, который на этой сети работает —
// включая домашний роутер, который сам делает обход, гостевые сети с
// порталом и корпоративные DNS. Зашитый tcp://1.1.1.1, наоборот, ровно та
// мишень, которую провайдер рубит первой; когда он падает, вместе с ним
// падает всё, что не попало ни в один rule_set.
//
// В режиме туннеля этого делать НЕЛЬЗЯ, и функция там не зовётся: с поднятым
// tun системный резолвер сам завёрнут в ядро, и спрашивать его — замкнуть
// петлю на себя.
func pryamoyResolverCherezSistemu(d map[string]any) {
	teg := pryamoyResolverTeg(d)
	if teg == "" {
		return
	}
	dns, ok := d["dns"].(map[string]any)
	if !ok {
		return
	}
	servery, _ := dns["servers"].([]any)
	for i, sv := range servery {
		m, ok := sv.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := m["tag"].(string); t != teg {
			continue
		}
		if tip, _ := m["type"].(string); tip == "local" {
			return // уже системный, трогать нечего
		}
		// Новый объект целиком, а не правка старого: у сервера типа tcp
		// остаются поля (server, server_port, detour), которых тип local не
		// знает, и ядро на них ругается. Тег сохраняем — на него ссылаются
		// dns.final, dns.rules и route.default_domain_resolver.
		servery[i] = map[string]any{"tag": teg, "type": "local"}
		return
	}
}

// tegFakeip — тег DNS-сервера типа fakeip, "" если такого нет.
func tegFakeip(d map[string]any) string {
	dns, ok := d["dns"].(map[string]any)
	if !ok {
		return ""
	}
	servery, _ := dns["servers"].([]any)
	for _, sv := range servery {
		m, ok := sv.(map[string]any)
		if !ok {
			continue
		}
		if tip, _ := m["type"].(string); tip == "fakeip" {
			teg, _ := m["tag"].(string)
			return teg
		}
	}
	return ""
}

// naFakeipSsylayutsya — ссылается ли хоть одно правило dns.rules на этот тег.
func naFakeipSsylayutsya(d map[string]any, teg string) bool {
	est := false
	perebratDnsPravila(d, func(pr map[string]any) {
		if srv, _ := pr["server"].(string); srv == teg {
			est = true
		}
	})
	return est
}

// obychnyyResolver — на какой тег переводить ссылки, отобранные у fakeip:
// сперва dns.final (в боевом профиле — «local»), иначе первый попавшийся
// сервер, который не сам fakeip.
func obychnyyResolver(d map[string]any, krome string) string {
	dns, ok := d["dns"].(map[string]any)
	if !ok {
		return ""
	}
	if f, _ := dns["final"].(string); f != "" && f != krome {
		return f
	}
	servery, _ := dns["servers"].([]any)
	for _, sv := range servery {
		m, ok := sv.(map[string]any)
		if !ok {
			continue
		}
		if teg, _ := m["tag"].(string); teg != "" && teg != krome {
			return teg
		}
	}
	return ""
}

// pryamoyResolverTeg — каким сервером ядро резолвит домены, уходящие в direct.
// Сперва route.default_domain_resolver (строкой или объектом — sing-box
// принимает обе формы), иначе dns.final.
func pryamoyResolverTeg(d map[string]any) string {
	if r, ok := d["route"].(map[string]any); ok {
		switch v := r["default_domain_resolver"].(type) {
		case string:
			if v != "" {
				return v
			}
		case map[string]any:
			if srv, _ := v["server"].(string); srv != "" {
				return srv
			}
		}
	}
	if dns, ok := d["dns"].(map[string]any); ok {
		if f, _ := dns["final"].(string); f != "" {
			return f
		}
	}
	return ""
}

// ubratServerDns выбрасывает DNS-сервер с таким тегом из dns.servers.
func ubratServerDns(d map[string]any, teg string) {
	dns, ok := d["dns"].(map[string]any)
	if !ok {
		return
	}
	servery, ok := dns["servers"].([]any)
	if !ok {
		return
	}
	ostatok := make([]any, 0, len(servery))
	for _, sv := range servery {
		if m, ok := sv.(map[string]any); ok {
			if t, _ := m["tag"].(string); t == teg {
				continue
			}
		}
		ostatok = append(ostatok, sv)
	}
	dns["servers"] = ostatok
}

// perebratDnsPravila зовёт f на каждом правиле dns.rules, включая вложенные в
// logical-правила: ссылка на сервер может лежать и там.
func perebratDnsPravila(d map[string]any, f func(map[string]any)) {
	dns, ok := d["dns"].(map[string]any)
	if !ok {
		return
	}
	pravila, _ := dns["rules"].([]any)
	var obhod func([]any)
	obhod = func(spisok []any) {
		for _, p := range spisok {
			pr, ok := p.(map[string]any)
			if !ok {
				continue
			}
			f(pr)
			if vlozh, ok := pr["rules"].([]any); ok {
				obhod(vlozh)
			}
		}
	}
	obhod(pravila)
}

// estHijackDns — доходят ли DNS-запросы программ до ядра вообще.
func estHijackDns(d map[string]any) bool {
	r, ok := d["route"].(map[string]any)
	if !ok {
		return false
	}
	pravila, _ := r["rules"].([]any)
	for _, p := range pravila {
		pr, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if act, _ := pr["action"].(string); act == "hijack-dns" {
			return true
		}
	}
	return false
}

// dobavitHijackDns ставит перехват DNS первым правилом маршрута — той же
// формы, что стоит в боевом профиле (порт 53 ИЛИ опознанный сниффом DNS).
// Первым, потому что решать про DNS-запрос должно оно, а не rule_set ниже.
func dobavitHijackDns(d map[string]any) {
	r, ok := d["route"].(map[string]any)
	if !ok {
		r = map[string]any{}
		d["route"] = r
	}
	pravila, _ := r["rules"].([]any)
	novoye := map[string]any{
		"type":   "logical",
		"mode":   "or",
		"rules":  []any{map[string]any{"protocol": "dns"}, map[string]any{"port": 53}},
		"action": "hijack-dns",
	}
	r["rules"] = append([]any{novoye}, pravila...)
}
