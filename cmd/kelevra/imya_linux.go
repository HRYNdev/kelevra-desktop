//go:build linux

package main

import "github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"

// На Linux обновление берёт из релиза свою сборку, а не Kelevra.exe.
func init() { obnovlenie.ImyaFayla = obnovlenie.ImyaFaylaLinux }
