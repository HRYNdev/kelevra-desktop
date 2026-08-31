//go:build !windows

package ustroystvo

import "runtime"

// Не-Windows сборка существует только ради стенда (проверки гоняются в
// контейнере, где Windows нет). Модель там спрашивать негде и не у кого —
// пусть честно сработает ZapasnayaModel, а платформа скажет правду про
// систему, на которой это крутится, а не выдуманную «Windows».
func modelSistemy() string { return "" }

func platformaSistemy() string { return runtime.GOOS }
