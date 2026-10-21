//go:build !windows && !unix

package sluzhba

import "os"

// nomerFayla — номера файла здесь не знаем: решают отпечатки.
func nomerFayla(_ *os.File, _ os.FileInfo) string { return "" }
