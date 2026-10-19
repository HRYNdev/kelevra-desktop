// Пакет sluzhba: вся работа приложения, выставленная наружу как маленький
// локальный HTTP API, и интерфейс поверх него.
//
// Интерфейс — обычная страница в окне приложения. Такой стык даёт две вещи:
// окно можно проверить в браузере на сервере, где Windows нет, а логика
// проверяется без окна вовсе.
package sluzhba

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/avtozapusk"
	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/ochered"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/prava"
	"github.com/HRYNdev/kelevra-desktop/internal/pravila"
	"github.com/HRYNdev/kelevra-desktop/internal/proksi"
	"github.com/HRYNdev/kelevra-desktop/internal/tunnel"
	"github.com/HRYNdev/kelevra-desktop/internal/ustroystvo"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
	"github.com/HRYNdev/kelevra-desktop/internal/zapisi"
	"github.com/HRYNdev/kelevra-desktop/internal/zhurnaly"
)

//go:embed oblik/*
var oblik embed.FS

// Sluzhba — приложение целиком, без оболочки окна.
type Sluzhba struct {
	Nastroyki *hranenie.Nastroyki
	Yadro     *yadro.Yadro
	Podpiska  *podpiska.Klient

	// vyhod — как эта копия уходит, уступив место (обновление, новая копия).
	// По умолчанию os.Exit(0); на стенде подменяется, иначе тест убил бы сам
	// себя.
	vyhod func()
	// zapustitYadro — точка подмены для тестов лестницы подстраховок в
	// PodnyatZashchitu (23-24.08): Yadro.Zapustit порождает настоящий
	// процесс, а тест должен управлять числом попыток и их исходом без
	// живого ядра. По умолчанию nil — PodnyatZashchitu зовёт s.Yadro.Zapustit.
	zapustitYadro func(context.Context) error
	// avtorezhimDlyaKnopki — точка подмены для тестов domaSeychas/podklyuchit:
	// собирает *avtorezhim.Avtorezhim для одиночного доверенного захода
	// кнопки «Подключиться» на подставных зондах, без настоящей сети. nil —
	// domaSeychas собирает боевой avtorezhim.Novyy() с s.tunnelPodnyat (см.
	// avtorezhimBoevoy).
	avtorezhimDlyaKnopki func() *avtorezhim.Avtorezhim
	// avtorezhimDlyaSluzhitelya — точка подмены для тестов ФОНОВОГО пути
	// (zapustitAvtorezhimSNachala → go sluzh.Krutit): по умолчанию (nil)
	// собирает тот же боевой avtorezhim.Novyy() через avtorezhimBoevoy, что и
	// сейчас. Нужна отдельно от avtorezhimDlyaKnopki: тот подменяет только
	// одиночный заход кнопки (domaSeychas), а служитель — второй, независимый
	// путь к тем же зондам (см. gotovStendLestnicy в pravila_lestnitsa_test.go
	// и диагноз падения на настоящей Windows 10.09 — незаглушенный служитель бил по настоящей
	// сети параллельно с тестом и накручивал посторонний podyomov).
	avtorezhimDlyaSluzhitelya func() *avtorezhim.Avtorezhim
	// avtorezhimDnsAdres — из KELEVRA_AVTOREZHIM_DNS (Novaya): "ip:port"
	// резолвера, которого DNS-зонд обязан спрашивать НАПРЯМУЮ вместо
	// системного пути, и в domaSeychas, и в фоновом авторежиме
	// (ZapustitAvtorezhim). Пусто в бою — поведение прежнее (см.
	// avtorezhimBoevoy). Нужно исключительно площадке стендов: сам контейнер,
	// в котором они гоняются, живёт за резолвером, отвечающим на контрольные
	// домены fake-ip подменой (тот же диапазон 198.18.0.0/15, что и у
	// нашего ядра) — без этой настройки зонд честно, но ложно решает «дома»
	// и «Подключиться» молча не поднимает защиту (см. stend/zond_doma.sh).
	avtorezhimDnsAdres string

	// avtorezhimShlyuz — из KELEVRA_AVTOREZHIM_SHLYUZ (Novaya): чем
	// подменить чтение номера шлюза, по которому авторежим узнаёт дом
	// (см. avtorezhim.MakiShlyuzov). Пусто в бою — читаются настоящие шлюзы.
	// Несколько задаётся через запятую (см. avtorezhimBoevoy).
	//
	// Нужно ровно затем же, зачем и avtorezhimDnsAdres рядом: у площадки, на
	// которой гоняются проверки, свой шлюз, и его номер не совпадает ни с
	// домашним, ни с чужим осмысленно. Значение «нет» означает «прочитать не
	// вышло» — так проверяется запасной путь через DNS; любое другое
	// значение отдаётся как номер шлюза.
	avtorezhimShlyuz string
	// avtorezhimKnopkaTaimaut — таймаут одиночного захода кнопки
	// «Подключиться» (domaSeychas). <=0 значит KnopkaTaimautPoUmolchaniyu
	// (8с, см. там же, почему именно столько) — своё поле только ради
	// теста «заход завис», чтобы не ждать боевые 8с на каждый прогон.
	avtorezhimKnopkaTaimaut time.Duration
	// estSetDlyaStenda — чем проверки подменяют признак «сеть есть» при
	// ожидании сети после сна (povtor_seti.go). nil в бою — avtorezhim.EstShlyuz.
	estSetDlyaStenda func() bool
	// OblachkoObnovleniya — как сказать о находке пузырём в трее (cmd/kelevra/
	// trey_windows.go: pokazatOblachkoObnovleniya), подключается снаружи из
	// main.go: пакет sluzhba про Windows-трей ничего не знает и знать не
	// должен (см. шапку файла — окно и логика разведены нарочно). nil —
	// сборка вызывающего кода не подключила хук (например, стенд-тесты
	// внутри этого пакета) — тогда находка всё равно видна в
	// /api/sostoyanie, просто без звука. Возвращает true, только если пузырь
	// на деле показан: от этого зависит отметка «объявлено» (povestitEsliNovaya).
	OblachkoObnovleniya func(versiya string) bool

	// MetkaObnovleniya — как показать СОСТОЯНИЕ «обновление ждёт» на значке
	// трея (cmd/kelevra/metka_obnovleniya.go: pometitObnovlenie). Отдельный
	// хук от OblachkoObnovleniya нарочно, и вот почему: пузырь — СОБЫТИЕ,
	// про версию он звучит ровно один раз навсегда (povestitEsliNovaya,
	// отметка на диске), а метка — СОСТОЯНИЕ, она обязана держаться, пока
	// обновление не поставлено. Свяжи их вместе — и после перезапуска копии
	// (пузырь про эту версию уже сказан в прошлой жизни) значок снова
	// выглядел бы так, будто ставить нечего, и тыкать человеку было бы не
	// во что. Поэтому зовётся на КАЖДОЙ фоновой проверке: версия — есть
	// находка, "" — версия и так свежая, метку снять. nil — хук не
	// подключён (стенд-тесты внутри пакета).
	MetkaObnovleniya func(versiya string)

	// MetkaZashchity — как показать на значке трея, поднята ли связь
	// (cmd/kelevra/metka_zashchity.go: pometitZashchitu). Той же природы, что
	// MetkaObnovleniya выше: состояние, а не событие. Подсказка значка до 31.08
	// была константой «Kelevra: VPN включён» и висела и на опущенной защите.
	// Неполного режима с 0.6.77 нет, и говорить значку про «половину» нечего.
	// nil — хук не подключён (стенд-тесты внутри этого пакета).
	MetkaZashchity func(podnyata bool)

	// PerezapuskPosleObnovleniya — как поднять новую копию после того, как
	// PostavitNaydennoe заменила .exe на диске: передача смены
	// (cmd/kelevra/main.go: zapustitSmenuPosleObnovleniya, --smena,
	// zhdatSmenu) — новая копия сама
	// дожидается смерти этого pid, а не гонка на фиксированной паузе.
	// Подключается снаружи из main.go по тому же принципу, что и
	// OblachkoObnovleniya выше: пакет sluzhba не порождает процессы сам. nil —
	// хук не подключён (стенд-тесты внутри этого пакета); тогда установка всё
	// равно считается удавшейся (файл на диске уже новый), но эта копия себя
	// НЕ гасит — иначе человек остался бы вообще без работающей копии.
	//
	// put — путь к новому .exe, УЖЕ разрешённый до Postavit(), а не то, что
	// хук получил бы, спроси он obnovlenie.PutSebya() заново сам: на Linux
	// os.Executable() у ещё живого (старого) процесса после Postavit()
	// возвращает путь ПЕРЕИМЕНОВАННОГО файла (readlink /proc/self/exe следует
	// за inode при rename) — замерено живьём при разборе stend/
	// obnovlenie_postavit.sh: вторая копия PutSebya() отдавала «...Kelevra.old»,
	// и новая копия лишний раз сама себя перекачивала поверх уже поставленной
	// версии, прежде чем встать правильно. Тот же put, что уже использован для
	// самого Postavit(), snимает вопрос совсем — его и передаём.
	PerezapuskPosleObnovleniya func(put string, pid int) error

	// PostavitSluzhbuWindows — попросить у человека права и поставить службу
	// Windows (кнопка «Дать права», ручка /api/dat_prava). Хук, а не прямой
	// вызов: решение о том, как именно просить права и что при этом
	// запускать, живёт в cmd/kelevra, где известен режим запуска. nil
	// означает «на этой системе службы нет» — ручка тогда честно откажет.
	PostavitSluzhbuWindows func() error
	// SluzhbaWindowsEst — установлена ли служба. Тоже хук: пакету незачем
	// знать, как устроен диспетчер служб, ему нужен ответ да или нет.
	SluzhbaWindowsEst func() bool

	zamok      sync.Mutex
	svedeniya  *podpiska.Svedeniya
	klyuch     string
	kachaemBin bool // идёт скачивание ядра
	kartina    konfig.Kartina
	// otkaz — почему последняя попытка не подняла связь, словами человека
	// (Oshibka* из PodnyatZashchitu). Пусто — связь поднята или её не
	// поднимали. Окно показывает строку в состоянии «Нет подключения».
	otkaz string

	// naydennoeObnovlenie — находка ФОНОВОЙ проверки (SleditZaObnovleniem,
	// obnovlenieProveritRuchka), не ручного нажатия кнопки: та отвечает
	// прямо в HTTP-ответ и не запоминает себя. nil — фон ничего не нашёл
	// (или ещё не спрашивал). Видна человеку через /api/sostoyanie тем же
	// полем, что и остальная правда о приложении — молчащая в логе находка
	// человеку не видна вообще.
	naydennoeObnovlenie *obnovlenie.Novaya
	// idetProverkaObnovleniya — не даёт фоновому тику и толчку от открытия
	// окна другой копии (obnovlenieProveritRuchka) спросить GitHub разом.
	idetProverkaObnovleniya bool
	// summaExe / summaYadra — короткие отпечатки файлов, посчитанные один раз.
	// Паспорт уходит с КАЖДЫМ запросом к серверу, а файлы весят десятки
	// мегабайт: считать их отпечаток каждый раз значило бы жечь диск и время
	// на данные, которые между перезапусками не меняются.
	summaExe   string
	summaYadra string

	// tihoStavili — версия, которую тихая установка уже пробовала поставить.
	// Без неё редкий фоновый тик дёргал бы установку по кругу на одной и той же
	// сборке, если та не встаёт (занятый файл, нет прав на запись).
	tihoStavili string

	// idetUstanovkaObnovleniya — свой замок, отдельный от idetProverkaObnovleniya
	// выше: второй тычок в пузырь, пока первый ещё качает найденную сборку, не
	// должен звать obnovlenie.Postavit второй раз (см. PostavitNaydennoe).
	idetUstanovkaObnovleniya bool
	// posleObnovleniya — номер версии, которую человеку надо показать в окне
	// разъяснением, и признак того, что показывать вообще надо. Гасится
	// первым же опросом состояния (см. Sostoyanie): один раз, не навсегда.
	posleObnovleniya string
	// svezhihPravil — сколько наборов правил обновилось на диске и ждёт
	// применения. Ядро читает наборы только при старте, поэтому свежие файлы
	// вступают в силу при следующем подъёме связи, а не сразу. Держим число,
	// чтобы это было видно и в окне, и в журнале, а не молчаливо копилось.
	svezhihPravil int
	// podyomIdet — прямо сейчас работает PodnyatZashchitu.
	//
	// Заведено 08.09 по замеру с живой машины. Хронология оттуда: полный
	// режим упал на занятом адаптере, пошёл откат в половинный, ядро уже
	// поднималось — и в эту же секунду авторежим решил «дома» и погасил его
	// своей рукой. Подъём, разумеется, провалился, приложение записало
	// «откат тоже не удался» и показало человеку красную беду. Беды при этом
	// не было вовсе: дома связь и не нужна, авторежим сработал правильно.
	// Плохо было то, что приложение сначала само себя погасило, а потом само
	// на себя пожаловалось.
	podyomIdet bool
	// skachanoObnovleniya и vsegoObnovleniya — ход ИДУЩЕЙ прямо сейчас
	// загрузки (obnovlenie.PostavitSHodom докладывает сюда). Живут под тем же
	// zamok и ровно столько же, сколько idetUstanovkaObnovleniya: и удача, и
	// обрыв обнуляют их вместе с замком — иначе окно показывало бы застывшие
	// «47%» уже после того, как всё кончилось, а это враньё хуже молчания.
	// vsegoObnovleniya == 0 — длины ответа сервер не назвал, доли нет вовсе
	// (см. obnovlenie.Hod).
	skachanoObnovleniya int64
	vsegoObnovleniya    int64

	// oknoSprosilo — когда открытое окно приложения последний раз спрашивало
	// /api/sostoyanie. Живёт под тем же zamok, что и находка выше.
	//
	// Зачем служба вообще знает про окно. Про одну и ту же находку человека
	// дёргали ДВАЖДЫ: пузырь из трея Windows и модальный диалог в самом окне
	// (жалоба 02.09: «уведомлялка некрасивая»). Свести к одному пути значит
	// решить, кто говорит, а решает это ровно один факт — смотрит ли человек
	// прямо сейчас в окно. Смотрит — говорит диалог, он и подробнее, и с
	// кнопкой, и с процентами скачивания; не смотрит — говорить может только
	// трей, иначе находка останется немой до следующего открытия окна.
	//
	// Признак — ЗАМЕР, а не догадка: окно опрашивает состояние раз в 2 секунды
	// и метит свой запрос (index.html: api("sostoyanie?okno=1")). Ничего
	// другого служба про окно знать не может: окно — отдельный процесс, и
	// умереть оно может без единого прощального слова (тем же крестиком, тем
	// же Диспетчером задач). Отметка времени, а не тумблер «окно есть», именно
	// поэтому: молчание дольше srokZhivogoOkna само означает, что окна нет.
	oknoSprosilo time.Time

	// Авторежим (переключение защиты по смене сети) живёт под своим замком,
	// отдельным от zamok выше: запуск/остановка служителя не должны ждать
	// того же замка, что держат при пересборке конфига или скачивании ядра.
	avtorezhimZamok  sync.Mutex
	avtorezhimOtmena context.CancelFunc     // не nil, пока служитель крутится
	avtorezhimEkz    *avtorezhim.Avtorezhim // тот же экземпляр — источник обстановки для /api/sostoyanie

	// avtorezhimPokolenie — номер ПОСЛЕДНЕГО решения человека про автомат.
	// Растёт на каждое его слово: включил автомат (vklyuchitAvtorezhim,
	// zapustitAvtorezhimSNachala) и выключил (OstanovitAvtorezhim — её же
	// зовут ручка /api/avtorezhim и кнопка «Отключить»).
	//
	// Зачем. Заход авторежима идёт секундами (зонды), а подъём защиты — до
	// 70 с (ядро стартует). Человек за это время успевает нажать «Отключить»,
	// и заход, начатый ДО нажатия, доводил своё решение до конца уже ПОСЛЕ
	// него: защита поднималась обратно сама. Со стороны это «нажал отключить,
	// а оно само включилось» — кнопка выглядит сломанной, и жалоба на это
	// уже была: авторежим вообще не работает.
	//
	// Служитель запоминает поколение, при котором был заведён, и приведение
	// защиты сверяет его перед тем, как что-то сделать (avtorezhimAktualen).
	// Слово человека всегда бьёт решение автомата, начатое раньше, и никакой
	// синхронизации по времени для этого не нужно — только сравнение чисел.
	avtorezhimPokolenie uint64

	// avtorezhimKnopkaObstanovka — обстановка, увиденная последним заходом
	// domaSeychas (кнопка «Подключиться»). Отдельно от avtorezhimEkz: тот
	// живёт, только пока фоновый служитель поднят тумблером
	// Nastroyki.Avtorezhim, а кнопка обязана спрашивать обстановку всегда,
	// тумблера не касаясь (заказ 28.08). /api/sostoyanie берёт отсюда,
	// когда фонового служителя нет.
	avtorezhimKnopkaObstanovka avtorezhim.Sostoyanie

	// Суточная отправка журналов (internal/zhurnaly). Оба поля под общим
	// s.zamok: читает их тикер, а пишет он же и ручка тумблера.
	//
	// zhurnalyPopytka — когда ПЫТАЛИСЬ в последний раз, удачно или нет. В
	// памяти процесса, а не на диске, нарочно: она держит правило «повтор не
	// чаще раза в час», а перезапуск копии — это и так новая попытка, ждать
	// после него лишний час незачем. Удача, в отличие от попытки, живёт на
	// диске (hranenie.Nastroyki.OtpravkaZhurnalovKogda).
	zhurnalyPopytka time.Time
	// zhurnalyPovtorCherez — сколько ждать после последней неудачи, прежде чем
	// пробовать снова. Живёт рядом с zhurnalyPopytka и под тем же замком:
	// вдвоём они и составляют правило повтора. Ноль означает «как обычно»
	// (PovtorPosleOtkaza); короткое значение ставится, когда отказ случился до
	// первого отправленного байта — см. otkazDoPeredachi.
	zhurnalyPovtorCherez time.Duration
	// zhurnalyIdut — посылка уже в пути. Тик каждые несколько минут не должен
	// начать вторую отправку поверх первой: 25 МБ по слабому каналу уходят
	// дольше, чем идёт тик.
	zhurnalyIdut bool
	// otpravshchikZhurnalovDlyaStenda — точка подмены для проверок: настоящий
	// Otpravshchik ходит в сеть и читает живую папку приложения. nil в бою.
	otpravshchikZhurnalovDlyaStenda *zhurnaly.Otpravshchik

	// pravaDlyaStenda — точка подмены prava.Est(): есть ли у этой копии права
	// поднять туннель. nil в бою, и тогда спрашивается настоящий процесс.
	//
	// Зачем она есть. Туннель бывает только при правах администратора, а
	// prava.Est() отвечает про настоящий процесс: гонять проверки от
	// администратора значит поднимать на машине проверяющего настоящий
	// сетевой адаптер (запрет трогать сеть). Тот же приём, что Adapter в
	// internal/tunnel и zapustitYadro рядом: подменяем ровно один ответ
	// системы, а проверяем своё решение по нему.
	pravaDlyaStenda func() bool

	// adapterZhivDlyaStenda — точка подмены tunnel.Zhivoy (есть ли в системе
	// сетевой адаптер с таким именем). nil в бою. Настоящих адаптеров в
	// проверках быть не должно вовсе — тот же запрет, что у pravaDlyaStenda
	// выше, и та же причина: занятое имя иначе не воспроизвести, не подняв на
	// машине проверяющего настоящий туннель.
	adapterZhivDlyaStenda tunnel.Adapter

	// zashchitaPodnyataDlyaStenda — точка подмены zashchitaPodnyata(). nil в бою.
	//
	// Настоящая поднятость смотрит на живое ядро, а на стенде его нет и быть
	// не должно (тот же запрет трогать сеть на машине человека, что у
	// pravaDlyaStenda рядом). Без подмены нельзя проверить приведение
	// авторежима вовсе: правило «дома опусти» срабатывает только при поднятой
	// связи, и проверка молча проходила бы мимо самого случая.
	zashchitaPodnyataDlyaStenda func() bool

	// snyatProksiDlyaStenda — точка подмены proksi.UbratSled() (уборка
	// системного прокси, оставленного старой версией в прокси-режиме). nil в
	// бою. Проверить уборку иначе нечем: на линуксе proksi.UbratSled —
	// заглушка, а гонять проверки на живой Windows нельзя (запрет трогать
	// сеть на машине человека, тот же, что у pravaDlyaStenda рядом).
	snyatProksiDlyaStenda func()

	// srokOsvobozhdeniyaDlyaStenda — сколько ждать, что занятое имя адаптера
	// освободится само. Ноль (бой) = tunnel.SrokOsvobozhdeniya; проверки
	// ставят наносекунду, чтобы не ждать по-настоящему: они судят ВЫБОР
	// приложения, а не его терпение.
	srokOsvobozhdeniyaDlyaStenda time.Duration
}

// srokOsvobozhdeniya — см. поле srokOsvobozhdeniyaDlyaStenda.
func (s *Sluzhba) srokOsvobozhdeniya() time.Duration {
	if s.srokOsvobozhdeniyaDlyaStenda != 0 {
		return s.srokOsvobozhdeniyaDlyaStenda
	}
	return tunnel.SrokOsvobozhdeniya
}

