//go:build !windows

package avtozapusk

import "errors"

// Ne — на не-Windows автозагрузки нет вовсе. Продукт живёт только на Windows,
// а линуксовая сборка нужна мне для сборки и части проверок, поэтому здесь
// честная заглушка, а не молчаливое «выключено»: молчаливое «выключено»
// однажды показало бы человеку тумблер, который ничего не делает.
var Ne = errors.New("автозапуск есть только на Windows")

func Vklyuchen() (bool, error) { return false, Ne }
func Ustarela() (bool, error)  { return false, Ne }
func Vklyuchit() error         { return Ne }
func Vyklyuchit() error        { return Ne }
