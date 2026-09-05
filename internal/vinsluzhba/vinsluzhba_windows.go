//go:build windows

package vinsluzhba

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// srokOtveta — сколько ждём, пока диспетчер служб доложит о смене состояния.
// Больше секунды тут не нужно: команды простые, а долгое ожидание в окне
// выглядит как зависание.
const srokOtveta = 15 * time.Second

// PodSluzhboy отвечает на вопрос «нас запустил диспетчер служб или человек».
// Нужен на самом старте: в службе нельзя ни открывать окно, ни ставить значок
// в трее — интерактивного сеанса у неё нет.
func PodSluzhboy() bool {
	pod, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return pod
}

// Ustanovlena — зарегистрирована ли служба в системе.
func Ustanovlena() (bool, error) {
	m, err := mgr.Connect()
	if err != nil {
		return false, fmt.Errorf("не подключиться к диспетчеру служб: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(Imya)
	if err != nil {
		// Отличить «нет такой службы» от «нет прав спросить» нечем: сообщение
		// системы одинаково недружелюбно. Для вызывающего разница есть только
		// при установке, и там ошибка придёт своя.
		return false, nil
	}
	s.Close()
	return true, nil
}

// Rabotaet — поднята ли служба прямо сейчас.
func Rabotaet() (bool, error) {
	m, err := mgr.Connect()
	if err != nil {
		return false, fmt.Errorf("не подключиться к диспетчеру служб: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(Imya)
	if err != nil {
		return false, fmt.Errorf("службы %s нет: %w", Imya, err)
	}
	defer s.Close()
	sost, err := s.Query()
	if err != nil {
		return false, fmt.Errorf("не спросить состояние службы: %w", err)
	}
	return sost.State == svc.Running, nil
}

// Ustanovit регистрирует службу и запускает её. Требует прав администратора —
// это единственное место, ради которого человека спрашивают, и спрашивают один
// раз за установку приложения.
//
// putExe — полный путь к Kelevra.exe. Он попадёт в реестр служб и будет
// запускаться системой при каждой загрузке, поэтому файл обязан лежать там,
// куда обычный пользователь писать не может: иначе подмена этого файла даёт
// кому угодно права системы.
func Ustanovit(putExe string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("не подключиться к диспетчеру служб: %w", err)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(Imya); err == nil {
		s.Close()
		return fmt.Errorf("служба %s уже установлена", Imya)
	}

	s, err := m.CreateService(Imya, putExe, mgr.Config{
		DisplayName: "Kelevra",
		Description: Opisanie,
		StartType:   mgr.StartAutomatic,
	}, Argument)
	if err != nil {
		return fmt.Errorf("не создать службу: %w", err)
	}
	defer s.Close()

	// Ядро может упасть, и вместе с ним уйдёт служба. Без этого человек
	// остался бы без защиты до следующей перезагрузки и не узнал бы почему.
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, uint32((24 * time.Hour).Seconds())); err != nil {
		// Не приговор: служба уже создана и работать будет, просто сама себя
		// не поднимет после падения. Говорим вслух, но установку не рушим.
		return fmt.Errorf("служба создана, но без самоподъёма после падения: %w", err)
	}
	return s.Start()
}

// Udalit снимает службу с регистрации, предварительно остановив.
func Udalit() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("не подключиться к диспетчеру служб: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(Imya)
	if err != nil {
		return fmt.Errorf("службы %s нет: %w", Imya, err)
	}
	defer s.Close()
	_ = ostanovitSluzhbu(s)
	return s.Delete()
}

// Zapustit поднимает уже установленную службу.
func Zapustit() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("не подключиться к диспетчеру служб: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(Imya)
	if err != nil {
		return fmt.Errorf("службы %s нет: %w", Imya, err)
	}
	defer s.Close()
	return s.Start()
}

// Ostanovit гасит службу и ждёт, пока она действительно встанет.
func Ostanovit() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("не подключиться к диспетчеру служб: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(Imya)
	if err != nil {
		return fmt.Errorf("службы %s нет: %w", Imya, err)
	}
	defer s.Close()
	return ostanovitSluzhbu(s)
}

func ostanovitSluzhbu(s *mgr.Service) error {
	sost, err := s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("не остановить службу: %w", err)
	}
	konec := time.Now().Add(srokOtveta)
	for sost.State != svc.Stopped {
		if time.Now().After(konec) {
			return fmt.Errorf("служба не остановилась за %s", srokOtveta)
		}
		time.Sleep(300 * time.Millisecond)
		sost, err = s.Query()
		if err != nil {
			return fmt.Errorf("не спросить состояние службы: %w", err)
		}
	}
	return nil
}