// estPrava — единственное место, где сборка конфига спрашивает про права
// администратора (см. поле pravaDlyaStenda).
func (s *Sluzhba) estPrava() bool {
	if s.pravaDlyaStenda != nil {
		return s.pravaDlyaStenda()
	}
	return prava.Est()
}

// rezhimKartiny — режим, под который собран лежащий на диске конфиг ядра.
// Отдельным методом, потому что спрашивают его из-под чужих замков и в разгар
// подъёма защиты: s.kartina меняется каждой пересборкой конфига, и читать её
// поле мимо s.zamok нельзя.
func (s *Sluzhba) rezhimKartiny() konfig.Rezhim {
	s.zamok.Lock()
	defer s.zamok.Unlock()
	return s.kartina.Rezhim
}

// Novaya собирает службу на настоящих путях приложения.
// KELEVRA_PODPISKA и KELEVRA_SHEMA переопределяют сервер подписки, а
// KELEVRA_AVTOREZHIM_DNS — резолвер авторежима (см. avtorezhimDnsAdres) —
// это нужно только для проверки приложения на стенде, у пользователя они не заданы.
func Novaya() (*Sluzhba, error) {
	n, err := hranenie.Zagruzit()
	if err != nil {
		return nil, err
	}
	if err := hranenie.Sohranit(n); err != nil { // закрепляем device_id при первом запуске
		return nil, err
	}
	s := &Sluzhba{
		Nastroyki:          n,
		Yadro:              &yadro.Yadro{Bin: hranenie.PutYadra(), Papka: hranenie.PapkaYadra()},
		Podpiska:           &podpiska.Klient{DeviceID: n.DeviceID, Host: os.Getenv("KELEVRA_PODPISKA"), Shema: os.Getenv("KELEVRA_SHEMA"), Trafik: n.TrafikUstroystva},
		klyuch:             sluchaynyy(),
		avtorezhimDnsAdres: os.Getenv("KELEVRA_AVTOREZHIM_DNS"),
		avtorezhimShlyuz:   os.Getenv("KELEVRA_AVTOREZHIM_SHLYUZ"),
	}
	// Паспорт назначаем ПОСЛЕ сборки: он читает саму службу (режим, права,
	// возраст профиля), поэтому раньше её существования не собирается.
	s.Podpiska.Pasport = s.SobratPasport
	// Профиль мог остаться с прошлого запуска: пересобираем его под нынешние
	// права, чтобы состояние в окне было правдой ещё до первого нажатия.
	_ = s.PerestroitKonfig()
	return s, nil
}

// PerestroitKonfig готовит рабочий конфиг ядра из профиля, который прислал
// сервер: убирает поля, работающие только на телефоне, и вход прокси. Без
// этого ядро на компьютере не стартует вообще.
func (s *Sluzhba) PerestroitKonfig() error { return s.perestroit(konfig.Vybor{}) }

// perestroit принимает отступления от обычной сборки конфига (имя адаптера,
// правила с диска).
func (s *Sluzhba) perestroit(dop konfig.Vybor) error {
	dop.ClashSekret = sekretClash()
	syroy, err := os.ReadFile(hranenie.PutProfilya())
	if err != nil {
		return err
	}
	gotovyy, k, err := konfig.Prigotovit(syroy, dop)
	if err != nil {
		return err
	}
	if err := s.Yadro.ZapisatKonfig(gotovyy); err != nil {
		return err
	}
	s.Yadro.Api, s.Yadro.Sekret = k.ClashAdres, k.ClashSekret
	s.zamok.Lock()
	s.kartina = k
	s.zamok.Unlock()
	// Размер пакета и источник правил — в ту же строку, где режим и права.
	//
	// Без них журнал десктопа за 08.09 не отвечал ни на один вопрос дня:
	// слова «mtu» в нём не было ни разу за всю историю, про наборы правил он
	// не писал ничего вовсе, и разбирать жалобы «сайты по 10 секунд» и
	// «списки не обновляются» было буквально нечем.
	log.Printf("конфиг собран: режим %s, права %v, туннель в профиле %v, Clash API %s%s%s%s",
		k.Rezhim, s.estPrava(), k.EstTunnel, k.ClashAdres, pripiskaMtu(k.MtuTun), pripiskaPravil(k), zametka(k.Zametka))
	return nil
}

// pripiskaMtu — размер пакета туннеля для строки журнала. Нет числа в
// профиле — нет и приписки.
func pripiskaMtu(mtu int) string {
	if mtu == 0 {
		return ""
	}
	return fmt.Sprintf(", размер пакета %d", mtu)
}

// pripiskaPravil — откуда взяты наборы правил. Пусто означает «ядро качает
// сам по профилю», и это тоже надо уметь прочитать в журнале.
func pripiskaPravil(k konfig.Kartina) string {
	if k.PravilaOtkuda == "" {
		return ", правила ядро качает само"
	}
	return fmt.Sprintf(", правила: %d наборов, %s", k.PravilNaborov, k.PravilaOtkuda)
}

// SohranitProfil кладёт присланный профиль на диск и пересобирает конфиг ядра.
func (s *Sluzhba) SohranitProfil(syroy []byte) error {
	if err := os.MkdirAll(hranenie.Papka(), 0o755); err != nil {
		return err
	}
	vremenny := hranenie.PutProfilya() + ".tmp"
	if err := os.WriteFile(vremenny, syroy, 0o600); err != nil {
		return err
	}
	if err := os.Rename(vremenny, hranenie.PutProfilya()); err != nil {
		return err
	}
	return s.PerestroitKonfig()
}

// Adres — на чём слушать: только петля, наружу приложение не смотрит.
func (s *Sluzhba) Slushat() (net.Listener, string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	url := fmt.Sprintf("http://%s/%s/", l.Addr().String(), s.klyuch)
	return l, url, nil
}

// Obsluzhit — маршруты приложения. Всё висит под случайным ключом в пути,
// чтобы чужая программа на этой же машине не дёргала наше API.
func (s *Sluzhba) Obsluzhit() http.Handler {
	m := http.NewServeMux()
	pref := "/" + s.klyuch
	stranicy, _ := fsPodpapki()
	m.Handle(pref+"/", http.StripPrefix(pref+"/", http.FileServer(http.FS(stranicy))))
	m.HandleFunc(pref+"/api/sostoyanie", s.sostoyanie)
	m.HandleFunc(pref+"/api/kod", s.kod)
	m.HandleFunc(pref+"/api/panel", s.panelRuchka)
	m.HandleFunc(pref+"/api/podklyuchit", s.podklyuchit)
	m.HandleFunc(pref+"/api/otklyuchit", s.otklyuchit)
	// «Дать права»: просьба прав и установка службы Windows. Прежние имена
	// ручки (кнопки «Включить для всех программ» и «Работать без
	// подтверждений») ведут туда же — их могут звать окна старых версий.
	m.HandleFunc(pref+"/api/dat_prava", s.datPrava)
	m.HandleFunc(pref+"/api/polnaya_zashchita", s.datPrava)
	m.HandleFunc(pref+"/api/avtozapusk", s.avtozapuskRuchka)
	m.HandleFunc(pref+"/api/avtorezhim", s.avtorezhimRuchka)
	m.HandleFunc(pref+"/api/sluzhba_postavit", s.datPrava)
	m.HandleFunc(pref+"/api/posle_obnovleniya", s.posleObnovleniyaRuchka)
	m.HandleFunc(pref+"/api/ustupit_mesto", s.ustupitMestoRuchka)
	m.HandleFunc(pref+"/api/uzly", s.uzly)
	m.HandleFunc(pref+"/api/vybrat", s.vybrat)
	m.HandleFunc(pref+"/api/zamerit", s.zamerit)
	m.HandleFunc(pref+"/api/zhurnal", s.zhurnal)
	m.HandleFunc(pref+"/api/obnovlenie", s.obnovlenieRuchka)
	m.HandleFunc(pref+"/api/obnovlenie_proverit", s.obnovlenieProveritRuchka)
	m.HandleFunc(pref+"/api/obnovlenie_postavit", s.obnovleniePostavitRuchka)
	return m
}

// srokProverkiObnovleniya — сколько ждём ответа GitHub на нажатие «Проверить
// обновление» в окне. Короче обычного (obnovlenie идёт в фоне при старте) —
// тут человек стоит и смотрит на подпись «Проверяем…».
// var, а не const: тест на таймаут (obnovlenie_test.go) сокращает срок,
// иначе пришлось бы ждать настоящие 6 секунд на каждый прогон.
var srokProverkiObnovleniya = 6 * time.Second

// srokFonovoyProverkiObnovleniya — тот же вопрос GitHub, но фоном (тикер и
// толчок от другой копии): тут никто не стоит и не смотрит, а до
// api.github.com через туннель у части людей ответ идёт около 10 секунд
// (разбор журналов 19.10.2026: шесть секунд кнопки давали в фоне
// «context deadline exceeded» раз за разом). Полминуты — с запасом втрое.
var srokFonovoyProverkiObnovleniya = 30 * time.Second

// srokUstanovkiObnovleniya — сколько ждём скачивание найденной сборки по
// тычку в пузырь (та же величина, что и у холодного обновления в
// cmd/kelevra/obnovlenie.go: srokZagruzki, ~8 МБ по обычной сети укладываются
// с большим запасом).
const srokUstanovkiObnovleniya = 3 * time.Minute

type otvetObnovleniya struct {
	Tekushchaya string `json:"tekushchaya"`
	Novaya      string `json:"novaya,omitempty"`
	Beda        string `json:"beda,omitempty"`
}

// prichinaBedyObnovleniya переводит ошибку obnovlenie.Proverit в русскую
// фразу без жаргона: человек за компьютером не программист, «не
// удалось проверить» на любую беду молчит о причине — нет сети у него дома,
// GitHub лёг или просто долго думает, это три разных «что делать».
func prichinaBedyObnovleniya(err error) string {
	var oshibkaStatusa *obnovlenie.OshibkaStatusa
	if errors.As(err, &oshibkaStatusa) {
		return fmt.Sprintf("GitHub ответил ошибкой %d", oshibkaStatusa.Kod)
	}
	var oshibkaRazbora *obnovlenie.OshibkaRazbora
	if errors.As(err, &oshibkaRazbora) {
		return "GitHub ответил непонятным"
	}
	if errors.Is(err, obnovlenie.ErrPodpis) {
		return "подпись нового выпуска не подтверждена, обновление не предлагается"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("GitHub не ответил за %d секунд", int(srokProverkiObnovleniya/time.Second))
	}
	var setevaya net.Error
	if errors.As(err, &setevaya) && setevaya.Timeout() {
		return fmt.Sprintf("GitHub не ответил за %d секунд", int(srokProverkiObnovleniya/time.Second))
	}
	return "нет интернета, проверить не у кого"
}

// obnovlenieRuchka — «Проверить обновление» в настройках. Обновление и так
// ставится само и молча при каждом запуске (cmd/kelevra/obnovlenie.go), но
// человеку негде спросить прямо сейчас, свежая ли у него версия (эталон
// телефона: SimpleSettingsScreen.kt, пункт «Проверить обновление»).
// Ошибка сети — обычное дело для приложения, которое чинит сеть: отдаём её
// строкой, а не роняем ручку, чтобы окно не повисло (стенд гоняется без
// сети нарочно, см. stend/oblik_snimok.py).
func (s *Sluzhba) obnovlenieRuchka(w http.ResponseWriter, r *http.Request) {
	adres := obnovlenie.SpisokReliza
	if svoy := os.Getenv("KELEVRA_RELIZY"); svoy != "" {
		adres = svoy // стенд
	}
	ctx, otmena := context.WithTimeout(r.Context(), srokProverkiObnovleniya)
	defer otmena()
	o := otvetObnovleniya{Tekushchaya: podpiska.Versiya}
	n, err := obnovlenie.Proverit(ctx, &http.Client{Timeout: srokProverkiObnovleniya}, adres, podpiska.Versiya)
	if err != nil {
		log.Printf("проверка обновления по нажатию: %v", err)
		o.Beda = prichinaBedyObnovleniya(err)
	} else if n != nil {
		o.Novaya = n.Versiya
	}
	otdat(w, o, nil)
}

// obnovlenieProveritRuchka — тихий толчок фоновой проверке от ДРУГОЙ копии
// (cmd/kelevra/main.go: chuzhaya в adresKopii). Человек кликнул значок в
// трее, пока Kelevra уже работала где-то в фоне неделями, — момент не хуже
// холодного старта, чтобы спросить GitHub. Сам ответ HTTP ждать нечего:
// проверка идёт в своей горутине, а ручка отвечает сразу, чтобы открытие
// чужого окна не задержалось на сетевой таймаут.
func (s *Sluzhba) obnovlenieProveritRuchka(w http.ResponseWriter, r *http.Request) {
	go s.ProveritObnovlenieFonom()
	otdat(w, map[string]any{"zapushcheno": true}, nil)
}

// ProveritObnovlenieFonom — тело фоновой проверки, общее для тикера
// (SleditZaObnovleniem) и толчка от другой копии (obnovlenieProveritRuchka).
//
// Не ставит найденную сборку сама (obnovlenie.Postavit тут не звучит): это
// значило бы менять .exe и просить человека на перезапуск прямо у него под
// руками, без предупреждения, посреди рабочего сеанса с поднятой защитой —
// то, что задача запрещает напрямую. Только запоминает находку в
// naydennoeObnovlenie, чтобы её увидел /api/sostoyanie — тем же путём, каким
// человек уже видит остальную правду о приложении (кнопка «Проверить
// обновление» ставит её так же, только сразу в HTTP-ответ и без памяти).
//
// idetProverkaObnovleniya не даёт наложиться двум одновременным проверкам
// (тик и толчок совпали): вторая тихо выходит, не трогая сеть повторно.
//
// Повтора после сетевого отказа у толчка нет: он разовый, а повторяет сам
// тикер (SleditZaObnovleniem через fonovoeDelo).
func (s *Sluzhba) ProveritObnovlenieFonom() {
	if err := s.proveritObnovlenieRaz(context.Background()); setevoyOtkaz(err) {
		log.Printf("фоновая проверка обновления: сеть не ответила (%s), работаю как есть", korotkoOSeti(err))
	}
}

// proveritObnovlenieRaz — одна фоновая проверка. Сетевую ошибку возвращает,
// не печатая: строку о ней пишет тот, кто решает про повтор (fonovoeDelo).
// Остальные отказы (ответ GitHub, подпись) печатает сама и тоже возвращает.
func (s *Sluzhba) proveritObnovlenieRaz(ctx context.Context) error {
	s.zamok.Lock()
	if s.idetProverkaObnovleniya {
		s.zamok.Unlock()
		return nil
	}
	s.idetProverkaObnovleniya = true
	s.zamok.Unlock()
	defer func() {
		s.zamok.Lock()
		s.idetProverkaObnovleniya = false
		s.zamok.Unlock()
	}()

	adres := obnovlenie.SpisokReliza
	if svoy := os.Getenv("KELEVRA_RELIZY"); svoy != "" {
		adres = svoy // стенд
	}
	ctx, otmena := context.WithTimeout(ctx, srokFonovoyProverkiObnovleniya)
	defer otmena()
	n, err := obnovlenie.Proverit(ctx, &http.Client{Timeout: srokFonovoyProverkiObnovleniya}, adres, podpiska.Versiya)
	if err != nil {
		// Нет сети — обычное дело для приложения, которое чинит сеть: тихо,
		// без паники и без падения процесса. Сетевой отказ повторит тикер
		// (fonovoeDelo), остальные ждут следующего тика.
		if !setevoyOtkaz(err) {
			log.Printf("фоновая проверка обновления: не вышло (%v), работаю как есть", err)
		}
		return err
	}
	s.zamok.Lock()
	s.naydennoeObnovlenie = n // может стать снова nil — «версия и так свежая», это тоже правда
	s.zamok.Unlock()
	if s.MetkaObnovleniya != nil {
		// СОСТОЯНИЕ, а не событие: и находку, и её исчезновение (версия и
		// так свежая) значок обязан отразить — см. поле MetkaObnovleniya.
		if n != nil {
			s.MetkaObnovleniya(n.Versiya)
		} else {
			s.MetkaObnovleniya("")
		}
	}
	if n != nil {
		log.Printf("фоновая проверка обновления: найдена версия %s", n.Versiya)
		s.povestitEsliNovaya(n.Versiya)
		s.postavitTiho(*n)
	}
	return nil
}

// SobratPasport — чем устройство описывает себя серверу сверх имени и версии.
//
// Зачем. Разбор 10.09.2026: сервер знал про устройство пять полей и не мог
// ответить ни на «человек сам переключил режим», ни на «какой возраст профиля»,
// ни на «та ли это сборка, что мы выпускали». А половина диагнозов складывается
// ровно из этого, и каждый такой разбор стоил по полсессии.
//
// Формат — «ключ=значение» через точку с запятой (см. ustroystvo.ZagolovokPasporta).
// Новый ключ можно добавить не трогая сервер: незнакомые он кладёт рядом.
//
// Ошибки чтения не мешают: поле просто не попадает в строку. Паспорт — сведения
// о себе, а не работа, и ронять из-за него запрос к серверу нельзя.
func (s *Sluzhba) SobratPasport() string {
	var chasti []string

	if prava.Est() {
		chasti = append(chasti, "prava=1")
	} else {
		chasti = append(chasti, "prava=0")
	}

	s.zamok.Lock()
	avto := s.Nastroyki.Avtorezhim
	ruchnoy := s.Nastroyki.RuchnoyVybor
	s.zamok.Unlock()
	switch {
	case avto:
		chasti = append(chasti, "rezhim=avto")
	case ruchnoy:
		chasti = append(chasti, "rezhim=ruchnoy")
	default:
		chasti = append(chasti, "rezhim=nevybran")
	}

	// Возраст профиля минутами: по нему видно, доезжают ли до человека наши
	// правки правил вообще. Профиль старше суток — уже беда.
	if st, err := os.Stat(hranenie.PutProfilya()); err == nil {
		chasti = append(chasti, fmt.Sprintf("profil=%d", int(time.Since(st.ModTime()).Minutes())))
	}

	if sum := s.otpechatokFayla(hranenie.PutYadra(), &s.summaYadra); sum != "" {
		chasti = append(chasti, "yadro="+sum)
	}
	if put, err := obnovlenie.PutSebya(); err == nil {
		if sum := s.otpechatokFayla(put, &s.summaExe); sum != "" {
			chasti = append(chasti, "exe="+sum)
		}
	}
	return strings.Join(chasti, ";")
}

// otpechatokFayla — короткий отпечаток файла с запоминанием в поле.
//
// Считаем по размеру и времени изменения, а не по содержимому: полный хэш
// десятимегабайтного файла ради строки в заголовке — трата на пустом месте, а
// «тот же файл или подменённый» эта пара показывает не хуже.
func (s *Sluzhba) otpechatokFayla(put string, kesh *string) string {
	s.zamok.Lock()
	gotovo := *kesh
	s.zamok.Unlock()
	if gotovo != "" {
		return gotovo
	}
	st, err := os.Stat(put)
	if err != nil {
		return ""
	}
	sum := fmt.Sprintf("%dk-%d", st.Size()/1024, st.ModTime().Unix())
	s.zamok.Lock()
	*kesh = sum
	s.zamok.Unlock()
	return sum
}

// postavitTiho доводит найденное обновление до конца САМО, не дожидаясь человека.
//
// Зачем. До 10.09.2026 установку запускал только тычок в пузырь трея. Замер по
// реестру устройств на сервере: на двух машинах стояли версии от 5 и от
// 31 августа при живой 0.6.66 — пузырь там не нажимают вовсе, и любая наша правка до
// них не доезжает НИКОГДА. Обновление, которое ждёт щелчка, для семьи равно
// отсутствию обновления. Без этого выпуск для них не имеет смысла вовсе.
//
// Почему это безопасно. Установка меняет файл и поднимает смену тихо (см.
// PostavitNaydennoe и zapustitSmenuPosleObnovleniya): окно не всплывает, связь
// поднимается заново сама, если была поднята. Фоновый тик редкий, поэтому одной
// попытки на версию достаточно, а неудача попадёт в журнал.
func (s *Sluzhba) postavitTiho(n obnovlenie.Novaya) {
	// KELEVRA_BEZ_OBNOVLENIYA=1 — тот же переключатель, что глушит немедленную
	// проверку в SleditZaObnovleniem: на нём стоит масса стендов (stend/*.sh),
	// которым нужна находка (naydennoeObnovlenie уже выставлена выше по
	// ProveritObnovlenieFonom), но НЕ нужна тихая установка в этот момент —
	// иначе ручной тычок (obnovleniePostavitRuchka), который сцена вызывает
	// следом, ставит ПОВТОРНО уже поставленную версию и затирает .old копией
	// новой сборки вместо старой (стенд обнаружил это на obnovlenie_postavit.sh
	// после появления подписи: df682ca8 перестал ронять тихую установку как
	// неподписанную, и гонка стала настоящей). У человека эта переменная
	// не задана — тихая установка идёт как и раньше.
	if os.Getenv("KELEVRA_BEZ_OBNOVLENIYA") == "1" {
		return
	}
	s.zamok.Lock()
	uzhe := s.idetUstanovkaObnovleniya
	stavili := s.tihoStavili
	s.zamok.Unlock()
	if uzhe {
		return
	}
	if stavili == n.Versiya {
		return // эту версию уже пробовали, второй раз незачем
	}
	s.zamok.Lock()
	s.tihoStavili = n.Versiya
	s.zamok.Unlock()

	log.Printf("тихая установка %s: начинаю, человека не спрашиваю", n.Versiya)
	put, err := s.PostavitNaydennoe()
	if err != nil {
		log.Printf("тихая установка %s: не вышло (%v), остаюсь на прежней", n.Versiya, err)
		return
	}
	log.Printf("тихая установка %s: поставлено, файл %s", n.Versiya, put)
}

