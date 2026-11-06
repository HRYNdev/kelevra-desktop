package zaslon

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Передача запрета новой копии при обновлении exe службы.
//
// Динамическая сессия WFP умирает с процессом. Обновление кончается выходом
// старой службы, а новую диспетчер служб поднимает только после этого (≥5 с
// плюс старт) — без передачи все эти секунды запрета нет. Persistent-фильтров
// не ставим (решение владельца 07.11: «мягко»), поэтому так:
//
//  1. старая служба пишет peredacha (правила, которые стоят, и своё намерение)
//     и запускает держателя — новый Kelevra.exe с --derzhat-zapret;
//  2. держатель ставит те же правила в СВОЕЙ динамической сессии и пишет
//     метку готовности; старая служба уходит только после метки (или срока —
//     тогда держатель снимается, щель как раньше);
//  3. новая служба на старте читает peredacha, ставит свои правила и удаляет
//     файл — держатель видит это и выходит (его сессия умирает).
//
// Держатель не живёт дольше Do: новая служба не поднялась — запрет уходит сам.

// Имена файлов в папке данных службы.
const (
	ImyaPeredachi   = "zapret_peredacha.json"
	ImyaGotovnosti  = "zapret_peredacha.gotov"
	SrokDerzhatelya = 2 * time.Minute
)

// Peredacha — что старая служба оставляет новой.
type Peredacha struct {
	Politika  Politika  `json:"politika"`
	Namerenie bool      `json:"namerenie"`
	Doma      bool      `json:"doma"`
	Kogda     time.Time `json:"kogda"`
	Do        time.Time `json:"do"` // держатель уходит не позже; позже файл не читается
	PID       int       `json:"pid"`
}

// ZapisatPeredachu — целиком, через временный файл.
func ZapisatPeredachu(papka string, p Peredacha) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	put := filepath.Join(papka, ImyaPeredachi)
	vrem := put + ".tmp"
	if err := os.WriteFile(vrem, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(vrem, put); err != nil {
		_ = os.Remove(vrem)
		return err
	}
	return nil
}

// ProchestPeredachu — передача, если она есть и не просрочена.
func ProchestPeredachu(papka string, seychas time.Time) (Peredacha, bool) {
	var p Peredacha
	b, err := os.ReadFile(filepath.Join(papka, ImyaPeredachi))
	if err != nil {
		return p, false
	}
	if json.Unmarshal(b, &p) != nil || !seychas.Before(p.Do) {
		return Peredacha{}, false
	}
	return p, true
}

// OtpustitDerzhatelya — удалить передачу: держатель увидит и уйдёт.
func OtpustitDerzhatelya(papka string) {
	_ = os.Remove(filepath.Join(papka, ImyaPeredachi))
}

// UbratGotovnost — метка готовности прошлого держателя.
func UbratGotovnost(papka string) {
	_ = os.Remove(filepath.Join(papka, ImyaGotovnosti))
}

// ZhdatGotovnosti — старая служба ждёт метку держателя не дольше srok.
func ZhdatGotovnosti(papka string, srok, shag time.Duration) bool {
	konec := time.Now().Add(srok)
	for {
		if _, err := os.Stat(filepath.Join(papka, ImyaGotovnosti)); err == nil {
			return true
		}
		if !time.Now().Before(konec) {
			return false
		}
		time.Sleep(shag)
	}
}

// Derzhat — тело держателя (--derzhat-zapret): поставить правила передачи,
// отметить готовность, держать, пока файл передачи на месте и срок не вышел,
// потом снять. seychas — часы (проверки подставляют свои).
func Derzhat(papka string, sys Sistema, shag time.Duration, seychas func() time.Time) error {
	if sys == nil {
		return errors.New("WFP нет")
	}
	p, est := ProchestPeredachu(papka, seychas())
	if !est {
		return errors.New("передачи нет или она просрочена")
	}
	if !p.Politika.Pusto() {
		if err := sys.Primenit(SobratFiltry(p.Politika)); err != nil {
			return fmt.Errorf("правила не встали: %w", err)
		}
	}
	gotov := filepath.Join(papka, ImyaGotovnosti)
	if err := os.WriteFile(gotov, []byte(fmt.Sprint(os.Getpid())), 0o600); err != nil {
		_ = sys.Snyat()
		return fmt.Errorf("метка готовности не записана: %w", err)
	}
	defer os.Remove(gotov)
	log.Printf("держатель запрета: %s до подъёма новой копии (не дольше %s)", p.Politika.Slovami(), p.Do.Sub(seychas()).Round(time.Second))
	nachalo := seychas()
	for {
		_, err := os.Stat(filepath.Join(papka, ImyaPeredachi))
		otpushchen := os.IsNotExist(err)
		if otpushchen || !seychas().Before(p.Do) {
			if snyatErr := sys.Snyat(); snyatErr != nil {
				log.Printf("держатель запрета: снять не вышло (%v) — уйду, сессия умрёт с процессом", snyatErr)
			}
			if otpushchen {
				log.Printf("держатель запрета: новая копия поставила свой запрет через %s — ухожу", seychas().Sub(nachalo).Round(100*time.Millisecond))
			} else {
				log.Printf("держатель запрета: новая копия за %s не поднялась — снимаю и ухожу", p.Do.Sub(nachalo).Round(time.Second))
				OtpustitDerzhatelya(papka)
			}
			return nil
		}
		time.Sleep(shag)
	}
}
