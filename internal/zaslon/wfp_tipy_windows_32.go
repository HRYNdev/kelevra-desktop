//go:build windows && (386 || arm)

// FWPM_FILTER0 для 32 бит — из wireguard-windows, tunnel/firewall/types_windows_32.go,
// тег v0.5.3. SPDX-License-Identifier: MIT, Copyright (C) 2019-2021 WireGuard LLC.
// Полный текст лицензии — LICENSE.wireguard рядом.

package zaslon

import "golang.org/x/sys/windows"

// Размеры и сдвиги по fwpmtypes.h (сверяет wfp_tipy_windows_test.go).
const (
	wtFwpmFilter0_Size                       = 152
	wtFwpmFilter0_layerKey_Offset            = 40
	wtFwpmFilter0_weight_Offset              = 72
	wtFwpmFilter0_numFilterConditions_Offset = 80
	wtFwpmFilter0_filterCondition_Offset     = 84
	wtFwpmFilter0_action_Offset              = 88
	wtFwpmFilter0_providerContextKey_Offset  = 112
	wtFwpmFilter0_filterID_Offset            = 136
	wtFwpmFilterCondition0_Size              = 28
	wtFwpmSession0_Size                      = 48
	wtFwpmSublayer0_Size                     = 44
	wtFwpmSublayer0_weight_Offset            = 40
	wtFwpmProvider0_Size                     = 40
	wtFwpValue0_Size                         = 8
)

// FWPM_FILTER0 (fwpmtypes.h).
type wtFwpmFilter0 struct {
	filterKey           windows.GUID
	displayData         wtFwpmDisplayData0
	flags               uint32
	providerKey         *windows.GUID
	providerData        wtFwpByteBlob
	layerKey            windows.GUID
	subLayerKey         windows.GUID
	weight              wtFwpValue0
	numFilterConditions uint32
	filterCondition     *wtFwpmFilterCondition0
	action              wtFwpmAction0
	offset1             [4]byte // выравнивание объединения providerContextKey на 8
	providerContextKey  windows.GUID
	reserved            *windows.GUID
	offset2             [4]byte // выравнивание filterID на 8
	filterID            uint64
	effectiveWeight     wtFwpValue0
}
