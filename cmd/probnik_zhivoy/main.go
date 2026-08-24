// Боевой прогон обновлятора против ЖИВОГО списка релизов GitHub.
// Стендовые тесты ходили только в заглушку KELEVRA_RELIZY; эта проба
// проверяет ту самую цепочку, которой обновляется человек.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
)

func main() {
	tek := os.Args[1]
	ctx, c := context.WithTimeout(context.Background(), 20*time.Second)
	defer c()
	n, err := obnovlenie.Proverit(ctx, &http.Client{Timeout: 20 * time.Second}, obnovlenie.SpisokReliza, tek)
	if err != nil {
		fmt.Println("ПРОВЕРКА ОТКАЗ:", err)
		os.Exit(2)
	}
	if n == nil {
		fmt.Printf("ПРОВЕРКА: клиент %s слышит «обновлений нет»\n", tek)
		os.Exit(3)
	}
	fmt.Printf("ПРОВЕРКА ok: %s -> %s, %d байт\n%s\n", tek, n.Versiya, n.Razmer, n.Ssylka)
	if len(os.Args) < 3 || os.Args[2] != "--stavit" {
		return
	}
	put := "/tmp/x/Kelevra.exe"
	os.WriteFile(put, []byte("старая сборка"), 0o755)
	ctx2, c2 := context.WithTimeout(context.Background(), 3*time.Minute)
	defer c2()
	if err := obnovlenie.Postavit(ctx2, &http.Client{Timeout: 3 * time.Minute}, *n, put); err != nil {
		fmt.Println("УСТАНОВКА ОТКАЗ:", err)
		os.Exit(4)
	}
	st, _ := os.Stat(put)
	old, _ := os.Stat(put + ".old")
	fmt.Printf("УСТАНОВКА ok: на месте %d байт, хвост .old %v\n", st.Size(), old != nil)
}
