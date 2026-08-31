package konfig

// Локальная сеть мимо туннеля — всегда, чем бы ни был профиль с сервера.
//
// Беда 31.08 на стенде: с поднятым туннелем машина теряет локальную сеть.
// SSH отработал около шести команд, потом передача файла зависла и связь
// пропала совсем — при живой Windows. Дома у человека на той же сети NAS,
// Home Assistant, роутер и принтер: терять их при включённом VPN нельзя, и
// это блокирует выпуск.
//
// Что именно уводит локалку. В секции tun профиля сервер присылает
// `strict_route: true`. Документация ядра про этот флаг на Windows дословно:
// «Let unsupported network unreachable», «prevent DNS leak caused by Windows'
// ordinary multihomed DNS resolution behavior» и прямая оговорка — «may
// prevent some Windows applications (such as VirtualBox) from working
// properly in certain situations». Делается это не маршрутами, а правилами
// фаервола: в бинаре ядра лежат FwpmEngineOpen0 / FwpmSubLayerAdd0 /
// FwpmFilterAdd0 (замер 31.08 по самому sing-box.exe с машины человека) —
// то есть блокирующие фильтры WFP, которые режут трафик, идущий мимо
// туннельного адаптера. Соединение, УЖЕ установленное до подъёма туннеля
// (та самая ssh-сессия), живёт ещё несколько пакетов и умирает — ровно то,
// что видели на стенде.
//
// Одного правила маршрутизации тут мало, и это главное в этом файле.
// В профиле УЖЕ есть `{"outbound":"direct","ip_is_private":true}` третьим
// правилом — и оно не спасло. Не потому, что неверно, а потому, что
// route.rules решают судьбу трафика, который ДОШЁЛ до ядра, а фильтр WFP
// режет пакет до всякой маршрутизации. Поэтому чиним в три слоя, каждый
// своей природы:
//
//  1. strict_route выключается принудительно (nastroitTunPodLokalnuyuSet) —
//     снимаем фильтры фаервола, из-за которых локалка и пропадала;
//  2. route_exclude_address — документированная грань ядра «Exclude custom
//     routes when auto_route is enabled»: локальные подсети не попадают в
//     маршруты, которые туннель забирает себе, то есть до ядра такой трафик
//     вообще не доходит;
//  3. правило route.rules с ip_cidr → прямой выход, ПЕРВЫМ после
//     sniff/hijack-dns: если что-то локальное всё-таки вошло в ядро, оно
//     уйдёт напрямую, а не в туннель.
//
// Ни один слой не зависит от того, что прислал сервер: профиль общий для
// всех клиентов и писался под телефон, а гарантию «дома всё доступно» даёт
// клиент.
//
// Чего это стоит. Выключенный strict_route возвращает Windows её обычное
// многоинтерфейсное разрешение имён, то есть теоретическую утечку DNS мимо
// туннеля. Плата принята сознательно: правило hijack-dns в профиле всё равно
// заворачивает порт 53 в ядро, а недоступный дома NAS — это отказ, который
// человек видит каждый день.

// lokalnyePodsetiIPv4 / lokalnyePodsetiIPv6 — что обязано идти напрямую
// всегда. Разделены нарочно: в маршруты туннеля (route_exclude_address) IPv6
// уходит только если у туннеля вообще есть IPv6-адрес, а в правило
// маршрутизации попадают обе половины — правило маршрутов в системе не
// создаёт и стоить ошибки не может.
//
//	10/8, 172.16/12, 192.168/16 — частные сети (RFC 1918): роутер, NAS,
//	                              Home Assistant, принтер;
//	169.254/16                  — link-local, адрес «сеть без DHCP»;
//	224/4                       — multicast: mDNS (.local-имена, по которым
//	                              и находят принтер и колонки), SSDP, LLMNR;
//	255.255.255.255/32          — широковещательный адрес (DHCP, обнаружение
//	                              устройств в сети).
var lokalnyePodsetiIPv4 = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"224.0.0.0/4",
	"255.255.255.255/32",
}

// lokalnyePodsetiIPv6 — то же самое в IPv6: link-local (fe80::/10 — именно по
// нему в домашних сетях ходит соседское обнаружение), уникальные локальные
// адреса (fc00::/7) и multicast (ff00::/8, там же mDNS).
//
// Петли (127.0.0.0/8 и ::1/128) в списках НЕТ нарочно. Туннель её и так не
// забирает: auto_route ставит маршруты на 0.0.0.0/1 и 128.0.0.0/1, а трафик
// на себя же в них не попадает. Зато лишняя строка тут стоила бы дорого —
// правило встало бы перед срезом QUIC, и живой стенд, который проверяет срез
// на подставном «заблокированном» 127.0.0.2, перестал бы проверять что-либо
// вообще (поймано этим самым стендом 31.08, а не рассуждением).
var lokalnyePodsetiIPv6 = []string{
	"fe80::/10",
	"fc00::/7",
	"ff00::/8",
}

// LokalnyePodseti — весь список целиком, в порядке «сперва IPv4». Открыт
// наружу ради тестов и стендов: проверять надо ровно тот набор, который
// уходит в конфиг, а не его копию рядом.
func LokalnyePodseti() []string {
	return append(append([]string{}, lokalnyePodsetiIPv4...), lokalnyePodsetiIPv6...)
}

