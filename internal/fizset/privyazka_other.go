//go:build !windows

package fizset

import "syscall"

// privyazat — на стенде (линукс) сокет к интерфейсу не привязывается:
// продукт живёт на Windows, где привязка — IP_UNICAST_IF.
func privyazat(uint32) func(network, address string, c syscall.RawConn) error {
	return nil
}
