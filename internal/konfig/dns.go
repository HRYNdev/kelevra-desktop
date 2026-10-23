// DNS-часть подготовки конфига: fakeip и резолвер прямого выхода.
//
// Почему это отдельный файл, а не ещё сто строк в konfig.go. Всё остальное в
// пакете решает один вопрос — какие ВХОДЫ останутся в конфиге. Здесь решается
// другой: как должен вести себя DNS в туннеле, чтобы конфиг был
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
// мимо прокси, — и попадает в никуда. Прокси-режима с 0.6.77 нет, fakeip
// живёт только в туннеле.
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

import "net/netip"

// DiapazonFakeipKlienta — где выдаёт подменные адреса НАШЕ ядро на компьютере.
//
// Разбор 23.10 по журналам одного ноутбука: за двое суток 2777
// строк «router: missing fakeip record, try enable `experimental.cache_file`».
// Хранилище тут ни при чём — за тот же срок ядро перезапускалось, и все 789
// повторных ответов дали домену тот же адрес, что и до перезапуска. Ни один из
// адресов, на которые стучались программы (198.18.5.170 — 1328 раз, 198.18.6.29
// — 495, 198.18.2.78 — 165…), наше ядро не выдавало никогда: его собственные
// адреса за двое суток не ушли дальше 198.18.0.121. Их выдал ДОМАШНИЙ РОУТЕР:
// podkop на нём (как и наш шлюз) подменяет домены из того же диапазона
// 198.18.0.0/15, раздавая адреса подряд снизу (замер: роутер 192.168.1.1
// отвечает на www.googletagmanager.com адресом 198.18.2.78 — ровно тем, куда
// 165 раз ломились Edge и Яндекс.Браузер).
//
// Как чужой адрес попадает к нам. Дома туннель на ноутбуке поднят, и ответ
// роутера доходит до программы двумя дорогами: (1) ядро само спрашивает его
// резолвером "local" про всё, что не попало в наши списки, и отдаёт программе
// как есть (в журнале ядра: «exchanged A www.googletagmanager.com. 60 IN A
// 198.18.2.78», TTL 60 — роутерный, у нашего fakeip 600); (2) strict_route
// выключен (lokalnaya_set.go), и обычное многоинтерфейсное разрешение имён
// Windows спрашивает роутер напрямую, мимо ядра. Дальше программа идёт по
// адресу, маршрут к 198.18.0.0/15 ведёт в tun, и ядро, увидев адрес из СВОЕГО
// диапазона, ищет его в своей карте — и отказывает раньше всех правил
// маршрутизации (route/route.go sing-box: Store().Contains → Lookup → ошибка).
// Хуже того: оба хранилища раздают адреса с одного и того же нижнего края, и
// адрес, который роутер дал одному домену, у нас может значить другой.
//
// Лечение — развести диапазоны. Наш подменный адрес никогда не покидает ядро
// (ядро само разворачивает его обратно в домен), поэтому выбор диапазона на
// маршрутизацию не влияет; зато чужой адрес 198.18.x.x, оказавшись вне нашего
// диапазона, перестаёт быть «нашим без записи» и идёт как обычный IP: по
// route.final в direct, через физическую сеть к тому же роутеру, который его
// и выдал и знает, что с ним делать.
//
// Почему верхняя половина 198.19, а не что-то вне 198.18.0.0/15. Весь /15 —
// служебный диапазон под испытания сетей (RFC 2544), в интернете его нет, и
// на нём держатся предположения авторежима: подмену роутера он узнаёт по
// попаданию в 198.18.0.0/15 (avtorezhim, fakeIPPervyy/fakeIPPosledniy), а
// «слепота» зондов рассчитана на то, что наш fakeip выглядит так же. Уйти
// за /15 — значит переписывать и это, ради выгоды, которой нет. Роутеры
// заполняют /15 подряд снизу, и до 198.19.128.0 им 98 тысяч разных доменов
// (сегодня у домашнего видно 198.18.19.x, то есть меньше пяти тысяч).
// 32 тысяч адресов нашему ядру хватает с запасом: за двое суток ноутбук
// занял 120.
const DiapazonFakeipKlienta = "198.19.128.0/17"