// povestitEsliNovaya решает, стоит ли беспокоить человека пузырём в трее.
//
// ГОВОРИМ РОВНО ОДИН РАЗ НА ВЕРСИЮ. Тик крутится раз в несколько часов
// (SleditZaObnovleniem) и без этой проверки копия, которая молча висит в
// трее сутками, повторяла бы один и тот же пузырь на каждом тике — хозяин
// продукта 26.08 ругался матом именно на повторяющиеся уведомления.
//
// Отметка хранится на диске (hranenie.Nastroyki.ObyavlennoeObnovlenie), не
// только в памяти процесса: перезапуск НЕ повод сказать заново про версию,
// про которую уже сказали в прошлом запуске — само обновление умеет
// перезапускать себя (cmd/kelevra/obnovlenie.go), а человек может закрыть и
// снова открыть приложение вручную; ни то ни другое не значит, что он забыл
// уже увиденный пузырь. Вышла версия ЕЩЁ новее — строка не совпадёт, и
// объявление придёт снова: каждая новая версия имеет право прозвучать один раз.
// ОДИН ПУТЬ, А НЕ ДВА. С 02.09 про находку говорит либо диалог в окне, либо
// пузырь в трее — и никогда оба разом (жалоба 02.09: «уведомлялка
// некрасивая»). Диалог главный: он подробнее, с кнопкой «ОБНОВИТЬ» и с
// настоящими процентами скачивания, — поэтому при открытом окне пузырь
// молчит вовсе, а трей ограничивается тихой меткой на значке
// (MetkaObnovleniya, зовётся выше по ProveritObnovlenieFonom и от этого
// решения не зависит). Пузырь остаётся ровно для того случая, ради которого
// его и заводили: окно закрыто, копия висит в трее, и сказать больше нечем.
//
// Отметку на диске ставим, когда сказано на деле: окно открыто (говорит
// диалог в нём) или пузырь показан. Иначе человек, закрывший диалог кнопкой
// «ПОЗЖЕ» и следом закрывший окно, получил бы на ближайшем тике ещё и пузырь
// про ту же версию — то самое второе дёрганье об одном и том же.
//
// Пузырь не показан (значок трея ещё не поднят, Windows отказала, хук не
// подключён) — отметку НЕ ставим: раньше она ставилась до показа, и версия,
// о которой человеку так и не сказали, считалась объявленной навсегда.
// Следующий тик попробует снова.
func (s *Sluzhba) povestitEsliNovaya(versiya string) {
	if s.Nastroyki.ObyavlennoeObnovlenie == versiya {
		return
	}
	if s.oknoOtkryto() {
		log.Printf("обновление %s: окно открыто, говорит диалог в окне — пузырь трея молчит", versiya)
		s.otmetitObyavlennoe(versiya)
		return
	}
	if s.OblachkoObnovleniya == nil || !s.OblachkoObnovleniya(versiya) {
		log.Printf("обновление %s: сказать не вышло, отметку не ставлю — скажу на следующей проверке", versiya)
		return
	}
	s.otmetitObyavlennoe(versiya)
}

// otmetitObyavlennoe — версия объявлена человеку; запоминаем на диске.
func (s *Sluzhba) otmetitObyavlennoe(versiya string) {
	s.Nastroyki.ObyavlennoeObnovlenie = versiya
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		log.Printf("не сохранил отметку об объявленном обновлении: %v", err)
	}
}

// srokZhivogoOkna — сколько отметка «окно спрашивало» считается свежей. Окно
// опрашивает состояние раз в 2 секунды (index.html), так что 10 секунд — это
// пять пропущенных опросов подряд: столько подряд живое окно не молчит, а
// закрытое молчит уже навсегда. Короче брать нельзя (разовая заминка службы
// под нагрузкой выглядела бы как закрытое окно), длиннее незачем: всё это
// время закрытое окно ещё считалось бы открытым, и находка молчала бы совсем.
const srokZhivogoOkna = 10 * time.Second

// otmetitZhivoeOkno — открытое окно только что спросило состояние (см. поле
// oknoSprosilo и ручку sostoyanie).
func (s *Sluzhba) otmetitZhivoeOkno() {
	s.zamok.Lock()
	s.oknoSprosilo = time.Now()
	s.zamok.Unlock()
}

// oknoOtkryto — смотрит ли человек прямо сейчас в окно приложения. Нулевая
// отметка (окно не спрашивало ни разу за всю жизнь службы) — честное «нет»:
// так выглядит копия, поднятая автозапуском с Windows и висящая в трее.
func (s *Sluzhba) oknoOtkryto() bool {
	s.zamok.Lock()
	defer s.zamok.Unlock()
	if s.oknoSprosilo.IsZero() {
		return false
	}
	return time.Since(s.oknoSprosilo) < srokZhivogoOkna
}

// obnovleniePostavitRuchka — тычок в пузырь трея (cmd/kelevra/trey_windows.go:
// tychokVPuzyr) либо ручной путь к тому же самому: заказ человека 26.08 —
// «приходит обновление и ты тыкаешь, и всё», а не открывать окно и жать
// «Проверить обновление» самому. Только POST: это действие, а не чтение.
func (s *Sluzhba) obnovleniePostavitRuchka(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		otdat(w, nil, fmt.Errorf("только POST"))
		return
	}
	versiya, err := s.PostavitNaydennoe()
	if err != nil {
		otdat(w, nil, err)
		return
	}
	otdat(w, map[string]any{"gotovo": true, "versiya": versiya}, nil)
}

// PostavitNaydennoe скачивает и ставит НАЙДЕННУЮ фоновой проверкой сборку
// (naydennoeObnovlenie) — в отличие от ProveritObnovlenieFonom, который
// только запоминает находку и никогда сам не качает и не ставит (см. её
// комментарий). Тычок человека — вот то согласие, которого не хватало.
//
// idetUstanovkaObnovleniya под тем же zamok, что и naydennoeObnovlenie: второй
// тычок, пока первый ещё качает, тихо получает «установка уже идёт» вместо
// того, чтобы звать obnovlenie.Postavit второй раз (риск наложения записи —
// см. комментарий Postavit про CreateTemp на процесс).
//
// Любая неудача — ничего не гасим и не трогаем: эта копия продолжает жить
// старой версией, человек может нажать «Проверить обновление» и попробовать
// снова. Успех — ядро НЕ гасим (новая копия его примет), поднимаем смену
// (PerezapuskPosleObnovleniya)
// и завершаем процесс, — с той же паузой ~300мс, чтобы HTTP-ответ успел уйти.
func (s *Sluzhba) PostavitNaydennoe() (string, error) {
	s.zamok.Lock()
	if s.naydennoeObnovlenie == nil {
		s.zamok.Unlock()
		return "", fmt.Errorf("обновления нет: сперва проверка")
	}
	if s.idetUstanovkaObnovleniya {
		s.zamok.Unlock()
		return "", fmt.Errorf("установка уже идёт")
	}
	n := *s.naydennoeObnovlenie
	s.idetUstanovkaObnovleniya = true
	s.skachanoObnovleniya, s.vsegoObnovleniya = 0, 0
	s.zamok.Unlock()
	defer func() {
		s.zamok.Lock()
		s.idetUstanovkaObnovleniya = false
		// Ход гасим вместе с замком — на ЛЮБОМ исходе, включая обрыв
		// посреди скачивания: доля пережившей себя загрузки — враньё.
		s.skachanoObnovleniya, s.vsegoObnovleniya = 0, 0
		s.zamok.Unlock()
	}()

	put, err := obnovlenie.PutSebya()
	if err != nil {
		log.Printf("установка обновления по тычку: не знаю, где лежу: %v", err)
		return "", fmt.Errorf("не знаю, где лежу: %w", err)
	}
	ctx, otmena := context.WithTimeout(context.Background(), srokUstanovkiObnovleniya)
	defer otmena()
	// Доля скачанного — единственное, что окно может показать человеку, пока
	// идёт установка: сама ручка отвечает ОДИН раз и уже в конце, а окно
	// тем временем опрашивает /api/sostoyanie раз в 2 секунды. До 02.09 там
	// не было ничего, кроме неопределённой полосы, — на телефоне же в этом
	// месте стоят настоящие проценты.
	hod := func(skachano, vsego int64) {
		s.zamok.Lock()
		s.skachanoObnovleniya, s.vsegoObnovleniya = skachano, vsego
		s.zamok.Unlock()
	}
	if err := obnovlenie.PostavitSHodom(ctx, &http.Client{Timeout: srokUstanovkiObnovleniya}, n, put, hod); err != nil {
		log.Printf("установка обновления по тычку: не вышло (%v), работаю старой версией", err)
		return "", fmt.Errorf("не поставилось: %w", err)
	}
	log.Printf("установка обновления по тычку: версия %s встала на диск", n.Versiya)

	pid := os.Getpid()
	go func() {
		time.Sleep(300 * time.Millisecond) // дать HTTP-ответу уйти в окно/пузырь
		if s.PerezapuskPosleObnovleniya == nil {
			// Не гасим себя: без хука новую копию поднять некому, и человек
			// остался бы вообще без работающей защиты. Файл на диске уже
			// новый — свежая версия встанет со следующего обычного запуска.
			log.Printf("установка обновления: хук перезапуска не подключён, эта копия продолжает работать старым процессом")
			return
		}
		// ЯДРО НЕ ГАСИМ. До 11.09.2026 здесь стоял Ostanovit — «ядро старой
		// копии гасим сами», чтобы на машине не оказалось двух хозяев одного
		// адаптера (авария 25.08). Цена этой предосторожности, замеренная на
		// стенде 11.09: старая копия ушла в 15:56:49, новая поднялась в
		// 15:56:57 — восемь секунд без туннеля, причём НЕЗАМЕТНЫХ, потому что
		// связь не пропадала, трафик просто шёл напрямую мимо обхода. А после
		// обновления туннель не возвращался вовсе: автоподключение у людей
		// выключено, авторежим дома считает туннель ненужным.
		//
		// Двух хозяев теперь не бывает по другой причине, надёжнее: новая
		// копия ПРИНИМАЕТ это самое ядро по записке (yadro.Peredacha) вместо
		// того, чтобы поднимать своё. Второго ядра не появляется, а туннель
		// не прерывается ни на пакет — адаптер принадлежит ядру, и пока живо
		// ядро, жив и он.
		//
		// Записка уже на диске: её пишет Zapustit, когда ядро ответило.
		if err := s.PerezapuskPosleObnovleniya(put, pid); err != nil {
			log.Printf("установка обновления: не поднял новую копию: %v", err)
			return
		}
		if s.vyhod != nil {
			s.vyhod()
			return
		}
		os.Exit(0)
	}()
	return n.Versiya, nil
}

// ZadatVyhod меняет способ, которым копия уходит после того, как поставила
// обновление или уступила место новой копии.
//
// По умолчанию это os.Exit(0) — обычному процессу больше ничего и не нужно.
// Службе Windows нужно другое: уйти так, чтобы диспетчер служб счёл это
// отказом и поднял её заново уже из нового файла. Своего кода выхода у службы
// тут нет, поэтому решение отдаётся наружу, в cmd/kelevra, где про режим
// запуска известно всё.
func (s *Sluzhba) ZadatVyhod(f func()) { s.vyhod = f }

// SleditZaObnovleniem крутит ProveritObnovlenieFonom по расписанию, пока
// живёт служба (тот же ctx, что и ObnovlyatProfil — оба гасит один и тот же
// defer otmena() в zapustitSluzhbu).
//
// Первая проверка — СРАЗУ при старте, до входа в цикл тикера (заказ 26.08:
// обновление приходит само, а ставит его человек нажатием — раньше
// единственным моментом узнать о новой версии был холодный старт, где
// cmd/kelevra/obnovlenie.go молча ставил её сам; теперь установка требует
// тычка человека, а найти обновление служба обязана сама и сразу, не через
// period часов — иначе «приходит само» не выполняется, просто откладывается
// на 4 часа). Отменённый ДО первого вызова ctx уважаем и в сеть не ходим
// вовсе. KELEVRA_BEZ_OBNOVLENIYA=1 — тот же переключатель, что раньше глушил
// obnovitsya() на холодном старте, — глушит и эту немедленную проверку: на
// нём стоит масса стендов (stend/*.sh), которым сеть тут не нужна и не
// должна понадобиться неожиданно. Сам тикер эта переменная не трогает, как и
// раньше.
// period внедряемый (не голая obnovlenie.PeriodFonovoyProverki внутри
// функции): стенд гоняет его в миллисекундах, а не ждёт настоящих часов.
//
// Сетевой отказ (машина только что проснулась, Wi-Fi ещё не поднят)
// повторяется сам с нарастающей паузой, а тик после сна сперва ждёт сеть —
// см. fonovoeDelo в povtor_seti.go.
func (s *Sluzhba) SleditZaObnovleniem(ctx context.Context, period time.Duration) {
	d := s.delo("фоновая проверка обновления", period, s.proveritObnovlenieRaz)
	d.srazu = os.Getenv("KELEVRA_BEZ_OBNOVLENIYA") != "1"
	d.krutit(ctx)
}

// VecherOtpravkiZhurnalov — во сколько по МЕСТНОМУ времени клиент отдаёт свои
// журналы разработчику. Конец дня выбран не случайно: за день уже случилось
// всё, что случится, машина ещё не выключена, а канал человеку в этот час не
// нужен. var, а не const — стенд двигает время, чтобы не ждать 23:30.
var VecherOtpravkiZhurnalov = 23*time.Hour + 30*time.Minute

// ShagSlezhkiZaZhurnalami — как часто тикер смотрит на часы. Само расписание
// суточное; шаг нужен лишь чтобы не проспать вечер, если машина проснулась из
// сна в 23:31, и чтобы повтор после отказа случился в ближайший подходящий
// час, а не ровно через сутки.
var ShagSlezhkiZaZhurnalami = 5 * time.Minute

// PovtorPosleOtkaza — не чаще раза в час. Сервер мог лежать, канал мог
// пропасть; долбиться в него каждые пять минут с посылкой на десятки
// мегабайт — это не настойчивость, а трата чужого трафика.
const PovtorPosleOtkaza = time.Hour

// PovtorPosleOtkazaDoPeredachi — для отказов, случившихся ДО того, как ушёл
// хоть один байт тела: имя не разрешилось, соединение не установилось.
//
// Зачем отдельно от PovtorPosleOtkaza. Час молчания там оплачен трафиком:
// посылка в десятки мегабайт могла оборваться на середине, и повторять её
// сразу — это швырять чужой канал. Но отказ на разрешении имени не стоит
// ничего и почти всегда означает одно: машина только что проснулась, а сеть
// ещё не поднялась. Замер 11.09.2026 на десктопе владельца: компьютер вышел из сна
// в 13:28:03, тик отправки пришёлся на 13:28:00 — за три секунды ДО того, как
// система доложила о пробуждении, — и упал с «lookup … no such host». По
// старому правилу следующая попытка была только в 14:31, и всё это время
// устройство числилось молчащим, хотя было включено и здорово.
//
// Значение равно шагу тикера: повтор случается на ближайшем тике, а не через
// час, и при этом всё равно не чаще, чем тикер вообще просыпается.
const PovtorPosleOtkazaDoPeredachi = 3 * time.Minute

// PeriodOtpravkiZhurnalov — как часто посылка уходит при удачах.
//
// var, а не const: стенду нужно двигать период, чтобы не ждать час.
var PeriodOtpravkiZhurnalov = time.Hour

// vecherOtpravki — момент «конца дня» для суток, в которые попадает t.
func vecherOtpravki(t time.Time) time.Time {
	den := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return den.Add(VecherOtpravkiZhurnalov)
}

// poraOtpravlyatZhurnaly — всё расписание одной чистой функцией, чтобы оно
// проверялось таблицей случаев, а не ожиданием настоящего часа.
//
// Правило: раз в час, считая от последней УДАЧНОЙ отправки. Отказ не ускоряет
// и не замедляет: он лишь запрещает лезть снова раньше PovtorPosleOtkaza.
//
// Почему больше не «раз в вечер». До 10.09.2026 посылка уходила по вечернему
// расписанию, и это разбилось об очевидное: ноутбуки вечером выключены, и у
// половины машин журнал не уезжал вовсе. Час — это ещё и то, что обещано планом
// телеметрии: сводки едут часто и мелко, и к моменту жалобы данные уже здесь,
// а не ждут ночи, которая может не наступить.
//
// Дорого это не стоит: отправка ведёт отметки по каждому файлу и посылает
// ТОЛЬКО новое (internal/zhurnaly, Metka.Otpravleno). Часовая посылка — это
// килобайты, а не те десятки мегабайт, которыми пугало суточное накопление.
// povtorCherez — сколько ждать после последней НЕУДАЧНОЙ попытки. Параметром,
// а не константой внутри: цена отказа разная. Оборвавшаяся на середине посылка
// стоила трафика и ждёт час (PovtorPosleOtkaza), а отказ до первого байта не
// стоил ничего и ждёт до ближайшего тика (PovtorPosleOtkazaDoPeredachi).
func poraOtpravlyatZhurnaly(seychas, uspeh, popytka time.Time, povtorCherez time.Duration) bool {
	if !uspeh.IsZero() && seychas.Sub(uspeh) < PeriodOtpravkiZhurnalov {
		return false // час с удачной посылки ещё не прошёл
	}
	if povtorCherez <= 0 {
		povtorCherez = PovtorPosleOtkaza
	}
	if !popytka.IsZero() && seychas.Sub(popytka) < povtorCherez {
		return false // недавно пробовали и не вышло — ждём положенное
	}
	return true
}

// otkazDoPeredachi — отказ случился до того, как ушёл хоть один байт тела.
//
// Разбор по типам ошибок, а не по тексту: текст у net-ошибок разный на разных
// системах и языках, а «не разрешилось имя» и «не установилось соединение» —
// это ровно *net.DNSError и *net.OpError с Op == "dial".
func otkazDoPeredachi(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return true
	}
	return false
}

// SleditZaZhurnalami крутит суточную отправку по расписанию, пока живёт
// служба — тем же тикером и тем же ctx, что SleditZaObnovleniem выше.
//
// Первой проверки «сразу при старте», в отличие от обновлений, тут нет
// сознательно: обновление человек ждёт, а посылка журналов — фоновая вещь,
// которой незачем занимать канал в ту же секунду, когда он открыл приложение.
// KELEVRA_BEZ_OTPRAVKI_ZHURNALOV=1 глушит слежку целиком — на нём стоят
// стенды (stend/*.sh), которым сеть тут не нужна и не должна понадобиться.
func (s *Sluzhba) SleditZaZhurnalami(ctx context.Context, shag time.Duration) {
	if os.Getenv("KELEVRA_BEZ_OTPRAVKI_ZHURNALOV") == "1" {
		return
	}
	if shag <= 0 {
		shag = ShagSlezhkiZaZhurnalami
	}
	t := time.NewTicker(shag)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.OtpravitZhurnalyEsliPora(ctx, time.Now())
		}
	}
}

