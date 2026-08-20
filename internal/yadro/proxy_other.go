//go:build !windows

package yadro

// sbrositSistemnyProksi — реестр Windows тут ни при чём, и на этих
// платформах ядро гасится сигналом (см. process_other.go), у которого есть
// шанс на свою очистку. Снимать нечего.
func sbrositSistemnyProksi() {}
