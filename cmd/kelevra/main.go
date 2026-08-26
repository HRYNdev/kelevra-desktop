// Kelevra для компьютера: код доступа, кнопка «Подключить», ядро sing-box под капотом.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/kopiya"
	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/proksi"
	"github.com/HRYNdev/kelevra-desktop/internal/sluzhba"
)

// argSluzhba/argTiho — режимы запуска этого же .exe (см. шапку файла).
// argSmena — не режим, а сообщение от прошлой, ещё не повышенной копии:
// значение после флага — её pid (см. prava.Poprosit и polnayaZashchita).
const (
	argSluzhba = "--sluzhba"
	argTiho    = "--tiho"
	argSmena   = "--smena"
)

// srokOzhidaniyaSmeny — сколько новая, уже повышенная копия ждёт смерти
// старой перед тем, как всё равно занять её место. Гонка 25.08 («2 нахуй
// открыто») была устроена ровно наоборот — фиксированным time.Sleep(300ms) на
// СТОРОНЕ СТАРОЙ копии без всякой обратной связи; здесь ждёт та копия, что
// реально должна знать правду, и явным опросом, а не наугад.
const srokOzhidaniyaSmeny = 10 * time.Second

// srokPodnyatiyaSluzhby — сколько окно ждёт, пока отдельно запущенная служба
// отметится меткой копии. Дольше держать человека перед пустым экраном
// бессмысленно: если за 20 секунд служба не встала, дело не в медленном
// диске, а в реальном отказе (см. umeret ниже).
const srokPodnyatiyaSluzhby = 20 * time.Second

