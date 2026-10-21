package sluzhba

// Замена ядра у стоящих установок (устройство — internal/yadro/smena.go).
//
// Фоном служба сверяет стоящее ядро с самой свежей подписанной сборкой core-*
// и откладывает новую (SveryatYadro). Ставится отложенная сборка при
// следующем подъёме связи, когда ядро не запущено (PodnyatZashchitu): связь
// ради замены ядра не рвётся. Не поднялось новое ядро — служба возвращает
// прежнее, поднимает связь на нём и больше эту сборку не берёт.

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// srokPodgotovkiYadra — сколько даём на скачивание сборки ядра фоном.
const srokPodgotovkiYadra = 15 * time.Minute

// novoeYadro — ядро на настоящих путях приложения.
//
// KELEVRA_RELIZY_YADRA подменяет список сборок ядра, KELEVRA_YADRO_PROBNYE=1
// разрешает пробные (prerelease) сборки — только для стенда, у человека они
// не заданы. Подпись ed25519 требуется в любом случае.
func novoeYadro() *yadro.Yadro {
	return &yadro.Yadro{
		Bin:         hranenie.PutYadra(),
		Papka:       hranenie.PapkaYadra(),
		Spisok:      os.Getenv("KELEVRA_RELIZY_YADRA"),
		BratProbnye: os.Getenv("KELEVRA_YADRO_PROBNYE") == "1",
	}
}

// SveryatYadro — фоновая сверка стоящего ядра со свежей сборкой. Первая
// попытка сразу при запуске (кроме стендов с KELEVRA_BEZ_OBNOVLENIYA=1),
// дальше раз в period; сетевой отказ повторяется сам (fonovoeDelo).
func (s *Sluzhba) SveryatYadro(ctx context.Context, period time.Duration) {
	d := s.delo("сверка ядра", period, s.prigotovitYadroRaz)
	d.srazu = os.Getenv("KELEVRA_BEZ_OBNOVLENIYA") != "1"
	d.krutit(ctx)
}

// prigotovitYadroRaz — одна сверка. Без прав администратора не делается:
// папку ядра пишет только служба.
func (s *Sluzhba) prigotovitYadroRaz(ctx context.Context) error {
	if !s.estPrava() || !s.Yadro.EstBinar() {
		return nil
	}
	ctx, otmena := context.WithTimeout(ctx, srokPodgotovkiYadra)
	defer otmena()
	itog, err := s.Yadro.Prigotovit(ctx, s.klientSeti(srokPodgotovkiYadra))
	if err != nil {
		if errors.Is(err, yadro.ErrNetYadra) {
			return nil
		}
		if !setevoyOtkaz(err) {
			log.Printf("сверка ядра: не вышло (%v), ядро прежнее", err)
		}
		return err
	}
	switch {
	case itog.Otvergnuto:
		log.Printf("сверка ядра: свежая сборка %s на этой машине не поднималась — остаётся прежнее (%s)", itog.Svezhaya, slovoVersii(itog.Stoit))
	case itog.Priznano:
		log.Printf("сверка ядра: стоящее ядро — сборка %s (версия записана)", itog.Stoit)
	case itog.Otlozheno:
		log.Printf("сверка ядра: стоит %s, свежая %s — подпись верна (ed25519), заменю при следующем подключении", slovoVersii(itog.Stoit), itog.Svezhaya)
	case itog.Stoit == itog.Svezhaya:
		log.Printf("сверка ядра: стоит свежая сборка %s", itog.Stoit)
	default:
		log.Printf("сверка ядра: стоит %s, в релизах не новее (%s) — замена не нужна", itog.Stoit, itog.Svezhaya)
	}
	return nil
}

func slovoVersii(teg string) string {
	if teg == "" {
		return "ядро неизвестной сборки"
	}
	return teg
}

