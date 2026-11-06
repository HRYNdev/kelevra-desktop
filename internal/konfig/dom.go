package konfig

import (
	"encoding/json"
	"net/netip"
	"strings"
)

// Вход домой (договор «Своя сеть Kelevra: вход домой», часть Desktop).
//
// Дома стоит хаб WireGuard. Компьютер поднимает к нему эндпоинт «Дом»
// (sing-box 1.14, секция endpoints), и его UDP уходит не напрямую, а через
// обычный туннель «Соединение» (detour): на VPS этот UDP уходит в обратный
// канал домой, и VPS видит только шифр WireGuard. В конфиг идут три вещи:
//
//  1. эндпоинт wireguard «Дом» — закрытый ключ этого устройства, пир — хаб
//     дома (uzel_pub), endpoint — мост, allowed_ips — сети дома;
//  2. правило «сети дома → Дом» первым после ведущего сниффа: раньше
//     hijack-dns (DNS дома по адресу 100.77.0.1:53 не должен уйти в DNS ядра),
//     раньше правила частных сетей и всего, что ведёт мимо туннеля;
//  3. DNS: имена зоны (domain_suffix dom) — серверу дома 100.77.0.1 через «Дом».
//
// 100.77.0.0/16 ядро частной не считает (sing common/network/addr.go:
// IsPublicAddr смотрит netip.IsPrivate — только RFC 1918 и fc00::/7), и в
// исключениях туннеля Desktop её нет (lokalnaya_set.go) — трафик к дому
// честно входит в tun и доходит до правила. Проверяет это dom_test.go.
//
// Без блока dom (Vybor.Dom == nil) конфиг ровно прежний: ни эндпоинта, ни
// правил, ни DNS.

// TegDoma — тег эндпоинта и выхода правила. Человек видит его в списке
// выходов ядра, поэтому по-русски, как «Соединение».
const TegDoma = "Дом"

// TegMostaDoma — группа выходов, через которую идёт мост «Дом», когда
// порталов в профиле больше одного: urltest между ними, сам по себе, без
// связи с выбором человека в «Соединении». В окне её нет (yadro.Gruppy).
const TegMostaDoma = "Дом · мост"

// TegDnsDoma — тег DNS-сервера дома.
const TegDnsDoma = "dns-dom"

// KeepaliveDoma — persistent_keepalive_interval, секунд (договор: 25).
const KeepaliveDoma = 25

// Dom — что вписать для входа домой. Собирает служба из проверенного блока
// dom (internal/dom) и ключа устройства.
type Dom struct {
	PrivatnyyKlyuch string   // закрытый ключ устройства, base64
	Adres           string   // «100.77.0.N/32»
	UzelPub         string   // открытый ключ хаба, base64
	MostHost        string   // «100.76.0.1»
	MostPort        int      // 51820
	Seti            []string // allowed_ips и правило маршрута
	DNS             string   // «100.77.0.1»
	Zona            string   // «dom»
	MTU             int      // 1280
	// Portaly — входы VPS с порталом моста (блок dom, поле portaly). Пусто —
	// мост идёт через «Соединение», как до появления поля.
	Portaly []PortalDoma
}

// PortalDoma — вход VPS с порталом: выход профиля VLESS с этими server и
// server_port ведёт ровно в него.
type PortalDoma struct {
	Uzel   string // для журнала
	Server string
	Port   int
}

func (dm Dom) goden() bool {
	return dm.PrivatnyyKlyuch != "" && dm.UzelPub != "" && dm.Adres != "" &&
		dm.MostHost != "" && dm.MostPort > 0 && dm.MostPort <= 65535 && len(dm.Seti) > 0
}

