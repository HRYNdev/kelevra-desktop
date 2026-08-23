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
	"syscall"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/kopiya"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/proksi"
	"github.com/HRYNdev/kelevra-desktop/internal/sluzhba"
)

// argSluzhba/argTiho — режимы запуска этого же .exe (см. шапку файла).
const (
	argSluzhba = "--sluzhba"
	argTiho    = "--tiho"
)

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
	// сам не откроет Kelevra и не выйдет аккуратно. До obnovitsya(): мёртвый
	// системный прокси рубит и саму проверку обновления.
	snyatOsirotevshiySled(papka)

	// Свежесть — забота приложения, а не человека: иначе каждая новая сборка
	// это моё письмо со ссылкой и его ручное «скачай заново». Служба сама
	// обновление не проверяет: её поднимает уже проверенная копия.
	if obnovitsya() {
		return
	}

	// --tiho — для автозапуска с Windows: поднять службу молча, без окна.
	tiho := estArg(argTiho)

	// Окно — ВНЕ замка (см. adresKopii): pokazatOkno не возвращается, пока
	// человек не закроет окно.
	adres := adresKopii(papka, putZhurnala)
	if !tiho {
		pokazatOkno(adres)
	}
}

// adresKopii отвечает на единственный вопрос запуска: по какому адресу живёт
// ядро — уже поднятое кем-то или поднятое нами прямо сейчас. Не возвращается,
// пока ответа нет: если своя служба не встала, зовёт umeret.
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
func adresKopii(papka, putZhurnala string) string {
	// Срок ожидания тот же, что окно и так готово ждать службу: дольше держать
	// человека всё равно нечем. Не взяли (истёк срок или примитив недоступен) —
	// идём по старой схеме: это прежнее поведение, а не новый отказ.
	zamok, estZamok := kopiya.Vzyat(srokPodnyatiyaSluzhby)
	if !estZamok {
		log.Printf("замок запуска не взят за %s (держит другой запуск или примитив недоступен), продолжаю без него", srokPodnyatiyaSluzhby)
	}
	defer zamok.Otdat()

	// Приложение уже работает: открываем его окно, а не поднимаем второе ядро
	// на те же порты. Для человека двойной запуск выглядит как «показать окно».
	if adres, est := kopiya.Nayti(papka); est {
		log.Printf("копия уже запущена, открываю её окно: %s", adres)
		return adres
	}

	// Службы ещё нет: поднимаем её ОТДЕЛЬНЫМ отсоединённым процессом и ждём
	// метку копии. Раньше это же место поднимало службу прямо тут, в процессе
	// окна, — и служба умирала вместе с окном.
	adres, err := podnyatSluzhbuOtdelno(papka)
	if err != nil {
		umeret(putZhurnala, "служба Kelevra не поднялась за 20 секунд", err)
	}
	return adres
}

// snyatOsirotevshiySled чинит жёсткую смерть предыдущего запуска: Диспетчер
// задач, выключение/перезагрузка Windows (оконная сборка без консоли не
// получает SIGTERM) или пропадание питания не дают службе дойти до
// proksi.Snyat() в конце zapustitSluzhbu — реестр остаётся с ProxyEnable=1
// и нашим адресом, а ядра за ним уже нет. Дословно жалоба хозяина 20.08 10:23
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
	// открываться сайты (сказано хозяином 20.08).
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
