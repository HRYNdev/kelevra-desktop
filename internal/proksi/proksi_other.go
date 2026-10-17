//go:build !windows

package proksi

// UbratSled на не-Windows реестр не трогает — трогать там нечего. Метку всё же
// убираем: она платформо-независимая, и UbratSled — единственное общее место,
// откуда её положено снимать.
func UbratSled() bool {
	UbratMetku()
	return false
}