// dobavitDom вписывает вход домой. Возвращает пусто, если вписал, иначе —
// почему нет (для журнала); конфиг в этом случае не тронут. most — через что
// пошёл мост (для журнала).
func dobavitDom(d map[string]any, dm Dom, tunTeg string) (beda, most string) {
	if !dm.goden() {
		return "блок неполон", ""
	}
	if tegZanyat(d, TegDoma) || tegZanyat(d, TegMostaDoma) {
		return "тег «" + TegDoma + "» в профиле уже занят", ""
	}
	r, ok := d["route"].(map[string]any)
	if !ok {
		return "в профиле нет route", ""
	}
	detour, most := vyhodMosta(d, dm.Portaly)
	if detour == "" {
		return "в профиле нет туннельного выхода для моста", ""
	}

	mtu := dm.MTU
	if mtu <= 0 {
		mtu = 1280
	}
	endpoint := map[string]any{
		"type":        "wireguard",
		"tag":         TegDoma,
		"mtu":         mtu,
		"address":     []any{dm.Adres},
		"private_key": dm.PrivatnyyKlyuch,
		"peers": []any{map[string]any{
			"address":                       dm.MostHost,
			"port":                          dm.MostPort,
			"public_key":                    dm.UzelPub,
			"allowed_ips":                   strokiVSpisokAny(dm.Seti),
			"persistent_keepalive_interval": KeepaliveDoma,
		}},
		"detour": detour,
	}
	endpoints, _ := d["endpoints"].([]any)
	d["endpoints"] = append(endpoints, endpoint)

	// Правило — после ведущих sniff (без условий они ничего не решают, только
	// опознают), но ДО hijack-dns и всего остального.
	pravila, _ := r["rules"].([]any)
	vstavka := 0
	for vstavka < len(pravila) {
		pr, ok := pravila[vstavka].(map[string]any)
		if !ok {
			break
		}
		if act, _ := pr["action"].(string); act == "sniff" {
			vstavka++
			continue
		}
		break
	}
	novoye := map[string]any{"ip_cidr": strokiVSpisokAny(dm.Seti), "outbound": TegDoma}
	poVhodu(novoye, tunTeg)
	obnovlennye := make([]any, 0, len(pravila)+2)
	obnovlennye = append(obnovlennye, pravila[:vstavka]...)
	obnovlennye = append(obnovlennye, novoye)
	// Всё, что в свою подсеть пришло НЕ через свой tun, в дом не идёт (п.15).
	obnovlennye = append(obnovlennye, praviloRejectSeti())
	obnovlennye = append(obnovlennye, pravila[vstavka:]...)
	r["rules"] = obnovlennye

	dobavitDnsDoma(d, dm, tunTeg)
	return "", most
}

// vyhodMosta — через что пустить UDP моста. Блок назвал входы с порталом —
// выходы профиля VLESS ровно на эти server:port (порядок блока, без
// повторов): один — прямо в него, несколько — группа TegMostaDoma (urltest,
// живой выбор, с выбором человека для интернета не связан). Ни одного
// такого выхода в профиле нет, или блок их не назвал (старый мозг), — мост
// через «Соединение», как раньше. Возвращает тег и строку для журнала.
func vyhodMosta(d map[string]any, portaly []PortalDoma) (string, string) {
	vyhody, _ := d["outbounds"].([]any)
	var tegi []string
	byl := map[string]bool{}
	for _, p := range portaly {
		for _, v := range vyhody {
			vh, ok := v.(map[string]any)
			if !ok {
				continue
			}
			tip, _ := vh["type"].(string)
			teg, _ := vh["tag"].(string)
			server, _ := vh["server"].(string)
			port, _ := vh["server_port"].(float64)
			if tip != "vless" || teg == "" || byl[teg] || int(port) != p.Port ||
				!strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(server), "."), strings.TrimSuffix(p.Server, ".")) {
				continue
			}
			byl[teg] = true
			tegi = append(tegi, teg)
		}
	}
	switch {
	case len(tegi) == 1:
		return tegi[0], "портал: " + tegi[0]
	case len(tegi) > 1:
		d["outbounds"] = append(vyhody, map[string]any{
			"type":      "urltest",
			"tag":       TegMostaDoma,
			"outbounds": strokiVSpisokAny(tegi),
			"url":       "https://www.gstatic.com/generate_204",
			"interval":  "1m",
			"tolerance": 300,
			// Сменился выход — сокет моста закрывается, WireGuard
			// переоткрывает его уже через новый (transport/wireguard,
			// ClientBind.connect).
			"interrupt_exist_connections": true,
		})
		return TegMostaDoma, "порталы: " + strings.Join(tegi, ", ")
	}
	t := tegSoedineniya(d)
	if len(portaly) > 0 {
		return t, "входов с порталом в профиле нет, мост через " + t
	}
	return t, "мост через " + t
}

