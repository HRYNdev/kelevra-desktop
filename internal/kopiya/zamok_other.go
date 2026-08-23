//go:build !windows

package kopiya

import (
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Vzyat вне Windows нужен затем же, зачем zapustitOtdelnuyuSluzhbu в
// zapusk_other.go: продукт тут не живёт, но пакет должен собираться и
// тестироваться на сервере без Windows. flock — тоже настоящий атомарный
// примитив ОС (не файл-метка, которую можно прочитать в промежутке между
// чужими «прочитал» и «записал»): каждый вызов LOCK_EX|LOCK_NB — один
// системный вызов, который либо забирает лок целиком, либо отказывает
// целиком, без временного окна. Опрашиваем его в цикле, потому что flock(2)
// не умеет ждать с таймаутом сам — LOCK_NB делает саму попытку н***кирующей
// и атомарной, а таймаут вокруг нужен только чтобы решить, сколько раз
// пробовать, что не возвращает гонку: каждая попытка сама по себе честна.
func Vzyat(timeout time.Duration) (*Zamok, bool) {
	put := filepath.Join(os.TempDir(), imyaZamka+".lock")
	f, err := os.OpenFile(put, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false
	}
	predel := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &Zamok{zakryt: func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}}, true
		}
		if time.Now().After(predel) {
			_ = f.Close()
			return nil, false
		}
		time.Sleep(20 * time.Millisecond)
	}
}
