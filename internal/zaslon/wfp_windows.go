//go:build windows

// Настоящий запрет — фильтры WFP (Windows Filtering Platform) в динамической
// сессии FWPM_SESSION_FLAG_DYNAMIC: все объекты сессии (поставщик, подслой,
// фильтры) Windows удаляет сама, как только закрыт дескриптор сессии, — в
// том числе когда процесс службы умер. Вечной блокировки интернета не бывает.
//
// Устройство перенесено из wireguard-windows (golang.zx2c4.com/wireguard/windows,
// tunnel/firewall, тег v0.5.3; SPDX-License-Identifier: MIT, Copyright (C)
// 2019-2021 WireGuard LLC, полный текст — LICENSE.wireguard рядом): сессия,
// поставщик, подслой с наибольшим весом, разрешения своих программ по
// ALE_APP_ID, петля, DHCP и «запретить всё» с наименьшим весом. Отличия:
//   - только исходящие (слои ALE_AUTH_CONNECT_V4/V6): входящие не трогаем;
//   - разрешение не «своего процесса», а списка программ по пути (ядро
//     sing-box.exe — отдельный процесс — и служба Kelevra.exe), без условия
//     ALE_USER_ID;
//   - разрешена локальная сеть (RFC1918, link-local, ULA, мультикаст) — дом
//     и шлюз не ломаются;
//   - адреса и маски для условий живут в переменных пакета, а не на стеке
//     функции: в условие WFP указатель уходит числом (uintptr), и сборщик
//     мусора Go о нём не знает;
//   - ошибка вызова — его код возврата (DWORD), а не GetLastError.

package zaslon

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// NovayaSistema — настоящая система этой ОС: WFP. Нужны права системы или
// администратора (служба Kelevra работает от системы).
func NovayaSistema() Sistema { return &wfp{} }

type wfp struct {
	mu      sync.Mutex
	sessiya uintptr
}

// Веса фильтров внутри своего подслоя: решает совпавший фильтр с наибольшим.
const (
	vesProgramm = 15
	vesPetli    = 14
	vesDhcp     = 13
	vesSeti     = 12
	vesZapreta  = 0
)

// Локальная сеть. Переменные пакета, а не литералы в функции: см. шапку.
var (
	lokalnyeV4 = [...]wtFwpV4AddrAndMask{
		{0x0A000000, 0xFF000000}, // 10.0.0.0/8
		{0xAC100000, 0xFFF00000}, // 172.16.0.0/12
		{0xC0A80000, 0xFFFF0000}, // 192.168.0.0/16
		{0xA9FE0000, 0xFFFF0000}, // 169.254.0.0/16 link-local
		{0xE0000000, 0xF0000000}, // 224.0.0.0/4 мультикаст (mDNS, SSDP)
		{0xFFFFFFFF, 0xFFFFFFFF}, // 255.255.255.255 широковещание
	}
	lokalnyeV6 = [...]wtFwpV6AddrAndMask{
		{[16]uint8{0xfe, 0x80}, 10}, // fe80::/10 link-local (в том числе NDP)
		{[16]uint8{0xfc}, 7},        // fc00::/7 ULA
		{[16]uint8{0xff}, 8},        // ff00::/8 мультикаст (NDP, DHCPv6, mDNS)
	}
)

func (w *wfp) Zakryt(programmy []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sessiya != 0 {
		return nil
	}
	if err := modfwpuclnt.Load(); err != nil {
		return fmt.Errorf("WFP недоступен: %w", err)
	}
	if len(programmy) == 0 {
		// Без разрешения ядру туннель не поднимется вовсе: запрет держал бы
		// человека без сети до срока.
		return fmt.Errorf("не названо ни одной программы, которой можно наружу")
	}
	sessiya, err := otkrytSessiyu()
	if err != nil {
		return err
	}
	if err := vTranzakcii(sessiya, func() error { return postavitFiltry(sessiya, programmy) }); err != nil {
		_ = fwpmEngineClose0(sessiya)
		return err
	}
	w.sessiya = sessiya
	return nil
}

