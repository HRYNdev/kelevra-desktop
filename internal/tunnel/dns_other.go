//go:build !linux

package tunnel

// NastroitDNS — на Windows DNS туннеля задаёт сам адаптер ядра.
func NastroitDNS(imya string) {}
