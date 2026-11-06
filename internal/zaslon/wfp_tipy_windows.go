//go:build windows

// Типы и константы WFP перенесены из wireguard-windows, пакет tunnel/firewall,
// файл types_windows.go, тег v0.5.3 (golang.zx2c4.com/wireguard/windows):
// только то, что нужно запрету на время перезапуска ядра.
//
// SPDX-License-Identifier: MIT
// Copyright (C) 2019-2021 WireGuard LLC. All Rights Reserved.
// Полный текст лицензии — LICENSE.wireguard рядом.

package zaslon

import "golang.org/x/sys/windows"

// FWP_ACTION_TYPE (fwptypes.h).
type wtFwpActionType uint32

const (
	cFWP_ACTION_FLAG_TERMINATING                 = 0x00001000
	cFWP_ACTION_BLOCK            wtFwpActionType = 0x00000001 | cFWP_ACTION_FLAG_TERMINATING
	cFWP_ACTION_PERMIT           wtFwpActionType = 0x00000002 | cFWP_ACTION_FLAG_TERMINATING
)

// FWP_BYTE_BLOB (fwptypes.h).
type wtFwpByteBlob struct {
	size uint32
	data *uint8
}

// FWP_MATCH_TYPE (fwptypes.h).
type wtFwpMatchType uint32

const (
	cFWP_MATCH_EQUAL         wtFwpMatchType = 0
	cFWP_MATCH_FLAGS_ALL_SET wtFwpMatchType = 6
)

// FWPM_ACTION0 (fwpmtypes.h).
type wtFwpmAction0 struct {
	_type      wtFwpActionType
	filterType windows.GUID
}

// Поля условий (fwpmu.h).
var (
	// b235ae9a-1d64-49b8-a44c-5ff3d9095045
	cFWPM_CONDITION_IP_REMOTE_ADDRESS = windows.GUID{Data1: 0xb235ae9a, Data2: 0x1d64, Data3: 0x49b8, Data4: [8]byte{0xa4, 0x4c, 0x5f, 0xf3, 0xd9, 0x09, 0x50, 0x45}}
	// 3971ef2b-623e-4f9a-8cb1-6e79b806b9a7
	cFWPM_CONDITION_IP_PROTOCOL = windows.GUID{Data1: 0x3971ef2b, Data2: 0x623e, Data3: 0x4f9a, Data4: [8]byte{0x8c, 0xb1, 0x6e, 0x79, 0xb8, 0x06, 0xb9, 0xa7}}
	// 0c1ba1af-5765-453f-af22-a8f791ac775b
	cFWPM_CONDITION_IP_LOCAL_PORT = windows.GUID{Data1: 0x0c1ba1af, Data2: 0x5765, Data3: 0x453f, Data4: [8]byte{0xaf, 0x22, 0xa8, 0xf7, 0x91, 0xac, 0x77, 0x5b}}
	// c35a604d-d22b-4e1a-91b4-68f674ee674b
	cFWPM_CONDITION_IP_REMOTE_PORT = windows.GUID{Data1: 0xc35a604d, Data2: 0xd22b, Data3: 0x4e1a, Data4: [8]byte{0x91, 0xb4, 0x68, 0xf6, 0x74, 0xee, 0x67, 0x4b}}
	// d78e1e87-8644-4ea5-9437-d809ecefc971
	cFWPM_CONDITION_ALE_APP_ID = windows.GUID{Data1: 0xd78e1e87, Data2: 0x8644, Data3: 0x4ea5, Data4: [8]byte{0x94, 0x37, 0xd8, 0x09, 0xec, 0xef, 0xc9, 0x71}}
	// 632ce23b-5167-435c-86d7-e903684aa80c
	cFWPM_CONDITION_FLAGS = windows.GUID{Data1: 0x632ce23b, Data2: 0x5167, Data3: 0x435c, Data4: [8]byte{0x86, 0xd7, 0xe9, 0x03, 0x68, 0x4a, 0xa8, 0x0c}}
)

const cFWP_CONDITION_FLAG_IS_LOOPBACK = 0x00000001

