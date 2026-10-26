//go:build !windows

package nrpt

// NovayaSistema — NRPT есть только у Windows: ничего не делать.
func NovayaSistema() Sistema { return nil }