// OtpravitZhurnalyEsliPora — один тик расписания. seychas параметром, а не
// time.Now() внутри: расписание так проверяется без ожидания вечера.
func (s *Sluzhba) OtpravitZhurnalyEsliPora(ctx context.Context, seychas time.Time) {
	// Тумблера «отправлять или нет» здесь БОЛЬШЕ НЕТ (был до 01.09, вместе с
	// hranenie.Nastroyki.OtpravlyatZhurnaly). Жалоба: зачем-то есть кнопка не
	// отправлять данные разработчику, хотя об этом уже говорили — тем же днём
	// выбор убрали и на телефоне. Отправка безусловна; единственное, что её
	// сдерживает, — расписание (poraOtpravlyatZhurnaly).
	var uspeh time.Time
	if u := s.Nastroyki.KogdaOtpravlyaliZhurnaly(); u > 0 {
		uspeh = time.Unix(u, 0)
	}
	s.zamok.Lock()
	if s.zhurnalyIdut || !poraOtpravlyatZhurnaly(seychas, uspeh, s.zhurnalyPopytka, s.zhurnalyPovtorCherez) {
		s.zamok.Unlock()
		return
	}
	s.zhurnalyIdut = true
	s.zhurnalyPopytka = seychas
	otpravshchik := s.otpravshchikZhurnalovDlyaStenda
	s.zamok.Unlock()
	defer func() {
		s.zamok.Lock()
		s.zhurnalyIdut = false
		s.zamok.Unlock()
	}()
	if otpravshchik == nil {
		otpravshchik = s.otpravshchikZhurnalov()
	}
	otchet, err := otpravshchik.Otpravit(ctx)
	if err != nil {
		// Не удалось — отметки на диске не тронуты, те же байты уйдут в
		// следующую попытку. Когда именно, решает цена отказа: до первого
		// отправленного байта ждать час незачем (машина обычно просто ещё не
		// подняла сеть после сна), а оборвавшаяся посылка ждёт как прежде.
		povtor := PovtorPosleOtkaza
		if otkazDoPeredachi(err) {
			povtor = PovtorPosleOtkazaDoPeredachi
		}
		s.zamok.Lock()
		s.zhurnalyPovtorCherez = povtor
		s.zamok.Unlock()
		log.Printf("отправка журналов не удалась: %v (повтор не раньше чем через %s)", err, povtor)
		return
	}
	// Удача снимает укороченный повтор: следующий отказ будет судиться заново.
	s.zamok.Lock()
	s.zhurnalyPovtorCherez = 0
	s.zamok.Unlock()
	if len(otchet.Kuski) == 0 {
		log.Printf("отправка журналов: нового с прошлого раза нет")
	} else {
		log.Printf("журналы отправлены: %d файлов, %d байт (сжато %d), сервер принял %d",
			len(otchet.Kuski), otchet.SyrykhBayt, otchet.SzhatoBayt, otchet.OtvetBayt)
	}
	// «Нечего слать» — тоже отчёт за этот вечер: без отметки тикер вернулся бы
	// сюда через час и ходил бы впустую до самой ночи.
	s.Nastroyki.OtmetitOtpravkuZhurnalov(seychas.Unix())
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		log.Printf("не сохранил отметку об отправке журналов: %v", err)
	}
}

// otpravshchikZhurnalov собирает боевой отправщик на настоящих путях.
//
// Адрес складывается из тех же схемы и хоста, что у подписки: KELEVRA_PODPISKA
// на стенде уводит и подписку, и журналы на подставной сервер разом — иначе
// стенд отправлял бы свои выдуманные логи в живой коллектор.
func (s *Sluzhba) otpravshchikZhurnalov() *zhurnaly.Otpravshchik {
	shema := s.Podpiska.Shema
	if shema == "" {
		shema = "https"
	}
	host := s.Podpiska.Host
	if host == "" {
		// Подписка по своему адресу (своя сеть) — журналы туда же, где выдача человека: её
		// журналы видит мозг. Иначе — прежний сервер.
		if u, err := url.Parse(s.Nastroyki.Podpiska); err == nil && u.Host != "" {
			host = u.Host
		} else {
			host = podpiska.Host
		}
	}
	return &zhurnaly.Otpravshchik{
		Adres:    fmt.Sprintf("%s://%s/logs", shema, host),
		DeviceID: s.Nastroyki.DeviceID,
		Versiya:  podpiska.Versiya,
		Puti: zhurnaly.Istochniki(
			hranenie.PutZhurnala(),
			hranenie.ZapasnayaPapkaZhurnala(),
			yadro.PutZhurnalaVPapke(hranenie.PapkaYadra()),
			filepath.Join(hranenie.Papka(), zapisi.ImyaFayla),
		),
		PutMetok:  hranenie.PutOtmetokZhurnalov(),
		Zagolovki: ustroystvo.Zagolovki,
	}
}

// zhurnal отдаёт хвост журнала прямо в окно.
//
// Иначе единственный способ прислать причину — идти в %LOCALAPPDATA% через
// проводник, а человек, у которого «не работает», этого делать не будет.
// Отдаём текстом, чтобы его можно было выделить и скопировать целиком.
func (s *Sluzhba) zhurnal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	b, err := os.ReadFile(hranenie.PutZhurnala())
	if err != nil {
		fmt.Fprintf(w, "журнал не читается: %v", err)
		return
	}
	const predel = 40 * 1024 // больше в окно всё равно не влезет
	if len(b) > predel {
		b = b[len(b)-predel:]
	}
	_, _ = w.Write(b)
}

// uzly — какие группы выходов есть у ядра и что в них выбрано.
//
// Пока ядро работает, спрашиваем сам Clash API — это правда с точностью до
// секунды. Пока оно стоит, спросить некого, но список узлов — не тайна: он
// часть конфига, который уже лежит на диске (PerestroitKonfig пишет его при
// каждом сохранении кода доступа, до всякого «Подключить»). Раньше в этом
// состоянии окно отдавало пустой список и человек видел 300px пустоты вместо
// списка (снимок 21.08) — здесь показываем ту же конфигурацию, только
// без задержек: их взять неоткуда без живого ядра.
func (s *Sluzhba) uzly(w http.ResponseWriter, r *http.Request) {
	if s.Yadro.Sost() == yadro.Rabotaet {
		g, err := s.Yadro.Gruppy()
		if err != nil {
			otdat(w, nil, err)
			return
		}
		otdat(w, map[string]any{"gruppy": g}, nil)
		return
	}
	syroy, err := os.ReadFile(s.Yadro.PutKonfiga())
	if err != nil {
		otdat(w, map[string]any{"gruppy": []any{}}, nil) // код ещё не введён — конфига нет
		return
	}
	g, err := yadro.GruppyStatik(syroy, s.Nastroyki.Uzly)
	if err != nil {
		otdat(w, map[string]any{"gruppy": []any{}}, nil) // конфиг битый — не валим окно из-за списка узлов
		return
	}
	otdat(w, map[string]any{"gruppy": g}, nil)
}

func (s *Sluzhba) vybrat(w http.ResponseWriter, r *http.Request) {
	var vhod struct {
		Gruppa string `json:"gruppa"`
		Uzel   string `json:"uzel"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&vhod); err != nil {
		otdat(w, nil, fmt.Errorf("не разобрал запрос"))
		return
	}
	if vhod.Gruppa == "" || vhod.Uzel == "" {
		otdat(w, nil, fmt.Errorf("не сказано, что и на что переключить"))
		return
	}
	var err error
	// Ядро работает — переключаем прямо сейчас через Clash API. Ядро стоит —
	// переключать пока нечего, но выбор всё равно обязан запомниться: человек
	// выбирает узел ДО «Подключить», а не только когда защита уже включена.
	// podklyuchit() применит сохранённое, как только ядро поднимется.
	if s.Yadro.Sost() == yadro.Rabotaet {
		err = s.Yadro.Vybrat(vhod.Gruppa, vhod.Uzel)
	}
	if err == nil {
		s.zapomnitUzel(vhod.Gruppa, vhod.Uzel)
	}
	otdat(w, map[string]any{"gotovo": true}, err)
}

// zapomnitUzel сохраняет выбор человека на диске — единое хранилище что для
// «выбрал до подключения», что для «выбрал во время работы» (второе тоже
// стоит запомнить: свежий выбор обязан пережить следующий холодный старт,
// даже если cache_file ядра почему-то не поднимется).
func (s *Sluzhba) zapomnitUzel(gruppa, uzel string) {
	if s.Nastroyki.Uzly == nil {
		s.Nastroyki.Uzly = map[string]string{}
	}
	s.Nastroyki.Uzly[gruppa] = uzel
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		log.Printf("не сохранил выбор узла: %v", err)
	}
}

// VestiOcheredUzlov — переход по очереди узлов своей сети (пакет ochered), пока ядро работает.
// Очередь берётся из конфига ядра и пересобирается, когда профиль обновился. Профиль с одним
// узлом (у семьи на старом сервере) очереди не имеет — тогда цикл только смотрит на конфиг.
func (s *Sluzhba) VestiOcheredUzlov(ctx context.Context) {
	var o *ochered.Ochered
	var bylKonfig []byte
	t := time.NewTicker(ochered.ShagMezhduZamerami)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if s.Yadro == nil || s.Yadro.Sost() != yadro.Rabotaet {
			continue
		}
		syroy, err := os.ReadFile(s.Yadro.PutKonfiga())
		if err != nil {
			continue
		}
		if !bytes.Equal(syroy, bylKonfig) {
			bylKonfig = syroy
			o = nil
			if gruppa, uzly, ok := ochered.IzKonfiga(syroy); ok {
				pervyy := uzly[0]
				o = &ochered.Ochered{Gruppa: gruppa, Uzly: uzly, Ya: yadroDlyaOcheredi{s.Yadro}, Log: log.Printf,
					// Человек выбрал в окне не первый узел — его выбор, очередь не вмешивается.
					Ruchnoy: func() bool { v := s.Nastroyki.Uzly[gruppa]; return v != "" && v != pervyy }}
				log.Printf("очередь узлов: %q, узлы %v", gruppa, uzly)
			}
		}
		if o == nil {
			continue
		}
		shctx, otmena := context.WithTimeout(ctx, 45*time.Second)
		o.Shag(shctx)
		otmena()
	}
}

// yadroDlyaOcheredi — ядро для очереди узлов с её сроками: замер 6 с (холодный узел через дом и
// маскировку не всегда укладывается в 3 с окна — на стенде 17.10 оба узла разом «не ответили»),
// переключение — до 15 с (ядро рвёт живые соединения и отвечает позже).
type yadroDlyaOcheredi struct{ y *yadro.Yadro }

func (a yadroDlyaOcheredi) Vybrano(g string) (string, error) { return a.y.Vybrano(g) }
func (a yadroDlyaOcheredi) Vybrat(g, u string) error         { return a.y.VybratZa(g, u, 15*time.Second) }
func (a yadroDlyaOcheredi) Zamerit(ctx context.Context, u string) (int, error) {
	return a.y.ZameritZa(ctx, u, 6*time.Second)
}

// primenitSohranennyeUzly переносит выбор узла, сделанный раньше (в том числе
// до самого первого подключения), на только что поднятое ядро: у него своя
// Clash API появляется именно сейчас, а не раньше. Ошибка одной группы — не
// повод рушить подключение целиком: профиль мог обновиться, и сохранённое имя
// узла — устареть.
func (s *Sluzhba) primenitSohranennyeUzly() {
	for gruppa, uzel := range s.Nastroyki.Uzly {
		if err := s.Yadro.Vybrat(gruppa, uzel); err != nil {
			log.Printf("не применил сохранённый узел %q для группы %q: %v", uzel, gruppa, err)
		}
	}
}

// zamerit гоняет пробу через каждый узел группы. Ошибка одного узла — это его
// ответ, а не отказ замера: узел, который не отвечает, человеку тоже надо видеть.
func (s *Sluzhba) zamerit(w http.ResponseWriter, r *http.Request) {
	var vhod struct {
		Uzly []string `json:"uzly"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&vhod); err != nil {
		otdat(w, nil, fmt.Errorf("не разобрал запрос"))
		return
	}
	if len(vhod.Uzly) > 32 {
		vhod.Uzly = vhod.Uzly[:32]
	}
	ctx, otmena := context.WithTimeout(r.Context(), 20*time.Second)
	defer otmena()
	itog := make([]map[string]any, len(vhod.Uzly))
	var gr sync.WaitGroup
	for i, u := range vhod.Uzly {
		gr.Add(1)
		go func(i int, u string) {
			defer gr.Done()
			ms, err := s.Yadro.Zamerit(ctx, u)
			z := map[string]any{"imya": u, "zaderzhka": ms}
			if err != nil {
				z["beda"] = err.Error()
			}
			itog[i] = z
		}(i, u)
	}
	gr.Wait()
	otdat(w, map[string]any{"zamer": itog}, nil)
}

type otvetSostoyaniya struct {
	// Versiya — обновляется приложение само, и версия обязана быть видна
	// человеку (правка 20.08). До 23.08 её печатала шапка; на телефоне
	// значка версии в шапке нет вовсе (эталон SimpleSettingsScreen.kt),
	// поэтому версия переехала в подвал вкладки «Настройки» (index.html).
	Versiya    string `json:"versiya"`
	Sost       string `json:"sost"`
	KodEst     bool   `json:"kod_est"`
	YadroEst   bool   `json:"yadro_est"`
	KachaemBin bool   `json:"kachaem_yadro"`
	Beda       string `json:"beda,omitempty"`
	VverhBayt  int64  `json:"vverh_bayt"`
	VnizBayt   int64  `json:"vniz_bayt"`
	Imya       string `json:"imya,omitempty"`
	DoUnix     int64  `json:"do_unix,omitempty"`
	PID        string `json:"pid,omitempty"`
	Rezhim     string `json:"rezhim,omitempty"`  // tunnel (другого режима нет)
	Zametka    string `json:"zametka,omitempty"` // что сейчас идёт через Kelevra
	Prava      bool   `json:"prava"`             // запущены ли мы администратором
	SluzhbaEst bool   `json:"sluzhba_est"`       // установлена ли служба Windows
	// NuzhnyPrava — у этой копии нет прав поднять туннель (служба Windows не
	// установлена, человек отказал в запросе прав). Окно рисует «Не
	// подключено», строку OshibkaNetPrav и кнопку «Дать права».
	NuzhnyPrava bool `json:"nuzhny_prava"`
	// Otkaz — почему последняя попытка не подняла связь, словами человека
	// (sluzhba.Oshibka*). Пусто — сказать нечего.
	Otkaz string `json:"otkaz,omitempty"`
	// PosleObnovleniya — окно открыто сразу после установки новой версии.
	// Окно по нему один раз показывает, что изменилось и что делать дальше:
	// тихая подсказка в значке этого не делает — у второго человека семьи
	// она провисела неделю, а версия так и осталась старой (замер 08.09).
	// Значение — номер свежей версии, чтобы окно могло её назвать.
	PosleObnovleniya string `json:"posle_obnovleniya,omitempty"`
	// SvezhihPravil — сколько наборов правил уже скачано на диск и ждёт
	// следующего подключения, чтобы попасть в работу. Ядро читает наборы
	// только при старте, и без этого поля обновление правил остаётся
	// невидимым: файлы поменялись, а человеку это никак не показано.
	SvezhihPravil int `json:"svezhih_pravil,omitempty"`
	// Автозапуск с Windows. Podderzhivaetsya — ложь на не-Windows сборке (тумблер
	// там нечестно показывать хоть включённым, хоть выключенным: он ничего не
	// делает). Ustarela — запись есть, но ведёт на другой .exe (переустановка в
	// другую папку) — человеку это тоже нужно видеть, а не молчаливое «выключено».
	AvtozapuskPodderzhivaetsya bool   `json:"avtozapusk_podderzhivaetsya"`
	AvtozapuskVklyuchen        bool   `json:"avtozapusk_vklyuchen,omitempty"`
	AvtozapuskUstarela         bool   `json:"avtozapusk_ustarela,omitempty"`
	AvtozapuskBeda             string `json:"avtozapusk_beda,omitempty"`
	// Avtorezhim — переключение защиты по смене сети (дома/не дома).
	// Obstanovka пустая, пока служитель не крутится (Vklyuchen == false).
	AvtorezhimVklyuchen  bool   `json:"avtorezhim_vklyuchen"`
	AvtorezhimObstanovka string `json:"avtorezhim_obstanovka,omitempty"`
	// AvtorezhimSlepPrichina — почему авторежим PodryadDoPrichiny заходов
	// подряд не может понять, дома ли ноутбук (см. avtorezhim.Avtorezhim.
	// PrichinaSlepoty). Пусто — либо слепоты нет, либо она ещё не длится
	// достаточно, чтобы тревожить человека.
	AvtorezhimSlepPrichina string `json:"avtorezhim_slep_prichina,omitempty"`
	// OzhidanieDoma — авторежим включён, обстановка «дома» и защита осознанно
	// не поднята (см. podklyuchit и avtorezhimKolbek): человек за своим
	// роутером, обход блокировок уже делает он, а автомат сам поднимет
	// защиту, как только уйдёт из дома. Круг в окне (index.html) знает
	// только rabotaet/podnimaem/slomalos из ядра — без этого поля «дома» и
	// «выключено вручную» неотличимы, и подсказка врёт «нажмите, чтобы
	// включить» на то, что человек уже нажал (правка 27.08).
	OzhidanieDoma bool `json:"ozhidanie_doma"`
	// AvtorezhimRuchnoy — человек САМ отказался от автомата (выключил тумблер
	// или нажал «Отключить» при работающем автомате), и клиент больше ничего
	// за него не решает, пока тот не вернёт тумблер. Отличается от простого
	// !AvtorezhimVklyuchen тем, что различает «ещё не выбирал» (кнопка
	// «Подключиться» включит автомат сама) и «выбрал руками» (не включит).
	AvtorezhimRuchnoy bool `json:"avtorezhim_ruchnoy,omitempty"`
	// AvtorezhimPolozhenie — вся схема одной строкой для окна: «дома — режим
	// ожидания», «вне дома — защита включена», «ухожу из дома — поднимаю
	// защиту» и т.д. Заведено по требованию (27.08, повторено 28.08 и 29.08),
	// чтобы переход был ЗАМЕТЕН человеку, а не угадывался по цвету круга: окно
	// склеивало бы эту строку из четырёх полей само и врало бы на переходах,
	// потому что порядок их обновления знает только служба.
	// Пусто — сказать нечего (обстановка неизвестна и автомат не крутится).
	AvtorezhimPolozhenie string `json:"avtorezhim_polozhenie,omitempty"`
	// VyhodAvto и VyhodImya — что окно пишет в строке выбора выхода. Жалоба
	// 27.08: россыпь конкретных узлов на главном экране не нужна, нужен
	// простой выбор авторежима. Пока человек в список не лазил,
	// выход ровно один — «Автоматически» (VyhodAvto == true), а конкретные
	// узлы окно показывает, только если человек полез глубже: их отдаёт
	// отдельная ручка /api/uzly, и грузить ими главный экран незачем.
	VyhodAvto bool   `json:"vyhod_avto"`
	VyhodImya string `json:"vyhod_imya,omitempty"`
	// NovayaVersiyaDostupna — находка ФОНОВОЙ проверки (SleditZaObnovleniem,
	// obnovlenieProveritRuchka), а не ручного нажатия кнопки «Проверить
	// обновление» — та отвечает своим отдельным otvetObnovleniya.Novaya.
	// Пусто — фон ничего не нашёл или ещё не спрашивал.
	NovayaVersiyaDostupna string `json:"novaya_versiya_dostupna,omitempty"`
	// ObnovlenieDolya — сколько уже скачано от всей новой сборки, 0..1. Поля
	// НЕТ вовсе (omitempty), когда установка не идёт или сервер не назвал
	// длины ответа: окно тогда оставляет неопределённую полосу и пишет
	// «Скачиваю…» без процента. Выдуманный процент хуже честного «идёт» —
	// поэтому доля и считается от ContentLength живого соединения
	// (obnovlenie.Hod), а не от размера релиза из списка GitHub.
	ObnovlenieDolya float64 `json:"obnovlenie_dolya,omitempty"`
	// PravaUzheSprosheny — приложение уже (само или кнопкой) спрашивало права
	// администратора хоть раз, отдельно от Prava (есть ли они СЕЙЧАС): вместе
	// эти два поля различают «ещё не спрашивали» и «спрашивали и отказали».
	PravaUzheSprosheny bool `json:"prava_uzhe_sprosheny"`
	// ChelovekImya и UstroystvoImya — как сервер называет ВЛАДЕЛЬЦА ключа и
	// ИМЕННО ЭТУ машину (/info, поля person.name и device.name; вторую он
	// узнаёт по заголовкам устройства, см. internal/ustroystvo). Пусто —
	// сервер старый и таких полей не шлёт: окно тогда просто не рисует
	// строку, пустоты на её месте не остаётся.
	ChelovekImya   string `json:"chelovek_imya,omitempty"`
	UstroystvoImya string `json:"ustroystvo_imya,omitempty"`
	// Остальное из /info — для вкладки «Подписка». Состав и порядок сняты со
	// шторки подписки на телефоне (HomeScreen.kt 464-514): состояние
	// (Активна/Приостановлена), под ним одной строкой срок и трафик, ниже
	// «кто пользуется» и «устройство». Считать эту строку в окне нечем без
	// самих чисел, поэтому сюда едут они, а не готовый текст: как именно
	// склеивать «до 12 сентября · 12 из 50 ГБ» — дело облика, и на телефоне
	// это тоже делает экран (Subscription.kt: note), а не сервер.
	//
	// PodpiskaEst отличает «сервер ещё не отвечал» от «ответил и подписка
	// приостановлена»: без него окно на свежем запуске рисовало бы
	// «Приостановлена» по нулевому Aktivna — то есть врало бы.
	PodpiskaEst bool `json:"podpiska_est"`
	// Panel — машина админа своей сети: окно показывает кнопку «Панель сети».
	Panel               bool  `json:"panel,omitempty"`
	PodpiskaAktivna     bool  `json:"podpiska_aktivna,omitempty"`
	PodpiskaLimitBayt   int64 `json:"podpiska_limit_bayt,omitempty"`
	PodpiskaSyedenoBayt int64 `json:"podpiska_syedeno_bayt,omitempty"`
	// KodMaska — код доступа звёздочками и последними двумя знаками
	// (podpiska.Maska). Самого кода в ответе НЕТ и быть не должно: окно не
	// заперто ничем, кроме адреса, а снимок экрана с открытым ключом человек
	// шлёт в поддержку не задумываясь.
	KodMaska string `json:"kod_maska,omitempty"`
	// ZhurnalyOtpravkaKogda — unix последней удавшейся суточной отправки
	// журналов разработчику, 0 — не отправляли ни разу. Соседнего поля
	// «включена» тут больше нет: выбор убран 01.09, отправка безусловна, и
	// окно показывает эту дату справкой, а не подписью под тумблером.
	ZhurnalyOtpravkaKogda int64 `json:"zhurnaly_otpravka_kogda,omitempty"`
	// Ustroystva — весь список машин под этим кодом доступа, для шторки
	// подписки. Пусто у старого сервера и до первого удачного ответа.
	Ustroystva []ustroystvoOkna `json:"ustroystva,omitempty"`
}

