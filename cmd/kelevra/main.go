// Kelevra для компьютера: код доступа, кнопка «Подключить», ядро sing-box под капотом.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/kopiya"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/sluzhba"
)

func main() {
	papka := hranenie.Papka()
	putZhurnala, zakryt := otkrytZhurnal(papka)
	defer zakryt()
	defer lovitPaniku(putZhurnala)
	log.Printf("--- запуск Kelevra %s (%s/%s), данные: %s", podpiska.Versiya, runtime.GOOS, runtime.GOARCH, papka)

	// Приложение уже работает: открываем его окно, а не поднимаем второе ядро
	// на те же порты. Для человека двойной запуск выглядит как «показать окно».
	if adres, est := kopiya.Nayti(papka); est {
		log.Printf("копия уже запущена, открываю её окно: %s", adres)
		pokazatOkno(adres)
		return
	}

	s, err := sluzhba.Novaya()
	if err != nil {
		umeret(putZhurnala, "Kelevra не смогла подготовить свои файлы", err)
	}
	slushatel, url, err := s.Slushat()
	if err != nil {
		umeret(putZhurnala, "Kelevra не смогла занять локальный порт (обычно это фаервол или антивирус)", err)
	}
	server := &http.Server{Handler: s.Obsluzhit()}
	go func() {
		if err := server.Serve(slushatel); err != nil && err != http.ErrServerClosed {
			log.Printf("служба остановилась: %v", err)
		}
	}()
	if err := kopiya.Zanyat(papka, url, time.Now()); err != nil {
		log.Printf("не смог отметить запуск (второй запуск не будет пойман): %v", err)
	}
	defer kopiya.Osvobodit(papka)

	ctx, otmena := context.WithCancel(context.Background())
	defer otmena()
	go s.ObnovlyatProfil(ctx)

	// Окно живёт ровно столько, сколько приложение; после закрытия гасим ядро,
	// чтобы не оставлять за собой работающий процесс.
	pokazatOkno(url)
	_ = s.Yadro.Ostanovit()
}

// umeret — единственный выход из строя, который видит пользователь.
// Просто упасть приложение не имеет права: у оконной сборки нет консоли,
// и «ничего не произошло» — это всё, что человек увидел бы вместо причины.
func umeret(putZhurnala, chto string, err error) {
	log.Printf("ОТКАЗ: %s: %v", chto, err)
	tekst := chto + ".\n\n" + err.Error()
	if putZhurnala != "" {
		tekst += "\n\nПодробности записаны в файл:\n" + putZhurnala
	}
	skazat("Kelevra не запустилась", tekst)
	os.Exit(1)
}

// lovitPaniku превращает аварию в текст на экране и строку в журнале.
// Ловится только авария главной горутины — этого хватает для старта,
// где и случается почти всё, что может пойти не так у пользователя.
func lovitPaniku(putZhurnala string) {
	r := recover()
	if r == nil {
		return
	}
	log.Printf("АВАРИЯ: %v\n%s", r, debug.Stack())
	tekst := fmt.Sprintf("Kelevra аварийно остановилась.\n\n%v", r)
	if putZhurnala != "" {
		tekst += "\n\nПодробности записаны в файл:\n" + putZhurnala
	}
	skazat("Kelevra остановилась", tekst)
	os.Exit(2)
}