// dobavitDnsDoma — сервер дома и правило зоны первым в dns.rules (раньше
// fakeip и reject рекламы). Без секции dns или без серверов в ней ничего не
// делает: единственный сервер стал бы dns.final, и все имена машины ушли бы
// в дом.
func dobavitDnsDoma(d map[string]any, dm Dom, tunTeg string) {
	if dm.DNS == "" || dm.Zona == "" {
		return
	}
	dn, ok := d["dns"].(map[string]any)
	if !ok {
		return
	}
	servery, _ := dn["servers"].([]any)
	if len(servery) == 0 {
		return
	}
	dn["servers"] = append(servery, map[string]any{
		"type":        "udp",
		"tag":         TegDnsDoma,
		"server":      dm.DNS,
		"server_port": 53,
		"detour":      TegDoma,
	})
	pravila, _ := dn["rules"].([]any)
	pr := map[string]any{"domain_suffix": []any{dm.Zona}, "server": TegDnsDoma}
	poVhodu(pr, tunTeg)
	dn["rules"] = append([]any{pr}, pravila...)
}

// poVhodu — правило дома действует только для трафика, вошедшего через свой
// tun (договор, «Безопасность» п.10): ни служебный вход, ни что-то ещё в дом
// не попадает. Проба связи службы (sluzhba/dom.go) идёт с машины через tun —
// под условие подходит. Тега нет — условие не ставится (вход в конфиге один).
func poVhodu(pravilo map[string]any, tunTeg string) {
	if tunTeg != "" {
		pravilo["inbound"] = []any{tunTeg}
	}
}

// vyrezatReverse — у выходов и эндпоинтов профиля не бывает «reverse»
// (обратный канал домой держит только узел дома; договор, «Безопасность»
// п.10). Подменённый профиль с reverse сделал бы из ноутбука мост, через
// который сторона VPS ходит в его сеть.
func vyrezatReverse(d map[string]any) {
	for _, sekciya := range []string{"outbounds", "endpoints"} {
		spisok, _ := d[sekciya].([]any)
		for _, v := range spisok {
			if vh, ok := v.(map[string]any); ok {
				delete(vh, "reverse")
			}
		}
	}
}