// ustroystvoOkna — одно устройство глазами окна. Отдельный тип от
// podpiska.Ustroystvo по одной причине: там даты строками в формате сервера,
// здесь unix, потому что считать «был два часа назад» умеет облик, а разбирать
// ISO-8601 в JavaScript ради этого незачем.
type ustroystvoOkna struct {
	Svoyo      bool   `json:"svoyo"`
	Imya       string `json:"imya"`
	Vid        string `json:"vid"`
	Versiya    string `json:"versiya,omitempty"`
	Vpervye    int64  `json:"vpervye,omitempty"`
	Poslednee  int64  `json:"poslednee,omitempty"`
	TrafikBayt int64  `json:"trafik_bayt,omitempty"`
}

// ozhidanieDoma — истинно ровно тогда, когда авторежим включён, обстановка
// «дома» и ядро прямо сейчас не работает (защита осознанно опущена или ещё
// не поднималась). Вынесена отдельной функцией от полей otvetSostoyaniya,
// чтобы условие проверялось таблицей случаев без поднятия HTTP-стенда.
// obyomZashchity — что окно узнаёт про то, что сейчас идёт через Kelevra.
//
// Единственное правило: пока ядро НЕ работает, ответ пуст. s.kartina
// заполняется при сборке конфига и переживает опускание защиты молча — без
// этого условия окно продолжало бы говорить про связь, которой в этот момент
// нет вовсе (регрессия 22.08, TestSostoyanieNeNesetZametkuKogdaZashchitaOpushchena).
func obyomZashchity(sost string, k konfig.Kartina) string {
	if sost != string(yadro.Rabotaet) {
		return ""
	}
	return k.Zametka
}

func ozhidanieDoma(avtorezhimVklyuchen bool, obstanovka string, sost string) bool {
	return avtorezhimVklyuchen && obstanovka == avtorezhim.Doma.String() && sost != string(yadro.Rabotaet)
}

// ImyaAvtoVyhoda — как называется выбор выхода, пока человек не лазил в
// список сам.
const ImyaAvtoVyhoda = "Автоматически"

// vyhodDlyaOkna — что окно пишет в строке выбора выхода (см. поля VyhodAvto
// и VyhodImya). Пустой uzly значит «человек ничего не выбирал» — тогда выход
// один и называется «Автоматически»; выбранный руками узел показывается как
// есть, иначе человек не поймёт, почему автоматика вдруг перестала работать.
//
// Чистая функция, а не метод: правило «пока не выбирал — Автоматически»
// проверяется таблицей, без HTTP-стенда и живого ядра.
func vyhodDlyaOkna(uzly map[string]string) (avto bool, imya string) {
	if len(uzly) == 0 {
		return true, ImyaAvtoVyhoda
	}
	klyuchi := make([]string, 0, len(uzly))
	for k := range uzly {
		klyuchi = append(klyuchi, k)
	}
	sort.Strings(klyuchi)
	return false, uzly[klyuchi[0]]
}

// polozhenieAvtorezhima — схема авторежима одной строкой для окна: где мы, что
// автомат из этого делает и что человек сейчас увидит.
//
// Строка пишется для ЧЕЛОВЕКА, поэтому называет и обстановку, и следствие:
// «домашняя сеть — подключение не требуется» вместо голого «дома». Переход
// обязан быть заметен (требование 27.08, повторено 28.08 и 29.08), а заметен
// ровно в тот момент, когда обстановка уже сменилась, а ядро ещё не догнало, —
// отсюда отдельные «подключаюсь» и «отключаюсь».
//
// Слова «защита» и «защищено» тут не употребляются вовсе: 09.09 владелец
// продукта прямо сказал, что Kelevra ничего не защищает и это слово его злит.
// Прежняя запись в коде («слово защита второй раз режет глаз», 24.08) описывала
// только повтор и увела правку не туда.
//
// Чистая функция от четырёх фактов, а не сборка внутри обработчика: правило
// проверяется таблицей случаев.
func polozhenieAvtorezhima(vklyuchen, ruchnoy bool, obstanovka, sost, slepPrichina string) string {
	if ruchnoy {
		return "автоматический режим выключен, переключение вручную"
	}
	if !vklyuchen {
		return ""
	}
	rabotaet := sost == string(yadro.Rabotaet) || sost == string(yadro.Podnimaem)
	if slepPrichina != "" {
		// Слепота дольше avtorezhim.PodryadDoPrichiny заходов: автомат
		// честно признаётся, что не решает ничего, вместо того чтобы
		// изображать работу молча.
		return "тип сети не определён: " + slepPrichina
	}
	switch obstanovka {
	case avtorezhim.Doma.String():
		if rabotaet {
			return "домашняя сеть — отключаюсь"
		}
		return "домашняя сеть — подключение не требуется"
	case avtorezhim.VneDoma.String():
		if rabotaet {
			return "сеть не домашняя — подключено"
		}
		return "сеть не домашняя — подключение"
	default:
		return "идёт определение типа сети"
	}
}

func (s *Sluzhba) sostoyanie(w http.ResponseWriter, r *http.Request) {
	// okno=1 ставит только открытое окно приложения (index.html), и по этой
	// метке служба решает, кому говорить про найденное обновление — диалогу в
	// окне или пузырю в трее (см. поле oknoSprosilo и povestitEsliNovaya).
	// Метку ставит именно КЛИЕНТ, а не «любой, кто спросил состояние»: по
	// этому же адресу ходят стенды, сторож окна и проверка живой копии
	// (kopiya.Otvechaet), и считать их за открытое окно значило бы навсегда
	// заглушить пузырь у человека, который окно давно закрыл.
	if r.URL.Query().Get("okno") == "1" {
		s.otmetitZhivoeOkno()
	}
	o := otvetSostoyaniya{
		Versiya:  podpiska.Versiya,
		Sost:     string(s.Yadro.Sost()),
		KodEst:   s.Nastroyki.Kod != "",
		KodMaska: podpiska.Maska(s.Nastroyki.Kod),
		YadroEst: s.Yadro.EstBinar(),
		Beda:     s.Yadro.PoslednyayaBeda(),
		PID:      s.Yadro.PID(),
	}
	if t, err := s.Yadro.Trafik(); err == nil {
		o.VverhBayt, o.VnizBayt = t.VverhBayt, t.VnizBayt
	}
	s.zamok.Lock()
	o.KachaemBin = s.kachaemBin
	if s.svedeniya != nil {
		o.Imya, o.DoUnix = s.svedeniya.Imya, s.svedeniya.Do
		o.ChelovekImya, o.UstroystvoImya = s.svedeniya.ImyaCheloveka(), s.svedeniya.ImyaUstroystva()
		o.PodpiskaEst = true
		o.Panel = s.svedeniya.Panel
		o.PodpiskaAktivna = s.svedeniya.Aktivna
		o.PodpiskaLimitBayt, o.PodpiskaSyedenoBayt = s.svedeniya.LimitBayt, s.svedeniya.SyedenoB
		for _, u := range s.svedeniya.Ustroystva {
			o.Ustroystva = append(o.Ustroystva, ustroystvoOkna{
				Svoyo:      u.Svoyo,
				Imya:       u.Imya,
				Vid:        u.Vid,
				Versiya:    u.Versiya,
				Vpervye:    u.VpervyeUnix(),
				Poslednee:  u.PosledneeUnix(),
				TrafikBayt: u.TrafikBayt,
			})
		}
	}
	k := s.kartina
	if o.Sost != string(yadro.Rabotaet) && o.Sost != string(yadro.Podnimaem) {
		o.Otkaz = s.otkaz
	}
	if s.naydennoeObnovlenie != nil {
		o.NovayaVersiyaDostupna = s.naydennoeObnovlenie.Versiya
	}
	// Долю отдаём ТОЛЬКО пока установка идёт и знаменатель известен: вне
	// установки поля нет вовсе, и окно рисует то, что честно, — ничего.
	if s.idetUstanovkaObnovleniya && s.vsegoObnovleniya > 0 {
		d := float64(s.skachanoObnovleniya) / float64(s.vsegoObnovleniya)
		// Сервер вправе прислать больше, чем обещал заголовком; «112%» в
		// окне — беда прибора, а не сети.
		if d > 1 {
			d = 1
		}
		o.ObnovlenieDolya = d
	}
	s.zamok.Unlock()
	o.Rezhim = string(k.Rezhim)
	// Zametka описывает, что идёт через Kelevra (internal/konfig/konfig.go) —
	// правда, только пока защита реально поднята (API ядра отвечает). k.kartina не чистится, когда защита
	// опущена — ни ручным тумблером (OpustitZashchitu), ни авторежимом
	// (avtorezhimKolbek), — так что без этого условия окно продолжало бы
	// врать про объём защиты, которой в этот момент нет вовсе. Заметка
	// авторежима (зачем защита опущена) — отдельный текст, живёт в JS
	// (zametkaAvtorezhima, oblik/index.html) и этой правки не касается.
	o.Zametka = obyomZashchity(o.Sost, k)
	o.Prava = prava.Est()
	o.NuzhnyPrava = !s.estPrava()
	if s.SluzhbaWindowsEst != nil {
		o.SluzhbaEst = s.SluzhbaWindowsEst()
	}
	// Разъяснение после обновления показывается ОДИН раз: первый же опрос
	// состояния его забирает и гасит. Окно спрашивает состояние раз в две
	// секунды, и карточка, висящая до конца сеанса, стала бы не объяснением,
	// а помехой.
	s.zamok.Lock()
	o.PosleObnovleniya = s.posleObnovleniya
	s.posleObnovleniya = ""
	o.SvezhihPravil = s.svezhihPravil
	s.zamok.Unlock()
	o.PravaUzheSprosheny = s.Nastroyki.UzheSprosiliPrava()
	// runtime.GOOS, а не сам факт ошибки: на не-Windows avtozapusk всегда
	// отвечает своей заглушкой avtozapusk.Ne, и эту ошибку незачем нести в
	// окно текстом — там и так спрятан весь блок.
	o.AvtozapuskPodderzhivaetsya = runtime.GOOS == "windows"
	if o.AvtozapuskPodderzhivaetsya {
		vkl, err := avtozapusk.Vklyuchen()
		if err != nil {
			o.AvtozapuskBeda = err.Error()
		} else {
			o.AvtozapuskVklyuchen = vkl
			if vkl {
				if ust, err := avtozapusk.Ustarela(); err == nil {
					o.AvtozapuskUstarela = ust
				}
			}
		}
	}
	s.avtorezhimZamok.Lock()
	o.AvtorezhimVklyuchen = s.Nastroyki.Avtorezhim
	if s.avtorezhimEkz != nil {
		o.AvtorezhimObstanovka = s.avtorezhimEkz.Zadvizhka.Tekushcheye().String()
		o.AvtorezhimSlepPrichina = s.avtorezhimEkz.PrichinaSlepoty()
	} else if s.avtorezhimKnopkaObstanovka != avtorezhim.Neizvestno {
		// Фоновый служитель не крутится (тумблер выключен) — обстановка,
		// увиденная последним нажатием «Подключиться» (domaSeychas), это
		// единственное, что вообще известно; человек обязан её видеть (см.
		// zametkaAvtorezhima, oblik/index.html — Правка 2 сняла там условие
		// на тумблер).
		o.AvtorezhimObstanovka = s.avtorezhimKnopkaObstanovka.String()
	}
	o.AvtorezhimRuchnoy = s.Nastroyki.RuchnoyVybor
	s.avtorezhimZamok.Unlock()
	o.OzhidanieDoma = ozhidanieDoma(o.AvtorezhimVklyuchen, o.AvtorezhimObstanovka, o.Sost)
	o.AvtorezhimPolozhenie = polozhenieAvtorezhima(o.AvtorezhimVklyuchen, o.AvtorezhimRuchnoy, o.AvtorezhimObstanovka, o.Sost, o.AvtorezhimSlepPrichina)
	o.VyhodAvto, o.VyhodImya = vyhodDlyaOkna(s.Nastroyki.Uzly)
	o.ZhurnalyOtpravkaKogda = s.Nastroyki.KogdaOtpravlyaliZhurnaly()
	otdat(w, o, nil)
}

// avtozapuskRuchka включает или выключает запуск Kelevra вместе с Windows.
// Повторное «включить» на устаревшей записи — это и есть починка: Vklyuchit
// перезаписывает путь на нынешний .exe.
func (s *Sluzhba) avtozapuskRuchka(w http.ResponseWriter, r *http.Request) {
	var vhod struct {
		Vklyuchit bool `json:"vklyuchit"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&vhod); err != nil {
		otdat(w, nil, fmt.Errorf("не разобрал запрос"))
		return
	}
	var err error
	if vhod.Vklyuchit {
		err = avtozapusk.Vklyuchit()
	} else {
		err = avtozapusk.Vyklyuchit()
	}
	otdat(w, map[string]any{"gotovo": true}, err)
}

// avtorezhimRuchka включает или выключает автоматическое переключение
// защиты по смене сети (дома/не дома) — по образцу avtozapuskRuchka выше.
func (s *Sluzhba) avtorezhimRuchka(w http.ResponseWriter, r *http.Request) {
	var vhod struct {
		Vklyuchit bool `json:"vklyuchit"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&vhod); err != nil {
		otdat(w, nil, fmt.Errorf("не разобрал запрос"))
		return
	}
	if vhod.Vklyuchit {
		// Neizvestno — ручка /api/avtorezhim, в отличие от кнопки
		// «Подключиться», не делает своего захода прямо сейчас: пусть
		// свежий служитель узнает обстановку сам, спешить некуда.
		if err := s.vklyuchitAvtorezhim(avtorezhim.Neizvestno); err != nil {
			otdat(w, nil, err)
			return
		}
	} else {
		// Человек сам отказался от автомата — запоминаем это отдельным полем
		// (hranenie.Nastroyki.RuchnoyVybor), чтобы кнопка «Подключиться»
		// больше не включала автомат за него: с этой минуты решает он.
		s.Nastroyki.Avtorezhim = false
		s.Nastroyki.RuchnoyVybor = true
		if err := hranenie.Sohranit(s.Nastroyki); err != nil {
			otdat(w, nil, err)
			return
		}
		s.OstanovitAvtorezhim()
	}
	otdat(w, map[string]any{"gotovo": true}, nil)
}

// vklyuchitAvtorezhim — общее тело «включить автомат»: сохранить выбор на
// диск (переживёт перезапуск) и поднять фонового служителя. Используется и
// ручкой /api/avtorezhim, и кнопкой «Подключиться» (podklyuchit). Требование
// 27.08: человек включает VPN нажатием, дальше программа сама определяет
// обстановку, а по возвращении домой сама же возвращается в положение
// «дома» — нажатие кнопки обязано включать автомат навсегда, а не
// один раз спросить обстановку и забыть про неё.
//
// nachalo — обстановка, уже доказанная отдельным заходом ДО этого вызова
// (domaSeychas кнопки), или Neizvestno, если такого захода не было. Свежий
// служитель заводится сразу на неё, а не с нуля: без этого /api/sostoyanie
// после нажатия кнопки на секунды показал бы «неизвестно» вместо только что
// доказанной обстановки, пока фоновый служитель не проведёт свой первый
// заход — см. TestPodklyuchitDomaNePodnimaetZashchitu.
func (s *Sluzhba) vklyuchitAvtorezhim(nachalo avtorezhim.Sostoyanie) error {
	s.Nastroyki.Avtorezhim = true
	// Автомат включён — прежний ручной отказ снят: человек передумал.
	s.Nastroyki.RuchnoyVybor = false
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		return err
	}
	s.zapustitAvtorezhimSNachala(context.Background(), nachalo)
	return nil
}

// ZapustitAvtorezhim поднимает фонового служителя авторежима (see
// internal/avtorezhim.Sluzhitel), если он ещё не крутится. Идемпотентно:
// повторный вызов, пока служитель уже работает, ничего не делает — так
// вызвать можно и из ручки /api/avtorezhim, и один раз при старте службы
// (cmd/kelevra/main.go), не заботясь, кто из них подоспел первым.
func (s *Sluzhba) ZapustitAvtorezhim(roditelskiy context.Context) {
	s.zapustitAvtorezhimSNachala(roditelskiy, avtorezhim.Neizvestno)
}

// zapustitAvtorezhimSNachala — тело ZapustitAvtorezhim, которому можно
// задать стартовую обстановку задвижки (см. vklyuchitAvtorezhim). Идемпотентно
// так же, как ZapustitAvtorezhim: если служитель уже крутится, nachalo
// молча игнорируется — второй заход кнопки не должен дёргать задвижку уже
// работающего служителя.
func (s *Sluzhba) zapustitAvtorezhimSNachala(roditelskiy context.Context, nachalo avtorezhim.Sostoyanie) {
	s.avtorezhimZamok.Lock()
	defer s.avtorezhimZamok.Unlock()
	if s.avtorezhimOtmena != nil {
		return
	}
	ctx, otmena := context.WithCancel(roditelskiy)
	s.avtorezhimOtmena = otmena
	// Новое слово человека: всё, что решил предыдущий служитель и не успело
	// примениться, с этой секунды недействительно.
	s.avtorezhimPokolenie++
	moyoPokolenie := s.avtorezhimPokolenie
	sobratSluzhitelya := s.avtorezhimDlyaSluzhitelya
	if sobratSluzhitelya == nil {
		sobratSluzhitelya = s.avtorezhimBoevoy
	}
	s.avtorezhimEkz = sobratSluzhitelya()
	if nachalo != avtorezhim.Neizvestno {
		s.avtorezhimEkz.Zadvizhka = avtorezhim.NovayaZadvizhka(nachalo)
	}
	sluzh := &avtorezhim.Sluzhitel{
		Avtorezhim: s.avtorezhimEkz,
		Sledchik:   avtorezhim.NovySledchik(),
		// Primenit, а не Kolbek: колбэк смены обстановки молчит, когда
		// обстановка УЖЕ стоит на нужном значении, а защита ему не отвечает —
		// ровно та яма, из-за которой VPN не гас при возврате домой (жалоба
		// хозяина 25.08, 28.08, 29.08, 30.08). См. avtorezhim.Sluzhitel.Primenit.
		//
		// Поколение зашито в замыкание: этот служитель применяет свои решения,
		// только пока человек не сказал ничего нового (см. avtorezhimPokolenie).
		Primenit: func(ctx context.Context, sost avtorezhim.Sostoyanie, povtor bool) {
			s.avtorezhimPrimenit(ctx, moyoPokolenie, sost, povtor)
		},
	}
	log.Printf("авторежим: включаю слежение за сетью")
	go sluzh.Krutit(ctx)
}

// OstanovitAvtorezhim гасит служителя авторежима, если он крутится.
// Идемпотентно так же, как ZapustitAvtorezhim.
func (s *Sluzhba) OstanovitAvtorezhim() {
	s.avtorezhimZamok.Lock()
	defer s.avtorezhimZamok.Unlock()
	if s.avtorezhimOtmena == nil {
		return
	}
	log.Printf("авторежим: выключаю слежение за сетью")
	// Поколение растёт ДО отмены и под тем же замком, что его читает
	// avtorezhimAktualen: заход, уже ушедший в полёт, после этой строки не
	// применит ничего, даже если доберётся до защиты позже.
	s.avtorezhimPokolenie++
	s.avtorezhimOtmena()
	s.avtorezhimOtmena = nil
	s.avtorezhimEkz = nil
}

// avtorezhimAktualen — можно ли ЕЩЁ применять решение служителя поколения
// pokolenie. Три условия, и все три про то, что человек с тех пор не
// передумал: его ctx жив (служителя не гасили), автомат всё ещё включён и
// поколение то же самое.
//
// Проверка стоит вплотную перед действием, а не в начале захода: между
// опросом зондов и подъёмом ядра проходят секунды, и нажатие «Отключить»
// приходится ровно на них.
func (s *Sluzhba) avtorezhimAktualen(ctx context.Context, pokolenie uint64) bool {
	if ctx.Err() != nil {
		return false
	}
	s.avtorezhimZamok.Lock()
	defer s.avtorezhimZamok.Unlock()
	return s.Nastroyki.Avtorezhim && s.avtorezhimPokolenie == pokolenie
}