func (w *wfp) Otkryt() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sessiya == 0 {
		return nil
	}
	// Закрытие динамической сессии снимает её фильтры. Не закрылась — дескриптор
	// не теряем: иначе снять запрет было бы уже нечем до смерти службы. Ещё
	// попытка сразу, не вышло — сессия остаётся, следующий Otkryt повторит.
	err := fwpmEngineClose0(w.sessiya)
	if err != nil {
		time.Sleep(200 * time.Millisecond)
		err = fwpmEngineClose0(w.sessiya)
	}
	if err != nil {
		return fmt.Errorf("сессия WFP не закрылась, повторю при следующем снятии: %w", err)
	}
	w.sessiya = 0
	return nil
}

func otkrytSessiyu() (uintptr, error) {
	dd, err := podpis("Kelevra", "Kelevra: перезапуск ядра")
	if err != nil {
		return 0, err
	}
	sessiya := wtFwpmSession0{
		displayData: *dd,
		flags:       cFWPM_SESSION_FLAG_DYNAMIC,
		// Не INFINITE: транзакцию BFE может держать чужая программа (антивирус,
		// другой VPN), и тогда перезапуск висел бы под zamokPodyoma вечно, а с
		// ним «Подключиться» и «Отключить». Не дождались — ошибка, перезапуск как раньше.
		txnWaitTimeoutInMSec: 5000,
	}
	var h uintptr
	if err := fwpmEngineOpen0(&sessiya, &h); err != nil {
		return 0, err
	}
	return h, nil
}

func vTranzakcii(sessiya uintptr, delo func() error) error {
	if err := fwpmTransactionBegin0(sessiya); err != nil {
		return err
	}
	if err := delo(); err != nil {
		_ = fwpmTransactionAbort0(sessiya)
		return err
	}
	if err := fwpmTransactionCommit0(sessiya); err != nil {
		_ = fwpmTransactionAbort0(sessiya)
		return err
	}
	return nil
}

