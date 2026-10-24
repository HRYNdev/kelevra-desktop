//go:build !windows

package main

import "time"

// Вне Windows значка нет (trey_other.go): замок всегда «взят», значок
// всегда «жив» — окно ничего не поднимает.
func zanyatMestoZnachka(time.Duration) bool { return true }
func oknoTreyaEst() bool                    { return false }
func znachokZhivOS() bool                   { return true }
