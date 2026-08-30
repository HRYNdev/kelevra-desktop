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
	"time"
)

// dataPoChelovecheski — дата снимка комплекта хранится машинно («2026-08-23»:
// так её удобно сравнивать и сортировать), а в окно её читает человек, который
// не программист. В окне дата русская: «23.08.2026». Чужой формат не ломаем и
// не глотаем — отдаём как есть: пустая заметка врала бы сильнее непривычной
// даты.
func dataPoChelovecheski(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("02.01.2006")
}

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

// ZametkaProksiRezhima — обычная заметка прокси-режима, когда системный
// прокси в системе реально стоит (его поставило само ядро, или, если ядро
// отказалось, приложение прописало реестр за него — internal/proksi.Postavit).
// Общая для Prigotovit (сборка конфига) и для страховки в sluzhba.go
// (PodnyatZashchitu, ветка «system proxy»), чтобы текст не разъезжался
// между двумя местами, которые решают один и тот же вопрос.
func ZametkaProksiRezhima(estTunnel bool) string {
	if estTunnel {
		return ZametkaBezPrav
	}
	return ZametkaBezTunnelya
}

// Заметки — единственные строки этого пакета, которые ЧИТАЕТ ЧЕЛОВЕК: окно
// показывает Zametka как есть. Поэтому здесь нет ни «ядра», ни «туннеля», ни
// «прокси-режима»: открывает окно не программист, и он должен понять, что
// защищено и что нажать. Стенд облика (stend/oblik_snimok.py) вытаскивает этот
// блок прямо отсюда и требует, чтобы каждая заметка попала хотя бы на один
// снимок, — иначе новая формулировка уедет человеку, не показавшись никому.
const (
	// ZametkaVes — режим Tunnel: защищено всё. Слово «защищён» тут нарочно
	// убрано (хозяин, 24.08: «слово "защита" второй раз режет») — круг над
	// заметкой уже назвал состояние словом, заметка добавляет только то, чего
	// круг не говорит: объём.
	ZametkaVes = "Любая программа идёт через Kelevra."
	// ZametkaBezPrav — туннель в профиле есть, но прав администратора нет.
	// Ровно в этом случае окно показывает кнопку «Включить полную защиту»
	// (sluzhba.go: MozhnoTun = EstTunnel && !Prava), и она сама просит права у
	// Windows. Поэтому шлём человека на кнопку, а не на ручной перезапуск:
	// совет, который дороже соседней кнопки, человек читает как «всё сложно».
	// Без «Защищены только браузеры» в начале (то же слово уже стоит в круге)
	// и без повтора текста самой кнопки следом — она и так прямо под этой
	// строкой, второй раз называть её не нужно.
	ZametkaBezPrav = "Чтобы пустить через VPN все программы, нажмите ниже."
	// ZametkaBezTunnelya — в самом ключе доступа полного режима нет.
	ZametkaBezTunnelya = "Остальные программы идут напрямую — так настроен ваш ключ доступа."
	// ZametkaRuchnoyProksi — Windows не дал прописать себя в настройках сети.
	// %s — адрес, который человеку придётся вписать руками. «Прокси», не
	// «защиту»: круг над заметкой уже стоит словом «защищено» — тут второй
	// раз это же слово только путало (хозяин, 24.08: «слово "защита" второй
	// раз режет»), а «прокси» точнее — ровно то, что человек сейчас впишет.
	ZametkaRuchnoyProksi = "Windows не прописал прокси сам. Откройте Параметры → " +
		"Сеть и Интернет → Прокси и впишите там адрес %s"
	// ZametkaBezSetevyhPravil — Vybor.BezSetevyhPravil: список правил не
	// скачался, включён упрощённый режим (весь трафик через VPN, без разбора).
	ZametkaBezSetevyhPravil = "Список правил не скачался — весь трафик идёт через VPN."
	// ZametkaPravilaIzKomplekta — Vybor.PravilaIzKomplekta: свежие правила не
	// скачались, но вместо упрощённого режима включён встроенный в приложение
	// комплект — умная маршрутизация (что через VPN, что напрямую) жива. %s —
	// дата снимка комплекта (Vybor.PravilaKomplektData, обычно internal/pravila.Data()).
	ZametkaPravilaIzKomplekta = "Свежие правила не скачались — работают встроенные (от %s)."
)

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
	// BezSetevyhPravil — не тянуть правила маршрутизации с сервера правил, а
	// пустить вообще весь трафик через туннель. Взводится, когда сервер правил
	// недоступен: боевой профиль несёт 22 route.rule_set (тип remote, качаются
	// с subkv.chickenkiller.com detour:"direct" — мимо VPN). Проверено настоящим
	// ядром на этом профиле: если источник недостижим (connection refused) или
	// молчит (i/o timeout) и локальный кеш пуст, ядро не открывает порт вовсе,
	// а ПАДАЕТ целиком за 0.4–5.2 секунды («initialize rule-set[N]: initial
	// rule-set: ...: connect: connection refused»). Наполненный кеш эту беду
	// прячет (старт за 0.04 с) — значит бьёт она именно по первому запуску и по
	// человеку, у которого сеть слабая или провайдер режет домен правил.
	// В профиле route.final == "direct": голое удаление rule_set пустило бы
	// трафик мимо VPN молча, поэтому вместе с правилами final переставляется
	// на туннельный выход (см. Prigotovit).
	BezSetevyhPravil bool
	// PravilaIzKomplekta — тег→путь до правила, разложенного из встроенного в
	// бинарь комплекта (internal/pravila.Razlozhit). Непустая карта — сигнал:
	// сервер правил недоступен, но умную маршрутизацию можно сохранить, а не
	// приносить в жертву как BezSetevyhPravil. Каждый route.rule_set с
	// type:"remote" переписывается в {type:"local", path:<из карты>} —
	// route.final не трогается, потому что разбор по доменам/подсетям остаётся.
	//
	// Строгость: применяется ТОЛЬКО целиком. Если хотя бы для одного remote-
	// тега профиля пути в карте нет, Prigotovit НЕ трогает профиль и
	// возвращает ошибку — иначе ядро упадёт на первом же правиле, ссылающемся
	// на исчезнувший rule_set. Вызывающий в этом случае уходит на прежнюю
	// деградацию (BezSetevyhPravil).
	//
	// Вместе с BezSetevyhPravil взводиться не должны: если оба, комплект
	// главнее (см. Prigotovit) — он не жертвует разбором трафика, а BezSetevyhPravil жертвует.
	PravilaIzKomplekta map[string]string
	// PravilaKomplektData — дата снимка встроенного комплекта, подставляется
	// в ZametkaPravilaIzKomplekta, чтобы человек в окне видел, от какого числа
	// работают встроенные правила.
	PravilaKomplektData string
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
		k.Zametka = ZametkaVes
		// route.rules — это WHITELIST «что пустить в туннель» (rule_set гоняет
		// подписку), а не blacklist того, что оставить снаружи: route.final в
		// профиле подписки — "direct", и всё, чего нет ни в одном rule_set
		// (например ещё не размеченный домен), сегодня тихо уходит мимо VPN и
		// режется провайдером. В режиме полной защиты (права есть, tun
		// поднят) переворачиваем страховку — final должен быть туннелем, а
		// не direct; правило {"ip_is_private":true,"outbound":"direct"} чуть
		// выше в route.rules никуда не делось и по-прежнему держит домашнюю
		// сеть прямой. Если в профиле нет ни одного туннельного выхода
		// (tunnelnyyVyhod вернул ""), final не трогаем — профиль без выхода
		// работал так и раньше, это не наш случай.
		if r, ok := d["route"].(map[string]any); ok {
			if teg := tunnelnyyVyhod(d); teg != "" {
				r["final"] = teg
			}
		}
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
			k.Zametka = fmt.Sprintf(ZametkaRuchnoyProksi, k.ProksiAdres)
		} else {
			k.Zametka = ZametkaProksiRezhima(k.EstTunnel)
		}
	}

	d["inbounds"] = ostavshiesya
	if len(udalennyeTegi) > 0 {
		pochistitPravila(d, udalennyeTegi)
	}
	// Тегами входов — уже после того, как выброшенные входы (например,
	// tun-in без прав) отфильтрованы: правило про udp/443 не должно
	// ссылаться на вход, которого в итоговом конфиге больше нет — это та же
	// беда, что чинит pochistitPravila чуть выше, и мы намеренно ставим свой
	// вызов после неё, а не до.
	dobavitPravilomRezhimQuic(d, tegiVhodov(ostavshiesya))
	if len(v.PravilaIzKomplekta) > 0 {
		// Комплект главнее BezSetevyhPravil (см. комментарий поля): он не
		// жертвует разбором трафика, а BezSetevyhPravil жертвует.
		if err := primenitPravilaIzKomplekta(d, v.PravilaIzKomplekta); err != nil {
			return nil, k, err
		}
		k.Zametka = fmt.Sprintf(ZametkaPravilaIzKomplekta, dataPoChelovecheski(v.PravilaKomplektData))
	} else if v.BezSetevyhPravil {
		if err := ubratSetevyePravila(d); err != nil {
			return nil, k, err
		}
		k.Zametka = ZametkaBezSetevyhPravil
	}
	k.ClashAdres, k.ClashSekret = clash(d)
	zapomnitVybor(d)

	gotovyy, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, k, err
	}
	return gotovyy, k, nil
}

