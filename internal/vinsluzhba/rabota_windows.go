//go:build windows

package vinsluzhba

import (
	"context"
	"log"

	"golang.org/x/sys/windows/svc"
)

// Krutit отдаёт процесс диспетчеру служб и держит его живым, пока система не
// попросит остановиться. rabota получает контекст, который отменяется по
// команде «стоп» или при выключении компьютера; вернуться она обязана сама,
// после отмены — иначе Windows убьёт процесс жёстко и туннель останется
// висеть без хозяина.
//
// Ошибка возвращается только когда процесс вообще не смог стать службой.
func Krutit(rabota func(ctx context.Context)) error {
	return svc.Run(Imya, &obrabotchik{rabota: rabota})
}

type obrabotchik struct {
	rabota func(ctx context.Context)
}

// Execute — то, что диспетчер служб вызывает в своём потоке.
//
// Порядок здесь не украшательство: пока не отправлен StartPending, система
// считает службу зависшей, а пока не отправлен Running — не считает
// запущенной и может её прибить. Поэтому сначала докладываем, потом работаем.
func (o *obrabotchik) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	const prinimaem = svc.AcceptStop | svc.AcceptShutdown

	s <- svc.Status{State: svc.StartPending}

	ctx, otmena := context.WithCancel(context.Background())
	gotovo := make(chan struct{})
	go func() {
		defer close(gotovo)
		o.rabota(ctx)
	}()

	s <- svc.Status{State: svc.Running, Accepts: prinimaem}

	for {
		select {
		case zapros := <-r:
			switch zapros.Cmd {
			case svc.Interrogate:
				// Система спрашивает «ты жива?» — отвечаем тем же состоянием,
				// которое она нам показала, иначе служба считается зависшей.
				s <- zapros.CurrentStatus
			case svc.Stop, svc.Shutdown:
				log.Printf("служба: получена команда остановки")
				s <- svc.Status{State: svc.StopPending}
				otmena()
				<-gotovo
				s <- svc.Status{State: svc.Stopped}
				return false, 0
			default:
				log.Printf("служба: неизвестная команда диспетчера: %v", zapros.Cmd)
			}
		case <-gotovo:
			// Работа кончилась сама (например, ядро не поднимается вовсе).
			// Молча висеть живой службой нельзя: диспетчер должен узнать,
			// чтобы сработал самоподъём, прописанный при установке.
			log.Printf("служба: работа завершилась сама, останавливаюсь")
			otmena()
			s <- svc.Status{State: svc.Stopped}
			return false, 1
		}
	}
}
