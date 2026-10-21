package sluzhba

import (
	"fmt"
	"os"
	"syscall"
)

// nomerFayla — том и индекс файла NTFS: у каждого существующего файла свой,
// переживает перезапуски и переименование. "" — система не ответила.
func nomerFayla(f *os.File, _ os.FileInfo) string {
	var d syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &d); err != nil {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x", d.VolumeSerialNumber, d.FileIndexHigh, d.FileIndexLow)
}