func main() {
	papka := hranenie.Papka()
	putZhurnala, zakryt := otkrytZhurnal(papka)
	defer zakryt()

	// Окно и служба — теперь РАЗНЫЕ процессы одного .exe (беда 20.08: окно и
	// служба жили в одном процессе, и крестик на окне гасил ядро с прокси
	// молча). Прокси ставит и снимает только служба, поэтому только она имеет
	// право звать proksi.Snyat() из lovitPaniku — окну чужой прокси не трогать.
	// Исключение — snyatOsirotevshiySled ниже: это не «окно гасит прокси
	// живой службы», а уборка за службой, которая уже доказанно мертва
	// (без этого исключения жёсткая смерть процесса, которую не ловит ни
	// один сигнал, оставляла бы прокси висеть навсегда — см. её комментарий).
	//
	// KELEVRA_BEZ_OKNA=1 — синоним --sluzhba: на нём стоит мой стенд
	// (stend/windows.sh) и разбор беды у человека, менять эту переменную нельзя.
	rezhimSluzhby := os.Getenv("KELEVRA_BEZ_OKNA") == "1" || estArg(argSluzhba)
	defer lovitPaniku(putZhurnala, rezhimSluzhby)
	log.Printf("--- запуск Kelevra %s (%s/%s), данные: %s", podpiska.Versiya, runtime.GOOS, runtime.GOARCH, papka)

	if rezhimSluzhby {
		zapustitSluzhbu(papka, putZhurnala)
		return
	}

	// Жёсткая смерть предыдущего запуска (Диспетчер задач, выключение или
	// перезагрузка Windows, пропадание питания) не даёт службе дойти до
	// proksi.Snyat() в конце zapustitSluzhbu — реестр остаётся с нашим прокси
	// висеть без ядра за ним, и у человека НЕ грузится ни один сайт, пока он
	// сам не откроет Kelevra и не выйдет аккуратно.
	snyatOsirotevshiySled(papka)

	// Свежесть больше не подменяет .exe САМА на холодном старте (заказ Вовы
	// 26.08: «просто приходит обновление и ты тыкаешь, а не автоматом» —
	// раньше здесь стоял obnovitsya(), который качал и ставил новую сборку
	// молча, без единого клика, и человек это «автоматом» не видел никогда:
	// копия успевала стать свежей раньше, чем он моргал). Находка и предложение
	// — теперь дело службы сразу после подъёма (internal/sluzhba.
	// SleditZaObnovleniem: первая проверка не ждёт периода), установка —
	// только по тычку человека в пузырь трея (PostavitNaydennoe). Здесь
	// остаётся только уборка ХВОСТА прошлого обновления.
	ubratHvostProshlogoObnovleniya()

	// --smena <pid> — эта копия только что повышена через UAC из
	// polnayaZashchita: старая копия (тот самый pid) ещё может быть жива и
	// держит метку. adresKopii ждёт её смерти сама, а не идёт по обычной
	// ветке «нашёл чужую живую копию» (см. её комментарий).
	smenaPID, estSmena := argSmenaPID()

	// --tiho — для автозапуска с Windows: поднять службу молча, без окна.
	// Смена режима идёт тем же путём: это внутренний перезапуск самого
	// приложения, а не человек, дважды щёлкнувший по значку, — новое окно
	// тут никто не просил. Раньше его всё равно открывал бы pokazatOkno,
	// и человек увидел бы то самое второе окно поверх старого, которое
	// сторож (storozh_okna.go) ещё не успел закрыть.
	tiho := estArg(argTiho) || estSmena

	// Окно — ВНЕ замка (см. adresKopii): pokazatOkno не возвращается, пока
	// человек не закроет окно.
	adres, chuzhaya := adresKopii(papka, putZhurnala, smenaPID)
	if !tiho {
		// Беда 23.08: adresKopii уже отличает «нашёл чужую копию» от «поднял
		// свою службу», но раньше main звал pokazatOkno одинаково в обоих
		// случаях — второе окно создавалось поверх уже открытого первого, оба
		// опрашивали /api/sostoyanie независимо, и второе слало podklyuchit
		// на уже работающее ядро (та самая ошибка Zapustit, см. yadro.go).
		// Если копия чужая, сперва пробуем поднять уже существующее окно —
		// и только если его не нашли (не успело появиться), откатываемся на
		// старое поведение и создаём своё.
		if chuzhaya {
			log.Printf("адрес взят у чужой копии, пробую поднять её окно вместо создания своего")
			// Человек кликнул значок в трее — живая копия могла висеть там
			// неделями, и её собственная периодическая проверка
			// (internal/sluzhba.SleditZaObnovleniem) ещё нескоро дойдёт до
			// своего тика. Открытие окна — момент не хуже холодного старта,
			// чтобы спросить: толкаем чужую копию проверить, но не ждём её
			// ответа — это не наше дело здесь и не имеет права задержать
			// открытие окна.
			podtolknutFonovuyuProverku(adres)
			if podnyatChuzheeOkno() {
				return
			}
			log.Printf("чужое окно не нашлось, открываю своё")
		}
		pokazatOkno(adres)
	}
}