// dobavitPravilomRezhimQuic режет QUIC в самом начале route.rules (после
// уже существующих sniff/hijack-dns, до правил с rule_set).
//
// Диагноз 29.08: ядро sing-box опознаёт googlevideo.com сниффом по SNI, а
// снифф QUIC (common/sniff/quic.go в ядре) разбирает только Initial-пакет и
// промахивается на retry/coalescing — YouTube ходит по QUIC и часть его
// соединений проваливается мимо ВСЕХ rule_set сразу на route.final=direct, к
// провайдеру, который душит видео. Chrome, не получив ответа по QUIC,
// мгновенно откатывается на TCP/443, где снифф по SNI надёжен — там
// googlevideo.com уже попадает в свой rule_set и уходит в туннель как
// положено.
//
// Диагноз 30.08: правило {"protocol":"quic","action":"reject"} само зависело
// от того же ненадёжного сниффа — на retry/coalescing пакетах снифф не
// узнаёт QUIC, поле protocol не проставляется, и наше же reject-правило не
// срабатывает ровно там, где сниффер промахнулся. Заменено на матч по
// транспорту и порту напрямую (network:udp, port:443) — HTTP/3 всегда ходит
// по udp/443, и решение больше не зависит от результата сниффа: снифф может
// вообще не отработать (retry/coalescing), правило сработает всё равно,
// потому что смотрит на транспорт пакета, а не на то, что о нём решил
// снифф. У обычных клиентских приложений легитимного не-QUIC udp/443
// практически не бывает — это то же допущение, на котором стоял исходный
// фикс.
//
// Риск самоотстрела (проверено на internal/konfig/testdata/profil_telefona.json,
// 30.08): выход профиля — vless с tls+reality на server_port 443, транспорт
// TCP (никакого поля transport, "network" в самом vless-outbound не udp), и
// socks — тоже TCP. Ни hysteria2, ни tuic, ни wireguard, чей канал к серверу
// сам является UDP/443, в профиле нет. Дальше: даже будь такой outbound,
// route.rules матчит только траффик, ВОШЕДШИЙ через inbound (метаданные
// соединения несут Inbound-тег) — собственный dial исходящего до его же
// сервера идёт мимо таблицы маршрутизации внутри реализации outbound'а и под
// это правило в принципе не попадает. Тем не менее ограничиваем правило
// явным списком "inbound" — тегами входов, реально оставшихся в профиле
// ПОСЛЕ фильтрации (вызывающая сторона передаёт их отдельным параметром,
// см. Prigotovit — вызов стоит после d["inbounds"] = ostavshiesya и после
// pochistitPravila): это и защита на будущее (если когда-то появится
// UDP-транспортный outbound или служебный inbound не для трафика
// пользователя, правило его не заденет), и требование по заданию — теги
// берутся из профиля, а не хардкодятся.
//
// Идемпотентно: если в route.rules уже есть правило с network:"udp" и
// port:443 (то ли своё же, вставленное прошлым прогоном, то ли профиль сам
// решил про QUIC/443), ничего не делает — профиль главнее. Идемпотентность
// проверяется только по network+port, без учёта "inbound": состав входов
// между прогонами не меняется (см. вызов в Prigotovit), а если бы поменялся
// — новый список всё равно был бы правильнее старого, перезаписывать
// чужое/своё правило поверх не наша забота этой функции.
func dobavitPravilomRezhimQuic(d map[string]any, tegiVhodovPolzovatelya []string) {
	r, ok := d["route"].(map[string]any)
	if !ok {
		return
	}
	pravila, _ := r["rules"].([]any)
	for _, p := range pravila {
		pr, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if praviloUdp443(pr) {
			return
		}
	}

	// Вставляем после ведущих sniff/hijack-dns — снифф должен успеть
	// отработать до нашего reject — но до первого правила с rule_set,
	// иначе решение комплекта/rule_set перехватит трафик раньше среза.
	vstavka := 0
	for vstavka < len(pravila) {
		pr, ok := pravila[vstavka].(map[string]any)
		if !ok {
			break
		}
		if act, _ := pr["action"].(string); act == "sniff" || act == "hijack-dns" {
			vstavka++
			continue
		}
		break
	}

	novoye := map[string]any{"network": "udp", "port": 443, "action": "reject"}
	if len(tegiVhodovPolzovatelya) > 0 {
		novoye["inbound"] = tegiVhodovPolzovatelya
	}
	obnovlennye := make([]any, 0, len(pravila)+1)
	obnovlennye = append(obnovlennye, pravila[:vstavka]...)
	obnovlennye = append(obnovlennye, novoye)
	obnovlennye = append(obnovlennye, pravila[vstavka:]...)
	r["rules"] = obnovlennye
}

