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
// локальный прокси `mixed-in`. На компьютере работает ТОЛЬКО туннель: вход
// прокси выбрасывается всегда, системный прокси приложение не ставит никогда.
// Решение владельца 17.10: «скачал, дал права админа, ВСЁ, РАБОТАЕТ, нет
// никаких неполных режимов». Прежний прокси-режим (до 0.6.77) пускал через
// Kelevra только браузеры и только TCP — это была не работа, а её видимость.
// Права администратора для туннеля проверяет не сборка конфига, а подъём
// связи (sluzhba.PodnyatZashchitu): конфиг один и тот же при любых правах.
//
// Третье. Адрес Clash API у профиля свой (`experimental.clash_api`), и жёстко
// зашитый в приложении адрес его не угадает: приложение решит, что ядро мертво,
// и убьёт живой процесс. Адрес берём из профиля.
package konfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/zapasnoy"
)

// OshibkaBezTunnelya — в профиле нет входа-туннеля, поднимать нечего.
// Сервер шлёт туннель всем; встретить такое можно только на чужом или
// испорченном профиле. Ступени «тогда хотя бы прокси» больше нет.
var OshibkaBezTunnelya = errors.New("в профиле нет туннеля — подключаться нечем")

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

// Rezhim — как именно идёт трафик. С 0.6.77 режим один: туннель. Тип оставлен
// ради журнала и /api/sostoyanie (поле rezhim), которые читают и разбирают.
type Rezhim string

const (
	// Tunnel — весь трафик машины идёт через ядро. Нужны права администратора.
	Tunnel Rezhim = "tunnel"
)

// Kartina — что приложение знает о подготовленном конфиге.
type Kartina struct {
	Rezhim      Rezhim `json:"rezhim"`
	ClashAdres  string `json:"clash_adres"`  // где ядро поднимет Clash API
	ClashSekret string `json:"clash_sekret"` // пароль к нему, если задан
	EstTunnel   bool   `json:"est_tunnel"`   // есть ли в профиле вход-туннель
	Zametka     string `json:"zametka"`      // человеку: что сейчас идёт через Kelevra

	// TunImya — interface_name туннельного входа («tun125» в боевом профиле).
	// Нужен НЕ ядру (оно берёт его из конфига), а приложению: по этому имени
	// след туннеля на диске (internal/tunnel) проверяет после жёсткой смерти
	// копии, не остался ли висеть адаптер.
	TunImya string `json:"tun_imya,omitempty"`
	// MtuTun — размер пакета туннеля в СОБРАННОМ конфиге, а не в профиле.
	//
	// Заведено 09.09. Профиль присылает 9000 (заводское значение sing-box,
	// рассчитанное на телефон), клиент сбивает его до 1420, и от этого числа
	// прямо зависит, открываются сайты за секунду или за десять (замер 08.09:
	// TLS-рукопожатие 6.5 с против 1.1 с). При этом слова «mtu» не было ни в
	// одной строке журналов десктопа за всю их историю — жалобу на скорость
	// нечем было проверить. Теперь оно попадает в журнал при каждой сборке.
	MtuTun int `json:"mtu_tun,omitempty"`
	// PravilaOtkuda — откуда взяты наборы правил в этом конфиге: «сеть»
	// (ядро качает само), «кеш» (свежие с диска) или «встроенный комплект».
	// Второе поле того же назначения, что и MtuTun: без него по журналу
	// нельзя сказать, какими правилами человек живёт прямо сейчас.
	PravilaOtkuda string `json:"pravila_otkuda,omitempty"`
	// PravilNaborov — сколько наборов правил в конфиге.
	PravilNaborov int `json:"pravil_naborov,omitempty"`
	// VhodSluzhby — адрес входа службы (host:port), если он вписан в конфиг
	// (см. Vybor.VhodSluzhby); пусто — не вписан.
	VhodSluzhby string `json:"vhod_sluzhby,omitempty"`
}

// ClashPoUmolchaniyu — адрес, если профиль про Clash API молчит.
const ClashPoUmolchaniyu = "127.0.0.1:9090"

