package main

// Два служебных режима запрета мимо туннеля (internal/zaslon):
//
//   - --derzhat-zapret: держатель при обновлении службы. Его запускает уходящая
//     служба (sluzhba.peredatZapretNovoyKopii); он ставит те же правила в своей
//     динамической сессии WFP и держит их, пока новая служба не поставит свои
//     (удалит файл передачи), но не дольше zaslon.SrokDerzhatelya;
//   - --snyat-zapret: аварийный выход руками (от администратора). Все наши
//     объекты WFP живут только в динамических сессиях процессов Kelevra, так
//     что снять всё — значит отпустить держателя и попросить службу
//     «Отключить»; служба не отвечает — остановить её (её сессия умрёт с ней).

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/kopiya"
	"github.com/HRYNdev/kelevra-desktop/internal/sluzhba"
	"github.com/HRYNdev/kelevra-desktop/internal/vinsluzhba"
	"github.com/HRYNdev/kelevra-desktop/internal/zaslon"
)

const (
	argDerzhatZapret = sluzhba.ArgDerzhatZapret
	argSnyatZapret   = "--snyat-zapret"
)

// derzhatZapret — тело держателя.
func derzhatZapret(papka string) {
	if err := zaslon.Derzhat(papka, zaslon.NovayaSistema(), 200*time.Millisecond, time.Now); err != nil {
		log.Printf("держатель запрета: %v", err)
	}
}

// snyatZapretAvariyno — тело --snyat-zapret. Итог — окном (консоли у
// приложения нет) и в журнал.
func snyatZapretAvariyno(papka string) {
	itog := snyatZapret(papka, otklyuchitSluzhbu, vinsluzhba.Rabotaet, vinsluzhba.Ostanovit)
	log.Printf("снятие запрета руками: %s", itog)
	skazat("Kelevra", itog)
}

// snyatZapret — порядок аварийного выхода; зависимости — параметрами, чтобы
// проверка шла без службы и без WFP.
func snyatZapret(papka string, otklyuchit func(adres string) error, rabotaet func() (bool, error), ostanovit func() error) string {
	zaslon.OtpustitDerzhatelya(papka)
	if adres, est := kopiya.Nayti(papka); est {
		err := otklyuchit(adres)
		if err == nil {
			return "Запрет снят: Отключено"
		}
		log.Printf("снятие запрета руками: служба по %s не ответила (%v) — останавливаю её", adres, err)
	}
	idet, err := rabotaet()
	if err != nil {
		return fmt.Sprintf("Запрет не снят: %v", err)
	}
	if !idet {
		return "Запрет снят: служба остановлена"
	}
	if err := ostanovit(); err != nil {
		return fmt.Sprintf("Запрет не снят: %v", err)
	}
	return "Запрет снят: служба остановлена"
}

// otklyuchitSluzhbu — то же, что кнопка «Отключить» в окне.
func otklyuchitSluzhbu(adres string) error {
	ctx, otmena := context.WithTimeout(context.Background(), 10*time.Second)
	defer otmena()
	zapros, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(adres, "/")+"/api/otklyuchit", nil)
	if err != nil {
		return err
	}
	otvet, err := http.DefaultClient.Do(zapros)
	if err != nil {
		return err
	}
	defer otvet.Body.Close()
	if otvet.StatusCode != http.StatusOK {
		return fmt.Errorf("ответ %d", otvet.StatusCode)
	}
	return nil
}
