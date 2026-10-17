//go:build windows

package vinsluzhba

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
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
//
// Спрашиваем с наименьшими правами, какие есть у обычного человека:
// подключиться к диспетчеру и прочитать состояние. mgr.Connect просит у
// диспетчера ВСЕ права (SC_MANAGER_ALL_ACCESS), а OpenService из mgr — все
// права на службу; у окна и значка без администратора их нет, и ответ
// «службы нет» приходил ровно тогда, когда служба стояла и работала.
// Замер с ПК 17.10: в журнале окна за всё время нет ни одной строки
// «работаю через службу Windows»; ручной запуск новой версии поднимал рядом
// со службой свой служебный процесс, тот падал на закрытой папке данных, и у
// человека на экране было окно ошибки.
//
// Теперь «службы нет» — только по коду системы ERROR_SERVICE_DOES_NOT_EXIST;
// любой другой отказ возвращается ошибкой, а не притворяется ответом.
func Ustanovlena() (bool, error) {
	_, zakryt, err := otkrytDlya(windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return false, nil
		}
		return false, err
	}
	zakryt()
	return true, nil
}

// Rabotaet — поднята ли служба прямо сейчас. Те же наименьшие права, что у
// Ustanovlena.
func Rabotaet() (bool, error) {
	s, zakryt, err := otkrytDlya(windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false, err
	}
	defer zakryt()
	var sost windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(s, &sost); err != nil {
		return false, fmt.Errorf("не спросить состояние службы: %w", err)
	}
	return sost.CurrentState == windows.SERVICE_RUNNING, nil
}

// otkrytDlya открывает службу ровно с нужными правами (dostup), не больше.
// zakryt закрывает оба описателя: службы и диспетчера.
func otkrytDlya(dostup uint32) (windows.Handle, func(), error) {
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, nil, fmt.Errorf("не подключиться к диспетчеру служб: %w", err)
	}
	imya, err := windows.UTF16PtrFromString(Imya)
	if err != nil {
		windows.CloseServiceHandle(m)
		return 0, nil, err
	}
	s, err := windows.OpenService(m, imya, dostup)
	if err != nil {
		windows.CloseServiceHandle(m)
		return 0, nil, fmt.Errorf("службу %s не открыть: %w", Imya, err)
	}
	return s, func() {
		windows.CloseServiceHandle(s)
		windows.CloseServiceHandle(m)
	}, nil
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
	//
	// Срок сброса счётчика — МИНУТА, а не сутки, и это принципиально.
	//
	// Обновление службы устроено так: поставили новую версию и ушли с
	// отказом, чтобы диспетчер поднял свежую копию (см. sluzhba.go,
	// perezapustitSluzhbuPosleObnovleniya). Для Windows каждый такой уход —
	// АВАРИЯ, и он их считает. С суточным сроком счётчик копится весь день:
	// после третьего обновления список действий кончается, и служба остаётся
	// лежать совсем.
	//
	// Замер 08.09 с живой машины, дословно из системного журнала: «Служба
	// Kelevra была неожиданно завершена. Это произошло 3 раз(а). Следующее
	// корректирующее действие будет предпринято через 60000 мсек». Человек
	// нажал «Обновить», увидел «Kelevra перезапускается…» и остался с этим
	// экраном навсегда: служба не поднялась, окно подождало три проверки и
	// закрылось.
	//
	// Минута решает это без потери смысла: настоящее падение ядра всё так же
	// поднимет службу трижды подряд, а спокойные обновления, разнесённые во
	// времени, перестают копить чужой счёт.
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, uint32(time.Minute.Seconds())); err != nil {
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

// Zapustit поднимает уже установленную службу. Просит только право запуска
// (не все права, как mgr): у кого его нет, получит отказ на самом запуске,
// а не на подключении к диспетчеру.
func Zapustit() error {
	s, zakryt, err := otkrytDlya(windows.SERVICE_START)
	if err != nil {
		return err
	}
	defer zakryt()
	return windows.StartService(s, 0, nil)
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

// PopravitSamopodyom приводит настройки восстановления УЖЕ УСТАНОВЛЕННОЙ
// службы к нынешним: короткий срок сброса счётчика отказов.
//
// Зачем отдельно от установки. Правка срока (сутки → минута) сама по себе
// чинит только НОВЫЕ установки, а у людей служба уже стоит — со старым
// суточным сроком, из-за которого три обновления подряд оставляют её лежать
// (замер 08.09, разбор в Ustanovit). Зовём при каждом старте службы: дёшево,
// идемпотентно и доезжает до всех с ближайшим обновлением.
//
// Молчаливый отказ намеренный: не смогли поправить — служба всё равно
// работает, а рушить её старт из-за настройки восстановления незачем.
func PopravitSamopodyom() {
	m, err := mgr.Connect()
	if err != nil {
		return
	}
	defer m.Disconnect()
	s, err := m.OpenService(Imya)
	if err != nil {
		return
	}
	defer s.Close()
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, uint32(time.Minute.Seconds()))
}