// postavitFiltry — поставщик, подслой и фильтры одной транзакцией.
func postavitFiltry(sessiya uintptr, programmy []string) error {
	postavshchik, err := windows.GenerateGUID()
	if err != nil {
		return err
	}
	podsloy, err := windows.GenerateGUID()
	if err != nil {
		return err
	}
	dd, err := podpis("Kelevra", "Kelevra: перезапуск ядра")
	if err != nil {
		return err
	}
	if err := fwpmProviderAdd0(sessiya, &wtFwpmProvider0{providerKey: postavshchik, displayData: *dd}); err != nil {
		return err
	}
	dd, err = podpis("Kelevra: трафик мимо туннеля", "Разрешения ядра и службы, затем запрет остального исходящего")
	if err != nil {
		return err
	}
	if err := fwpmSubLayerAdd0(sessiya, &wtFwpmSublayer0{
		subLayerKey: podsloy,
		displayData: *dd,
		providerKey: &postavshchik,
		weight:      ^uint16(0),
	}); err != nil {
		return err
	}
	f := filtry{sessiya: sessiya, postavshchik: &postavshchik, podsloy: podsloy}

	// Свои программы: ядро (до узлов VPS) и служба.
	for _, put := range programmy {
		if err := f.razreshitProgrammu(put); err != nil {
			return err
		}
	}
	// Петля.
	petlya := []wtFwpmFilterCondition0{{
		fieldKey:       cFWPM_CONDITION_FLAGS,
		matchType:      cFWP_MATCH_FLAGS_ALL_SET,
		conditionValue: wtFwpConditionValue0{_type: cFWP_UINT32, value: cFWP_CONDITION_FLAG_IS_LOOPBACK},
	}}
	if err := f.oba("петля", petlya, vesPetli, cFWP_ACTION_PERMIT); err != nil {
		return err
	}
	// DHCP: запрос клиента 68 → 67 (IPv4) и 546 → 547 (IPv6) на любой адрес —
	// сервер DHCP в гостинице или у провайдера бывает и не в частной сети.
	if err := f.dobavit("DHCP", cFWPM_LAYER_ALE_AUTH_CONNECT_V4, portyUdp(68, 67), vesDhcp, cFWP_ACTION_PERMIT); err != nil {
		return err
	}
	if err := f.dobavit("DHCPv6", cFWPM_LAYER_ALE_AUTH_CONNECT_V6, portyUdp(546, 547), vesDhcp, cFWP_ACTION_PERMIT); err != nil {
		return err
	}
	// Локальная сеть: несколько условий одного поля — «или».
	var setV4, setV6 []wtFwpmFilterCondition0
	for i := range lokalnyeV4 {
		setV4 = append(setV4, wtFwpmFilterCondition0{
			fieldKey:       cFWPM_CONDITION_IP_REMOTE_ADDRESS,
			matchType:      cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{_type: cFWP_V4_ADDR_MASK, value: uintptr(unsafe.Pointer(&lokalnyeV4[i]))},
		})
	}
	for i := range lokalnyeV6 {
		setV6 = append(setV6, wtFwpmFilterCondition0{
			fieldKey:       cFWPM_CONDITION_IP_REMOTE_ADDRESS,
			matchType:      cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{_type: cFWP_V6_ADDR_MASK, value: uintptr(unsafe.Pointer(&lokalnyeV6[i]))},
		})
	}
	if err := f.dobavit("локальная сеть", cFWPM_LAYER_ALE_AUTH_CONNECT_V4, setV4, vesSeti, cFWP_ACTION_PERMIT); err != nil {
		return err
	}
	if err := f.dobavit("локальная сеть", cFWPM_LAYER_ALE_AUTH_CONNECT_V6, setV6, vesSeti, cFWP_ACTION_PERMIT); err != nil {
		return err
	}
	// Всё остальное исходящее — запрет.
	return f.oba("остальное исходящее", nil, vesZapreta, cFWP_ACTION_BLOCK)
}

func portyUdp(svoy, chuzhoy uint16) []wtFwpmFilterCondition0 {
	return []wtFwpmFilterCondition0{
		{fieldKey: cFWPM_CONDITION_IP_PROTOCOL, matchType: cFWP_MATCH_EQUAL, conditionValue: wtFwpConditionValue0{_type: cFWP_UINT8, value: cIPPROTO_UDP}},
		{fieldKey: cFWPM_CONDITION_IP_LOCAL_PORT, matchType: cFWP_MATCH_EQUAL, conditionValue: wtFwpConditionValue0{_type: cFWP_UINT16, value: uintptr(svoy)}},
		{fieldKey: cFWPM_CONDITION_IP_REMOTE_PORT, matchType: cFWP_MATCH_EQUAL, conditionValue: wtFwpConditionValue0{_type: cFWP_UINT16, value: uintptr(chuzhoy)}},
	}
}

type filtry struct {
	sessiya      uintptr
	postavshchik *windows.GUID
	podsloy      windows.GUID
}

// razreshitProgrammu — всё исходящее программы put (ALE_APP_ID по пути).
func (f filtry) razreshitProgrammu(put string) error {
	putW, err := windows.UTF16PtrFromString(put)
	if err != nil {
		return err
	}
	// Блоб с номером программы выделяет сам WFP (память не Go — не движется
	// и не собирается); WFP копирует его в фильтр, после — освобождаем.
	var appID *wtFwpByteBlob
	if err := fwpmGetAppIdFromFileName0(putW, &appID); err != nil {
		return fmt.Errorf("%s: %w", put, err)
	}
	defer fwpmFreeMemory0(unsafe.Pointer(&appID))
	usloviya := []wtFwpmFilterCondition0{{
		fieldKey:       cFWPM_CONDITION_ALE_APP_ID,
		matchType:      cFWP_MATCH_EQUAL,
		conditionValue: wtFwpConditionValue0{_type: cFWP_BYTE_BLOB_TYPE, value: uintptr(unsafe.Pointer(appID))},
	}}
	return f.oba("программа "+put, usloviya, vesProgramm, cFWP_ACTION_PERMIT)
}

