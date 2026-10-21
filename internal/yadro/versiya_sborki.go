package yadro

import (
	"fmt"
	"strconv"
	"strings"
)

// versiyaSborki — версия сборки ядра из тега core-*.
//
// Тег собирает core.yml: core-<тег sing-box>-<номер сборки>, например
// core-v1.14.0-beta.4-1. Сравнивается сперва версия sing-box по правилам
// semver (пробные beta/alpha/rc ниже релиза), затем номер сборки:
// core-v1.14.0-beta.4-1 < core-v1.14.1-1 < core-v1.14.1-2.
//
// Порядок релизов в списке GitHub для этого не годится: он идёт по дате
// коммита тега, и сборка со старым ядром, выложенная позже, оказалась бы
// «свежей».
type versiyaSborki struct {
	chisla [3]int
	proba  []string // части после «-» в версии sing-box (beta.4 → beta, 4)
	nomer  int      // номер сборки
}

// razobratTeg разбирает тег core-*. Ошибка — тег не по образцу core.yml.
func razobratTeg(teg string) (versiyaSborki, error) {
	var v versiyaSborki
	ostatok, ok := strings.CutPrefix(teg, "core-v")
	if !ok {
		return v, fmt.Errorf("тег %q не начинается с core-v", teg)
	}
	i := strings.LastIndex(ostatok, "-")
	if i <= 0 {
		return v, fmt.Errorf("в теге %q нет номера сборки", teg)
	}
	nomer, err := strconv.Atoi(ostatok[i+1:])
	if err != nil || nomer < 0 {
		return v, fmt.Errorf("номер сборки в теге %q не число", teg)
	}
	v.nomer = nomer
	semver := ostatok[:i]
	osnova, proba, estProba := strings.Cut(semver, "-")
	chasti := strings.Split(osnova, ".")
	if len(chasti) == 0 || len(chasti) > 3 {
		return v, fmt.Errorf("версия в теге %q не разобрана", teg)
	}
	for j, c := range chasti {
		n, err := strconv.Atoi(c)
		if err != nil || n < 0 {
			return v, fmt.Errorf("версия в теге %q не разобрана", teg)
		}
		v.chisla[j] = n
	}
	if estProba {
		if proba == "" {
			return v, fmt.Errorf("версия в теге %q не разобрана", teg)
		}
		v.proba = strings.Split(proba, ".")
		for _, p := range v.proba {
			if p == "" {
				return v, fmt.Errorf("версия в теге %q не разобрана", teg)
			}
		}
	}
	return v, nil
}

// sravnitSborki: <0 — a старше b, 0 — равны, >0 — a новее b.
func sravnitSborki(a, b versiyaSborki) int {
	for i := range a.chisla {
		if a.chisla[i] != b.chisla[i] {
			return sravnitChisla(a.chisla[i], b.chisla[i])
		}
	}
	if r := sravnitProby(a.proba, b.proba); r != 0 {
		return r
	}
	return sravnitChisla(a.nomer, b.nomer)
}

func sravnitChisla(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// sravnitProby — правила semver для пробной части: без неё выше, чем с ней;
// числовые части сравниваются числами и ниже буквенных; короче — ниже.
func sravnitProby(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		na, ea := strconv.Atoi(a[i])
		nb, eb := strconv.Atoi(b[i])
		switch {
		case ea == nil && eb == nil:
			if r := sravnitChisla(na, nb); r != 0 {
				return r
			}
		case ea == nil:
			return -1
		case eb == nil:
			return 1
		default:
			if r := strings.Compare(a[i], b[i]); r != 0 {
				return r
			}
		}
	}
	return sravnitChisla(len(a), len(b))
}

// NoveeTega — сборка a новее сборки b. Неразбираемый тег новее не бывает.
func NoveeTega(a, b string) bool {
	va, err := razobratTeg(a)
	if err != nil {
		return false
	}
	vb, err := razobratTeg(b)
	if err != nil {
		return false
	}
	return sravnitSborki(va, vb) > 0
}