// smenitYadroPeredPodyomom ставит отложенную сборку, если ядро этой копии не
// запущено. nil — замены нет (нечего менять, нет прав, ядро работает).
func (s *Sluzhba) smenitYadroPeredPodyomom() *yadro.Smena {
	if !s.estPrava() || s.Yadro.PID() != "" {
		return nil
	}
	s.zamokSmenyYadra.Lock()
	defer s.zamokSmenyYadra.Unlock()
	smena, err := s.Yadro.Smenit()
	if err != nil {
		log.Printf("замена ядра: %v — остаюсь на прежнем", err)
		return nil
	}
	if smena == nil {
		return nil
	}
	s.zamok.Lock()
	s.summaYadra = "" // отпечаток для паспорта считается заново
	s.zamok.Unlock()
	log.Printf("замена ядра: %s → %s, подпись верна (ed25519), поднимаю на новом", slovoVersii(smena.Prezhniy), smena.Teg)
	return smena
}

// oshibkaZapuskaYadra — отказ самого запуска ядра (процесс упал на старте
// или не ответил). Текст и причина — как у исходной ошибки.
type oshibkaZapuskaYadra struct{ err error }

func (o oshibkaZapuskaYadra) Error() string { return o.err.Error() }
func (o oshibkaZapuskaYadra) Unwrap() error { return o.err }

// novoeYadroVinovato — мог ли отказ подъёма быть из-за нового ядра. Винится
// только отказ самого запуска ядра; нет прав, нет конфига, подбор имени
// адаптера, отмена и занятый адаптер от сборки ядра не зависят.
func novoeYadroVinovato(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	var zapusk oshibkaZapuskaYadra
	if !errors.As(err, &zapusk) {
		return false
	}
	return !errors.Is(err, OshibkaAdapterZanyat) && !adapterZanyatPoOshibke(zapusk.err)
}

// PodnyatZashchitu поднимает связь (podnyatZashchituRaz), перед этим ставит
// отложенную сборку ядра, если она есть. Общее тело для ручки «Подключить» и
// для авторежима.
func (s *Sluzhba) PodnyatZashchitu(ctx context.Context) error {
	smena := s.smenitYadroPeredPodyomom()
	err := s.podnyatZashchituRaz(ctx)
	if smena == nil {
		return err
	}
	if err == nil {
		smena.Zakrepit()
		log.Printf("замена ядра: сборка %s работает", smena.Teg)
		return nil
	}
	if !novoeYadroVinovato(ctx, err) {
		// Отложенная сборка остаётся на месте: проба продолжится при
		// следующем подъёме.
		return err
	}
	// Замок держится от отката до конца повторного подъёма: второе
	// подключение не должно ни менять файл ядра, ни поднимать его, пока
	// решается судьба сборки.
	s.zamokSmenyYadra.Lock()
	defer s.zamokSmenyYadra.Unlock()
	if s.Yadro.PID() != "" {
		// Ядро уже поднял кто-то другой (второе окно): файл не трогаем.
		return err
	}
	_ = s.Yadro.Ostanovit()
	oerr := smena.Otkatit()
	s.zamok.Lock()
	s.summaYadra = ""
	s.zamok.Unlock()
	if oerr != nil {
		log.Printf("замена ядра: сборка %s не поднялась (%v), и прежнее ядро не вернуть: %v", smena.Teg, err, oerr)
		return err
	}
	log.Printf("замена ядра: сборка %s не поднялась (%v) — вернул %s, поднимаю на нём", smena.Teg, err, slovoVersii(smena.Prezhniy))
	err = s.podnyatZashchituRaz(ctx)
	if err == nil {
		smena.Otvergnut()
		log.Printf("замена ядра: на прежнем ядре связь поднялась — сборку %s больше не ставлю", smena.Teg)
		return nil
	}
	log.Printf("замена ядра: прежнее ядро тоже не поднялось (%v) — дело не в сборке %s, попробую её снова при следующем подключении", err, smena.Teg)
	return err
}