// ochistitSvoyuSet — своя подсеть 100.77.0.0/16 обязана входить в tun
// (договор, «Безопасность» п.14): исключение маршрутов профиля, задевающее
// её, вырезается — широкое (100.64.0.0/10) заменяется остатком без своей
// подсети, узкое внутри неё убирается; список route_address, если профиль его
// задал и своей подсети в нём нет, дополняется ею. Возвращает, что сделано.
func ochistitSvoyuSet(vh map[string]any) []string {
	svoya := netip.MustParsePrefix(SvoyaSetDoma)
	var sdelano []string
	for _, pole := range []string{"route_exclude_address", "inet4_route_exclude_address"} {
		if _, est := vh[pole]; !est {
			continue
		}
		var itog []string
		izmeneno := false
		for _, s := range spisokStrok(vh[pole]) {
			p, err := netip.ParsePrefix(strings.TrimSpace(s))
			if err != nil || !p.Overlaps(svoya) {
				itog = append(itog, s)
				continue
			}
			izmeneno = true
			ostatok := bezPodseti(p.Masked(), svoya)
			stalo := strings.Join(ostatok, ", ")
			if stalo == "" {
				stalo = "убрано"
			}
			sdelano = append(sdelano, pole+": "+s+" → "+stalo)
			itog = append(itog, ostatok...)
		}
		if izmeneno {
			vh[pole] = strokiVSpisokAny(itog)
		}
	}
	for _, pole := range []string{"route_address", "inet4_route_address"} {
		spisok := spisokStrok(vh[pole])
		if len(spisok) == 0 {
			continue
		}
		pokryto := false
		for _, s := range spisok {
			if p, err := netip.ParsePrefix(strings.TrimSpace(s)); err == nil && p.Bits() <= svoya.Bits() && p.Contains(svoya.Addr()) {
				pokryto = true
			}
		}
		if !pokryto {
			vh[pole] = strokiVSpisokAny(append(spisok, SvoyaSetDoma))
			sdelano = append(sdelano, pole+": + "+SvoyaSetDoma)
		}
	}
	return sdelano
}

// MostDomaIzKonfiga — через какой выход идёт мост «Дом» (detour эндпоинта
// TegDoma) в готовом конфиге ядра; пусто — входа домой в конфиге нет.
func MostDomaIzKonfiga(syroy []byte) string {
	var d struct {
		Endpoints []struct {
			Tag    string `json:"tag"`
			Detour string `json:"detour"`
		} `json:"endpoints"`
	}
	if json.Unmarshal(syroy, &d) != nil {
		return ""
	}
	for _, e := range d.Endpoints {
		if e.Tag == TegDoma {
			return e.Detour
		}
	}
	return ""
}

// bezPodseti — p без s (p пересекается с s): если p внутри s — пусто; если
// p шире — соседние блоки по пути от p к s.
func bezPodseti(p, s netip.Prefix) []string {
	if p.Bits() >= s.Bits() {
		return nil
	}
	var itog []string
	for b := p.Bits() + 1; b <= s.Bits(); b++ {
		predok, _ := s.Addr().Prefix(b)
		a := predok.Addr().As4()
		bit := b - 1
		a[bit/8] ^= 1 << (7 - bit%8)
		itog = append(itog, netip.PrefixFrom(netip.AddrFrom4(a), b).String())
	}
	return itog
}

// tegSoedineniya — куда ведёт мост: селектор «Соединение» боевого профиля,
// иначе тот же туннельный выход, что выбирает упрощённый режим.
func tegSoedineniya(d map[string]any) string {
	vyhody, _ := d["outbounds"].([]any)
	for _, v := range vyhody {
		if vh, ok := v.(map[string]any); ok {
			if t, _ := vh["tag"].(string); t == "Соединение" {
				return t
			}
		}
	}
	return tunnelnyyVyhod(d)
}

// tegZanyat — есть ли выход или эндпоинт с таким тегом.
func tegZanyat(d map[string]any, teg string) bool {
	for _, sekciya := range []string{"outbounds", "endpoints"} {
		spisok, _ := d[sekciya].([]any)
		for _, v := range spisok {
			if vh, ok := v.(map[string]any); ok {
				if t, _ := vh["tag"].(string); t == teg {
					return true
				}
			}
		}
	}
	return false
}

// SetiDomaVIsklyucheniyah — какие исключения tun-входа (route_exclude_address)
// задевают сети дома: такой трафик ядра не достигнет. Пусто — не задевают.
// Для журнала: сами исключения не правим, их прислал профиль.
func SetiDomaVIsklyucheniyah(gotovyy map[string]any, seti []string) []string {
	var domashnie []netip.Prefix
	for _, s := range seti {
		if p, err := netip.ParsePrefix(s); err == nil {
			domashnie = append(domashnie, p)
		}
	}
	var zadetye []string
	vhody, _ := gotovyy["inbounds"].([]any)
	for _, v := range vhody {
		vh, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if tip, _ := vh["type"].(string); tip != "tun" {
			continue
		}
		for _, s := range spisokStrok(vh["route_exclude_address"]) {
			p, err := netip.ParsePrefix(strings.TrimSpace(s))
			if err != nil {
				continue
			}
			for _, dp := range domashnie {
				if p.Overlaps(dp) {
					zadetye = append(zadetye, s)
					break
				}
			}
		}
	}
	return zadetye
}