// Заметки — единственные строки этого пакета, которые ЧИТАЕТ ЧЕЛОВЕК: окно
// показывает Zametka как есть. Поэтому здесь нет ни «ядра», ни «туннеля»:
// открывает окно не программист. Стенд облика (stend/oblik_snimok.py)
// вытаскивает этот блок прямо отсюда и требует, чтобы каждая заметка попала
// хотя бы на один снимок, — иначе новая формулировка уедет человеку, не
// показавшись никому.
//
// Заметок неполного режима (только браузеры, ручной прокси, откат при
// неподнявшемся туннеле, занятое устройство) больше нет: нет и самого режима.
// Причины, по которым связь не поднялась, говорит служба (internal/sluzhba,
// Oshibka*), и связь в этих случаях не поднята вовсе.
const (
	// ZametkaVes — туннель поднят: через Kelevra идёт всё.
	ZametkaVes = "Любая программа идёт через Kelevra."
	// ZametkaBezSetevyhPravil — Vybor.BezSetevyhPravil: список правил не
	// скачался, включён упрощённый режим (весь трафик через VPN, без разбора).
	ZametkaBezSetevyhPravil = "Список правил не загружен, разбор трафика отключён."
	// ZametkaPravilaIzKomplekta — Vybor.PravilaIzKomplekta: свежие правила не
	// скачались, но вместо упрощённого режима включён встроенный в приложение
	// комплект — умная маршрутизация (что через VPN, что напрямую) жива. %s —
	// дата снимка комплекта (Vybor.PravilaKomplektData, обычно internal/pravila.Data()).
	ZametkaPravilaIzKomplekta = "Актуальные правила не загружены, применяются встроенные от %s."
)

// Поля профиля, которые ядро принимает только на Android. На компьютере они
// либо валят старт, либо не значат ничего.
var androidPolyaTun = []string{"exclude_package", "include_package", "platform"}

// Vybor — то, что решает не профиль, а машина, на которой он запускается.
//
// Полей Prava, BezTunnelya, AdapterZanyat и BezSistemnogoProksi больше нет
// (0.6.77): они выбирали ступень ниже туннеля, а ступени ниже нет. Права
// проверяет подъём связи, занятое устройство — тоже он (sluzhba).
type Vybor struct {
	// TunImya — под каким именем поднимать сетевой адаптер туннеля
	// (interface_name у tun-входа) ВМЕСТО имени из профиля. Пусто — имя из
	// профиля, как было всегда.
	//
	// Заводится, когда имя из профиля в системе занято: ядру всё равно,
	// tun125 там или tun126, а человеку не всё равно — на занятом имени ядро
	// падает через 15 секунд и связь не поднимается (замер с его
	// машины 01.09, разбор в internal/tunnel/imya.go).
	TunImya string
	// TunSdvigAdresa — на сколько соседних блоков подвинуть адреса туннельного
	// входа (поле address). Ноль — адреса из профиля, как было всегда.
	//
	// Едет вместе с TunImya и тем же числом: остаток прошлой попытки держит и
	// имя, и адрес, а меняли мы до 08.09 только имя. Ядро тогда падало второй
	// раз подряд, уже на «set ipv4 address: The object already exists», и
	// связь всё равно не поднималась (замер с машины 06.09,
	// разбор в internal/tunnel/adres.go).
	TunSdvigAdresa int
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
	// ClashSekret — пароль служебного порта ядра (Clash API), свой у каждого
	// устройства: создаётся на нём, сервер его не знает и не присылает.
	// Вписывается в experimental.clash_api.secret, если профиль своего пароля
	// не принёс (боевой шаблон его не несёт: порт на 127.0.0.1, но без пароля
	// его читает и переключает любая программа и любая страница в браузере).
	// Пароль из профиля главнее: профиль, где он задан, работает как раньше.
	// Пусто — ничего не вписывается, поведение прежнее.
	ClashSekret string
	// VhodSluzhby — вход для запросов самой службы через туннель (запасной
	// путь, internal/zapasnoy). nil — входа нет, поведение прежнее.
	VhodSluzhby *VhodSluzhby
}