// avtorezhimPrimenit — приведение защиты от имени служителя поколения
// pokolenie: сначала сверка с последним словом человека, потом действие.
// Кнопки («Подключиться», «Отключить») зовут avtorezhimKolbek напрямую —
// им сверяться не с чем, они и есть слово человека.
func (s *Sluzhba) avtorezhimPrimenit(ctx context.Context, pokolenie uint64, sost avtorezhim.Sostoyanie, povtor bool) {
	if !s.avtorezhimAktualen(ctx, pokolenie) {
		log.Printf("авторежим: заход решил «%s», но человек с тех пор передумал — решение не применяю", sost)
		return
	}
	s.avtorezhimKolbek(ctx, sost, povtor)
}

// tunnelPodnyat — стоит ли сейчас НАШ туннель на пути зондов авторежима.
// Именно туннель, а не «защита вообще»: в прокси-режиме ядро прописано
// системным прокси, а зонды авторежима ходят мимо системного прокси
// (net.Resolver и net.Dialer его не читают) — там они мерят настоящую сеть
// и слепыми не становятся.
// Зачем признак нужен — см. avtorezhim.Nablyudeniye.ZondSlep: в туннеле
// зонды видят подмену нашего же fakeip и решают «дома» где угодно.
func (s *Sluzhba) tunnelPodnyat() bool {
	// «Поднимается» СЧИТАЕТСЯ поднятым, и это главная правка 09.09.
	//
	// Раньше здесь стояло Sost() == Rabotaet, то есть «ядро доложило, что
	// связь работает». Но зондам важно не самочувствие ядра, а лежит ли его
	// туннель у них на пути, — а он ложится РАНЬШЕ доклада. Замер на стенде
	// 09.09: маршруты туннеля и подмена ответов публичного резолвера на
	// 198.18.0.x живут уже на 3.7-й секунде после запуска ядра, тогда как
	// адаптер к этому мигу ещё даже не виден в списке сетевых устройств, а
	// состояние Rabotaet ставится и вовсе на 7-й.
	//
	// В это окно заход авторежима шёл СИСТЕМНЫМ резолвером — то есть прямо в
	// перехват собственного ядра — и получал признак «дома» в любой точке
	// мира. Ровно это видно в журнале живой машины 08.09: «поднят туннель» в
	// 21:31:07, «обстановка сменилась на дома» в 21:31:09, защита опущена
	// вне дома.
	if s.Yadro == nil {
		return false
	}
	s.zamok.Lock()
	rezhim := s.kartina.Rezhim
	s.zamok.Unlock()
	return tunnelNaPutiZondov(s.Yadro.Sost(), rezhim)
}

// tunnelNaPutiZondov — вся суть решения выше, вынесенная отдельно, чтобы её
// проверял тест, а не пересказ: в каком состоянии ядра его туннель уже (или
// ещё) стоит на пути зондов авторежима.
func tunnelNaPutiZondov(sost yadro.Sostoyanie, rezhim konfig.Rezhim) bool {
	if rezhim != konfig.Tunnel {
		return false
	}
	return sost == yadro.Rabotaet || sost == yadro.Podnimaem
}

// zashchitaPodnyata — работает ли прямо сейчас ядро. Именно «работает», а не
// «поднимается»: пока Sost == Podnimaem, ядро уже порождено, и авторежиму
// нечего добавить — второй PodnyatZashchitu на том же круге только породил бы
// второй процесс.
func (s *Sluzhba) zashchitaPodnyata() bool {
	if s.zashchitaPodnyataDlyaStenda != nil {
		return s.zashchitaPodnyataDlyaStenda()
	}
	if s.Yadro == nil {
		return false
	}
	sost := s.Yadro.Sost()
	return sost == yadro.Rabotaet || sost == yadro.Podnimaem
}

// avtorezhimKolbek — ПРИВЕДЕНИЕ защиты к обстановке (avtorezhim.Sluzhitel.
// Primenit): дома защита опущена, вне дома поднята. Зовётся после каждого
// зрячего захода, а не только при смене обстановки, поэтому обязан быть
// идемпотентным — что и обеспечивает чистое правило avtorezhim.Nuzhno:
// когда защита уже отвечает обстановке, оно возвращает NeTrogat, и ядро
// никто не дёргает.
//
// Почему приведение, а не реакция на смену. Обстановка может УЖЕ стоять на
// Doma, пока защита поднята: кнопка «Подключиться» дома заводит задвижку
// сразу на Doma (podklyuchit → vklyuchitAvtorezhim), опускание могло не
// удаться с первого раза, защиту могли поднять руками поверх работающего
// автомата. Событийный колбэк в таких случаях молчал вечно — отсюда жалоба
// хозяина, повторённая четырежды (25.08, 28.08, 29.08, 30.08): «при
// переключении обратно на вайфай впн не выключился». Телефонный эталон
// делает ровно это же — apply() на каждом круге, включая repeat = true
// (AutoMode.kt:1262-1275, ветка Situation.Home → suspendTunnel).
//
// Neizvestno сюда не доходит вовсе (Sluzhitel отсеивает его вместе со
// слепыми заходами), но правило avtorezhim.Nuzhno всё равно отвечает на неё
// NeTrogat — неизвестность не повод дёргать чужой туннель.
func (s *Sluzhba) avtorezhimKolbek(ctx context.Context, sost avtorezhim.Sostoyanie, povtor bool) {
	switch avtorezhim.Nuzhno(sost, s.zashchitaPodnyata()) {
	case avtorezhim.Opustit:
		if povtor {
			// Отдельная строка: это НЕ смена обстановки, а расхождение
			// защиты с обстановкой, которая стоит уже давно. На живой машине
			// хозяина различить эти два случая по журналу обязательно —
			// именно второй четыре раза оставался незамеченным.
			log.Printf("авторежим: обстановка «дома» стоит, а защита поднята — опускаю (приведение)")
		} else {
			log.Printf("авторежим: обстановка «дома» — опускаю защиту")
		}
		s.zamok.Lock()
		idet := s.podyomIdet
		s.zamok.Unlock()
		if idet {
			// Подъём в работе — не дёргаем его на полпути. Обстановка никуда
			// не денется: авторежим ходит по кругу и опустит защиту
			// следующим же заходом, когда подъём кончится (ветка «приведение»
			// выше как раз про это). А погасив ядро сейчас, мы получили бы
			// провал подъёма и красную беду на ровном месте — замер 08.09.
			log.Printf("авторежим: обстановка «дома», но связь сейчас поднимается — не мешаю, опущу следующим заходом")
			return
		}
		if err := s.OpustitZashchitu(); err != nil {
			log.Printf("авторежим: не опустил защиту: %v", err)
		}
	case avtorezhim.Podnyat:
		if povtor {
			log.Printf("авторежим: обстановка «вне дома» стоит, а защиты нет — поднимаю (приведение)")
		} else {
			log.Printf("авторежим: обстановка «вне дома» — поднимаю защиту")
		}
		if err := s.PodnyatZashchitu(ctx); err != nil {
			log.Printf("авторежим: не поднял защиту: %v", err)
		}
	default:
		// NeTrogat — защита уже отвечает обстановке. Молчим: лишний рестарт
		// ядра рвёт живые соединения, а лишняя строка в журнале на каждом
		// заходе (а их сотни в сутки) топит в шуме те две, что выше.
	}
}

