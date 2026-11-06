//go:build windows

// Настоящий запрет — фильтры WFP (Windows Filtering Platform) в динамической
// сессии FWPM_SESSION_FLAG_DYNAMIC: все объекты сессии (поставщик, подслой,
// фильтры) Windows удаляет сама, как только закрыт дескриптор сессии, — в
// том числе когда процесс службы умер. Вечной блокировки интернета не бывает.
//
// Сессия одна на процесс и живёт, пока стоит хоть какое-то правило: смена
// правил — одна транзакция (удалить прежние фильтры по номерам, добавить
// новые), снятие всего — закрытие сессии. Поставщик и подслой — со случайными
// GUID на каждую сессию: при обновлении держатель и новая служба держат свои
// сессии одновременно, и одинаковые ключи не дали бы второй встать.
//
// Устройство перенесено из wireguard-windows (golang.zx2c4.com/wireguard/windows,
// tunnel/firewall, тег v0.5.3; SPDX-License-Identifier: MIT, Copyright (C)
// 2019-2021 WireGuard LLC, полный текст — LICENSE.wireguard рядом): сессия,
// поставщик, подслой с наибольшим весом, разрешения своих программ по
// ALE_APP_ID, петля, DHCP и «запретить всё» с наименьшим весом. Отличия:
//   - только исходящие (слои ALE_AUTH_CONNECT_V4/V6): входящие не трогаем;
//   - набор фильтров — данные (filtry.go), здесь только перевод в FWPM_FILTER0;
//   - разрешение не «своего процесса», а списка программ по пути (ядро
//     sing-box.exe — отдельный процесс — и служба Kelevra.exe), без условия
//     ALE_USER_ID;
//   - адреса и маски для условий выделяются в куче и держатся в wfp.hranit до
//     конца транзакции: в условие WFP указатель уходит числом (uintptr), и
//     сборщик мусора Go о нём не знает;
//   - ошибка вызова — его код возврата (DWORD), а не GetLastError.

package zaslon

import (
	"fmt"
	"runtime"
	"sort"
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
	mu           sync.Mutex
	sessiya      uintptr
	postavshchik windows.GUID
	podsloy      windows.GUID
	nomera       []uint64 // фильтры, которые стоят сейчас
	hranit       []any    // память условий текущей транзакции (см. шапку)
}

// Primenit — заменить фильтры одной транзакцией. Не вышло — стоят прежние.
//
// Не вышло в своей сессии (номер фильтра потерян, дескриптор умер вместе с
// перезапуском BFE) — второй заход в НОВОЙ сессии рядом: она встаёт целиком,
// и только потом закрывается старая. Промежутка без правил нет и тут.
func (w *wfp) Primenit(filtry []Filtr) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := modfwpuclnt.Load(); err != nil {
		return fmt.Errorf("WFP недоступен: %w", err)
	}
	if w.sessiya == 0 {
		if err := w.otkrytSessiyu(); err != nil {
			return err
		}
		return w.zamenit(filtry)
	}
	err := w.zamenit(filtry)
	if err == nil {
		return nil
	}
	staraya, stPost, stPods, stNomera := w.sessiya, w.postavshchik, w.podsloy, w.nomera
	vernut := func() { w.sessiya, w.postavshchik, w.podsloy, w.nomera = staraya, stPost, stPods, stNomera }
	if err2 := w.otkrytSessiyu(); err2 != nil {
		vernut()
		return fmt.Errorf("%w; новая сессия не открылась: %v", err, err2)
	}
	if err2 := w.zamenit(filtry); err2 != nil {
		_ = fwpmEngineClose0(w.sessiya)
		vernut()
		return fmt.Errorf("%w; в новой сессии: %v", err, err2)
	}
	_ = fwpmEngineClose0(staraya)
	return nil
}

// zamenit — в текущей сессии: снять прежние фильтры, поставить новые.
func (w *wfp) zamenit(filtry []Filtr) error {
	var novye []uint64
	err := vTranzakcii(w.sessiya, func() error {
		for _, id := range w.nomera {
			if err := fwpmFilterDeleteById0(w.sessiya, id); err != nil {
				return fmt.Errorf("снять фильтр %d: %w", id, err)
			}
		}
		for _, f := range filtry {
			id, err := w.dobavit(f)
			if err != nil {
				return err
			}
			novye = append(novye, id)
		}
		return nil
	})
	w.hranit = nil
	if err != nil {
		return err
	}
	w.nomera = novye
	return nil
}

