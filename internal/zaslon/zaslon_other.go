//go:build !windows

package zaslon

// NovayaSistema — WFP есть только у Windows: ничего не делать.
func NovayaSistema() Sistema { return nil }