// adresKopii отвечает на единственный вопрос запуска: по какому адресу живёт
// ядро — уже поднятое кем-то или поднятое нами прямо сейчас. Не возвращается,
// пока ответа нет: если своя служба не встала, зовёт umeret. Второе
// возвращаемое значение — адрес взят у ЧУЖОЙ копии (kopiya.Nayti), а не
// поднят нами только что: вызывающему это нужно, чтобы решить, пробовать ли
// поднять чужое уже открытое окно вместо создания своего (см. main()).
//
// Всё решение целиком — под замком ОС. Беда 23.08: Диспетчер задач показывал
// по несколько Kelevra.exe сразу («(3)» и «(9)»), и отключение с двумя
// открытыми окнами падало ошибкой. Nayti() читает файл-метку, а служба
// записывает её не мгновенно: между стартом первой службы и появлением метки
// есть окно длиной в подъём службы. Два .exe, запущенных подряд, оба проходят
// Nayti() с ответом «копии нет» и оба поднимают своё ядро на одни порты —
// stend/dvoynoy_zapusk.sh ловил это 5 раз из 5, с замком 0 из 5. Файл не
// умеет быть атомарным сам с собой между двумя процессами, замок ОС умеет.
//
// Замок живёт ровно в этой функции, и это главное в её существовании: окно
// показывает уже вызывающий, снаружи. Держи мы замок до конца процесса —
// починка гонки стоила бы штатного сценария «щёлкнул по значку, пока
// приложение работает»: второй .exe висел бы на чужом замке все 20 секунд,
// ещё не дойдя до чтения метки, и человек видел бы, что ничего не
// происходит. Под wine это не проверить (окна нет), поэтому запрет держится
// не стендом, а формой: pokazatOkno вызвать отсюда попросту нельзя.
func adresKopii(papka, putZhurnala string, smenaPID int) (string, bool) {
	// Срок ожидания тот же, что окно и так готово ждать службу: дольше держать
	// человека всё равно нечем. Не взяли (истёк срок или примитив недоступен) —
	// идём по старой схеме: это прежнее поведение, а не новый отказ.
	zamok, estZamok := kopiya.Vzyat(srokPodnyatiyaSluzhby)
	if !estZamok {
		log.Printf("замок запуска не взят за %s (держит другой запуск или примитив недоступен), продолжаю без него", srokPodnyatiyaSluzhby)
	}
	defer zamok.Otdat()

	if smenaPID > 0 {
		// Смена режима (см. polnayaZashchita): старая копия жива и держит
		// метку нарочно. Ждём её смерти вместо того, чтобы, как обычная
		// вторая копия, открыть её (умирающее) окно.
		zhdatSmenu(smenaPID)
		kopiya.Osvobodit(papka)
	} else if adres, est := kopiya.Nayti(papka); est {
		// Приложение уже работает: открываем его окно, а не поднимаем второе
		// ядро на те же порты. Для человека двойной запуск выглядит как
		// «показать окно».
		log.Printf("копия уже запущена, открываю её окно: %s", adres)
		return adres, true
	}

	// Службы ещё нет: поднимаем её ОТДЕЛЬНЫМ отсоединённым процессом и ждём
	// метку копии. Раньше это же место поднимало службу прямо тут, в процессе
	// окна, — и служба умирала вместе с окном.
	adres, err := podnyatSluzhbuOtdelno(papka)
	if err != nil {
		umeret(putZhurnala, "служба Kelevra не поднялась за 20 секунд", err)
	}
	return adres, false
}

// argSmenaPID ищет "--smena <pid>" среди аргументов запуска (см. константы
// вверху файла). Это не режим самого запуска, а сообщение от прошлой,
// ещё не повышенной копии, которая только что позвала ShellExecuteW.
func argSmenaPID() (int, bool) {
	for i, a := range os.Args {
		if a == argSmena && i+1 < len(os.Args) {
			if pid, err := strconv.Atoi(os.Args[i+1]); err == nil && pid > 0 {
				return pid, true
			}
		}
	}
	return 0, false
}