// Snyat — закрыть сессию: Windows снимает все её объекты. Не закрылась —
// дескриптор не теряем: иначе снять запрет было бы нечем до смерти процесса.
func (w *wfp) Snyat() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sessiya == 0 {
		return nil
	}
	err := fwpmEngineClose0(w.sessiya)
	if err != nil {
		time.Sleep(200 * time.Millisecond)
		err = fwpmEngineClose0(w.sessiya)
	}
	if err != nil {
		return fmt.Errorf("сессия WFP не закрылась, повторю при следующем снятии: %w", err)
	}
	w.sessiya, w.nomera = 0, nil
	return nil
}

// otkrytSessiyu — сессия, затем поставщик и подслой одной транзакцией.
func (w *wfp) otkrytSessiyu() error {
	dd, err := podpis("Kelevra", "Kelevra: запрет мимо туннеля")
	if err != nil {
		return err
	}
	opisanie := wtFwpmSession0{
		displayData: *dd,
		flags:       cFWPM_SESSION_FLAG_DYNAMIC,
		// Не INFINITE: транзакцию BFE может держать чужая программа (антивирус,
		// другой VPN), и тогда «Отключить» ждало бы вечно. Не дождались —
		// ошибка, правила прежние, повтор через 5 с.
		txnWaitTimeoutInMSec: 5000,
	}
	var h uintptr
	if err := fwpmEngineOpen0(&opisanie, &h); err != nil {
		return err
	}
	postavshchik, err := windows.GenerateGUID()
	if err != nil {
		_ = fwpmEngineClose0(h)
		return err
	}
	podsloy, err := windows.GenerateGUID()
	if err != nil {
		_ = fwpmEngineClose0(h)
		return err
	}
	err = vTranzakcii(h, func() error {
		dd, err := podpis("Kelevra", "Kelevra: запрет мимо туннеля")
		if err != nil {
			return err
		}
		if err := fwpmProviderAdd0(h, &wtFwpmProvider0{providerKey: postavshchik, displayData: *dd}); err != nil {
			return err
		}
		dd, err = podpis("Kelevra: трафик мимо туннеля",
			"Anthropic только через туннель; при полном запрете — туннель, ядро, служба, петля, DHCP, NDP, локальная сеть, остальное исходящее запрещено")
		if err != nil {
			return err
		}
		return fwpmSubLayerAdd0(h, &wtFwpmSublayer0{
			subLayerKey: podsloy,
			displayData: *dd,
			providerKey: &postavshchik,
			weight:      ^uint16(0),
		})
	})
	if err != nil {
		_ = fwpmEngineClose0(h)
		return err
	}
	w.sessiya, w.postavshchik, w.podsloy, w.nomera = h, postavshchik, podsloy, nil
	return nil
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

// dobavit — один Filtr как FWPM_FILTER0. Под w.mu, внутри транзакции.
func (w *wfp) dobavit(f Filtr) (uint64, error) {
	sloy := cFWPM_LAYER_ALE_AUTH_CONNECT_V4
	if f.Semeystvo == V6 {
		sloy = cFWPM_LAYER_ALE_AUTH_CONNECT_V6
	}
	// Условия одного поля — подряд (WFP сводит их в «или»).
	usloviya := append([]Uslovie(nil), f.Usloviya...)
	sort.SliceStable(usloviya, func(i, j int) bool { return usloviya[i].Pole < usloviya[j].Pole })
	var wt []wtFwpmFilterCondition0
	for _, u := range usloviya {
		c, osvobodit, err := w.uslovie(u)
		if err != nil {
			return 0, fmt.Errorf("фильтр «%s»: %w", f.Imya, err)
		}
		if osvobodit != nil {
			defer osvobodit()
		}
		wt = append(wt, c)
	}
	imya := "Kelevra: " + f.Imya
	deystvie := cFWP_ACTION_PERMIT
	if f.Zapret {
		imya = "Kelevra: запрет, " + f.Imya
		deystvie = cFWP_ACTION_BLOCK
	}
	dd, err := podpis(imya, "")
	if err != nil {
		return 0, err
	}
	postavshchik := new(windows.GUID)
	*postavshchik = w.postavshchik
	w.hranit = append(w.hranit, postavshchik)
	filtr := wtFwpmFilter0{
		displayData: *dd,
		providerKey: postavshchik,
		layerKey:    sloy,
		subLayerKey: w.podsloy,
		weight:      wtFwpValue0{_type: cFWP_UINT8, value: uintptr(f.Ves)},
		action:      wtFwpmAction0{_type: deystvie},
	}
	if len(wt) > 0 {
		filtr.numFilterConditions = uint32(len(wt))
		filtr.filterCondition = &wt[0]
	}
	var id uint64
	err = fwpmFilterAdd0(w.sessiya, &filtr, &id)
	runtime.KeepAlive(wt)
	if err != nil {
		return 0, fmt.Errorf("фильтр «%s»: %w", f.Imya, err)
	}
	return id, nil
}

// uslovie — FWPM_FILTER_CONDITION0. osvobodit — что освободить после
// добавления фильтра (блоб номера программы выделяет сам WFP).
func (w *wfp) uslovie(u Uslovie) (wtFwpmFilterCondition0, func(), error) {
	switch u.Pole {
	case PoleProgramma:
		putW, err := windows.UTF16PtrFromString(u.Put)
		if err != nil {
			return wtFwpmFilterCondition0{}, nil, err
		}
		// Блоб с номером программы выделяет сам WFP (память не Go — не движется
		// и не собирается); WFP копирует его в фильтр, после — освобождаем.
		var appID *wtFwpByteBlob
		if err := fwpmGetAppIdFromFileName0(putW, &appID); err != nil {
			return wtFwpmFilterCondition0{}, nil, fmt.Errorf("%s: %w", u.Put, err)
		}
		return wtFwpmFilterCondition0{
			fieldKey:       cFWPM_CONDITION_ALE_APP_ID,
			matchType:      cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{_type: cFWP_BYTE_BLOB_TYPE, value: uintptr(unsafe.Pointer(appID))},
		}, func() { fwpmFreeMemory0(unsafe.Pointer(&appID)) }, nil
	case PoleUdalennyy, PoleLokalnyy:
		pole := cFWPM_CONDITION_IP_REMOTE_ADDRESS
		if u.Pole == PoleLokalnyy {
			pole = cFWPM_CONDITION_IP_LOCAL_ADDRESS
		}
		a := u.Set.Masked().Addr()
		if a.Is4() {
			b := a.As4()
			m := &wtFwpV4AddrAndMask{
				addr: uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]),
				mask: maskaV4(u.Set.Bits()),
			}
			w.hranit = append(w.hranit, m)
			return wtFwpmFilterCondition0{fieldKey: pole, matchType: cFWP_MATCH_EQUAL,
				conditionValue: wtFwpConditionValue0{_type: cFWP_V4_ADDR_MASK, value: uintptr(unsafe.Pointer(m))}}, nil, nil
		}
		m := &wtFwpV6AddrAndMask{addr: a.As16(), prefixLength: uint8(u.Set.Bits())}
		w.hranit = append(w.hranit, m)
		return wtFwpmFilterCondition0{fieldKey: pole, matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{_type: cFWP_V6_ADDR_MASK, value: uintptr(unsafe.Pointer(m))}}, nil, nil
	case PolePetlya:
		return wtFwpmFilterCondition0{
			fieldKey:       cFWPM_CONDITION_FLAGS,
			matchType:      cFWP_MATCH_FLAGS_ALL_SET,
			conditionValue: wtFwpConditionValue0{_type: cFWP_UINT32, value: cFWP_CONDITION_FLAG_IS_LOOPBACK},
		}, nil, nil
	case PoleProtokol:
		return wtFwpmFilterCondition0{fieldKey: cFWPM_CONDITION_IP_PROTOCOL, matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{_type: cFWP_UINT8, value: uintptr(u.Chislo)}}, nil, nil
	case PoleLokalnyyPort:
		return wtFwpmFilterCondition0{fieldKey: cFWPM_CONDITION_IP_LOCAL_PORT, matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{_type: cFWP_UINT16, value: uintptr(u.Chislo)}}, nil, nil
	case PoleUdalennyyPort:
		return wtFwpmFilterCondition0{fieldKey: cFWPM_CONDITION_IP_REMOTE_PORT, matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{_type: cFWP_UINT16, value: uintptr(u.Chislo)}}, nil, nil
	}
	return wtFwpmFilterCondition0{}, nil, fmt.Errorf("незнакомое условие %d", u.Pole)
}

// maskaV4 — маска числом в порядке хоста: /23 → 0xFFFFFE00.
func maskaV4(bity int) uint32 {
	if bity <= 0 {
		return 0
	}
	return ^uint32(0) << (32 - bity)
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
	procFwpmFilterDeleteById0     = modfwpuclnt.NewProc("FwpmFilterDeleteById0")
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

func fwpmFilterDeleteById0(h uintptr, id uint64) error {
	if err := procFwpmFilterDeleteById0.Find(); err != nil {
		return err
	}
	// UINT64 по значению: на 64 битах одно слово, на 32 — два (argU64).
	r1, _, _ := syscall.SyscallN(procFwpmFilterDeleteById0.Addr(), append([]uintptr{h}, argU64(id)...)...)
	return oshibkaWfp("FwpmFilterDeleteById0", r1)
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