// VhodSluzhby — локальный вход ядра, через который служба повторяет свои
// запросы (подписка, правила, журналы, обновление), когда прямой путь режется.
//
// Это НЕ прокси-режим и не системный прокси (решение владельца 17.10 «нет
// никаких неполных режимов» в силе): socks на 127.0.0.1, только с паролем
// устройства, системе о нём не сообщается, и порт не 2412 — на тот мог
// остаться смотреть системный прокси старой версии. Трафик с этого входа ядро
// ведёт в тот же выход, что и заблокированное (селектор), — ровно как телефон
// своей страховкой LifeLine ведёт наш домен в туннель.
type VhodSluzhby struct {
	Port  int    // 127.0.0.1:<Port>; занятый порт выбирает служба заранее
	Parol string // пароль устройства; пусто — вход не вписывается
}

// Prigotovit готовит профиль под эту машину.
func Prigotovit(syroy []byte, v Vybor) ([]byte, Kartina, error) {
	var d map[string]any
	if err := json.Unmarshal(syroy, &d); err != nil {
		return nil, Kartina{}, fmt.Errorf("профиль не разобрать: %w", err)
	}

	if r, ok := d["route"].(map[string]any); ok {
		delete(r, "override_android_vpn")
	}

	vhody, _ := d["inbounds"].([]any)
	k := Kartina{}
	// Имя адаптера из Vybor запоминаем ДО обхода входов: внутри него `v` —
	// уже элемент списка, а не Vybor (имена совпали исторически), и обратиться
	// к полю оттуда нельзя.
	podmenaImeni := v.TunImya
	sdvigAdresa := v.TunSdvigAdresa

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
			if imya, _ := vh["interface_name"].(string); imya != "" && k.TunImya == "" {
				k.TunImya = imya
			}
			// Через туннель пойдёт ВЕСЬ трафик машины, включая домашнюю сеть.
			// Профиль присылает strict_route: true, и это ровно то, из-за чего
			// на стенде 31.08 с поднятым туннелем пропала локальная сеть (см.
			// internal/konfig/lokalnaya_set.go).
			nastroitTunPodLokalnuyuSet(vh)
			// Размер пакета — в картину: по нему разбирают жалобы на
			// скорость, а до 09.09 его не было видно нигде (см. MtuTun).
			switch m := vh["mtu"].(type) {
			case float64:
				k.MtuTun = int(m)
			case int:
				k.MtuTun = m
			}
			// Имя адаптера подменяем ТОЛЬКО когда вызывающий его назвал:
			// он один знает, занято ли имя профиля в системе прямо сейчас
			// (sluzhba.PodnyatZashchitu спрашивает про это до запуска ядра).
			// Kartina.TunImya обязана поехать вместе с конфигом: по ней след
			// туннеля на диске ищет адаптер после жёсткой смерти копии, и
			// разъехаться этим двум именам нельзя.
			if podmenaImeni != "" {
				vh["interface_name"] = podmenaImeni
				k.TunImya = podmenaImeni
			}
			// Адрес двигаем тем же сдвигом, что и имя: остаток прошлой
			// попытки держит их вместе, и разъехаться им нельзя.
			if sdvigAdresa > 0 {
				podvinutAdresaTun(vh, sdvigAdresa)
			}
		case "mixed", "http", "socks":
			// Локальный прокси на компьютере не нужен никогда. В профиле он
			// стоит ради Android (`platform.http_proxy` у tun-входа указывает
			// ровно на него, и это поле мы всё равно выкидываем —
			// androidPolyaTun). Весь трафик машины заходит в ядро через tun.
			// Оставленный вход означал бы открытый порт, на который может
			// смотреть системный прокси, — ровно та авария 31.08, когда прокси
			// остался висеть на мёртвом порту и у человека пропал интернет.
			// Нет двери — нечему висеть.
			udalennyeTegi = append(udalennyeTegi, teg)
			continue
		}
		ostavshiesya = append(ostavshiesya, vh)
	}
	if !k.EstTunnel {
		return nil, k, OshibkaBezTunnelya
	}
	k.Rezhim = Tunnel
	k.Zametka = ZametkaVes

	d["inbounds"] = ostavshiesya
	if len(udalennyeTegi) > 0 {
		pochistitPravila(d, udalennyeTegi)
	}
	if len(v.PravilaIzKomplekta) > 0 {
		// Комплект главнее BezSetevyhPravil (см. комментарий поля): он не
		// жертвует разбором трафика, а BezSetevyhPravil жертвует.
		if err := primenitPravilaIzKomplekta(d, v.PravilaIzKomplekta); err != nil {
			return nil, k, err
		}
		k.PravilNaborov = len(v.PravilaIzKomplekta)
		// ДВА РАЗНЫХ СЛУЧАЯ, а заметка была одна на оба.
		//
		// Наборы с диска — это либо свежий кеш (скачан этой машиной при
		// прошлом подъёме связи), либо встроенный комплект (вшит в .exe и
		// стареет от выпуска к выпуску). Дата есть только у комплекта;
		// у кеша PravilaKomplektData пуст, и человек видел в окне
		// «работают встроенные (от )» — с пустой скобкой и неправдой по
		// сути: правила у него были свежее комплекта.
		if v.PravilaKomplektData == "" {
			k.PravilaOtkuda = "кеш"
		} else {
			k.PravilaOtkuda = "встроенный комплект"
			k.Zametka = fmt.Sprintf(ZametkaPravilaIzKomplekta, dataPoChelovecheski(v.PravilaKomplektData))
		}
	} else if v.BezSetevyhPravil {
		if err := ubratSetevyePravila(d); err != nil {
			return nil, k, err
		}
		k.Zametka = ZametkaBezSetevyhPravil
	}
	// Правило про QUIC — ПОСЛЕ лестницы правил маршрутизации, а не до неё:
	// оно ссылается на rule_set заблокированного, и знать, какие rule_set в
	// конфиге в итоге остались, можно только когда решено про комплект и про
	// BezSetevyhPravil (последний выбрасывает rule_set целиком).
	//
	// Теги входов — из уже отфильтрованного списка: правило не должно
	// ссылаться на вход, которого в итоговом конфиге больше нет (та же беда,
	// что чинит pochistitPravila выше).
	dobavitPravilomRezhimQuic(d, tegiVhodov(ostavshiesya))
	// Локальная сеть — ПОСЛЕ правила про QUIC, чтобы встать перед ним:
	// оба правила вставляются сразу за ведущими sniff/hijack-dns, и первым
	// в готовом конфиге окажется то, которое вставлено последним. Порядок
	// именно такой: домашний NAS на udp/443 не должен попасть под срез QUIC,
	// а срез QUIC про локальную сеть ничего не знает.
	dobavitPravilomLokalnayaSetNapryamuyu(d)
	// DNS правим ПОСЛЕДНИМ шагом, после лестницы правил: BezSetevyhPravil
	// сама выбрасывает из dns.rules всё, что ссылается на rule_set (а
	// правило про fakeip в боевом профиле — именно такое), и решать про
	// fakeip надо по тому, что от dns осталось, а не по тому, что было.
	soglasovatFakeip(d)
	okrepitPryamoyResolverVTunnele(d)
	bezAdresovV6(d)
	vpisatSekretClash(d, v.ClashSekret)
	k.ClashAdres, k.ClashSekret = clash(d)
	zapomnitVybor(d)
	// Вход службы — ПОСЛЕДНИМ: правила про QUIC и локальную сеть выше
	// ограничены входами человека (tegiVhodov), и наш вход в них попасть не
	// должен; а его правило обязано встать первым в route.rules.
	if v.VhodSluzhby != nil {
		k.VhodSluzhby = dobavitVhodSluzhby(d, *v.VhodSluzhby, k.ClashAdres)
	}

	gotovyy, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, k, err
	}
	return gotovyy, k, nil
}

