//go:build !windows

package main

import (
	"fmt"
	"os"
)

// skazat вне Windows печатает то же самое в терминал: на стенде консоль есть,
// и отдельное окно там ни к чему.
func skazat(zagolovok, tekst string) {
	fmt.Fprintf(os.Stderr, "\n=== %s ===\n%s\n", zagolovok, tekst)
}
