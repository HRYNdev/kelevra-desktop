//go:build !windows

package vinsluzhba

import (
	"context"
	"errors"
)

// Служб Windows на других системах нет. Заглушки нужны, чтобы приложение
// собиралось и проверялось на Linux — там же, где идут тесты и сборка.
var neZdes = errors.New("служба Windows доступна только в Windows")

func PodSluzhboy() bool { return false }

func Ustanovlena() (bool, error) { return false, nil }

func Rabotaet() (bool, error) { return false, neZdes }

func Ustanovit(putExe string) error { return neZdes }

func Udalit() error { return neZdes }

func Zapustit() error { return neZdes }

func Ostanovit() error { return neZdes }

func Krutit(rabota func(ctx context.Context)) error { return neZdes }