// dobavitPravilomRezhimQuic отбивает QUIC ТОЧЕЧНО — только там, где он и так
// обязан идти в туннель, — и вставляет это правило в начало route.rules
// (после уже существующих sniff/hijack-dns, до правил с rule_set).
//
// Зачем правило вообще есть. Диагноз 29.08: ядро опознаёт googlevideo.com
// сниффом по SNI, а снифф QUIC (common/sniff/quic.go) разбирает только
// Initial-пакет и промахивается на retry/coalescing — часть QUIC-соединений
// YouTube проваливалась мимо ВСЕХ rule_set сразу на route.final=direct, к
// провайдеру, который душит видео. Chrome, не получив ответа по QUIC,
// мгновенно откатывается на TCP/443, где снифф по SNI надёжен, и там домен
// уже попадает в свой rule_set и уходит в туннель.
//
// Вторая, более твёрдая причина (проверено 31.08 по самому ядру, а не по
// документации): UDP через наш выход НЕ ХОДИТ. Основной выход профиля —
// vless с flow "xtls-rprx-vision", а протокол vless запрещает UDP при этом
// flow на стороне СЕРВЕРА: в бинаре ядра лежит строка «flow does not support
// UDP», в исходнике sing-vmess (vless/service.go) это
// `if request.Flow == FlowVision && request.Command == vmess.NetworkUDP`.
// То есть QUIC, честно уведённый в туннель, там и умрёт — молча, таймаутом.
// Быстрый reject лучше: браузер откатится на TCP сразу, а не через ожидание.
//
// Матч по транспорту и порту, а не по protocol:"quic" (диагноз 30.08):
// поле protocol проставляет тот же ненадёжный снифф, и своё же reject-правило
// не срабатывало ровно там, где сниффер промахнулся. HTTP/3 всегда ходит по
// udp/443, поэтому network:udp + port:443 решает то же самое, но без слепого
// пятна сниффа.
//
// ПОЧЕМУ НЕ ВЕСЬ UDP/443 МАШИНЫ (правка 31.08). Раньше правило резало udp/443
// без всяких условий. В режиме прокси это ещё почти не жгло (UDP в клиент
// почти не заходит), а вот в режиме туннеля в клиент заходит ВЕСЬ UDP
// машины — и правило рубило QUIC всему компьютеру: браузер молча откатится на
// TCP, а игра, звонок и всё, что умеет только QUIC, просто перестанет
// работать, в том числе к российским и нейтральным адресам, которым туннель
// вообще не нужен. Поэтому правило сужено полем rule_set: теми же списками,
// которые профиль и так уводит в туннель (см. tegiZablokirovannogo). Что
// идёт напрямую — идёт напрямую и по QUIC, ядро его не трогает.
//
// Домен для сужения ядро знает и без сниффа: в режиме туннеля fakeip живёт
// ровно на этих же rule_set (см. dns.go, soglasovatFakeip) — адрес назначения
// сам несёт домен. Плюс часть списков (main-subnets, cloudflare, hetzner…)
// матчится по IP и сниффа не требует вовсе.
//
// Единственный случай, когда правило остаётся широким, — упрощённый режим
// BezSetevyhPravil: там rule_set выброшены целиком, а route.final переставлен
// на туннель, то есть В ТУННЕЛЬ ИДЁТ ВСЁ и сужать не по чему. Широкий reject
// там честнее тишины: без него весь UDP уходил бы в туннель, где UDP не
// работает, и умирал бы по таймауту.
//
// Если в туннель по правилам не идёт ничего (route.final=direct и ни одного
// rule_set в туннель — так выглядит чужой профиль-заглушка), правило не
// вставляется вовсе: резать нечего.
//
// Риск самоотстрела (проверено на internal/konfig/testdata/profil_telefona.json):
// route.rules матчит только траффик, ВОШЕДШИЙ через inbound, а собственный
// dial исходящего до его же сервера идёт мимо таблицы маршрутизации. Тем не
// менее ограничиваем правило явным списком "inbound" — тегами входов, реально
// оставшихся в профиле ПОСЛЕ фильтрации: это защита на будущее (появится
// UDP-транспортный outbound или служебный inbound — правило его не заденет).
//
// Идемпотентно: если в route.rules уже есть правило с network:"udp" и
// port:443 (своё же от прошлого прогона или собственное решение профиля про
// QUIC/443), ничего не делает — профиль главнее.
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

	novoye := map[string]any{"network": "udp", "port": 443, "action": "reject"}
	switch tegi := tegiZablokirovannogo(d); {
	case len(tegi) > 0:
		// Точечно: только то, что и так уводится в туннель.
		novoye["rule_set"] = tegi
	case finalVTunnel(d):
		// Упрощённый режим: в туннель идёт всё, сужать не по чему.
	default:
		// В туннель не идёт ничего — резать нечего.
		return
	}
	if len(tegiVhodovPolzovatelya) > 0 {
		novoye["inbound"] = tegiVhodovPolzovatelya
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

	obnovlennye := make([]any, 0, len(pravila)+1)
	obnovlennye = append(obnovlennye, pravila[:vstavka]...)
	obnovlennye = append(obnovlennye, novoye)
	obnovlennye = append(obnovlennye, pravila[vstavka:]...)
	r["rules"] = obnovlennye
}