// kod принимает код доступа, качает по нему конфиг и запоминает код,
// только если конфиг настоящий: иначе приложение запомнит нерабочий код.
func (s *Sluzhba) kod(w http.ResponseWriter, r *http.Request) {
	var vhod struct {
		Kod string `json:"kod"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&vhod); err != nil {
		otdat(w, nil, fmt.Errorf("не разобрал запрос"))
		return
	}
	kod := strings.TrimSpace(vhod.Kod)
	ctx, otmena := context.WithTimeout(r.Context(), 30*time.Second)
	defer otmena()
	// Ввод — либо готовая ссылка на подписку (https://<дверь>/k/<код> или /u/<ключ>), либо
	// код: тогда вход по дверям, как на телефоне (своя сеть выдаёт этой машине свой ключ).
	adres, hvost, ssylka := podpiska.RazobratSsylku(kod)
	if ssylka {
		kod = hvost
	} else {
		var err error
		adres, err = s.Podpiska.Voyti(ctx, kod, s.dveri())
		if err != nil {
			log.Printf("вход по коду не удался: %v", err)
			otdat(w, nil, err)
			return
		}
	}
	profil, err := s.Podpiska.KonfigPoAdresu(ctx, adres)
	if err != nil {
		log.Printf("сервер подписки не дал профиль: %v", err)
		otdat(w, nil, err)
		return
	}
	log.Printf("профиль получен с сервера подписки: %d байт (%s)", len(profil), adresDlyaZhurnala(adres))
	if err := s.SohranitProfil(profil); err != nil {
		otdat(w, nil, err)
		return
	}
	s.Nastroyki.Kod = kod
	s.Nastroyki.Podpiska = adres
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		otdat(w, nil, err)
		return
	}
	if sv, err := s.Podpiska.SvedeniyaPoAdresu(ctx, adres); err == nil {
		s.zamok.Lock()
		s.svedeniya = sv
		s.zamok.Unlock()
		s.zapomnitDveri(sv)
	}
	otdat(w, map[string]any{"gotovo": true}, nil)
}

// panelRuchka — одноразовая ссылка в панель своей сети для окна (кнопка «Панель сети»).
func (s *Sluzhba) panelRuchka(w http.ResponseWriter, r *http.Request) {
	ctx, otmena := context.WithTimeout(r.Context(), 20*time.Second)
	defer otmena()
	ssylka, err := s.Podpiska.PropuskPaneli(ctx, s.Nastroyki.Podpiska)
	if err != nil {
		log.Printf("панель сети не открылась: %v", err)
		otdat(w, nil, err)
		return
	}
	otdat(w, map[string]any{"ssylka": ssylka}, nil)
}

// dveri — куда стучаться с кодом: стенд (KELEVRA_PODPISKA) — только он; иначе двери, которые
// сервер назвал в /info; иначе двери «из коробки».
func (s *Sluzhba) dveri() []string {
	if h := os.Getenv("KELEVRA_PODPISKA"); h != "" {
		return []string{h}
	}
	if len(s.Nastroyki.Dveri) > 0 {
		return s.Nastroyki.Dveri
	}
	return podpiska.Dveri
}

// zapomnitDveri — сервер своей сети называет свои двери в /info: запоминаем для следующего входа.
func (s *Sluzhba) zapomnitDveri(sv *podpiska.Svedeniya) {
	if sv == nil || len(sv.Dveri) == 0 || slices.Equal(sv.Dveri, s.Nastroyki.Dveri) {
		return
	}
	s.Nastroyki.Dveri = slices.Clone(sv.Dveri)
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		log.Printf("двери из сведений не сохранились: %v", err)
	}
}

// adresDlyaZhurnala — адрес подписки без кода и ключа: журнал уезжает разработчику.
func adresDlyaZhurnala(adres string) string {
	if i := strings.LastIndex(adres, "/"); i > 0 {
		return adres[:i+1] + "…"
	}
	return "…"
}

// vzyatProfil и vzyatSvedeniya — по сохранённому адресу подписки; старые установки
// (адреса нет) — по коду на зашитом хосте, как раньше.
func (s *Sluzhba) vzyatProfil(ctx context.Context) ([]byte, error) {
	if s.Nastroyki.Podpiska != "" {
		return s.Podpiska.KonfigPoAdresu(ctx, s.Nastroyki.Podpiska)
	}
	return s.Podpiska.Konfig(ctx, s.Nastroyki.Kod)
}

func (s *Sluzhba) vzyatSvedeniya(ctx context.Context) (*podpiska.Svedeniya, error) {
	if s.Nastroyki.Podpiska != "" {
		return s.Podpiska.SvedeniyaPoAdresu(ctx, s.Nastroyki.Podpiska)
	}
	return s.Podpiska.Svedeniya(ctx, s.Nastroyki.Kod)
}

// Отказы подъёма связи, которые читает человек: окно показывает их текст как
// есть, поэтому тон официальный, без первого лица и без советов «попробуйте
// ещё раз» — связь в этих случаях не поднята вовсе, и ступени «хотя бы
// браузеры» больше нет (решение владельца 17.10: «нет никаких неполных
// режимов и не рабочих»).
var (
	// OshibkaNetPrav — у этой копии нет прав администратора: туннель поднять
	// нечем. Так бывает, только пока служба Windows не установлена (человек
	// отказал в запросе прав); окно показывает кнопку «Дать права».
	OshibkaNetPrav = errors.New("Для работы Kelevra нужны права администратора.")
	// OshibkaAdapterZanyat — сетевое устройство туннеля держит остаток
	// прошлого подключения, и соседние имена тоже заняты. Повтор не поможет:
	// устройство освобождает перезагрузка (замер с машины человека 01.09).
	OshibkaAdapterZanyat = errors.New("Сетевое устройство занято прошлым подключением. Требуется перезагрузка компьютера.")
	// OshibkaTunnelNePodnyalsya — ядро не создало туннель по другой причине
	// (драйвер, отказ системы). Сырой текст ядра остаётся в журнале и под
	// «подробностями» окна (s.beda), человеку — эта строка.
	OshibkaTunnelNePodnyalsya = errors.New("Не удалось создать сетевое подключение Kelevra.")
)

// otkazPodyoma — отказ подъёма с двумя лицами: Error() — строка для человека,
// а errors.Is/As видят и её, и настоящую причину (ошибку ядра).
type otkazPodyoma struct {
	dlyaCheloveka error
	prichina      error
}

func (o otkazPodyoma) Error() string   { return o.dlyaCheloveka.Error() }
func (o otkazPodyoma) Unwrap() []error { return []error{o.dlyaCheloveka, o.prichina} }

// povtorovAdaptera — сколько раз поднимать туннель заново на соседнем имени
// адаптера, если ядро упало на занятом. Три: остаток одной прошлой попытки
// снимается первым же повтором (замер 09.09: подъём на свободном имени — 1 с),
// а если заняты три имени подряд, дело в системе, и четвёртая попытка только
// тратит время человека.
const povtorovAdaptera = 3

// adapterZanyatPoOshibke — ядро упало потому, что имя или адрес сетевого
// адаптера держит остаток прошлой попытки: «create adapter … file already
// exists» (имя) или «set ipv4 address: The object already exists» (адрес,
// замер 06.09).
func adapterZanyatPoOshibke(err error) bool {
	if err == nil {
		return false
	}
	return tunnel.ZanyatoePoOshibke(err) || strings.Contains(strings.ToLower(err.Error()), "object already exists")
}

// tunnelPoOshibke — ядро упало именно на туннеле, а не на сервере, правилах
// или порте.
func tunnelPoOshibke(err error) bool {
	if err == nil {
		return false
	}
	t := strings.ToLower(err.Error())
	return strings.Contains(t, "inbound/tun") || strings.Contains(t, "configure tun") ||
		strings.Contains(t, "wintun") || strings.Contains(t, "create adapter")
}

// PodnyatZashchitu поднимает туннель, а если ядра на машине ещё нет — сначала
// приносит его сам. Общее тело для ручки «Подключить» (podklyuchit) и для
// авторежима (вызывается из avtorezhimKolbek, когда обстановка стала «вне
// дома»).
//
// Режим один — туннель. Нет прав — OshibkaNetPrav, ядро не запускается вовсе.
// Туннель не поднялся — связь не поднята, человеку причина (Oshibka*).
// Системный прокси не ставится никогда.
//
// ctx — основа для собственных таймаутов метода (15 минут на скачивание
// ядра, 70 секунд на его старт); HTTP-обработчик передаёт context.Background().
func (s *Sluzhba) PodnyatZashchitu(ctx context.Context) error {
	// Пока идём вверх, авторежиму нельзя гасить ядро из-под нас (см. поле
	// podyomIdet и OpustitZashchitu).
	s.zamok.Lock()
	s.podyomIdet = true
	s.zamok.Unlock()
	defer func() {
		s.zamok.Lock()
		s.podyomIdet = false
		s.zamok.Unlock()
	}()

	// Права — раньше всего, даже раньше скачивания ядра: без них туннель не
	// поднять, а качать мегабайты ради отказа незачем.
	if !s.estPrava() {
		log.Printf("подключение не начато: у этой копии нет прав администратора (служба Windows не установлена)")
		s.soobshchitTreyuProZashchitu(false)
		return OshibkaNetPrav
	}

	if !s.Yadro.EstBinar() {
		s.zamok.Lock()
		uzhe := s.kachaemBin
		s.kachaemBin = true
		s.zamok.Unlock()
		if uzhe {
			return fmt.Errorf("ядро уже качается")
		}
		log.Printf("ядра на машине нет, качаю")
		zctx, otmena := context.WithTimeout(ctx, 15*time.Minute)
		nachalo := time.Now()
		err := s.Yadro.Zagruzit(zctx)
		otmena()
		if err == nil {
			log.Printf("ядро скачано за %s", time.Since(nachalo).Round(time.Second))
		} else {
			log.Printf("не смог скачать ядро: %v", err)
		}
		s.zamok.Lock()
		s.kachaemBin = false
		s.zamok.Unlock()
		if err != nil {
			return err
		}
	}
	vybor := konfig.Vybor{}
	// Правила берём С ДИСКА, если кеш полон, — и ядро на старте в сеть за ними
	// не ходит вовсе.
	//
	// Ради этого кеш и заведён (разбор — в шапке internal/pravila/kesh.go).
	// Замер 08.09: домен правил был жив и файл качался за секунду, но ИМЕННО
	// в момент старта ядра резолв не работал — Windows перестраивает DNS под
	// туннель, старый резолвер уже молчит, новый ещё не поднялся, потому что
	// ядро и не стартовало. Человек в этой петле оставался без связи.
	// ЗДЕСЬ НЕТ ЗАКАЧКИ ПРАВИЛ, И ЭТО РЕШЕНИЕ, А НЕ ЗАБЫВЧИВОСТЬ: всякая
	// сетевая работа на пути нажатой кнопки сдвигает подъём, а за ним — весь
	// авторежим. Кеш наполняют часы (SveryatPravila).
	s.dobavitPravilaIzKesha(&vybor, true)
	if err := s.perestroit(vybor); err != nil {
		log.Printf("не подготовил конфиг: %v", err)
		return fmt.Errorf("не подготовил конфиг: %w", err)
	}
	// Имя адаптера из профиля — от него считается сдвиг адресов на повторах.
	s.zamok.Lock()
	imyaIzProfilya := s.kartina.TunImya
	s.zamok.Unlock()
	// Имя сетевого адаптера туннеля должно быть свободно ДО запуска ядра.
	//
	// Беда с машины человека 01.09: он нажал «Отключить», через 13 секунд
	// подключился снова, и ядро упало на «create adapter: файл уже существует
	// | open existing adapter: элемент не найден» — имя tun125 осталось занято
	// остатком прошлой попытки. Падает ядро не сразу, а через ПЯТНАДЦАТЬ
	// секунд. Вопрос системе про имя стоит миллисекунды — задаём его сами и
	// заранее (internal/tunnel.SvobodnoeImya).
	vybor, err := s.podobratImyaAdaptera(vybor)
	if err != nil {
		s.zapomnitOtkaz(err)
		s.soobshchitTreyuProZashchitu(false)
		return err
	}
	zapustit := s.Yadro.Zapustit
	if s.zapustitYadro != nil {
		zapustit = s.zapustitYadro
	}
	// Отменённый ctx обязан обрывать повторы, а не проходить их до конца:
	// каждая попытка до 70 с, и за это время человек успевает нажать
	// «Отключить» (авторежим — единственный, кто зовёт этот метод с
	// отменяемым ctx, см. avtorezhimKolbek). Кнопкам это ничего не меняет:
	// podklyuchit зовёт PodnyatZashchitu с context.Background().
	iskhodnyyZapusk := zapustit
	zapustit = func(c context.Context) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("подъём защиты прерван: %w", err)
		}
		return iskhodnyyZapusk(c)
	}
	zctx, otmena := context.WithTimeout(ctx, 70*time.Second)
	defer otmena()
	err = zapustit(zctx)
	// Ядро всё-таки упало на занятом адаптере — повторяем на ДРУГОМ имени.
	//
	// Проверка перед запуском (podobratImyaAdaptera выше) ловит не всё, и это
	// видно в самой строке ядра: «open existing adapter: Element not found»
	// значит, что устройство есть на уровне драйвера, а в списке сетевых
	// интерфейсов Windows его уже нет — то есть net.Interfaces() честно
	// ответил нам «свободно». Переспрашивать систему тут бессмысленно, надо
	// менять имя, не глядя на старое (tunnel.OboytiZanyatoe). Адрес двигаем
	// тем же сдвигом, что и имя: остаток держит их вместе.
	for i := 0; i < povtorovAdaptera && adapterZanyatPoOshibke(err); i++ {
		s.zamok.Lock()
		zanyatoe := s.kartina.TunImya
		s.zamok.Unlock()
		_ = s.Yadro.Ostanovit() // ядро могло не умереть, а зависнуть на таймауте
		novoe, zanyaty := tunnel.OboytiZanyatoe(zanyatoe, s.adapterZhivDlyaStenda)
		if novoe == "" {
			log.Printf("ядро не создало сетевой адаптер %q, и все соседние имена тоже заняты (%s)",
				zanyatoe, strings.Join(zanyaty, ", "))
			break
		}
		log.Printf("ядро не создало сетевой адаптер %q (занят остатком прошлой попытки) — повтор %d из %d на имени %q",
			zanyatoe, i+1, povtorovAdaptera, novoe)
		povtor := vybor
		povtor.TunImya = novoe
		povtor.TunSdvigAdresa = tunnel.Sdvig(imyaIzProfilya, novoe)
		if e := s.perestroit(povtor); e != nil {
			log.Printf("конфиг с именем адаптера %q не собрался: %v", novoe, e)
			break
		}
		zctxA, otmenaA := context.WithTimeout(ctx, 70*time.Second)
		err = zapustit(zctxA)
		otmenaA()
		if err == nil {
			log.Printf("повтор на имени %q удался — туннель поднят", novoe)
			vybor = povtor
			break
		}
		log.Printf("повтор на имени %q не удался: %v", novoe, err)
	}
	// Второй такой же отказ, найден 23.08 замером настоящего ядра на боевом
	// профиле (22 route.rule_set, качаются с subkv.chickenkiller.com
	// detour:"direct" — мимо туннеля). Источник мёртв или молчит, кеш пуст →
	// ядро НЕ открывает порт вовсе, а падает целиком строкой «initialize
	// rule-set[N]: initial rule-set: ...». Сперва пробуем встроенный комплект
	// (konfig.Vybor.PravilaIzKomplekta) — умная маршрутизация выживает.
	if err != nil && strings.Contains(err.Error(), "initialize rule-set") {
		log.Printf("источник правил маршрутизации недоступен, пробую встроенный комплект правил (умная маршрутизация сохранится)")
		podnyalsyaKomplektom := false
		if komplekt, kErr := pravila.Razlozhit(filepath.Join(hranenie.PapkaYadra(), "pravila")); kErr != nil {
			log.Printf("не разложил встроенный комплект правил: %v", kErr)
		} else {
			vyborKomplekt := vybor
			vyborKomplekt.PravilaIzKomplekta = komplekt
			vyborKomplekt.PravilaKomplektData = pravila.Data()
			if e := s.perestroit(vyborKomplekt); e != nil {
				log.Printf("встроенный комплект правил не применился к профилю: %v", e)
			} else {
				zctx3, otmena3 := context.WithTimeout(ctx, 70*time.Second)
				defer otmena3()
				errKomplekt := zapustit(zctx3)
				if errKomplekt == nil {
					err = nil
					vybor = vyborKomplekt
					podnyalsyaKomplektom = true
					log.Printf("поднялся на встроенном комплекте правил (снимок от %s) — умная маршрутизация жива", pravila.Data())
				} else {
					log.Printf("встроенный комплект не поднял ядро: %v", errKomplekt)
					err = errKomplekt
				}
			}
		}
		if !podnyalsyaKomplektom {
			// Ступени «поднять совсем без правил» нет: без правил в туннель
			// уходит ВЕСЬ трафик, включая российские сайты, и они отвечают
			// «выключите VPN» (замер 08.09). Честнее не поднимать связь и
			// сказать причину.
			log.Printf("ни свежих правил, ни встроенного комплекта — связь не поднимаю: без разбора трафика весь он уйдёт в туннель, включая российские сайты")
		}
	}
	// Отказ туннеля — словами человека. Сырой текст ядра остаётся причиной
	// (errors.Is/As) и строкой журнала.
	switch {
	case err == nil:
	case adapterZanyatPoOshibke(err):
		err = otkazPodyoma{OshibkaAdapterZanyat, err}
	case tunnelPoOshibke(err):
		err = otkazPodyoma{OshibkaTunnelNePodnyalsya, err}
	}
	if err == nil {
		// След туннеля на диске: жёсткую смерть процесса не переживёт ни один
		// defer, и следующий запуск должен УВИДЕТЬ, что туннель поднимали мы, и
		// проверить приборно, не остался ли висеть адаптер (internal/tunnel).
		s.zamok.Lock()
		tunImya := s.kartina.TunImya
		s.zamok.Unlock()
		zakrepitTunnel(tunImya)
		// Узел, выбранный в окне ДО этого нажатия (или на прошлом сеансе),
		// применяем прямо сейчас.
		s.primenitSohranennyeUzly()
	} else {
		log.Printf("связь не поднята: %v", err)
		_ = s.Yadro.Ostanovit()
		// Защита не поднялась — значит и туннеля нет, а оставленный след
		// заставил бы СЛЕДУЮЩИЙ запуск искать несуществующий адаптер.
		tunnel.UbratMetku()
		var otkaz otkazPodyoma
		if errors.As(err, &otkaz) {
			// Туннель не встал — журнал уезжает НЕМЕДЛЕННО, не дожидаясь
			// ночи: 08.09 разбор упирался в то, что факты приходят через сутки.
			go s.OtpravitZhurnalySrochno(context.WithoutCancel(ctx))
		}
	}
	s.zapomnitOtkaz(err)
	s.soobshchitTreyuProZashchitu(err == nil)
	return err
}

// zapomnitOtkaz — что окно говорит человеку о последней неудачной попытке
// (поле otkaz в /api/sostoyanie). Только отказы с человеческим лицом
// (otkazPodyoma); успех и прочие ошибки строку гасят.
func (s *Sluzhba) zapomnitOtkaz(err error) {
	tekst := ""
	var otkaz otkazPodyoma
	if errors.As(err, &otkaz) {
		tekst = otkaz.Error()
	}
	s.zamok.Lock()
	s.otkaz = tekst
	s.zamok.Unlock()
}

// podobratImyaAdaptera — под каким именем поднимать сетевой адаптер туннеля
// в ЭТОТ раз. Зовётся один раз, перед первым запуском ядра.
//
// Ошибка — свободного имени нет вовсе (заняты имя из профиля и все соседние):
// запускать ядро незачем, оно потратит пятнадцать секунд человека и упадёт
// ровно на этом. Человеку — OshibkaAdapterZanyat.
func (s *Sluzhba) podobratImyaAdaptera(vybor konfig.Vybor) (konfig.Vybor, error) {
	s.zamok.Lock()
	izProfilya := s.kartina.TunImya
	s.zamok.Unlock()

	podbor := tunnel.SvobodnoeImya(izProfilya, s.srokOsvobozhdeniya(), s.adapterZhivDlyaStenda)
	if slova := podbor.Slovami(); slova != "" {
		log.Printf("%s", slova)
	}
	switch {
	case podbor.Ishodnoe == "":
		// Профиль не назвал interface_name — имя выберет само ядро.
		return vybor, nil
	case podbor.Vyshlo() && !podbor.Zamena:
		// Обычный путь: имя из профиля свободно.
		return vybor, nil
	case podbor.Vyshlo():
		novyy := vybor
		novyy.TunImya = podbor.Imya
		// Адрес двигаем тем же сдвигом, что и имя (замер 06.09: «set ipv4
		// address: The object already exists» на подобранном имени).
		novyy.TunSdvigAdresa = tunnel.Sdvig(podbor.Ishodnoe, podbor.Imya)
		if err := s.perestroit(novyy); err != nil {
			// Конфиг со свободным именем не собрался — идём как шли: пусть
			// ядро попробует занятое имя (вдруг поднимется).
			log.Printf("конфиг со свободным именем адаптера %q не собрался (%v) — пробую с прежним", podbor.Imya, err)
			_ = s.perestroit(vybor)
			return vybor, nil
		}
		return novyy, nil
	default:
		log.Printf("свободного имени сетевого адаптера нет (%s) — подключение не начато",
			strings.Join(podbor.Zanyaty, ", "))
		return vybor, otkazPodyoma{OshibkaAdapterZanyat,
			fmt.Errorf("заняты имена адаптера: %s", strings.Join(podbor.Zanyaty, ", "))}
	}
}

// zakrepitTunnel — всё, что приложение делает в системе после того, как
// туннель ДЕЙСТВИТЕЛЬНО поднялся.
//
// Первое: убрать системный прокси, оставленный старой версией (прокси-режим
// до 0.6.77). Он указывает на порт 2412, который больше никто не слушает:
// туннель работал бы, а браузеры молчали бы все до одного. Чужой прокси
// уборка не трогает (internal/proksi.UbratSled).
//
// Второе: оставить след на диске. Жёсткую смерть процесса не переживёт ни
// один defer, и следующий запуск должен узнать, что туннель поднимали мы, и
// проверить приборно, не остался ли висеть адаптер (internal/tunnel).
func zakrepitTunnel(tunImya string) {
	proksi.UbratSled()
	tunnel.Otmetit(tunImya, os.Getpid())
	log.Printf("поднят туннель (адаптер %q)", tunImya)
}

// soobshchitTreyuProZashchitu отдаёт значку в трее, поднята ли связь.
// Подъём и опускание обязаны говорить значку одно и то же — именно так
// подсказка «Kelevra: VPN включён» и висела на опущенной защите.
func (s *Sluzhba) soobshchitTreyuProZashchitu(podnyata bool) {
	if s.MetkaZashchity == nil {
		return // хук не подключён (стенд-тесты внутри пакета) — значка нет
	}
	s.MetkaZashchity(podnyata)
}

// domaSeychas — один доверенный заход авторежима перед подъёмом защиты
// (podklyuchit): кнопка «Подключиться» обязана сама решить, дома человек
// или нет, а не поднимать VPN безусловно (жалоба 28.08: при нажатии
// «Подключиться» обстановка не определялась вовсе, а при выключенной
// защите — лишь иногда). Нарочно НЕ зависит от тумблера Nastroyki.Avtorezhim —
// тот управляет только фоновым автопереключением (avtorezhimKolbek); кнопка
// спрашивает обстановку всегда, своим собственным заходом.
//
// dovereno=true у Zahod: решение принимается по ПЕРВОМУ наблюдению, не
// дожидаясь Podtverzhdeniy заходов подряд (см. Zadvizhka.Predlozhit) — кнопку
// нажали один раз, набирать гистерезис для неё бессмысленно.
//
// Заход ограничен собственным таймаутом (по умолчанию 8с,
// avtorezhimKnopkaTaimaut): не ответили зонды — ctx истекает, DomaPoDns
// вернёт ошибку, а Avtorezhim.Zahod уже трактует её как «не дома»
// (безопасный дефолт) — неизвестность не должна оставлять человека без VPN,
// это дороже лишнего VPN дома. 8с, а не 5 — потому что бюджет обязан вмещать
// СУММУ номиналов подзондов (3с+4с=7с худший случай, см.
// KnopkaTaimautPoUmolchaniyu), иначе кнопка обрубает заход раньше, чем оба
// зонда честно доответят.
//
// Увиденная обстановка запоминается в avtorezhimKnopkaObstanovka —
// /api/sostoyanie показывает её человеку тем же полем, что и фоновый
// авторежим, даже если тумблер выключен (zametkaAvtorezhima, oblik/index.html).
// KnopkaTaimautPoUmolchaniyu — сколько всего отведено одному заходу кнопки
// «Подключиться» (domaSeychas), если avtorezhimKnopkaTaimaut не задан.
//
// Число не круглое ради круглого: авторежим гоняет подзонды ПОСЛЕДОВАТЕЛЬНО
// на общем ctx (см. Avtorezhim.Zahod — там же объяснено, почему общий ctx не
// разрывается ради «свежего бюджета» каждому зонду), поэтому этот таймаут
// обязан вмещать СУММУ их номиналов: 3с DNS + 4с трафик = 7с худшего случая,
// плюс 1с запаса. Пока было 5с, честно медленный DNS съедал остаток у
// прямого зонда и вердикт переворачивался на «не дома» без всякой вины сети
// (жалоба 28.08: при нажатии «Подключиться» дом не определялся). Сторож
// условия — TestKnopkaVmeshchaetSummuNominalovZondov.
const KnopkaTaimautPoUmolchaniyu = 8 * time.Second

// avtorezhimBoevoy собирает боевой avtorezhim.Novyy() с s.tunnelPodnyat —
// общая точка для domaSeychas (кнопка «Подключиться») и ZapustitAvtorezhim
// (фоновое слежение), чтобы KELEVRA_AVTOREZHIM_DNS (см. avtorezhimDnsAdres)
// подменяла резолвер одинаково в обоих путях, а не только в одном из них.
func (s *Sluzhba) avtorezhimBoevoy() *avtorezhim.Avtorezhim {
	a := avtorezhim.Novyy()
	a.TunnelPodnyat = s.tunnelPodnyat
	if s.avtorezhimShlyuz != "" {
		podmena := s.avtorezhimShlyuz
		a.MakiShlyuzovFunc = func() ([]string, error) {
			if podmena == "нет" {
				return nil, fmt.Errorf("номера шлюзов подменены на «нет» (KELEVRA_AVTOREZHIM_SHLYUZ)")
			}
			// Через запятую задаётся НЕСКОЛЬКО шлюзов — иначе на стенде не
			// разыграть ту самую беду 09.09, где домашний роутер был в
			// списке вторым, за Radmin VPN, и вердикт выносил не он.
			var maki []string
			for _, ch := range strings.Split(podmena, ",") {
				if ch = strings.TrimSpace(ch); ch != "" {
					maki = append(maki, ch)
				}
			}
			if len(maki) == 0 {
				return nil, fmt.Errorf("KELEVRA_AVTOREZHIM_SHLYUZ задан, но пуст")
			}
			return maki, nil
		}
	}
	if s.avtorezhimDnsAdres != "" {
		podmena := func() avtorezhim.DnsProver {
			return &avtorezhim.DnsZond{AdresResolvera: s.avtorezhimDnsAdres}
		}
		a.Dns = podmena()
		a.DnsPryamoy = func(_, _ string) avtorezhim.DnsProver { return podmena() }
	}
	return a
}

func (s *Sluzhba) domaSeychas(roditelskiy context.Context) avtorezhim.Sostoyanie {
	sobrat := s.avtorezhimDlyaKnopki
	if sobrat == nil {
		sobrat = s.avtorezhimBoevoy
	}
	taimaut := s.avtorezhimKnopkaTaimaut
	if taimaut <= 0 {
		taimaut = KnopkaTaimautPoUmolchaniyu
	}
	ctx, otmena := context.WithTimeout(roditelskiy, taimaut)
	defer otmena()
	_, _, tekushcheye := sobrat().Zahod(ctx, true, true)
	s.avtorezhimZamok.Lock()
	s.avtorezhimKnopkaObstanovka = tekushcheye
	s.avtorezhimZamok.Unlock()
	return tekushcheye
}

func (s *Sluzhba) podklyuchit(w http.ResponseWriter, r *http.Request) {
	err := s.Podklyuchit(r.Context())
	otdat(w, map[string]any{"gotovo": true}, err)
}

// Podklyuchit — то, что делает нажатие «Подключиться», без HTTP: зовут его
// ручка podklyuchit и служба Windows, когда её только что поставили по просьбе
// человека (PodklyuchitPosleUstanovki). ctx нужен только ветке «дома»
// (avtorezhimKolbek); сам подъём связи идёт с context.Background(), как у
// кнопки всегда.
func (s *Sluzhba) Podklyuchit(ctx context.Context) error {
	// Развилка по режиму, в котором человек УЖЕ был ДО этого нажатия (тумблер
	// авторежима — /api/avtorezhim, avtorezhimRuchka), а не по обстановке:
	// жалоба 28.08: определив, что человек дома, программа переставала
	// давать включить защиту вручную. Раньше (#84) каждое нажатие
	// «Подключиться» включало автомат навсегда, поэтому выйти в ручной режим кнопкой было
	// нельзя вообще — ручной режим, единожды покинутый, не возвращался.
	//
	// Эталон — мобильный AutoMode.kt.chooseManually: обстановку не
	// спрашивает вовсе (chooseAutoMode остаётся false) и ручное решение
	// держится до смены сети; гашение дома живёт ТОЛЬКО в авторежиме.
	s.avtorezhimZamok.Lock()
	avtorezhimVklyuchen := s.Nastroyki.Avtorezhim
	ruchnoyVybor := s.Nastroyki.RuchnoyVybor
	s.avtorezhimZamok.Unlock()
	// Заказанная схема (27.08): нажатие «Подключить» → программа сама
	// определяет обстановку → дома «режим ожидания», вне дома включается.
	// Одно нажатие — и дальше
	// решает клиент. Поэтому автомат тут по умолчанию ВКЛЮЧАЕТСЯ сам, а не
	// ждёт, пока человек найдёт отдельный тумблер: пока он тумблер не трогал
	// (RuchnoyVybor == false), нажатие «Подключиться» — это просьба «сделай
	// как надо», а не «подними туннель что бы ни было».
	//
	// Ручной режим при этом не отнят и отнят быть не может (регрессия #84,
	// жалоба 28.08: определив, что человек дома, программа переставала
	// давать включить защиту вручную): осознанный отказ от автомата — выключенный
	// тумблер /api/avtorezhim или нажатая «Отключить» — ставит RuchnoyVybor,
	// и с этой минуты кнопка поднимает защиту безусловно, ничего не
	// спрашивая. Тот же расклад, что Settings.autoModeEnabled (по умолчанию
	// автомат) + chooseManually() (осознанный ручной) в AutoMode.kt.
	if !avtorezhimVklyuchen && ruchnoyVybor {
		// Ручной режим: поднимаем защиту безусловно, без захода domaSeychas
		// (это лишние секунды ожидания на пустом месте) и не трогая
		// авторежим — человек сам решил не доверять автомату.
		return s.PodnyatZashchitu(context.Background())
	}

	s.avtorezhimZamok.Lock()
	pokolenieNaVhode := s.avtorezhimPokolenie
	s.avtorezhimZamok.Unlock()

	tekushcheye := s.domaSeychas(context.Background())

	// Заход обстановки занимает до KnopkaTaimautPoUmolchaniyu (8 с) — за это
	// время человек успевает нажать «Отключить». Гонка того же рода, что у
	// фонового авторежима (см. avtorezhimPokolenie): решение, начатое ДО
	// нажатия, не смеет примениться ПОСЛЕ него. Сверяемся тем же числом.
	s.avtorezhimZamok.Lock()
	peredumal := s.avtorezhimPokolenie != pokolenieNaVhode
	s.avtorezhimZamok.Unlock()
	if peredumal {
		log.Printf("«Подключиться»: пока спрашивал обстановку, человек решил иначе — защиту не трогаю")
		return nil
	}

	// Авторежим уже включён — «Подключиться» лишь подтверждает его тем же
	// заходом обстановки, каким живёт сам автомат (nachalo), см. комментарий
	// у vklyuchitAvtorezhim выше. Ошибку сохранения настройки только
	// логируем: сам факт подключения (или ожидания дома) важнее для
	// человека, чем то, переживёт ли автомат перезапуск — но молчать про
	// беду тоже нельзя.
	if err := s.vklyuchitAvtorezhim(tekushcheye); err != nil {
		log.Printf("«Подключиться»: не включил автоматический авторежим: %v", err)
	}
	if tekushcheye == avtorezhim.Doma {
		log.Printf("«Подключиться»: обстановка «дома» — защиту не поднимаю, обход блокировок уже делает роутер; автомат включён и сам поднимет её, когда обстановка сменится")
		// Если защита СЕЙЧАС поднята (прошлый сеанс, автоподключение при
		// запуске, нажатие руками) — опускаем прямо здесь, не дожидаясь
		// первого захода служителя. Человек только что нажал кнопку и вправе
		// увидеть ответ немедленно, а не через страховочный тикер. Вызов
		// идемпотентный (avtorezhim.Nuzhno вернёт NeTrogat, если опускать
		// нечего), поэтому обычный путь «дома, защиты нет» он не трогает.
		s.avtorezhimKolbek(ctx, avtorezhim.Doma, true)
		return nil
	}
	return s.PodnyatZashchitu(context.Background())
}

// OpustitZashchitu останавливает ядро. Общее тело для ручки «Отключить»
// (otklyuchit) и для авторежима (когда обстановка стала «дома»). Попутно
// убирает системный прокси, если его оставила старая версия (snyatProksi):
// чужой прокси уборка не трогает.
func (s *Sluzhba) OpustitZashchitu() error {
	err := s.Yadro.Ostanovit()
	s.zapomnitOtkaz(nil)
	s.snyatProksi()
	// След туннеля снимаем тем же безусловным движением и по той же причине:
	// ядро могло умереть само ещё до вызова, а след на диске остался бы и
	// заставил следующий запуск искать адаптер, которого давно нет.
	tunnel.UbratMetku()
	s.soobshchitTreyuProZashchitu(false)
	return err
}

// otklyuchit — «Отключить» руками. Кроме опускания защиты гасит автомат:
// иначе нажатие ничего не значит — вне дома приведение (avtorezhimKolbek)
// поднимет защиту обратно на ближайшем же заходе, и кнопка будет выглядеть
// сломанной. Ровно то же делает телефон: chooseManually() ставит
// Settings.autoModeEnabled = false, и круг перестаёт решать за человека
// (AutoMode.kt:1035, ветка round() :1177-1237).
//
// Ручной отказ запоминается на диске (RuchnoyVybor), поэтому и следующее
// нажатие «Подключиться» уже не включит автомат обратно молча — вернуть его
// можно только тумблером /api/avtorezhim, то есть тем же осознанным
// движением, каким его выключили.
func (s *Sluzhba) otklyuchit(w http.ResponseWriter, r *http.Request) {
	log.Printf("человек нажал «Отключить»")

	s.avtorezhimZamok.Lock()
	bylVklyuchen := s.Nastroyki.Avtorezhim
	if bylVklyuchen {
		s.Nastroyki.Avtorezhim = false
		s.Nastroyki.RuchnoyVybor = true
	}
	// Поколение растёт БЕЗУСЛОВНО, а не только когда автомат был включён:
	// «Отключить» — это слово человека про защиту, и любое решение, начатое
	// до него (заход авторежима, длинный заход обстановки внутри
	// «Подключиться»), обязано после него замолчать.
	s.avtorezhimPokolenie++
	nastroyki := s.Nastroyki
	s.avtorezhimZamok.Unlock()

	if bylVklyuchen {
		log.Printf("«Отключить»: человек решил сам — автомат выключаю, больше за него не решаю")
		s.OstanovitAvtorezhim()
		if err := hranenie.Sohranit(nastroyki); err != nil {
			// Настройку не сохранили — но автомат уже остановлен в этом
			// процессе, а значит нажатие сработало здесь и сейчас. Валить
			// из-за этого весь ответ нельзя: человек нажал «Отключить», и
			// главное для него — что защита опустилась.
			log.Printf("«Отключить»: не сохранил ручной выбор: %v", err)
		}
	}

	err := s.OpustitZashchitu()
	otdat(w, map[string]any{"gotovo": true}, err)
}

// ObnovitSvedeniya спрашивает сервер, что у человека с подпиской, и кладёт
// ответ туда, откуда его берёт окно.
//
// Зачем отдельным методом. Раньше сведения о подписке добывались РОВНО ОДИН
// РАЗ — в ручке приёма кода доступа (kod выше). Код вводят однажды, дальше
// приложение живёт годами и перезапускается сотни раз, и после каждого
// перезапуска сведения пустые: карточка «Подписка» честно писала «Пока
// неизвестно» и не могла написать ничего другого до повторного ввода кода.
// Замер 02.09.2026 на сервере: с боевого ПК за всю историю ноль запросов
// /info при семи выдачах конфига — ровно эта дыра.
func (s *Sluzhba) ObnovitSvedeniya(ctx context.Context) {
	if s.Nastroyki.Kod == "" && s.Nastroyki.Podpiska == "" {
		return
	}
	sv, err := s.vzyatSvedeniya(ctx)
	if err != nil {
		// Молчим не полностью: без этой строки непонятно, врёт ли окно про
		// «Пока неизвестно» или сервер правда недоступен.
		log.Printf("сведения о подписке не обновились: %v", err)
		return
	}
	s.zamok.Lock()
	s.svedeniya = sv
	s.zamok.Unlock()
	s.zapomnitDveri(sv)

	// Сервер попросил журнал — отдаём сейчас, не дожидаясь ночной посылки.
	//
	// Так разбор беды перестаёт зависеть от человека: 08.09 у двух человек
	// семьи связь легла среди рабочего дня, и единственным способом узнать
	// причину было попросить их скопировать журнал из окна — то есть дёргать
	// того, кто и так сидит без интернета. Теперь достаточно поставить
	// пометку на сервере, а клиент придёт за ней сам: он спрашивает эту
	// сводку раз в 15 минут и при каждом открытии окна.
	if sv != nil && sv.ZhurnalSrochno {
		log.Printf("сервер просит журнал сейчас — отправляю, не дожидаясь расписания")
		go s.OtpravitZhurnalySrochno(context.WithoutCancel(ctx))
	}
}

// ObnovlyatProfil перекачивает конфиг по расписанию, как это делает мобильный клиент.
func (s *Sluzhba) ObnovlyatProfil(ctx context.Context) {
	// Сведения о подписке — сразу, не дожидаясь первого тика: он придёт через
	// 15 минут, а карточка «Подписка» видна человеку с первой секунды.
	s.ObnovitSvedeniya(ctx)

	shag := time.Duration(s.Nastroyki.ObnovlyatMin) * time.Minute
	if shag < 15*time.Minute {
		shag = 15 * time.Minute
	}
	// Сетевой отказ повторяется сам, тик после сна сперва ждёт сеть
	// (povtor_seti.go): раньше тут был голый тикер, и профиль, упавший на
	// «no such host» в первую секунду после сна, ждал следующего часа.
	s.delo("обновление профиля", shag, s.obnovitProfilRaz).krutit(ctx)
}

// obnovitProfilRaz — один плановый заход за профилем и сведениями о подписке.
//
// Профиль первым: при сетевом отказе сведения упали бы на той же сети, и их
// строка в журнале (с адресом подписки) лишь удвоила бы строку повтора.
func (s *Sluzhba) obnovitProfilRaz(ctx context.Context) error {
	if s.Nastroyki.Kod == "" && s.Nastroyki.Podpiska == "" {
		return nil
	}
	k, err := s.vzyatProfil(ctx)
	if err != nil {
		if setevoyOtkaz(err) {
			return err // строку о повторе пишет fonovoeDelo; работаем на прежнем конфиге
		}
		log.Printf("плановое обновление профиля не удалось: %v", err)
	} else if err := s.SohranitProfil(k); err != nil {
		log.Printf("плановое обновление профиля не сохранилось: %v", err)
	}
	// Подписка едет вместе с профилем: срок, объём и список устройств
	// меняются на сервере сами по себе, без участия этой машины.
	s.ObnovitSvedeniya(ctx)
	return err
}

// ShagUchetaTrafika — как часто спрашивать у ядра его счётчики.
//
// Минута — не про точность, а про цену промаха. Счётчик ядра живёт в самом
// ядре и умирает вместе с ним, поэтому недоспрошенным остаётся ровно то, что
// прошло между последним опросом и падением: минута расхода — потеря, которой
// не видно, час — заметная дыра в отчёте. Чаще минуты незачем: каждый
// изменившийся тик пишет файл настроек на диск.
var ShagUchetaTrafika = time.Minute

// SchitatTrafik крутит учёт расхода по расписанию, пока живёт служба — тем же
// ctx, что ObnovlyatProfil и SleditZaZhurnalami.
//
// Зачем это вообще есть. Сервер считает трафик по КЛЮЧУ доступа, а под одним
// ключом ходят и телефон, и компьютер: сколько скачал каждый, по серверной
// цифре не узнать никак. Различить может только само устройство — оно и
// считает, а итог уезжает заголовком X-Device-Traffic на каждом запросе к
// подписке (internal/podpiska).
func (s *Sluzhba) SchitatTrafik(ctx context.Context, shag time.Duration) {
	if shag <= 0 {
		shag = ShagUchetaTrafika
	}
	t := time.NewTicker(shag)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.UchestTrafik()
		}
	}
}

// UchestTrafik — один тик учёта: снять показания ядра, добавить прирост к
// итогу, закрепить итог на диске.
//
// Молчаливый возврат при любой беде — не небрежность, а требование. Ядро
// стоит девять раз из десяти (человек не держит защиту включённой круглые
// сутки), и Clash API тогда не отвечает по самой обычной причине; жаловаться
// на это в журнал раз в минуту значит утопить в шуме те записи, ради которых
// журнал и шлётся разработчику. Главное — что несостоявшийся опрос НИЧЕГО не
// меняет: ни итога, ни отметки последнего показания, ни файла на диске.
func (s *Sluzhba) UchestTrafik() {
	if s.Yadro == nil || s.Nastroyki == nil {
		return
	}
	t, err := s.Yadro.Trafik()
	if err != nil || t == nil {
		return
	}
	bylo := s.Nastroyki.TrafikUstroystva()
	// Вверх и вниз складываются в одно число: сервер держит на устройство
	// одну графу расхода, и разделение ему некуда положить.
	stalo := s.Nastroyki.UchestPokazanieYadra(t.VverhBayt + t.VnizBayt)
	if stalo == bylo {
		return // прироста нет — незачем трогать диск каждую минуту простоя
	}
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		log.Printf("расход трафика посчитан, но не сохранён: %v", err)
	}
}

// datPrava — кнопка «Дать права»: просит у Windows права администратора и
// ставит службу Windows (тот же путь, что запрос прав при первом запуске,
// cmd/kelevra: prava.PoprositIZhdat(argPostavitSluzhbu)).
//
// Эта копия сама никуда не уходит: служба, поднявшись, объявляет себя меткой
// в общей папке, служебный процесс в сеансе человека видит её и уступает
// место (cmd/kelevra: peredatMestoSluzhbeWindows), а окно переходит на новый
// адрес (сторож окна).
func (s *Sluzhba) datPrava(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "только POST", http.StatusMethodNotAllowed)
		return
	}
	if s.estPrava() {
		otdat(w, map[string]any{"gotovo": true}, nil)
		return
	}
	if s.PostavitSluzhbuWindows == nil {
		otdat(w, nil, fmt.Errorf("Служба Kelevra устанавливается только в Windows."))
		return
	}
	log.Printf("«Дать права»: прошу права администратора и ставлю службу Windows")
	if err := s.PostavitSluzhbuWindows(); err != nil {
		log.Printf("«Дать права»: служба не поставлена: %v", err)
		otdat(w, nil, OshibkaNetPrav)
		return
	}
	log.Printf("«Дать права»: служба Windows поставлена")
	otdat(w, map[string]any{"gotovo": true}, nil)
}

func otdat(w http.ResponseWriter, telo any, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"beda": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(telo)
}

func zametka(z string) string {
	if z == "" {
		return ""
	}
	return ", " + z
}

func sluchaynyy() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// fsPodpapki отдаёт встроенные страницы так, будто они лежат в корне.
func fsPodpapki() (fs.FS, error) { return fs.Sub(oblik, "oblik") }

// SkazatChtoPosleObnovleniya помечает, что окно этой копии открыто сразу после
// установки новой версии, и один раз показывает человеку разъяснение.
//
// Зачем метод, а не поле в конструкторе: копию после обновления поднимает
// main (cmd/kelevra: zapustitSmenuPosleObnovleniya → --tiho --smena), а
// служба к этому моменту уже собрана. Пусто — обычный запуск, ничего не
// показываем.
func (s *Sluzhba) SkazatChtoPosleObnovleniya(versiya string) {
	s.zamok.Lock()
	s.posleObnovleniya = versiya
	s.zamok.Unlock()
}

// posleObnovleniyaRuchka — окно новой копии говорит службе: этот запуск сразу
// после установки версии, покажи человеку разъяснение.
//
// Ручкой, а не полем при сборке: окно и служба — РАЗНЫЕ процессы (замер с
// машины человека: «окно 13924» и «служба 13664» пишут в один журнал), и
// узнаёт про обновление именно окно — оно поднято сменой с --tiho --smena.
// Служба к этому моменту уже работает и своей версии смены не видела.
//
// Версия берётся своя, а не из запроса: врать про чужой номер тут нечему, а
// принимать его снаружи значит пускать в окно человека чужую строку.
func (s *Sluzhba) posleObnovleniyaRuchka(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "только POST", http.StatusMethodNotAllowed)
		return
	}
	s.SkazatChtoPosleObnovleniya(podpiska.Versiya)
	log.Printf("окно сообщило: запуск после обновления — покажу разъяснение один раз")
	w.WriteHeader(http.StatusOK)
}

// snyatProksi — уборка системного прокси, оставленного старой версией, через
// точку подмены стенда. В бою это ровно proksi.UbratSled(); в проверках —
// счётчик вызовов.
func (s *Sluzhba) snyatProksi() {
	if s.snyatProksiDlyaStenda != nil {
		s.snyatProksiDlyaStenda()
		return
	}
	proksi.UbratSled()
}

// OtpravitZhurnalySrochno шлёт журнал вне расписания — когда человеку уже
// плохо и ждать ночи нельзя.
//
// Отдельно от OtpravitZhurnalyEsliPora, а не флагом внутри неё: та отвечает
// на вопрос «пора ли», и ответ «нет» у неё правильный. Здесь вопрос другой —
// «случилась беда, о которой надо рассказать сейчас», и расписание к нему
// отношения не имеет.
//
// Защита от повторов остаётся общая: zhurnalyIdut не даёт двум отправкам
// наложиться, а отметка об удаче не даёт ночной посылке уйти второй раз с
// теми же байтами.
func (s *Sluzhba) OtpravitZhurnalySrochno(ctx context.Context) {
	s.zamok.Lock()
	if s.zhurnalyIdut {
		s.zamok.Unlock()
		return
	}
	s.zhurnalyIdut = true
	otpravshchik := s.otpravshchikZhurnalovDlyaStenda
	s.zamok.Unlock()
	defer func() {
		s.zamok.Lock()
		s.zhurnalyIdut = false
		s.zamok.Unlock()
	}()
	if otpravshchik == nil {
		otpravshchik = s.otpravshchikZhurnalov()
	}
	otchet, err := otpravshchik.Otpravit(ctx)
	if err != nil {
		log.Printf("срочная отправка журнала не удалась: %v — уйдёт по расписанию", err)
		return
	}
	log.Printf("срочная отправка журнала после неудачи связи: кусков %d, сырых %d Б, сжато %d Б",
		len(otchet.Kuski), otchet.SyrykhBayt, otchet.SzhatoBayt)
	s.Nastroyki.OtmetitOtpravkuZhurnalov(time.Now().Unix())
}

// tegiPravil — теги наборов правил боевого профиля, взятые из него самого.
//
// Из профиля, а не из зашитого списка: наборы добавляют на сервере, и любой
// список в приложении разъедется молча. Ровно так 08.09 и вышло со встроенным
// комплектом — в профиле стало 23 набора, в комплекте осталось 22, и вся
// подстраховка отказалась применяться целиком.
func (s *Sluzhba) tegiPravil() []string {
	syroy, err := os.ReadFile(hranenie.PutProfilya())
	if err != nil {
		return nil
	}
	adresa := konfig.AdresaPravil(syroy)
	tegi := make([]string, 0, len(adresa))
	for teg := range adresa {
		tegi = append(tegi, teg)
	}
	return tegi
}

// ShagSverkiPravil — как часто служба сверяет наборы правил с сервером.
//
// Полчаса, а решает всё равно pravila.Ustarel (час): тик чаще срока нужен,
// чтобы час не превращался в два из-за того, что первый тик пришёл на минуту
// раньше срока и промолчал.
var ShagSverkiPravil = 30 * time.Minute

// SveryatPravila — часы обновления правил, живут столько же, сколько служба.
//
// ЗАЧЕМ ОТДЕЛЬНЫЕ ЧАСЫ. До 09.09 обновление кеша звалось РОВНО В ОДНОМ месте
// — сразу после удачного подъёма связи. Человек, у которого машина не
// выключается, не подключался заново сутками, и правила у него не менялись
// никогда. Его слова: (цитата убрана)
// . По делу — так и было.
func (s *Sluzhba) SveryatPravila(ctx context.Context, shag time.Duration) {
	if shag <= 0 {
		shag = ShagSverkiPravil
	}
	// Первый заход — сразу, не дожидаясь тика: на свежей машине кеша ещё нет
	// вовсе, а через полчаса человек уже сто раз нажмёт «Подключить». Тот же
	// приём, что у ObnovlyatProfil со сведениями о подписке.
	// Сетевой отказ повторяется сам, тик после сна сперва ждёт сеть
	// (povtor_seti.go).
	d := s.delo("сверка правил", shag, s.ObnovitKeshPravil)
	d.srazu = true
	d.krutit(ctx)
}

// ObnovitKeshPravil сверяет наборы правил с сервером и обновляет изменившиеся.
//
// Зовётся ФОНОМ: на старте ядра сеть занята собой, и именно поэтому кеш
// вообще понадобился (шапка internal/pravila/kesh.go). Два повода: удачный
// подъём связи и свои часы (SveryatPravila).
//
// Ошибку возвращает для повтора (fonovoeDelo): сетевой отказ своими часами
// повторяется скоро и строку о себе получает там же, остальные печатаются
// здесь и ждут тика.
func (s *Sluzhba) ObnovitKeshPravil(ctx context.Context) error {
	syroy, err := os.ReadFile(hranenie.PutProfilya())
	if err != nil {
		return nil // профиля ещё нет — сверять нечего, это не отказ
	}
	adresa := konfig.AdresaPravil(syroy)
	if len(adresa) == 0 {
		return nil
	}
	tegi := make([]string, 0, len(adresa))
	for teg := range adresa {
		tegi = append(tegi, teg)
	}
	vozrast := pravila.Vozrast(hranenie.PapkaYadra(), tegi)
	if !pravila.Ustarel(hranenie.PapkaYadra(), tegi) {
		return nil
	}
	// Возраст прошлой сверки — в строке итога, а не отдельной строкой «сверяю…»
	// до похода в сеть: при сетевом отказе та строка повторялась бы на каждом
	// повторе рядом со строкой самого повтора.
	itog, err := pravila.Obnovit(ctx, &http.Client{Timeout: 2 * time.Minute}, adresa, hranenie.PapkaYadra())
	if err != nil {
		if setevoyOtkaz(err) && itog.Svereno == 0 {
			return err // ни один набор не дошёл — сети нет; строку пишет повтор
		}
		log.Printf("правила сверены не полностью: %d из %d, обновлено %d, отказов %d (%v), прошлая сверка %s назад — на старте возьмётся то, что есть",
			itog.Svereno, len(adresa), itog.Obnovleno, itog.Otkazy, err, okruglit(vozrast))
		return err
	}
	if itog.Obnovleno == 0 {
		log.Printf("правила сверены: %d наборов, на сервере ничего не менялось (прошлая сверка %s назад)", itog.Svereno, okruglit(vozrast))
		return nil
	}
	// Ядро читает наборы только при старте: свежие файлы на диске попадут в
	// работу при следующем подъёме связи. Говорим об этом прямо, чтобы
	// «обновлено 3» из журнала не читалось как «уже действует».
	log.Printf("правила сверены: %d наборов, обновилось %d — в работу пойдут при следующем подключении (прошлая сверка %s назад)", itog.Svereno, itog.Obnovleno, okruglit(vozrast))
	s.zapomnitSvezhiePravila(itog.Obnovleno)
	return nil
}

// zapomnitSvezhiePravila запоминает, что на диске появились новые наборы.
//
// В работу они пойдут при следующем подъёме связи: ядро читает наборы только
// при старте. Число нужно, чтобы человек видел в окне, что правила свежие и
// ждут, а не гадал, обновляются ли они вообще.
func (s *Sluzhba) zapomnitSvezhiePravila(skolko int) {
	s.zamok.Lock()
	s.svezhihPravil += skolko
	s.zamok.Unlock()
}

// okruglit — длительность для журнала без хвоста из наносекунд.
func okruglit(d time.Duration) time.Duration {
	if d > time.Hour {
		return d.Round(time.Minute)
	}
	return d.Round(time.Second)
}

// ustupitMestoRuchka — «на машине запустили копию новее, уходи».
//
// Зачем. Замер 08.09 с живой машины: человек обновился, файл на диске стал
// новым, новая копия запустилась — и замолчала, потому что место занято
// живой старой. Связью продолжала рулить прежняя версия, а человек был
// уверен, что работает свежая. Все правки того дня до него просто не
// доезжали, и выглядело это как «ничего не чинится».
//
// Само по себе занятое место — правильная защита (две копии дерутся за один
// сетевой адаптер, разбор в internal/kopiya). Не хватало одного: способа
// сказать старой копии, что пришла новее. Теперь он есть.
//
// Версию проверяем ЗДЕСЬ, а не доверяем вызывающему: уступать место копии
// старее себя нельзя ни при каких словах — так одно случайное нажатие на
// прошлогодний файл в Загрузках откатило бы человека назад.
func (s *Sluzhba) ustupitMestoRuchka(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "только POST", http.StatusMethodNotAllowed)
		return
	}
	chuzhaya := strings.TrimSpace(r.URL.Query().Get("versiya"))
	if chuzhaya == "" {
		http.Error(w, "не сказана версия", http.StatusBadRequest)
		return
	}
	if obnovlenie.Sravnit(chuzhaya, podpiska.Versiya) <= 0 {
		log.Printf("копия версии %s просит место, а работает %s — не уступаю", chuzhaya, podpiska.Versiya)
		http.Error(w, "пришедшая копия не новее", http.StatusConflict)
		return
	}
	log.Printf("на машине запущена версия %s (работает %s) — гашу ядро и ухожу, место за ней", chuzhaya, podpiska.Versiya)
	w.WriteHeader(http.StatusOK)
	go func() {
		// Пауза — чтобы ответ успел уйти: тот же приём, что в PostavitNaydennoe.
		time.Sleep(300 * time.Millisecond)
		_ = s.OpustitZashchitu()
		if s.vyhod != nil {
			s.vyhod()
			return
		}
		os.Exit(0)
	}()
}

// sekretClash — пароль служебного порта ядра для этого устройства (создаётся
// при первом обращении). Нет возможности его сохранить — пусто, ядро
// поднимется без пароля, как раньше; об этом один раз пишем в журнал.
var sekretClashOdinRaz sync.Once

func sekretClash() string {
	s := hranenie.SekretClash()
	if s == "" {
		sekretClashOdinRaz.Do(func() {
			log.Printf("пароль Clash API не создан (нет записи в %s): служебный порт ядра останется без пароля", hranenie.PutSekretaClash())
		})
	}
	return s
}
