//go:build unix

package sluzhba

import (
	"fmt"
	"os"
	"syscall"
)

// nomerFayla — устройство и inode. "" — система не ответила.
func nomerFayla(_ *os.File, st os.FileInfo) string {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%v-%v", s.Dev, s.Ino)
	}
	return ""
}