// tegiZablokirovannogo — теги rule_set, которые профиль уводит В ТУННЕЛЬ, в
// порядке профиля и без повторов. Это и есть «список заблокированного» в
// терминах человека: домены и подсети, ради которых VPN и включают.
//
// Берём их из самого профиля, а не хардкодим: сервер подписки список меняет
// (22 rule_set на 31.08), и захардкоженный набор устарел бы молча. Правила с
// action:"reject" (список рекламы) сюда не попадают — там и так reject,
// сужать наше правило ими незачем.
func tegiZablokirovannogo(d map[string]any) []string {
	r, _ := d["route"].(map[string]any)
	if r == nil {
		return nil
	}
	pravila, _ := r["rules"].([]any)
	var tegi []string
	vidnye := map[string]bool{}
	for _, p := range pravila {
		pr, ok := p.(map[string]any)
		if !ok {
			continue
		}
		// Правило уводит трафик, только если это маршрутизация (action
		// пустой или "route") в выход, который не прямой.
		if act, _ := pr["action"].(string); act != "" && act != "route" {
			continue
		}
		vyhod, _ := pr["outbound"].(string)
		if vyhod == "" || vyhodPryamoy(d, vyhod) {
			continue
		}
		for _, t := range spisokStrok(pr["rule_set"]) {
			if t == "" || vidnye[t] {
				continue
			}
			vidnye[t] = true
			tegi = append(tegi, t)
		}
	}
	return tegi
}