// zonaRoutera — где выдают адреса роутеры с fakeip (podkop, наш шлюз): нижняя
// половина 198.18.0.0/15, заполняемая подряд с 198.18.0.2.
var zonaRoutera = netip.MustParsePrefix("198.18.0.0/16")

// razvestiFakeipSRouterom переносит IPv4-диапазон нашего fakeip из зоны, где
// раздают адреса домашние роутеры, в DiapazonFakeipKlienta (разбор — у
// константы). Возвращает прежний диапазон, если перенёс, и "" — если трогать
// было нечего: fakeip нет, диапазон не задан или уже не пересекается с зоной
// роутера (сервер вправе сам прислать другой — его выбор не перебиваем).
//
// Цена переноса один раз: при первом запуске с новым диапазоном ядро видит
// в cache_file карту под другой диапазон и сбрасывает её (fakeip/store.go:
// метаданные не совпали → FakeIPReset). Адреса старой карты, ещё лежащие в
// кешах программ (TTL нашего ответа 600 с), уходят мимо ядра как обычные IP,
// пока кеш не истечёт; кеш DNS самой Windows сбрасывается при подъёме ядра
// (yadro.sbrositKeshDns).
func razvestiFakeipSRouterom(d map[string]any) string {
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
		if tip, _ := m["type"].(string); tip != "fakeip" {
			continue
		}
		bylo, _ := m["inet4_range"].(string)
		p, err := netip.ParsePrefix(bylo)
		if err != nil || !p.Addr().Is4() || !p.Overlaps(zonaRoutera) {
			return ""
		}
		m["inet4_range"] = DiapazonFakeipKlienta
		return bylo
	}
	return ""
}

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
// Зовётся, когда на fakeip перестали ссылаться (после BezSetevyhPravil):
// сервер выброшен за ненадобностью, и хранить его карту
// нечему.
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

// okrepitPryamoyResolverVTunnele — режим туннеля, вторая половина беды 31.08
// (см. шапку файла). Системный резолвер в туннеле не годится:
// с поднятым tun он сам завёрнут в ядро, спрашивать его — петля на
// себя. Сам резолвер и его маршрут не трогаем ни на бит — тег, detour,
// dns.final остаются как в профиле, — меняем только протокол: tcp на udp.
//
// Почему это чинит именно ту фрагильность, что убила резолв на боевой
// машине. В журнале ядра резолвер умирал одной строкой: «lookup
// www.bing.com: exchange4: use of closed network connection» — то есть
// ломалась ОДНА TCP-сессия к 1.1.1.1, и с ней вставал резолв всего, что не
// попало в fakeip (два десятка rule_set не покрывают, например, yandex.ru —
// разбор 09.09). Держащаяся сессия — узнаваемая цель: провайдер рубит
// именно такие, длинные и отличимые от обычного трафика. У UDP держать
// нечего: каждый запрос — новый пакет, состояния между ними нет. На
// усечённый ответ (>512 байт без EDNS0) сам sing-box открывает отдельное
// разовое TCP-соединение (dns/transport/udp.go: exchangeTCP) — не тот
// долгоживущий канал, что умирал в журнале, а одноразовый запрос-ответ.
func okrepitPryamoyResolverVTunnele(d map[string]any) {
	teg := pryamoyResolverTeg(d)
	if teg == "" {
		return
	}
	dns, ok := d["dns"].(map[string]any)
	if !ok {
		return
	}
	servery, _ := dns["servers"].([]any)
	for _, sv := range servery {
		m, ok := sv.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := m["tag"].(string); t != teg {
			continue
		}
		if tip, _ := m["type"].(string); tip == "tcp" {
			m["type"] = "udp"
		}
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