// zhdatSmenu ждёт смерть старой копии (её pid передан аргументом --smena),
// прежде чем эта, уже повышенная копия займёт её место. Замена гонки
// (фиксированный time.Sleep(300ms) на стороне СТАРОЙ копии из прежней
// polnayaZashchita, без всякой обратной связи от новой) явным опросом:
// новая копия — та, что реально должна знать правду о смерти старой, а не
// наоборот. Не дождалась за srokOzhidaniyaSmeny — не виснем вечно, старая
// копия и правда могла зависнуть, но след в журнале обязателен: молчать о
// таком нельзя.
func zhdatSmenu(pid int) {
	predel := time.Now().Add(srokOzhidaniyaSmeny)
	for zhivProcess(pid) {
		if time.Now().After(predel) {
			log.Printf("смена режима: старая копия (pid %d) не завершилась за %s, занимаю её место всё равно", pid, srokOzhidaniyaSmeny)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Printf("смена режима: старая копия (pid %d) завершилась, занимаю её место", pid)
}

// snyatOsirotevshiySled чинит жёсткую смерть предыдущего запуска: Диспетчер
// задач, выключение/перезагрузка Windows (оконная сборка без консоли не
// получает SIGTERM) или пропадание питания не дают службе дойти до
// proksi.Snyat() в конце zapustitSluzhbu — реестр остаётся с ProxyEnable=1
// и нашим адресом, а ядра за ним уже нет. Дословно жалоба Вовы 20.08 10:23
// про закрытие приложения — эта же дыра, только для смерти без выхода.
//
// Три проверки подряд, и снимаем только если прошли все три:
//  1. metka.est — метка на диске есть: кто-то когда-то поставил прокси нашей
//     рукой или рукой ядра с нашим подтверждением (см. internal/proksi.Otmetit).
//     Нет метки — нам сюда вообще нечего смотреть, чужой прокси не наше дело.
//  2. !zhiva — живой копии службы нет. Если служба жива, прокси её и
//     трогать его отсюда, из процесса окна, — та самая дыра из 20.08.
//  3. proksi.Stoit(adres) — то, что СЕЙЧАС стоит в реестре, совпадает именно
//     с адресом из метки. Не совпадает (человек успел прописать свой прокси
//     после смерти нашей службы) — снимать НЕЧЕГО, чужой прокси не трогаем
//     никогда; метку при этом всё равно убираем, она устарела.
func snyatOsirotevshiySled(papka string) {
	adres, est := proksi.ProchestMetku()
	if !est {
		return
	}
	if _, zhiva := kopiya.Nayti(papka); zhiva {
		return
	}
	if !proksi.Stoit(adres) {
		proksi.UbratMetku()
		return
	}
	log.Printf("прошлый запуск умер жёстко и не снял системный прокси (%s), снимаю сам", adres)
	proksi.Snyat()
}

// ubratHvostProshlogoObnovleniya убирает <имя>.old, оставшийся от прошлого
// тычка в пузырь (obnovlenie.Postavit переименовывает старый файл, а не
// удаляет — пока он был запущен, удалить было нельзя). Раньше эту же строку
// звал obnovitsya() на каждом холодном старте попутно со своей автоматической
// подменой .exe (см. её комментарий в main()); теперь это единственное, что
// здесь осталось от того пути — тихо, без сети и не блокируя запуск.
func ubratHvostProshlogoObnovleniya() {
	put, err := obnovlenie.PutSebya()
	if err != nil {
		log.Printf("уборка хвоста обновления: не знаю, где лежу: %v", err)
		return
	}
	obnovlenie.UbratHvost(put)
}

// estArg — есть ли такой флаг среди os.Args.
func estArg(arg string) bool {
	for _, a := range os.Args[1:] {
		if a == arg {
			return true
		}
	}
	return false
}

// podnyatSluzhbuOtdelno запускает службу отдельным процессом (zapusk_windows.go
// / zapusk_other.go) и ждёт, пока она отметится меткой копии — так же, как
// это видит следующий двойной щелчок пользователя.
func podnyatSluzhbuOtdelno(papka string) (string, error) {
	if err := zapustitOtdelnuyuSluzhbu(); err != nil {
		return "", err
	}
	predel := time.Now().Add(srokPodnyatiyaSluzhby)
	for time.Now().Before(predel) {
		if adres, est := kopiya.Nayti(papka); est {
			log.Printf("служба поднялась отдельным процессом: %s", adres)
			return adres, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return "", fmt.Errorf("не дождался метки службы за %s", srokPodnyatiyaSluzhby)
}

// zapustitSluzhbu — режим --sluzhba (и его синоним KELEVRA_BEZ_OKNA=1):
// ядро, HTTP-служба и ожидание сигнала остановки, без окна и без проверки
// обновлений (её уже сделала копия, которая эту службу подняла).
//
// Это единственное место, где ставится системный прокси, поэтому и снимать
// его при выходе (штатном и аварийном) должно только оно — иначе процесс окна
// снимал бы прокси, который поставил не он, и делал бы это на закрытии
// крестиком, что и было исходной бедой.
func zapustitSluzhbu(papka, putZhurnala string) {
	s, err := sluzhba.Novaya()
	if err != nil {
		umeret(putZhurnala, "Kelevra не смогла подготовить свои файлы", err)
	}
	// Хук подключается тут, а не живёт внутри internal/sluzhba: пакет sluzhba
	// про трей и Windows ничего не знает (см. комментарий поля). На Windows
	// это настоящий пузырь (trey_windows.go), на остальных платформах — след
	// в журнале для стенда (trey_other.go); обе реализации лежат в одном
	// пакете main, поэтому здесь имя одно и то же независимо от ОС.
	s.OblachkoObnovleniya = pokazatOblachkoObnovleniya
	s.PerezapuskPosleObnovleniya = zapustitSmenuPosleObnovleniya
	// Тычок в пузырь трея зовёт этот же метод напрямую (trey_windows.go:
	// tychokVPuzyr) — пакет trey про internal/sluzhba ничего не знает, тем же
	// принципом, что и OblachkoObnovleniya выше.
	ustanovitObnovlenie = s.PostavitNaydennoe
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
	// Копия, которую человек не закрывал днями, никогда больше не проходит
	// obnovitsya() (он звучит один раз при холодном старте, выше по main()) —
	// без этой строки 0.6.23 и 0.6.24 не долетели бы до него никогда, сколько
	// бы дней приложение ни висело в трее. Только СПРАШИВАЕТ и запоминает
	// находку для /api/sostoyanie — не ставит сама (см. комментарий метода).
	go s.SleditZaObnovleniem(ctx, periodFonovoyProverki())

	// Авторежим по умолчанию выключен (см. hranenie.Nastroyki.Avtorezhim) —
	// заводим служителя, только если человек сам его включил в прошлый раз.
	// Останавливать отдельно не нужно: служитель слушает тот же ctx, что
	// ObnovlyatProfil выше, и defer otmena() гасит обоих разом.
	if s.Nastroyki.Avtorezhim {
		s.ZapustitAvtorezhim(ctx)
	}

	log.Printf("служба слушает %s", url)
	fmt.Println("KELEVRA-SLUZHBA", url)

	// Значок в трее — обязательное завершение переделки из b924080: без
	// него служба живёт невидимкой (закрыл окно — защита работает, но
	// понять это и выключить нечем). Своя горутина со своим recover: отказ
	// трея (включая Windows без explorer.exe, как на моём стенде под wine)
	// не имеет права уронить службу с прокси.
	vyhodIzTreya := make(chan struct{}, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("трей: авария в потоке значка, продолжаю без него: %v\n%s", r, debug.Stack())
			}
		}()
		zapustitTrey(vyhodIzTreya)
	}()

	zhdatSignal(vyhodIzTreya)

	_ = s.Yadro.Ostanovit()
	// Ядро гасится жёстко и откатить системный прокси за собой не успевает.
	// Без этой строки после закрытия приложения у человека перестают
	// открываться сайты (сказано Вовой 20.08).
	proksi.Snyat()
}

// zhdatSignal держит служебный режим живым до Ctrl+C, остановки извне или
// «Выход» из значка в трее (vyhodIzTreya) — оба пути должны довести дело до
// одного и того же штатного завершения ниже (Ostanovit + Snyat), а не
// выйти через os.Exit из чужого потока.
func zhdatSignal(vyhodIzTreya <-chan struct{}) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case <-stop:
		log.Printf("служебный режим: получен сигнал, останавливаюсь")
	case <-vyhodIzTreya:
		log.Printf("служебный режим: «Выход» из трея, останавливаюсь")
	}
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
//
// snyatProksi — этот процесс сейчас служба (единственная, кто ставит системный
// прокси)? Процесс окна прокси не ставил и снимать чужой не должен: иначе
// авария окна (или само его закрытие) гасила бы защиту, которую поднял
// отдельный процесс службы, — та же дыра, которую разводит вся эта переделка.
func lovitPaniku(putZhurnala string, snyatProksi bool) {
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
	if snyatProksi {
		// os.Exit минует код после lovitPaniku в main (там же снимается
		// прокси), а авария ядра оставляет системный прокси включённым точно
		// так же, как обычное закрытие окна — снимаем его и на этом пути.
		proksi.Snyat()
	}
	os.Exit(2)
}