// Слои (fwpmu.h): только исходящие соединения — входящие запрет не трогает.
var (
	// FWPM_LAYER_ALE_AUTH_CONNECT_V4, c38d57d1-05a7-4c33-904f-7fbceee60e82
	cFWPM_LAYER_ALE_AUTH_CONNECT_V4 = windows.GUID{Data1: 0xc38d57d1, Data2: 0x05a7, Data3: 0x4c33, Data4: [8]byte{0x90, 0x4f, 0x7f, 0xbc, 0xee, 0xe6, 0x0e, 0x82}}
	// FWPM_LAYER_ALE_AUTH_CONNECT_V6, 4a72393b-319f-44bc-84c3-ba54dcb3b6b4
	cFWPM_LAYER_ALE_AUTH_CONNECT_V6 = windows.GUID{Data1: 0x4a72393b, Data2: 0x319f, Data3: 0x44bc, Data4: [8]byte{0x84, 0xc3, 0xba, 0x54, 0xdc, 0xb3, 0xb6, 0xb4}}
)

// FWP_DATA_TYPE (fwptypes.h). Тип uint, а не uint32, — так у wireguard-windows:
// вместе с uintptr в wtFwpValue0 даёт размер и сдвиг FWP_VALUE0 на 32 и 64
// битах без поправок.
type wtFwpDataType uint

const (
	cFWP_UINT8          wtFwpDataType = 1
	cFWP_UINT16         wtFwpDataType = 2
	cFWP_UINT32         wtFwpDataType = 3
	cFWP_BYTE_BLOB_TYPE wtFwpDataType = 12
	cFWP_V4_ADDR_MASK   wtFwpDataType = 0x100
	cFWP_V6_ADDR_MASK   wtFwpDataType = 0x101
)

// FWP_V4_ADDR_AND_MASK (fwptypes.h): адрес и маска числом в порядке хоста
// (10.0.0.0 — 0x0A000000).
type wtFwpV4AddrAndMask struct {
	addr uint32
	mask uint32
}

// FWP_V6_ADDR_AND_MASK (fwptypes.h).
type wtFwpV6AddrAndMask struct {
	addr         [16]uint8
	prefixLength uint8
}

// FWP_VALUE0 (fwptypes.h).
type wtFwpValue0 struct {
	_type wtFwpDataType
	value uintptr
}

// FWP_CONDITION_VALUE0 (fwptypes.h).
type wtFwpConditionValue0 wtFwpValue0

// FWPM_DISPLAY_DATA0 (fwptypes.h).
type wtFwpmDisplayData0 struct {
	name        *uint16
	description *uint16
}

// FWPM_FILTER_CONDITION0 (fwpmtypes.h).
type wtFwpmFilterCondition0 struct {
	fieldKey       windows.GUID
	matchType      wtFwpMatchType
	conditionValue wtFwpConditionValue0
}

// FWPM_SESSION0 (fwpmtypes.h).
type wtFwpmSession0 struct {
	sessionKey           windows.GUID
	displayData          wtFwpmDisplayData0
	flags                uint32
	txnWaitTimeoutInMSec uint32
	processId            uint32
	sid                  *windows.SID
	username             *uint16
	kernelMode           uint8
}

// FWPM_SESSION_FLAG_DYNAMIC (fwpmtypes.h): объекты сессии живут, пока жив
// её дескриптор (а он — пока жив процесс).
const cFWPM_SESSION_FLAG_DYNAMIC = 0x00000001

// FWPM_SUBLAYER0 (fwpmtypes.h).
type wtFwpmSublayer0 struct {
	subLayerKey  windows.GUID
	displayData  wtFwpmDisplayData0
	flags        uint32
	providerKey  *windows.GUID
	providerData wtFwpByteBlob
	weight       uint16
}

// FWPM_PROVIDER0 (fwpmtypes.h).
type wtFwpmProvider0 struct {
	providerKey  windows.GUID
	displayData  wtFwpmDisplayData0
	flags        uint32
	providerData wtFwpByteBlob
	serviceName  *uint16
}

// RPC_C_AUTHN_WINNT (rpcdce.h).
const cRPC_C_AUTHN_WINNT = 10

const cIPPROTO_UDP = 17
