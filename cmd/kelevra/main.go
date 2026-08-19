// Kelevra для компьютера: код доступа, кнопка «Подключить», ядро sing-box под капотом.
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/HRYNdev/kelevra-desktop/internal/sluzhba"
)

func main() {
	s, err := sluzhba.Novaya()
	if err != nil {
		log.Fatalf("не смог подготовить приложение: %v", err)
	}
	slushatel, url, err := s.Slushat()
	if err != nil {
		log.Fatalf("не смог занять локальный порт: %v", err)
	}
	server := &http.Server{Handler: s.Obsluzhit()}
	go func() {
		if err := server.Serve(slushatel); err != nil && err != http.ErrServerClosed {
			log.Printf("служба остановилась: %v", err)
		}
	}()

	ctx, otmena := context.WithCancel(context.Background())
	defer otmena()
	go s.ObnovlyatProfil(ctx)

	// Окно живёт ровно столько, сколько приложение; после закрытия гасим ядро,
	// чтобы не оставлять за собой работающий процесс.
	pokazatOkno(url)
	_ = s.Yadro.Ostanovit()
}