// finalVTunnel — весь неопознанный трафик уходит в туннель (route.final
// переставлен на туннельный выход). Так выглядит упрощённый режим
// BezSetevyhPravil; в боевом профиле final == "direct".
func finalVTunnel(d map[string]any) bool {
	r, _ := d["route"].(map[string]any)
	if r == nil {
		return false
	}
	final, _ := r["final"].(string)
	if final == "" {
		return false
	}
	return !vyhodPryamoy(d, final)
}

// vyhodPryamoy — ведёт ли выход с таким тегом мимо VPN. Смотрим на ТИП
// выхода в outbounds, а не на имя: боевой профиль зовёт туннельный выход
// «Соединение», а прямой — "direct", но чужой профиль вправе назвать их как
// угодно, и ошибка в эту сторону дорогая (лишний тег в правиле — срезанный
// QUIC там, где он был не нужен). Если выхода с таким тегом в профиле нет
// вовсе — судим по имени: это последнее, что осталось.
func vyhodPryamoy(d map[string]any, teg string) bool {
	vyhody, _ := d["outbounds"].([]any)
	for _, v := range vyhody {
		vh, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := vh["tag"].(string); t != teg {
			continue
		}
		switch tip, _ := vh["type"].(string); tip {
		case "direct", "block", "dns":
			return true
		default:
			return false
		}
	}
	return teg == "direct" || teg == "block" || teg == "dns"
}