// SvoyaSetDoma — вся своя подсеть (договор, «Адреса и имена»).
const SvoyaSetDoma = "100.77.0.0/16"

// ZonaDoma — зона имён дома.
const ZonaDoma = "dom"

// zakrytDom — «закрыто по умолчанию» (договор, «Безопасность» п.5): вход
// домой на устройстве уже бывал, а действующего блока нет (снят, отозван,
// чужой ключ). Тогда адреса и имена дома не уходят ни в обычный туннель, ни
// напрямую: правило «своя подсеть → reject» на том же месте, где стоит
// правило дома, и DNS «зона dom → reject» первым.
func zakrytDom(d map[string]any, tunTeg string) bool {
	r, ok := d["route"].(map[string]any)
	if !ok {
		return false
	}
	pravila, _ := r["rules"].([]any)
	vstavka := 0
	for vstavka < len(pravila) {
		pr, ok := pravila[vstavka].(map[string]any)
		if !ok {
			break
		}
		if act, _ := pr["action"].(string); act == "sniff" {
			vstavka++
			continue
		}
		break
	}
	novoye := praviloRejectSeti()
	obnovlennye := make([]any, 0, len(pravila)+1)
	obnovlennye = append(obnovlennye, pravila[:vstavka]...)
	obnovlennye = append(obnovlennye, novoye)
	obnovlennye = append(obnovlennye, pravila[vstavka:]...)
	r["rules"] = obnovlennye
	if dn, ok := d["dns"].(map[string]any); ok {
		dp, _ := dn["rules"].([]any)
		zakr := map[string]any{"domain_suffix": []any{ZonaDoma}, "action": "reject"}
		poVhodu(zakr, tunTeg)
		dn["rules"] = append([]any{zakr}, dp...)
	}
	return true
}

// TegTuna — тег tun-входа, который ставит сам клиент (договор, «Безопасность»
// п.15): правила дома завязаны на него, а не на тег из профиля.
const TegTuna = "tun-in"

// praviloRejectSeti — «своя подсеть → reject» без условия inbound.
func praviloRejectSeti() map[string]any {
	return map[string]any{"ip_cidr": []any{SvoyaSetDoma}, "action": "reject"}
}

// ssylaetsyaNaDom — правило, сервер или эндпоинт задевает дом: «Дом»,
// «dns-dom» или 100.77.* в любом поле (в том числе во вложенных правилах).
func ssylaetsyaNaDom(v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return true
	}
	s := string(b)
	return strings.Contains(s, `"`+TegDoma+`"`) || strings.Contains(s, TegDnsDoma) || strings.Contains(s, TegMostaDoma) ||
		strings.Contains(s, "100.77.")
}

func tegiSpiska(spisok []any) map[string]bool {
	m := map[string]bool{}
	for _, v := range spisok {
		if o, ok := v.(map[string]any); ok {
			if t, _ := o["tag"].(string); t != "" {
				m[t] = true
			}
		}
	}
	return m
}

