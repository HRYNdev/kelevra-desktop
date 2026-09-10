//go:build windows

package avtorezhim

import (
	"fmt"
	"net"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// MakShlyuza — аппаратный номер (MAC) шлюза физической сети: то, по чему
// авторежим узнаёт домашний роутер.
//
// ЗАЧЕМ ЭТО ВМЕСТО DNS-ОТПЕЧАТКА. Прежний признак дома («роутер подменяет
// заблокированные домены») меряет ПОВЕДЕНИЕ сети, и это поведение ломается
// от чего угодно:
//
//   - при поднятом туннеле порт 53 перехватывает наше же ядро, и зонд видит
//     собственную подмену вместо чужой. Замер с машины человека 09.09: восемь
//     раз подряд зонд, пущенный через секунду после подъёма туннеля, получал
//     198.18.0.118/119/120 (наш fakeip) и объявлял «дома» на мобильной точке.
//     Авторежим гасил туннель, зонд становился честным, туннель поднимался —
//     карусель с периодом девять секунд;
//   - дома резолвер и без всякого туннеля молчит по 4-7 минут при переходе
//     между репитером и роутером (замер 28.08: 11 циклов из 14).
//
// MAC шлюза ничем из этого не болеет. Он берётся с канального уровня, ниже
// маршрутизации: наш туннель до него не дотягивается по устройству, а не по
// договорённости. Он одинаков на всей квартире, потому что репитеры работают
// мостом и шлюзом остаётся роутер (замер 09.09: MAC моста роутера Cudy
// 80:af:ca:82:17:91 совпал с тем, что видит ноутбук). У мобильной точки шлюз
// — сам телефон, и номер у него другой.
//
// Возвращаются ВСЕ найденные, нижним регистром с двоеточиями
// («80:af:ca:82:17:91»). Сравнивать напрямую не нужно — для этого есть
// [EstDomashniyShlyuz].
//
// ПОЧЕМУ СПИСОК, А НЕ ОДИН. Маршрут по умолчанию на машине не один. Замер
// с машины человека 09.09.2026, сразу после выхода 0.6.65:
//
//	ifIndex 13  Беспроводная сеть  192.168.1.1  метрика 0     80-AF-CA-82-17-91
//	ifIndex 21  Radmin VPN         26.0.0.1     метрика 9256  02-00-00-00-51-00
//
// Прежний код брал ПЕРВЫЙ адаптер со шлюзом и на том останавливался. Radmin
// VPN проходит все отсевы: тип у него Ethernet, а не туннель, и в описании
// «Famatech Radmin VPN Ethernet Adapter» нет ни одного слова из
// slovaSvoegoAdaptera. В списке Windows он оказался раньше Wi-Fi — и человек,
// сидя дома, получил «вне дома» и поднятый туннель.
//
// Чинить это пополнением списка слов бесполезно: Hamachi, ZeroTier и любой
// следующий такой адаптер сломают его снова, а узнаём мы об этом только когда
// у человека уже всё поднялось не вовремя. Отбор по метрике тоже не спасает:
// он опирается на то, что домашний маршрут окажется лучшим, а это как раз то,
// что виртуальные адаптеры и ломают. Поэтому вопрос ставится иначе: не «какой
// шлюз главный», а «видим ли мы домашний роутер рядом». Хоть один домашний
// среди всех — значит дома.
func MakiShlyuzov() ([]string, error) {
	shlyuzy, err := shlyuzyFizicheskihAdapterov()
	if err != nil {
		return nil, err
	}
	var maki []string
	var bedy []string
	for _, sh := range shlyuzy {
		mak, err := makSoseda(sh.adres, sh.indeks)
		if err != nil {
			// Один недоступный сосед не повод хоронить заход: у виртуального
			// адаптера ARP-записи может не быть вовсе, а домашний роутер рядом
			// и отвечает. Причины копим и рассказываем, только если не вышло
			// НИ С ОДНИМ.
			bedy = append(bedy, fmt.Sprintf("шлюз %s: %v", sh.adres, err))
			continue
		}
		maki = append(maki, mak)
	}
	if len(maki) == 0 {
		return nil, fmt.Errorf("ни один шлюз не опознан: %s", strings.Join(bedy, "; "))
	}
	return maki, nil
}

// shlyuzIntrfeysa — шлюз и индекс интерфейса, на котором он найден.
type shlyuzIntrfeysa struct {
	adres  string
	indeks uint32
}

// shlyuzFizicheskogoAdaptera — IPv4 шлюза и индекс интерфейса физического
// адаптера.
//
// Список берётся у GetAdaptersInfo, а не у GetAdaptersAddresses (которым
// пользуется [SetevoyAdapter]): шлюз там объявлен отдельным полем
// GatewayList, тогда как в нашей версии golang.org/x/sys/windows у
// IP_ADAPTER_ADDRESSES поле FirstGatewayAddress не объявлено вовсе, и
// доставать его пришлось бы арифметикой по смещению — то есть держать
// раскладку чужой структуры в своей голове. Отбор «что считать физическим»
// повторяет [SetevoyAdapter] по смыслу: не loopback, не туннель, не наш
// собственный TUN.
func shlyuzyFizicheskihAdapterov() ([]shlyuzIntrfeysa, error) {
	adaptery, err := poluchitSvedeniyaAdapterov()
	if err != nil {
		return nil, err
	}
	var nayden []shlyuzIntrfeysa
	for a := adaptery; a != nil; a = a.Next {
		if a.Type == windows.IF_TYPE_SOFTWARE_LOOPBACK || a.Type == windows.IF_TYPE_TUNNEL || a.Type == propVirtual {
			continue
		}
		if svoyPoOpisaniyu(a) {
			continue
		}
		gw := pervyyShlyuzIPv4(&a.GatewayList)
		if gw == "" {
			continue
		}
		// Не return: перебор идёт до конца списка. Ровно на раннем выходе
		// отсюда и сломалось опознание дома 09.09 (см. MakiShlyuzov).
		nayden = append(nayden, shlyuzIntrfeysa{adres: gw, indeks: a.Index})
	}
	if len(nayden) == 0 {
		return nil, fmt.Errorf("не нашёл физический адаптер со шлюзом IPv4")
	}
	return nayden, nil
}

// svoyPoOpisaniyu — тот же фильтр, что и svoyAdapter, но по ANSI-описанию
// из IP_ADAPTER_INFO (в GetAdaptersAddresses описание широкое, тут узкое).
func svoyPoOpisaniyu(a *windows.IpAdapterInfo) bool {
	opisaniye := strings.ToLower(strokaIzBaytov(a.Description[:]))
	imya := strings.ToLower(strokaIzBaytov(a.AdapterName[:]))
	for _, slovo := range slovaSvoegoAdaptera {
		if strings.Contains(opisaniye, slovo) || strings.Contains(imya, slovo) {
			return true
		}
	}
	return false
}

func strokaIzBaytov(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

// pervyyShlyuzIPv4 — первый непустой шлюз из списка. Windows кладёт в
// GatewayList «0.0.0.0» тем адаптерам, у которых шлюза нет, — такую запись
// считаем отсутствием шлюза, иначе адаптер без выхода наружу выглядел бы
// подходящим и уводил бы поиск с настоящего.
func pervyyShlyuzIPv4(spisok *windows.IpAddrString) string {
	for s := spisok; s != nil; s = s.Next {
		adres := strokaIzBaytov(s.IpAddress.String[:])
		ip := net.ParseIP(adres)
		if ip == nil {
			continue
		}
		v4 := ip.To4()
		if v4 == nil || v4.IsUnspecified() {
			continue
		}
		return v4.String()
	}
	return ""
}

// poluchitSvedeniyaAdapterov — обёртка над GetAdaptersInfo с ростом буфера:
// тот же протокол, что и у GetAdaptersAddresses (см. poluchitAdaptery).
func poluchitSvedeniyaAdapterov() (*windows.IpAdapterInfo, error) {
	razmer := uint32(15 * 1024)
	for popytka := 0; popytka < 3; popytka++ {
		bufer := make([]byte, razmer)
		svedeniya := (*windows.IpAdapterInfo)(unsafe.Pointer(&bufer[0]))
		err := windows.GetAdaptersInfo(svedeniya, &razmer)
		if err == nil {
			return svedeniya, nil
		}
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue // razmer уже обновлён вызовом
		}
		return nil, fmt.Errorf("GetAdaptersInfo: %w", err)
	}
	return nil, fmt.Errorf("GetAdaptersInfo: буфер не подошёл за 3 попытки")
}

// Таблица соседей (ARP) в golang.org/x/sys/windows не объявлена вовсе,
// поэтому объявляем сами. Это обычное объявление системного вызова, а не
// угадывание чужой раскладки: MIB_IPNET_ROW2 описана в netioapi.h и не
// менялась с Vista.
var (
	modiphlpapiSosedi     = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetIpNetTable2    = modiphlpapiSosedi.NewProc("GetIpNetTable2")
	procFreeMibTable      = modiphlpapiSosedi.NewProc("FreeMibTable")
	procResolveIpNetEntry = modiphlpapiSosedi.NewProc("ResolveIpNetEntry2")
)

// mibIPNetRow2 — MIB_IPNET_ROW2 (netioapi.h), поле в поле. Лишние для нас
// State/Flags/ReachabilityTime объявлены нарочно: размер строки задаёт шаг
// перебора таблицы, и «сократить» структуру значит читать чужую память.
type mibIPNetRow2 struct {
	Address          sockaddrInet
	InterfaceIndex   uint32
	InterfaceLuid    uint64
	PhysicalAddress  [32]byte
	PhysicalAddrLen  uint32
	State            int32
	Flags            uint8
	_                [3]byte
	ReachabilityTime uint32
}

// mibIPNetTable2 — MIB_IPNET_TABLE2: число строк и сами строки. Table
// объявлена массивом из одного элемента, как ANY_SIZE в заголовке; настоящая
// длина берётся из NumEntries через unsafe.Slice.
type mibIPNetTable2 struct {
	NumEntries uint32
	Table      [1]mibIPNetRow2
}

// sockaddrInet — SOCKADDR_INET: объединение IPv4/IPv6, различаются семейством
// в первых двух байтах. Нам нужен только IPv4.
type sockaddrInet struct {
	Family uint16
	Port   uint16
	Data   [24]byte
}

func (s *sockaddrInet) ipv4() net.IP {
	if s.Family != windows.AF_INET {
		return nil
	}
	// SOCKADDR_IN: за семейством и портом идут четыре байта адреса.
	return net.IPv4(s.Data[0], s.Data[1], s.Data[2], s.Data[3])
}

// makSoseda — MAC соседа по его IPv4 на заданном интерфейсе.
//
// Сначала читаем таблицу соседей: это кеш ARP, чтение ничего не шлёт в сеть
// и стоит микросекунды. Записи нет или она пустая (машина только что
// подключилась к сети) — просим Windows разрешить адрес и читаем второй раз.
//
// «Соседа нет» уходит наверх ОШИБКОЙ, а не пустой строкой, и авторежим
// обязан прочесть её как «не знаю». Иначе первые секунды в любой сети
// выглядели бы уходом из дома.
func makSoseda(adres string, indeks uint32) (string, error) {
	mak, err := iskatSoseda(adres, indeks)
	if err == nil {
		return mak, nil
	}
	if e := razreshitSoseda(adres, indeks); e != nil {
		return "", fmt.Errorf("%w; разрешить не вышло: %v", err, e)
	}
	return iskatSoseda(adres, indeks)
}

func iskatSoseda(adres string, indeks uint32) (string, error) {
	iskomyy := net.ParseIP(adres).To4()
	if iskomyy == nil {
		return "", fmt.Errorf("адрес шлюза %q не IPv4", adres)
	}
	// Указатель на таблицу — типизированный, а не uintptr: приведение
	// uintptr обратно в unsafe.Pointer запрещено (go vet ловит это как
	// «possible misuse of unsafe.Pointer»), и запрет по делу — между двумя
	// строками сборщик мусора вправе переставить память.
	var tablica *mibIPNetTable2
	// Только AF_INET: лишние строки IPv6 удлиняют перебор и нам не нужны.
	r, _, _ := procGetIpNetTable2.Call(uintptr(windows.AF_INET), uintptr(unsafe.Pointer(&tablica)))
	if r != 0 {
		return "", fmt.Errorf("GetIpNetTable2: код %d", r)
	}
	defer procFreeMibTable.Call(uintptr(unsafe.Pointer(tablica)))

	if tablica == nil || tablica.NumEntries == 0 {
		return "", fmt.Errorf("таблица соседей пуста")
	}
	for _, stroka := range unsafe.Slice(&tablica.Table[0], tablica.NumEntries) {
		if stroka.InterfaceIndex != indeks {
			continue
		}
		if !stroka.Address.ipv4().Equal(iskomyy) {
			continue
		}
		if stroka.PhysicalAddrLen == 0 {
			return "", fmt.Errorf("сосед %s есть в таблице, но без аппаратного адреса", adres)
		}
		return makStrokoy(stroka.PhysicalAddress[:stroka.PhysicalAddrLen]), nil
	}
	return "", fmt.Errorf("соседа %s в таблице нет", adres)
}

// razreshitSoseda — попросить Windows разрешить адрес (послать ARP).
func razreshitSoseda(adres string, indeks uint32) error {
	v4 := net.ParseIP(adres).To4()
	if v4 == nil {
		return fmt.Errorf("адрес шлюза %q не IPv4", adres)
	}
	stroka := mibIPNetRow2{InterfaceIndex: indeks}
	stroka.Address.Family = windows.AF_INET
	copy(stroka.Address.Data[0:4], v4)
	r, _, _ := procResolveIpNetEntry.Call(uintptr(unsafe.Pointer(&stroka)), 0)
	if r != 0 {
		return fmt.Errorf("ResolveIpNetEntry2 для %s: код %d", adres, r)
	}
	return nil
}

func makStrokoy(b []byte) string {
	chasti := make([]string, 0, len(b))
	for _, x := range b {
		chasti = append(chasti, fmt.Sprintf("%02x", x))
	}
	return strings.Join(chasti, ":")
}