// spisokStrok — поле правила, которое sing-box кодирует либо одиночной
// строкой, либо списком строк (badoption.Listable).
func spisokStrok(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []string:
		return x
	case []any:
		var out []string
		for _, s := range x {
			if str, ok := s.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
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
		if tip, _ := vh["type"].(string); tip == "tun" {
			k.EstTunnel, k.Rezhim = true, Tunnel
		}
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
// узел из него — эталон боевого профиля), иначе первый urltest (авто-выбор по
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

// dobavitVhodSluzhby вписывает вход службы и правило «с него — в туннель».
// Возвращает адрес входа; пусто — не вписан (нет пароля, нет туннельного
// выхода, порт совпал с чужим). Без входа всё работает как раньше, только
// без запасного пути — уронить из-за него подъём связи нельзя.
func dobavitVhodSluzhby(d map[string]any, vh VhodSluzhby, clashAdres string) string {
	if vh.Port <= 0 || vh.Port > 65535 || vh.Parol == "" {
		return ""
	}
	vyhod := vyhodDlyaSluzhby(d)
	if vyhod == "" {
		return ""
	}
	adres := fmt.Sprintf("127.0.0.1:%d", vh.Port)
	if _, port, err := splitPort(clashAdres); err == nil && port == vh.Port {
		return ""
	}
	vhody, _ := d["inbounds"].([]any)
	for _, v := range vhody {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := m["tag"].(string); t == zapasnoy.TegVhoda {
			return "" // уже есть (чужой профиль или повторная сборка) — не дублируем
		}
		if int(portChislom(m["listen_port"])) == vh.Port {
			return ""
		}
	}
	d["inbounds"] = append(vhody, map[string]any{
		"type":        "socks",
		"tag":         zapasnoy.TegVhoda,
		"listen":      "127.0.0.1",
		"listen_port": vh.Port,
		"users":       []any{map[string]any{"username": zapasnoy.Polzovatel, "password": vh.Parol}},
	})
	r, ok := d["route"].(map[string]any)
	if !ok {
		r = map[string]any{}
		d["route"] = r
	}
	pravila, _ := r["rules"].([]any)
	// Первым правилом: до сниффа, рекламы, частных адресов и среза QUIC —
	// ничто из них к запросам службы отношения не имеет.
	r["rules"] = append([]any{map[string]any{
		"inbound":  []any{zapasnoy.TegVhoda},
		"outbound": vyhod,
	}}, pravila...)
	return adres
}

// vyhodDlyaSluzhby — куда вести запросы службы: туда же, куда профиль ведёт
// заблокированное (выход первого правила с rule_set, не прямой); в
// упрощённом режиме — route.final; иначе — туннельный выход по типу.
func vyhodDlyaSluzhby(d map[string]any) string {
	if r, ok := d["route"].(map[string]any); ok {
		pravila, _ := r["rules"].([]any)
		for _, p := range pravila {
			pr, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if act, _ := pr["action"].(string); act != "" && act != "route" {
				continue
			}
			vyhod, _ := pr["outbound"].(string)
			if vyhod == "" || vyhodPryamoy(d, vyhod) || len(spisokStrok(pr["rule_set"])) == 0 {
				continue
			}
			return vyhod
		}
		if finalVTunnel(d) {
			final, _ := r["final"].(string)
			return final
		}
	}
	return tunnelnyyVyhod(d)
}

// splitPort — порт из «host:port».
func splitPort(adres string) (string, int, error) {
	host, p, err := net.SplitHostPort(adres)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(p)
	return host, port, err
}

// vpisatSekretClash кладёт пароль устройства в experimental.clash_api, если
// профиль своего не принёс. Секции clash_api нет — ничего не создаёт: нет
// служебного порта, нечего закрывать (и приложение его не ждёт).
func vpisatSekretClash(d map[string]any, sekret string) {
	if sekret == "" {
		return
	}
	e, ok := d["experimental"].(map[string]any)
	if !ok {
		return
	}
	c, ok := e["clash_api"].(map[string]any)
	if !ok {
		return
	}
	if s, _ := c["secret"].(string); s != "" {
		return
	}
	c["secret"] = sekret
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

// zapomnitVybor велит ядру хранить выбранный человеком узел между запусками.
// Хранилище — cache_file; в боевом профиле оно есть, но у чужого может
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

// AdresaPravil — откуда качаются наборы правил: тег → адрес.
//
// Нужна кешу правил (internal/pravila): чтобы скачать их ЗАРАНЕЕ и подсунуть
// ядру с диска, надо знать, что именно качать. Список живёт в профиле и
// меняется на сервере без нашего участия — зашивать его в приложение нельзя,
// разъедется молча (так уже вышло 08.09 со встроенным комплектом, где не
// хватало одного набора из двадцати трёх).
//
// Берутся только type:"remote": локальные и вшитые качать неоткуда и незачем.
func AdresaPravil(syroy []byte) map[string]string {
	var d map[string]any
	if err := json.Unmarshal(syroy, &d); err != nil {
		return nil
	}
	r, ok := d["route"].(map[string]any)
	if !ok {
		return nil
	}
	spisok, ok := r["rule_set"].([]any)
	if !ok {
		return nil
	}
	itog := map[string]string{}
	for _, rs := range spisok {
		m, ok := rs.(map[string]any)
		if !ok {
			continue
		}
		if tip, _ := m["type"].(string); tip != "remote" {
			continue
		}
		teg, _ := m["tag"].(string)
		adres, _ := m["url"].(string)
		if teg != "" && adres != "" {
			itog[teg] = adres
		}
	}
	return itog
}