// vyrezatChuzhoyDom — профиль не вправе приносить вход домой (п.15): все его
// endpoints выбрасываются (свой «Дом» клиент добавляет сам), как и правила
// route и dns, DNS-серверы и dns.final, ссылающиеся на «Дом», «dns-dom» или
// 100.77.0.0/16, и правила, ведущие в выброшенные эндпоинты.
func vyrezatChuzhoyDom(d map[string]any) {
	udalennye := tegiSpiska(func() []any { l, _ := d["endpoints"].([]any); return l }())
	delete(d, "endpoints")
	// Выход профиля с тегом «Дом» занял бы тег и сорвал бы вписывание своего
	// входа домой (а вместе с ним и reject своей подсети): выбрасывается.
	if o, ok := d["outbounds"].([]any); ok {
		ostalis := make([]any, 0, len(o))
		for _, v := range o {
			if vh, ok := v.(map[string]any); ok {
				if t, _ := vh["tag"].(string); t == TegDoma || t == TegMostaDoma {
					udalennye[t] = true
					continue
				}
			}
			ostalis = append(ostalis, v)
		}
		d["outbounds"] = ostalis
		// «Дом» в списках outbounds селекторов и urltest.
		for _, v := range ostalis {
			if vh, ok := v.(map[string]any); ok {
				if l, ok := vh["outbounds"].([]any); ok {
					vh["outbounds"] = otfiltrovat(l, func(x any) bool { t, _ := x.(string); return t == TegDoma || t == TegMostaDoma })
				}
				if t, _ := vh["default"].(string); t == TegDoma || t == TegMostaDoma {
					delete(vh, "default")
				}
			}
		}
	}
	plohoe := func(pr any) bool {
		if ssylaetsyaNaDom(pr) {
			return true
		}
		if o, ok := pr.(map[string]any); ok {
			for _, pole := range []string{"outbound", "server", "detour"} {
				if t, _ := o[pole].(string); t != "" && udalennye[t] {
					return true
				}
			}
		}
		return false
	}
	if r, ok := d["route"].(map[string]any); ok {
		if pravila, ok := r["rules"].([]any); ok {
			r["rules"] = otfiltrovat(pravila, plohoe)
		}
		if f, ok := r["final"].(string); ok && (f == TegDoma || udalennye[f]) {
			delete(r, "final")
		}
	}
	if dn, ok := d["dns"].(map[string]any); ok {
		if sv, ok := dn["servers"].([]any); ok {
			dn["servers"] = otfiltrovat(sv, plohoe)
		}
		if pr, ok := dn["rules"].([]any); ok {
			dn["rules"] = otfiltrovat(pr, plohoe)
		}
		if f, ok := dn["final"].(string); ok && (f == TegDnsDoma || udalennye[f]) {
			delete(dn, "final")
		}
	}
	if o, ok := d["outbounds"].([]any); ok {
		for _, v := range o {
			if vh, ok := v.(map[string]any); ok {
				if t, _ := vh["detour"].(string); t != "" && udalennye[t] {
					delete(vh, "detour")
				}
			}
		}
	}
}

func otfiltrovat(spisok []any, plohoe func(any) bool) []any {
	ostatok := make([]any, 0, len(spisok))
	for _, v := range spisok {
		if !plohoe(v) {
			ostatok = append(ostatok, v)
		}
	}
	return ostatok
}

// pereimenovatVhod — в правилах route и dns тег входа старый → новый.
func pereimenovatVhod(d map[string]any, staryy, novyy string) {
	var obhod func(pravila []any)
	obhod = func(pravila []any) {
		for _, p := range pravila {
			pr, ok := p.(map[string]any)
			if !ok {
				continue
			}
			switch v := pr["inbound"].(type) {
			case string:
				if v == staryy {
					pr["inbound"] = novyy
				}
			case []any:
				for i, s := range v {
					if s == staryy {
						v[i] = novyy
					}
				}
			}
			if vl, ok := pr["rules"].([]any); ok {
				obhod(vl)
			}
		}
	}
	if r, ok := d["route"].(map[string]any); ok {
		pr, _ := r["rules"].([]any)
		obhod(pr)
	}
	if dn, ok := d["dns"].(map[string]any); ok {
		pr, _ := dn["rules"].([]any)
		obhod(pr)
	}
}