// oba — тот же фильтр на IPv4 и IPv6.
func (f filtry) oba(chto string, usloviya []wtFwpmFilterCondition0, ves uint8, deystvie wtFwpActionType) error {
	if err := f.dobavit(chto, cFWPM_LAYER_ALE_AUTH_CONNECT_V4, usloviya, ves, deystvie); err != nil {
		return err
	}
	return f.dobavit(chto, cFWPM_LAYER_ALE_AUTH_CONNECT_V6, usloviya, ves, deystvie)
}

func (f filtry) dobavit(chto string, sloy windows.GUID, usloviya []wtFwpmFilterCondition0, ves uint8, deystvie wtFwpActionType) error {
	imya := "Kelevra: " + chto
	if deystvie == cFWP_ACTION_BLOCK {
		imya = "Kelevra: запрет, " + chto
	}
	dd, err := podpis(imya, "")
	if err != nil {
		return err
	}
	filtr := wtFwpmFilter0{
		displayData: *dd,
		providerKey: f.postavshchik,
		layerKey:    sloy,
		subLayerKey: f.podsloy,
		weight:      wtFwpValue0{_type: cFWP_UINT8, value: uintptr(ves)},
		action:      wtFwpmAction0{_type: deystvie},
	}
	if len(usloviya) > 0 {
		filtr.numFilterConditions = uint32(len(usloviya))
		filtr.filterCondition = &usloviya[0]
	}
	var id uint64
	err = fwpmFilterAdd0(f.sessiya, &filtr, &id)
	runtime.KeepAlive(usloviya)
	if err != nil {
		return fmt.Errorf("фильтр «%s»: %w", chto, err)
	}
	return nil
}

func podpis(imya, opisanie string) (*wtFwpmDisplayData0, error) {
	i, err := windows.UTF16PtrFromString(imya)
	if err != nil {
		return nil, err
	}
	o, err := windows.UTF16PtrFromString(opisanie)
	if err != nil {
		return nil, err
	}
	return &wtFwpmDisplayData0{name: i, description: o}, nil
}

// --- вызовы fwpuclnt.dll ---
//
// Каждый вызов SyscallN — с uintptr(unsafe.Pointer(...)) прямо в его
// аргументах: только так Go держит объект живым и на месте до конца вызова.

var (
	modfwpuclnt = windows.NewLazySystemDLL("fwpuclnt.dll")

	procFwpmEngineOpen0           = modfwpuclnt.NewProc("FwpmEngineOpen0")
	procFwpmEngineClose0          = modfwpuclnt.NewProc("FwpmEngineClose0")
	procFwpmProviderAdd0          = modfwpuclnt.NewProc("FwpmProviderAdd0")
	procFwpmSubLayerAdd0          = modfwpuclnt.NewProc("FwpmSubLayerAdd0")
	procFwpmFilterAdd0            = modfwpuclnt.NewProc("FwpmFilterAdd0")
	procFwpmGetAppIdFromFileName0 = modfwpuclnt.NewProc("FwpmGetAppIdFromFileName0")
	procFwpmFreeMemory0           = modfwpuclnt.NewProc("FwpmFreeMemory0")
	procFwpmTransactionBegin0     = modfwpuclnt.NewProc("FwpmTransactionBegin0")
	procFwpmTransactionCommit0    = modfwpuclnt.NewProc("FwpmTransactionCommit0")
	procFwpmTransactionAbort0     = modfwpuclnt.NewProc("FwpmTransactionAbort0")
)

// oshibkaWfp — функции Fwpm* возвращают код ошибки (DWORD) сами.
func oshibkaWfp(imya string, r1 uintptr) error {
	if r1 == 0 {
		return nil
	}
	return fmt.Errorf("%s: %w (0x%08X)", imya, syscall.Errno(r1), uint32(r1))
}

