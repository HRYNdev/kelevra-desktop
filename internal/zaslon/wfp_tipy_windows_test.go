//go:build windows

package zaslon

import (
	"testing"
	"unsafe"
)

// Раскладка структур WFP сверяется с fwpmtypes.h числами из wireguard-windows
// (types_windows_test.go там же): разъедься сдвиг — WFP прочтёт мусор.
// Только размеры, ни одного вызова WFP: сеть машины не трогается.
func TestRaskladkaStrukturWfp(t *testing.T) {
	var f wtFwpmFilter0
	proverki := []struct {
		chto        string
		est, nuzhno uintptr
	}{
		{"FWPM_FILTER0", unsafe.Sizeof(f), wtFwpmFilter0_Size},
		{"FWPM_FILTER0.layerKey", unsafe.Offsetof(f.layerKey), wtFwpmFilter0_layerKey_Offset},
		{"FWPM_FILTER0.weight", unsafe.Offsetof(f.weight), wtFwpmFilter0_weight_Offset},
		{"FWPM_FILTER0.numFilterConditions", unsafe.Offsetof(f.numFilterConditions), wtFwpmFilter0_numFilterConditions_Offset},
		{"FWPM_FILTER0.filterCondition", unsafe.Offsetof(f.filterCondition), wtFwpmFilter0_filterCondition_Offset},
		{"FWPM_FILTER0.action", unsafe.Offsetof(f.action), wtFwpmFilter0_action_Offset},
		{"FWPM_FILTER0.providerContextKey", unsafe.Offsetof(f.providerContextKey), wtFwpmFilter0_providerContextKey_Offset},
		{"FWPM_FILTER0.filterID", unsafe.Offsetof(f.filterID), wtFwpmFilter0_filterID_Offset},
		{"FWPM_FILTER_CONDITION0", unsafe.Sizeof(wtFwpmFilterCondition0{}), wtFwpmFilterCondition0_Size},
		{"FWPM_SESSION0", unsafe.Sizeof(wtFwpmSession0{}), wtFwpmSession0_Size},
		{"FWPM_SUBLAYER0", unsafe.Sizeof(wtFwpmSublayer0{}), wtFwpmSublayer0_Size},
		{"FWPM_SUBLAYER0.weight", unsafe.Offsetof(wtFwpmSublayer0{}.weight), wtFwpmSublayer0_weight_Offset},
		{"FWPM_PROVIDER0", unsafe.Sizeof(wtFwpmProvider0{}), wtFwpmProvider0_Size},
		{"FWP_VALUE0", unsafe.Sizeof(wtFwpValue0{}), wtFwpValue0_Size},
		{"FWP_V4_ADDR_AND_MASK", unsafe.Sizeof(wtFwpV4AddrAndMask{}), 8},
		{"FWP_V6_ADDR_AND_MASK", unsafe.Sizeof(wtFwpV6AddrAndMask{}), 17},
		{"FWPM_ACTION0", unsafe.Sizeof(wtFwpmAction0{}), 20},
	}
	for _, p := range proverki {
		if p.est != p.nuzhno {
			t.Errorf("%s: %d, по заголовку Windows %d", p.chto, p.est, p.nuzhno)
		}
	}
}