// tegiVhodov собирает теги входов из уже отфильтрованного списка inbounds
// (тех, что реально останутся в конфиге) — используется, чтобы ограничить
// защитное правило про udp/443 входами пользователя, а не хардкодить имена
// вроде "tun-in"/"mixed-in".
func tegiVhodov(vhody []any) []string {
	var tegi []string
	for _, v := range vhody {
		vh, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if teg, _ := vh["tag"].(string); teg != "" {
			tegi = append(tegi, teg)
		}
	}
	return tegi
}

// praviloUdp443 сообщает, матчит ли правило route.rules udp/443 напрямую —
// это форма нашего защитного правила (см. dobavitPravilomRezhimQuic), не
// зависящая от сниффа. Совпадение по network+port, action не проверяется —
// профиль может решить про udp/443 иначе (не обязательно reject), и это
// тоже повод не вставлять своё правило поверх.
func praviloUdp443(pr map[string]any) bool {
	if !setevoyEstUdp(pr["network"]) {
		return false
	}
	return portChislom(pr["port"]) == 443
}

// setevoyEstUdp проверяет поле network route.rule — sing-box кодирует его
// либо одиночной строкой, либо списком строк (badoption.Listable).
func setevoyEstUdp(v any) bool {
	switch x := v.(type) {
	case string:
		return x == "udp"
	case []any:
		for _, s := range x {
			if str, _ := s.(string); str == "udp" {
				return true
			}
		}
	}
	return false
}