func fwpmEngineOpen0(sessiya *wtFwpmSession0, h *uintptr) error {
	if err := procFwpmEngineOpen0.Find(); err != nil {
		return err
	}
	r1, _, _ := syscall.SyscallN(procFwpmEngineOpen0.Addr(), 0, cRPC_C_AUTHN_WINNT, 0, uintptr(unsafe.Pointer(sessiya)), uintptr(unsafe.Pointer(h)))
	return oshibkaWfp("FwpmEngineOpen0", r1)
}

func fwpmEngineClose0(h uintptr) error {
	if err := procFwpmEngineClose0.Find(); err != nil {
		return err
	}
	r1, _, _ := syscall.SyscallN(procFwpmEngineClose0.Addr(), h)
	return oshibkaWfp("FwpmEngineClose0", r1)
}

func fwpmProviderAdd0(h uintptr, p *wtFwpmProvider0) error {
	if err := procFwpmProviderAdd0.Find(); err != nil {
		return err
	}
	r1, _, _ := syscall.SyscallN(procFwpmProviderAdd0.Addr(), h, uintptr(unsafe.Pointer(p)), 0)
	return oshibkaWfp("FwpmProviderAdd0", r1)
}

func fwpmSubLayerAdd0(h uintptr, s *wtFwpmSublayer0) error {
	if err := procFwpmSubLayerAdd0.Find(); err != nil {
		return err
	}
	r1, _, _ := syscall.SyscallN(procFwpmSubLayerAdd0.Addr(), h, uintptr(unsafe.Pointer(s)), 0)
	return oshibkaWfp("FwpmSubLayerAdd0", r1)
}

func fwpmFilterAdd0(h uintptr, f *wtFwpmFilter0, id *uint64) error {
	if err := procFwpmFilterAdd0.Find(); err != nil {
		return err
	}
	r1, _, _ := syscall.SyscallN(procFwpmFilterAdd0.Addr(), h, uintptr(unsafe.Pointer(f)), 0, uintptr(unsafe.Pointer(id)))
	return oshibkaWfp("FwpmFilterAdd0", r1)
}

func fwpmGetAppIdFromFileName0(put *uint16, appID **wtFwpByteBlob) error {
	if err := procFwpmGetAppIdFromFileName0.Find(); err != nil {
		return err
	}
	r1, _, _ := syscall.SyscallN(procFwpmGetAppIdFromFileName0.Addr(), uintptr(unsafe.Pointer(put)), uintptr(unsafe.Pointer(appID)))
	return oshibkaWfp("FwpmGetAppIdFromFileName0", r1)
}

func fwpmFreeMemory0(p unsafe.Pointer) {
	if procFwpmFreeMemory0.Find() != nil {
		return
	}
	_, _, _ = syscall.SyscallN(procFwpmFreeMemory0.Addr(), uintptr(p))
}

func fwpmTransactionBegin0(h uintptr) error {
	if err := procFwpmTransactionBegin0.Find(); err != nil {
		return err
	}
	r1, _, _ := syscall.SyscallN(procFwpmTransactionBegin0.Addr(), h, 0)
	return oshibkaWfp("FwpmTransactionBegin0", r1)
}

func fwpmTransactionCommit0(h uintptr) error {
	if err := procFwpmTransactionCommit0.Find(); err != nil {
		return err
	}
	r1, _, _ := syscall.SyscallN(procFwpmTransactionCommit0.Addr(), h)
	return oshibkaWfp("FwpmTransactionCommit0", r1)
}

func fwpmTransactionAbort0(h uintptr) error {
	if err := procFwpmTransactionAbort0.Find(); err != nil {
		return err
	}
	r1, _, _ := syscall.SyscallN(procFwpmTransactionAbort0.Addr(), h)
	return oshibkaWfp("FwpmTransactionAbort0", r1)
}
