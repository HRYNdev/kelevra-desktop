//go:build !windows

package yadro

// sbrositKeshDns — вне Windows общего кеша DNS, который надо было бы сбросить
// (см. dns_kesh_windows.go), у нас нет: ничего не делаем.
func sbrositKeshDns(pochemu string) {}