// portChislom приводит поле port route.rule к float64 для сравнения — после
// json.Marshal/Unmarshal числа приходят как float64, а наше же только что
// вставленное правило хранит его как int.
func portChislom(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return -1
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

// ubratSetevyePravila — тело Vybor.BezSetevyhPravil (см. комментарий поля):
// выбрасывает route.rule_set целиком, выбрасывает из route.rules и dns.rules
// каждое правило со ссылкой на rule_set (включая условия внутри logical-
// правил) и переставляет route.final на туннельный выход, чтобы трафик, для
// которого теперь некому сказать «этот домен — через VPN», не потёк вместо
// этого напрямую.
func ubratSetevyePravila(d map[string]any) error {
	r, ok := d["route"].(map[string]any)
	if !ok {
		r = map[string]any{}
		d["route"] = r
	}
	delete(r, "rule_set")
	if pravila, ok := r["rules"].([]any); ok {
		r["rules"] = bezRuleSet(pravila)
	}
	if dn, ok := d["dns"].(map[string]any); ok {
		if pravila, ok := dn["rules"].([]any); ok {
			dn["rules"] = bezRuleSet(pravila)
		}
	}
	teg := tunnelnyyVyhod(d)
	if teg == "" {
		return fmt.Errorf("в профиле нет ни одного выхода в туннель — упрощённый режим (без правил) включить нечем")
	}
	r["final"] = teg
	return nil
}

// primenitPravilaIzKomplekta — тело Vybor.PravilaIzKomplekta (см. комментарий
// поля): переписывает каждый route.rule_set с type:"remote" на локальный
// файл из встроенного комплекта, ничего больше не трогая — теги (а значит и
// ссылки на них в route.rules/dns.rules, и route.final) остаются прежними.
//
// Применяется СТРОГО целиком: сперва проверяем, что путь в komplekt есть для
// каждого remote-тега, и только потом переписываем — частичная подмена хуже
// молчания, ядро упадёт на первом же правиле без rule_set.
func primenitPravilaIzKomplekta(d map[string]any, komplekt map[string]string) error {
	r, ok := d["route"].(map[string]any)
	if !ok {
		return fmt.Errorf("в профиле нет route — вшитый комплект правил применять не к чему")
	}
	spisok, ok := r["rule_set"].([]any)
	if !ok || len(spisok) == 0 {
		return fmt.Errorf("в профиле нет route.rule_set — вшитый комплект правил применять не к чему")
	}
	for _, rs := range spisok {
		m, ok := rs.(map[string]any)
		if !ok {
			continue
		}
		if tip, _ := m["type"].(string); tip != "remote" {
			continue
		}
		teg, _ := m["tag"].(string)
		if _, est := komplekt[teg]; !est {
			return fmt.Errorf("во встроенном комплекте нет правила %q — применяю прежнюю деградацию", teg)
		}
	}
	for i, rs := range spisok {
		m, ok := rs.(map[string]any)
		if !ok {
			continue
		}
		if tip, _ := m["type"].(string); tip != "remote" {
			continue
		}
		teg, _ := m["tag"].(string)
		spisok[i] = map[string]any{
			"tag":    teg,
			"type":   "local",
			"format": "binary",
			"path":   komplekt[teg],
		}
	}
	return nil
}

// bezRuleSet рекурсивно выбрасывает из списка правил каждое, у которого есть
// ключ "rule_set" — в том числе внутри вложенного списка logical-правила
// ("type":"logical","rules":[...]). Если у logical-правила после чистки не
// осталось ни одного вложенного условия, выбрасываем и его — правило без
// единого условия ядру не нужно.
func bezRuleSet(pravila []any) []any {
	var ostatok []any
	for _, p := range pravila {
		pr, ok := p.(map[string]any)
		if !ok {
			ostatok = append(ostatok, p)
			continue
		}
		if _, est := pr["rule_set"]; est {
			continue
		}
		if vlozh, ok := pr["rules"].([]any); ok {
			ochishchennye := bezRuleSet(vlozh)
			if len(ochishchennye) == 0 {
				continue
			}
			pr["rules"] = ochishchennye
		}
		ostatok = append(ostatok, pr)
	}
	return ostatok
}

// tunnelnyyVyhod ищет тег выхода, на который стоит пустить ВЕСЬ трафик, если
// правил маршрутизации больше нет: сперва селектор (человек сам выбирает
// узел из него — эталон профиля хозяина), иначе первый urltest (авто-выбор по
// скорости), иначе первый выход, который не direct/block/dns.
func tunnelnyyVyhod(d map[string]any) string {
	vyhody, _ := d["outbounds"].([]any)
	var selector, urltest, drugoy string
	for _, v := range vyhody {
		vh, ok := v.(map[string]any)
		if !ok {
			continue
		}
		tip, _ := vh["type"].(string)
		teg, _ := vh["tag"].(string)
		switch tip {
		case "selector":
			if selector == "" {
				selector = teg
			}
		case "urltest":
			if urltest == "" {
				urltest = teg
			}
		case "direct", "block", "dns":
			// не туннель — пропускаем
		default:
			if drugoy == "" {
				drugoy = teg
			}
		}
	}
	if selector != "" {
		return selector
	}
	if urltest != "" {
		return urltest
	}
	return drugoy
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