// isklyuchaemyeIzMarshrutov — то, что отдаётся в route_exclude_address.
// IPv6 добавляется только когда у туннеля есть IPv6-адрес: исключать
// маршруты, которых туннель и не ставил, — лишний повод ядру споткнуться на
// старте.
func isklyuchaemyeIzMarshrutov(estIPv6 bool) []string {
	out := append([]string{}, lokalnyePodsetiIPv4...)
	if estIPv6 {
		out = append(out, lokalnyePodsetiIPv6...)
	}
	return out
}

// nastroitTunPodLokalnuyuSet правит сам вход tun: гасит strict_route и
// выводит локальные подсети из маршрутов, которые забирает auto_route.
//
// strict_route ставится в false, а не удаляется: удалённое поле — это
// «как ядро решит по умолчанию», а false — наше явное решение, и в готовом
// конфиге его видно глазами при разборе аварии.
func nastroitTunPodLokalnuyuSet(vh map[string]any) {
	vh["strict_route"] = false

	uzhe := spisokStrok(vh["route_exclude_address"])
	est := map[string]bool{}
	for _, s := range uzhe {
		est[s] = true
	}
	itog := append([]string{}, uzhe...)
	for _, s := range isklyuchaemyeIzMarshrutov(tunEstIPv6(vh)) {
		if !est[s] {
			itog = append(itog, s)
			est[s] = true
		}
	}
	vh["route_exclude_address"] = strokiVSpisokAny(itog)
}

// tunEstIPv6 — есть ли у туннеля IPv6-адрес. Без него исключать IPv6-маршруты
// не из чего, а лишнее исключение — риск отказа ядра на старте на ровном
// месте. Поле address у ядра — список; старые профили писали inet4_address /
// inet6_address, их тоже смотрим.
func tunEstIPv6(vh map[string]any) bool {
	if _, est := vh["inet6_address"]; est {
		return true
	}
	for _, a := range spisokStrok(vh["address"]) {
		for i := 0; i < len(a); i++ {
			if a[i] == ':' {
				return true
			}
		}
	}
	return false
}

// dobavitPravilomLokalnayaSetNapryamuyu вставляет в route.rules правило
// «локальные подсети → прямой выход» и ставит его ПЕРВЫМ после ведущих
// sniff/hijack-dns — то есть раньше всех общих правил: раньше среза QUIC,
// раньше rule_set заблокированного и раньше route.final.
//
// Почему раньше общих, но позже hijack-dns. Раньше общих — иначе NAS по
// адресу из подсети, попавшей в чей-нибудь rule_set, уедет в туннель.
// Позже hijack-dns — иначе запрос к DNS домашнего роутера (192.168.1.1:53)
// ушёл бы мимо ядра, и в туннельном режиме, где имена разрешает ядро, это
// значило бы разъезд «кто и как резолвит» ровно в тот момент, когда всё уже
// работает.
//
// Идемпотентно по форме, а не по метке: своего поля в route.rule ядро не
// разрешает (лишний ключ — и `sing-box check` отвергает конфиг целиком),
// поэтому повтор узнаётся по совпадению ip_cidr со списком.
func dobavitPravilomLokalnayaSetNapryamuyu(d map[string]any) {
	r, ok := d["route"].(map[string]any)
	if !ok {
		return
	}
	teg := tegPryamogoVyhoda(d)
	if teg == "" {
		return
	}
	pravila, _ := r["rules"].([]any)
	for _, p := range pravila {
		pr, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if praviloProLokalnuyuSet(pr) {
			return
		}
	}

	novoye := map[string]any{
		"ip_cidr":  strokiVSpisokAny(LokalnyePodseti()),
		"outbound": teg,
	}

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

// praviloProLokalnuyuSet — это уже наше правило (или неотличимое от него):
// в ip_cidr перечислены ВСЕ локальные подсети сразу. Правило профиля
// `ip_is_private` сюда не попадает нарочно: оно не покрывает ни multicast,
// ни широковещательный адрес, и полагаться на него нельзя.
func praviloProLokalnuyuSet(pr map[string]any) bool {
	est := map[string]bool{}
	for _, s := range spisokStrok(pr["ip_cidr"]) {
		est[s] = true
	}
	if len(est) == 0 {
		return false
	}
	for _, s := range LokalnyePodseti() {
		if !est[s] {
			return false
		}
	}
	return true
}

// tegPryamogoVyhoda — тег выхода, который ведёт мимо VPN. Ищем по ТИПУ, а не
// по имени: чужой профиль вправе назвать прямой выход как угодно (тем же
// доводом, что и vyhodPryamoy). Если прямого выхода в профиле нет вовсе,
// заводим свой: гарантия «локалка доступна» не имеет права зависеть от того,
// что прислал сервер, а правило без существующего выхода ядро не примет.
func tegPryamogoVyhoda(d map[string]any) string {
	vyhody, _ := d["outbounds"].([]any)
	for _, v := range vyhody {
		vh, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if tip, _ := vh["type"].(string); tip != "direct" {
			continue
		}
		if teg, _ := vh["tag"].(string); teg != "" {
			return teg
		}
	}
	const svoy = "kelevra-pryamo"
	d["outbounds"] = append(vyhody, map[string]any{"type": "direct", "tag": svoy})
	return svoy
}

// strokiVSpisokAny — []string в []any. Конфиг живёт как map[string]any, и
// json.Marshal обязан увидеть однородный список, а не типизированный срез.
func strokiVSpisokAny(s []string) []any {
	out := make([]any, 0, len(s))
	for _, v := range s {
		out = append(out, v)
	}
	return out
}
