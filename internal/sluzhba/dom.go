package sluzhba

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/dom"
	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/nrpt"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Вход домой (internal/dom, internal/konfig/dom.go): ключ устройства, блок
// dom из сводки и его кеш, отправка открытого ключа мозгу, проба связи с
// домом, правило NRPT для имён .dom и строка окна «Домашняя сеть: …».
//
// Модель угроз — договор, «Безопасность»: VPS может быть захвачен целиком.
// Поэтому верится только подписанному: блок на чужой ключ не применяется,
// ключ перерождается только по подписанному отзыву в блоке (ответ двери
// «otozvan» — не повод), версия блока не откатывается, а без действующего
// блока сети и имена дома закрыты (reject), если вход домой уже бывал.
//
// Дома (авторежим «дома», туннель погашен) ничего из этого не работает и не
// нужно: имена .dom там отдаёт AdGuard, ядро не запущено, строки в окне нет.

// Строки окна. Официальный тон, без пояснений — только состояние (правила
// текстов для людей).
const (
	DomNaSvyazi        = "Домашняя сеть: подключено"
	DomNetSvyazi       = "Домашняя сеть: нет соединения"
	DomZhdetPodtverzh  = "Домашняя сеть: ожидает подтверждения"
	DomChuzhoyKlyuch   = "Домашняя сеть: ключ не совпадает"
	ShagSlezhkiZaDomom = 30 * time.Second
)

// domSluzhby — состояние входа домой в службе.
type domSluzhby struct {
	klyuch *dom.Hranilishche
	kesh   *dom.Kesh
	nrpt   *nrpt.Upravlenie

	mu sync.Mutex
	// svyaz — итог последней пробы: 0 — не мерили (ядро стоит или дома в
	// конфиге нет), 1 — дом ответил, -1 — не ответил.
	svyaz int
	// chuzhoy — последний подписанный блок выдан на другой ключ.
	chuzhoy bool
	// kogdaOtpravit — раньше этого ключ повторно не слать; pauza — следующий шаг.
	kogdaOtpravit time.Time
	pauza         time.Duration
	// kogdaSvodka — раньше этого сводку ради входа домой не спрашивать;
	// svodka — как спросить (nil — ObnovitSvedeniya; подмена для проверок).
	kogdaSvodka time.Time
	svodka      func(ctx context.Context)
	// sprosheno — когда мозг последний раз ответил на ключ.
	sprosheno time.Time
	// proba — проба связи; nil — DNS-запрос к дому (probaDnsDoma). Подмена для проверок.
	proba func(ctx context.Context, server, zona string) error
	// rabotaetDlyaStenda — подмена «ядро работает» (живого ядра на стенде нет); nil в бою.
	rabotaetDlyaStenda func() bool
	// perezapuski — идущие перезапуски ядра после смены блока и самоизлечения
	// (ждут проверки).
	perezapuski sync.WaitGroup
	// Самоизлечение (lechitDom): neudachPodryad — неудачных проб подряд;
	// lechim — сброс сети ядра или перезапуск ядра идёт; kogdaLechili — когда
	// начато последнее лечение; pauzaLecheniya — раньше
	// kogdaLechili+pauzaLecheniya следующее не начинать (0 — сразу по порогу).
	// Удачная проба и сон машины паузу снимают.
	neudachPodryad int
	lechim         bool
	kogdaLechili   time.Time
	pauzaLecheniya time.Duration
	// Сброс сети ядра (sbrositSetYadra) — первый шаг лечения, до перезапуска
	// ядра. sbrosiliV — когда сбросили и ждём пробы (нулевое — не ждём);
	// neudachPosleSbrosa — неудачных проб с тех пор.
	sbrosiliV          time.Time
	neudachPosleSbrosa int
	// Сброс по смене сети (dom_smena_seti.go): sbrosPoSmeneV — когда
	// сбросили по событию (для строки «на связи после сброса»), poslSbros —
	// последний сброс любого рода (отсечка srokMezhduSbrosami).
	sbrosPoSmeneV time.Time
	poslSbros     time.Time
	// pult — Clash API ядра для сброса сети; nil — s.Yadro. Подмена для проверок.
	pult pultYadra
	// sledchik — события смены сети; nil — avtorezhim.NovySledchik.
	// otpechatokSeti — отпечаток физической сети; nil —
	// avtorezhim.OtpechatokSeti. Подмены для проверок.
	sledchik       func() avtorezhim.Sledchik
	otpechatokSeti func() string
	// seychas — часы самоизлечения; nil — time.Now. Подмена для проверок.
	seychas func() time.Time
	// perezapustit — перезапуск ядра самоизлечения; nil — perezapustitDlyaDoma.
	// Подмена для проверок.
	perezapustit func(prichina string, pokolenie uint64)
	// mostMolchitS — с какого времени выход моста «Дом» не отвечает (сети
	// нет); нулевое — отвечает или не мерили.
	mostMolchitS time.Time
	// most — жив ли выход моста и его тег; nil — mostOtvechaet. Подмена для проверок.
	most func(ctx context.Context) (zhiv bool, teg string)
}

// nachatDom — ключ, кеш блока, память и NRPT на настоящих путях; зовёт
// Novaya до первой сборки конфига. Остатки правила NRPT от прошлой жизни
// службы (падение) снимаются сразу: правило ставится заново, когда туннель
// с блоком поднят.
//
// Стенд и проверки (KELEVRA_DIR задан) NRPT машины не трогают никогда —
// тот же запрет, что у PapkaSluzhbyWindows: на машине проверяющего системная
// сеть не меняется.
func (s *Sluzhba) nachatDom() {
	d := &domSluzhby{klyuch: &dom.Hranilishche{Put: hranenie.PutKlyuchaDoma()}}
	d.kesh = &dom.Kesh{
		Put: hranenie.PutBlokaDoma(), PutPamyati: hranenie.PutPamyatiDoma(), Komu: s.Nastroyki.Slot,
		SvoyPub: func() string {
			k, err := d.klyuch.Klyuch()
			if err != nil {
				return ""
			}
			return k.Pub
		},
	}
	sys := nrpt.NovayaSistema()
	if os.Getenv("KELEVRA_DIR") != "" {
		sys = nil
	}
	d.nrpt = &nrpt.Upravlenie{Sys: sys}
	s.dom = d
	d.nrpt.UbratOstatki()
	// Память версий блока потеряна (оба файла), а вход домой на устройстве
	// был (блок на диске или подтверждённый ключ): прежним блокам больше не
	// верим — новый ключ, новое подтверждение владельцем (договор,
	// «Безопасность» п.14). До него окно — «ожидает подтверждения».
	if k, err := d.klyuch.Klyuch(); err == nil &&
		(d.kesh.PamyatPoteryana() || d.kesh.BezPamyati() && d.klyuch.Sostoyanie() == dom.SostPodtverzhden) {
		log.Printf("дом: память версий блока потеряна — рождаю новый ключ, нужен новый подтверждённый блок")
		d.kesh.NachatZanovo()
		if _, err := d.klyuch.Pererodit(k.Pub); err != nil {
			log.Printf("дом: новый ключ не родился: %v", err)
		}
	}
	if b := d.kesh.Blok(); b != nil {
		log.Printf("дом: с диска блок %s", b)
	}
}

// domDlyaSborki — вход домой для сборки конфига; nil — блока нет (конфиг
// прежний или закрытый, domZakryt) или ключа нет (не родился, не сохранился).
func (s *Sluzhba) domDlyaSborki() *konfig.Dom {
	if s.dom == nil {
		return nil
	}
	b := s.dom.kesh.Blok()
	if b == nil {
		return nil
	}
	k, err := s.dom.klyuch.Klyuch()
	if err != nil {
		log.Printf("дом: блок есть, а ключа нет (%v) — без входа домой", err)
		return nil
	}
	host, port := b.MostHostPort()
	return &konfig.Dom{
		PrivatnyyKlyuch: k.Privatnyy(),
		Adres:           b.Adres,
		UzelPub:         b.UzelPub,
		MostHost:        host,
		MostPort:        port,
		Seti:            b.Seti,
		DNS:             b.DNS,
		Zona:            b.Zona,
		MTU:             b.Mtu(),
		Portaly:         portalyDlyaKonfiga(b.Portaly),
	}
}

func portalyDlyaKonfiga(p []dom.Portal) []konfig.PortalDoma {
	var itog []konfig.PortalDoma
	for _, x := range p {
		itog = append(itog, konfig.PortalDoma{Uzel: x.Uzel, Server: x.Server, Port: x.Port})
	}
	return itog
}

// domZakryt — «закрыто по умолчанию» (договор, «Безопасность» п.5): вход
// домой бывал (принят блок или мозг сказал «Дом включён»), а действующего
// блока нет.
func (s *Sluzhba) domZakryt() bool {
	return s.dom != nil && s.dom.kesh.Blok() == nil && s.dom.kesh.Vklyuchalsya()
}

// prinyatDom — блок dom из свежей сводки /info: адресат (свой слот), подпись,
// свой ключ, версия, кеш, пересборка конфига при смене. Битая подпись, чужой
// слот или откат версии — блок не применяется, прежний остаётся. Блок на
// чужой ключ — не применяется («чужой ключ» в окне). Подписанный отзыв своего
// ключа — новый ключ. Сменился блок при поднятом туннеле — ядро
// перезапускается сразу (perepodnyatDlyaDoma): ядро читает конфиг только на
// старте, а ждать следующего подключения человеку незачем.
func (s *Sluzhba) prinyatDom(sv *podpiska.Svedeniya) {
	if s.dom == nil || sv == nil {
		return
	}
	// Слот при входе по коду не запомнен (вошли до этой версии или по
	// ссылке) — берём его из сводки: без своего слота блок не сверить.
	if s.Nastroyki.Slot == "" && sv.Ustroystvo != nil {
		if slot := strings.TrimSpace(sv.Ustroystvo.Slot); slot != "" {
			s.Nastroyki.Slot = slot
			if err := hranenie.Sohranit(s.Nastroyki); err != nil {
				log.Printf("дом: слот устройства из сводки не сохранён: %v", err)
			}
			log.Printf("дом: слот устройства из сводки — %s", slot)
		}
	}
	s.dom.kesh.UstanovitKomu(s.Nastroyki.Slot)
	itog, err := s.dom.kesh.Prinyat(sv.Dom)
	s.dom.mu.Lock()
	s.dom.chuzhoy = itog.ChuzhoyKlyuch
	s.dom.mu.Unlock()
	if err != nil {
		if itog.ChuzhoyKlyuch {
			log.Printf("дом: подписанный блок выдан на другой ключ — не применяю (держу прежний: %s)", s.dom.kesh.Blok())
		} else {
			log.Printf("дом: %v — держу прежний блок (%s)", err, s.dom.kesh.Blok())
		}
		return
	}
	b := s.dom.kesh.Blok()
	if b != nil {
		// Блок приходит только на подтверждённый ключ при включённом «Доме».
		if k, err := s.dom.klyuch.Klyuch(); err == nil {
			s.dom.klyuch.Otmetit(k.Pub, dom.SostPodtverzhden, 1)
		}
	}
	if itog.Otozvan {
		// Подписанный отзыв своего ключа: тот же ключ мозг больше не примет.
		if k, err := s.dom.klyuch.Klyuch(); err == nil {
			if _, err := s.dom.klyuch.Pererodit(k.Pub); err != nil {
				log.Printf("дом: новый ключ после отзыва не родился: %v", err)
			}
		}
		s.dom.mu.Lock()
		s.dom.kogdaOtpravit, s.dom.pauza = time.Time{}, 0
		s.dom.mu.Unlock()
	}
	if !itog.Izmenilsya {
		return
	}
	if b == nil {
		log.Printf("дом: действующего блока нет — сети и имена дома закрыты")
		// Сняли — спросить мозг про ключ сразу (состояние для окна).
		s.dom.klyuch.SbrositOtpravku()
		s.dom.mu.Lock()
		s.dom.kogdaOtpravit, s.dom.pauza = time.Time{}, 0
		s.dom.mu.Unlock()
	} else {
		log.Printf("дом: принят блок %s", b)
	}
	if err := s.PerestroitKonfig(); err != nil {
		log.Printf("дом: конфиг не пересобран: %v", err)
		return
	}
	s.soglasovatNrpt()
	s.dom.perezapuski.Add(1)
	go func() {
		defer s.dom.perezapuski.Done()
		s.perepodnyatDlyaDoma()
	}()
}

// soglasovatNrpt — правило NRPT «.<зона> → DNS дома» стоит ровно тогда, когда
// действует блок, он вписан в конфиг и туннель поднят; иначе снято.
func (s *Sluzhba) soglasovatNrpt() {
	if s.dom == nil {
		return
	}
	b := s.dom.kesh.Blok()
	s.zamok.Lock()
	vKonfige := s.kartina.Dom
	s.zamok.Unlock()
	nuzhno := b != nil && vKonfige && s.zashchitaPodnyata()
	p := nrpt.Pravilo{}
	if b != nil {
		p = nrpt.Pravilo{Zona: "." + b.Zona, Server: b.DNS}
	}
	if err := s.dom.nrpt.Soglasovat(nuzhno, p); err != nil {
		log.Printf("дом: %v", err)
	}
}

// perepodnyatDlyaDoma — блок дома появился, сменился или снят, а туннель
// поднят: ядро гасится и поднимается заново тем же подъёмом связи, что у
// кнопки (PodnyatZashchitu), уже на новом конфиге. Разрыв — секунды.
// Туннель не поднят (дома, отключено) — ничего: новый конфиг возьмёт
// следующий подъём. Подъём уже идёт — он сам собирает конфиг с блоком.
func (s *Sluzhba) perepodnyatDlyaDoma() {
	s.perezapustit("вход домой сменился при поднятом туннеле — перезапускаю ядро на новом конфиге", s.pokolenieAvtorezhima(), true)
}

// perezapustitDlyaDoma — перезапуск ядра самоизлечения. Занят подъём или
// другой перезапуск — не ждёт: тот подъём сам соберёт конфиг заново.
func (s *Sluzhba) perezapustitDlyaDoma(prichina string, pokolenie uint64) {
	s.perezapustit(prichina, pokolenie, false)
}

// perezapustit — общее тело перезапуска ядра ради входа домой (смена блока,
// самоизлечение): то же, что «Отключить → Подключить», целиком под
// zamokPodyoma, чтобы между «погасить» и «поднять» никто не поднял ядро
// рядом (двойной подъём: второй не дожидался старта, и выбранный узел не
// применялся).
//
// prichina — строка журнала; pokolenie — решение о связи на момент, когда
// перезапуск решён: сменилось (человек нажал «Отключить», автомат решил
// иначе) — ядро не трогается, а погашенное не поднимается.
//
// zhdat — что делать, если очередь занята. Смена блока ждёт: конфиг занятого
// подъёма мог быть собран ещё без нового блока. Самоизлечение не ждёт:
// решение по пробе, снятой до чужого подъёма, устарело.
func (s *Sluzhba) perezapustit(prichina string, pokolenie uint64, zhdat bool) {
	if !s.zashchitaPodnyata() {
		return
	}
	if !s.zamokPodyoma.TryLock() {
		if !zhdat {
			log.Printf("дом: связь сейчас поднимается или перезапускается — новое ядро возьмёт тот подъём")
			return
		}
		log.Printf("дом: жду, пока закончится идущий подъём связи, чтобы перезапустить ядро на новом конфиге")
		s.zamokPodyoma.Lock()
	}
	defer s.zamokPodyoma.Unlock()
	// Пока ждали очереди, человек мог отключить связь или она упала сама.
	if !s.zashchitaPodnyata() {
		return
	}
	s.zamok.Lock()
	idet := s.podyomIdet
	s.zamok.Unlock()
	if idet {
		log.Printf("дом: связь сейчас поднимается — новое ядро возьмёт этот подъём")
		return
	}
	if s.pokolenieAvtorezhima() != pokolenie {
		log.Printf("дом: решение о связи сменилось — ядро не перезапускаю")
		return
	}
	log.Printf("дом: %s", prichina)
	// От «погасить» до «туннель поднят» трафик мимо туннеля закрыт
	// (zaslon_perezapuska.go): иначе программы в эти секунды уходят напрямую.
	s.zakrytMimoTunnelya()
	_ = s.Yadro.Ostanovit()
	if s.pokolenieAvtorezhima() != pokolenie {
		log.Printf("дом: пока гасил ядро, решение о связи сменилось — не поднимаю")
		s.otkrytMimoTunnelya("решение о связи сменилось, ядро не поднимаю")
		return
	}
	err := s.podnyatZashchituPodZamkom(context.Background())
	if err != nil {
		log.Printf("дом: связь после перезапуска ядра не поднялась: %v", err)
	}
	s.otkrytMimoTunnelyaPoItogu(err)
}

// SleditZaDomom — отправка открытого ключа (пока сервер его не принял) и
// проба связи с домом, пока ядро работает с входом домой.
func (s *Sluzhba) SleditZaDomom(ctx context.Context, shag time.Duration) {
	if s.dom == nil {
		return
	}
	if shag <= 0 {
		shag = ShagSlezhkiZaDomom
	}
	// Смена физической сети — сброс сети ядра сразу, не дожидаясь проб
	// (dom_smena_seti.go).
	go s.sleditZaSmenoySeti(ctx)
	t := time.NewTicker(shag)
	defer t.Stop()
	for {
		s.otpravitKlyuchDomaEsliPora(ctx)
		s.sprositSvodkuDlyaDoma(ctx)
		s.proveritSvyazDoma(ctx)
		s.soglasovatNrpt()
		zhdu := time.Now()
		var tik time.Time
		select {
		case <-ctx.Done():
			return
		case tik = <-t.C:
		}
		// Машина спала, пока ждали тика (prosnulsya на отрезке ожидания) —
		// самоизлечение начинает счёт заново. Тик, выпавший ещё во время
		// шага, опозданием не считается: шаг сам бывает долгим (сводка,
		// ключ) как раз при плохой сети.
		if tik.Before(zhdu) {
			tik = time.Time{}
		}
		if prosnulsya(zhdu, tik, time.Now(), shag) {
			s.dom.posleSna()
		}
	}
}

// --- открытый ключ мозгу ---

// Расписание. Отправка ключа: после неудачи (в том числе 404/405/403 — дверь
// ключ не знает или не пускает) повтор по лестнице 1 → 2 → 5 → 15 минут,
// потолок 15 минут. Принятый ключ без блока dom переспрашиваем раз в
// periodSverkiKlyucha. Сводка /info ради входа домой: пока ключ ждёт
// подтверждения — раз в periodZhdetPodtverzh (блок приезжает через минуту
// после «Подтвердить»); пока блок действует или ключ принят, а «Дом»
// выключен (DomVyklyuchen) — раз в periodSledaZaBlokom (отзыв, смена блока
// или включение «Дома» доезжают за минуты, а не за час). Один запрос на шаг.
const (
	potolokOtpravkiKlyucha  = 15 * time.Minute
	periodSverkiKlyucha     = time.Hour
	periodZhdetPodtverzh    = time.Minute
	periodSledaZaBlokom     = 5 * time.Minute
	srokOtpravkiKlyuchaDoma = 30 * time.Second
	srokSvodkiDlyaDoma      = 30 * time.Second
)

// lesenkaOtpravkiKlyucha — паузы повтора отправки по порядку неудач.
var lesenkaOtpravkiKlyucha = []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, potolokOtpravkiKlyucha}

// sleduyushchayaPauza — следующая ступень лестницы после tekushchaya (0 — первая).
func sleduyushchayaPauza(tekushchaya time.Duration) time.Duration {
	for _, p := range lesenkaOtpravkiKlyucha {
		if p > tekushchaya {
			return p
		}
	}
	return potolokOtpravkiKlyucha
}

// sprositSvodkuDlyaDoma — один запрос /info, когда пора: ключ ждёт
// подтверждения (ждём блок) или блок действует (следим за отзывом и сменой).
// Сводка уходит через обычный путь (prinyatSvedeniya → prinyatDom).
func (s *Sluzhba) sprositSvodkuDlyaDoma(ctx context.Context) {
	d := s.dom
	period := time.Duration(0)
	switch {
	case d.kesh.Blok() != nil:
		period = periodSledaZaBlokom
	case d.klyuch.ZhdetPodtverzhdeniya():
		period = periodZhdetPodtverzh
	case d.klyuch.DomVyklyuchen():
		// Ключ принят, а «Дом» этому устройству выключен: владелец включит
		// его в любую минуту, и это не должно ждать periodSverkiKlyucha
		// (час) — следим тем же темпом, что за действующим блоком.
		period = periodSledaZaBlokom
	default:
		return
	}
	d.mu.Lock()
	if time.Now().Before(d.kogdaSvodka) {
		d.mu.Unlock()
		return
	}
	d.kogdaSvodka = time.Now().Add(period)
	zapros := d.svodka
	d.mu.Unlock()
	octx, otmena := context.WithTimeout(ctx, srokSvodkiDlyaDoma)
	defer otmena()
	if zapros != nil {
		zapros(octx)
		return
	}
	s.ObnovitSvedeniya(octx)
}

func (s *Sluzhba) otpravitKlyuchDomaEsliPora(ctx context.Context) {
	d := s.dom
	if _, err := d.klyuch.Klyuch(); err != nil {
		log.Printf("дом: %v", err)
		return
	}
	d.mu.Lock()
	rano := time.Now().Before(d.kogdaOtpravit)
	sverit := d.kesh.Blok() == nil && time.Since(d.sprosheno) >= periodSverkiKlyucha
	d.mu.Unlock()
	if rano || !d.klyuch.NuzhnoOtpravit() && !sverit {
		return
	}
	octx, otmena := context.WithTimeout(ctx, srokOtpravkiKlyuchaDoma)
	err := s.otpravitKlyuchDoma(octx)
	otmena()
	d.mu.Lock()
	defer d.mu.Unlock()
	if err == nil {
		d.pauza, d.kogdaOtpravit, d.sprosheno = 0, time.Time{}, time.Now()
		return
	}
	d.pauza = sleduyushchayaPauza(d.pauza)
	d.kogdaOtpravit = time.Now().Add(d.pauza)
	if !setevoyOtkaz(err) || d.pauza >= potolokOtpravkiKlyucha {
		log.Printf("дом: открытый ключ не отправлен (%s), повтор после %s", podpiska.SkrytKod(err.Error()), d.kogdaOtpravit.Format("15:04"))
	}
}

var errNetKodaDlyaDoma = errors.New("нет кода доступа: устройство вошло по ссылке")

// otpravitKlyuchDoma — POST /k/<код>/dom/klyuch на двери подписки, затем на
// остальных дверях своей сети (тот же клиент с запасным путём через туннель,
// что у остальных запросов к /k/<код>). Уходит только открытый ключ.
func (s *Sluzhba) otpravitKlyuchDoma(ctx context.Context) error {
	k, err := s.dom.klyuch.Klyuch()
	if err != nil {
		return err
	}
	klyuchUstr := podpiska.KlyuchUstroystva(s.Nastroyki.Podpiska)
	kod := strings.TrimSpace(s.Nastroyki.Kod)
	if kod == "" || kod == klyuchUstr {
		return errNetKodaDlyaDoma
	}
	var posledn error
	for _, dver := range s.dveriDlyaDoma() {
		o, err := s.Podpiska.OtpravitKlyuchDoma(ctx, dver, kod, s.Nastroyki.Slot, k.Pub, klyuchUstr)
		if err != nil {
			posledn = err
			if ctx.Err() != nil {
				break
			}
			continue
		}
		flag := 0
		if o.Dom != nil {
			flag = map[bool]int{true: 1, false: -1}[*o.Dom]
		}
		bylo := s.dom.klyuch.Sostoyanie()
		s.dom.klyuch.Otmetit(k.Pub, o.Sostoyanie, flag)
		if o.Sostoyanie != bylo {
			log.Printf("дом: дверь %s приняла открытый ключ (%s): %s", dver, k, slovoKlyucha(o.Sostoyanie))
		}
		if o.Sostoyanie == dom.SostOtozvan {
			// Голый ответ двери ключ не перерождает: его может подделать
			// захваченный VPS (договор, «Безопасность» п.3). Новый ключ —
			// только по подписанному отзыву в блоке dom (prinyatDom).
			log.Printf("дом: дверь говорит «ключ отозван» — жду подписанного отзыва в сводке")
		}
		if flag > 0 && !s.dom.kesh.Vklyuchalsya() {
			// «Дом» этому устройству включён: с этой минуты без действующего
			// блока сети и имена дома закрыты (konfig, zakrytDom).
			s.dom.kesh.OtmetitVklyuchenie()
			if err := s.PerestroitKonfig(); err != nil {
				log.Printf("дом: конфиг не пересобран: %v", err)
			}
		}
		return nil
	}
	if posledn == nil {
		posledn = fmt.Errorf("нет ни одной двери")
	}
	return posledn
}

func slovoKlyucha(sost string) string {
	switch sost {
	case dom.SostZhdet:
		return "ждёт подтверждения"
	case dom.SostPodtverzhden:
		return "подтверждён"
	case dom.SostOtozvan:
		return "отозван"
	}
	return "состояние не названо"
}

// dveriDlyaDoma — дверь, выдавшая подписку, первой, затем остальные двери
// своей сети, без повторов.
func (s *Sluzhba) dveriDlyaDoma() []string {
	var itog []string
	bylo := map[string]bool{}
	dob := func(d string) {
		d = strings.TrimSpace(d)
		if d != "" && !bylo[d] {
			bylo[d] = true
			itog = append(itog, d)
		}
	}
	if u, err := url.Parse(s.Nastroyki.Podpiska); err == nil && u.Host != "" {
		dob(u.Host)
	} else if s.Podpiska.Host != "" {
		dob(s.Podpiska.Host)
	}
	for _, d := range s.dveri() {
		dob(d)
	}
	return itog
}

// --- связь с домом ---

func (s *Sluzhba) proveritSvyazDoma(ctx context.Context) {
	d := s.dom
	s.zamok.Lock()
	vKonfige := s.kartina.Dom
	s.zamok.Unlock()
	b := d.kesh.Blok()
	rabotaet := s.Yadro != nil && s.Yadro.Sost() == yadro.Rabotaet
	if d.rabotaetDlyaStenda != nil {
		rabotaet = d.rabotaetDlyaStenda()
	}
	if b == nil || !vKonfige || !rabotaet {
		d.mu.Lock()
		d.svyaz, d.neudachPodryad = 0, 0
		d.sbrosiliV, d.neudachPosleSbrosa, d.sbrosPoSmeneV = time.Time{}, 0, time.Time{}
		d.mu.Unlock()
		return
	}
	proba := d.proba
	if proba == nil {
		proba = probaDnsDoma
	}
	itog := -1
	for i := 0; i < 2 && itog < 0; i++ {
		pctx, otmena := context.WithTimeout(ctx, 3*time.Second)
		if proba(pctx, b.DNS, b.Zona) == nil {
			itog = 1
		}
		otmena()
	}
	t := s.chasyDoma()
	d.mu.Lock()
	bylo := d.svyaz
	d.svyaz = itog
	// Последний сброс сети — лечения или по смене сети: от него и считаем,
	// за сколько дом ожил.
	sbrosili := d.sbrosiliV
	if d.sbrosPoSmeneV.After(sbrosili) {
		sbrosili = d.sbrosPoSmeneV
	}
	if itog > 0 {
		d.neudachPodryad, d.pauzaLecheniya, d.kogdaLechili, d.mostMolchitS = 0, 0, time.Time{}, time.Time{}
		d.sbrosiliV, d.neudachPosleSbrosa, d.sbrosPoSmeneV = time.Time{}, 0, time.Time{}
	} else {
		d.neudachPodryad++
		if !d.sbrosiliV.IsZero() {
			d.neudachPosleSbrosa++
		}
	}
	neudach := d.neudachPodryad
	d.mu.Unlock()
	if itog > 0 && bylo != 1 && !sbrosili.IsZero() {
		log.Printf("дом: на связи после сброса сети за %d с", int(t.Sub(sbrosili).Round(time.Second)/time.Second))
	}
	if bylo != itog {
		log.Printf("дом: %s", map[int]string{1: "на связи", -1: "нет связи"}[itog])
	}
	if neudach >= porogLecheniyaDoma {
		s.lechitDom(ctx, neudach)
	}
}

// Самоизлечение входа домой. После обрыва связи (сон, пропал Wi-Fi)
// обычный туннель оживает своей очередью узлов, а эндпоинт «Дом» в ядре —
// нет: соединение моста под WireGuard умерло молча, ошибки чтения нет, и
// bind не переподключается (замер 06.11: ни строки у эндпоинта и моста за
// полчаса). Лечение — сперва сброс сети ядра (sbrositSetYadra: Clash API
// DELETE /connections, в ядре ResetNetwork → BindUpdate у WireGuard — тот же
// путь, которым ядро само оживило «Дом» после «updated default interface»),
// tun при этом не гаснет и трафик мимо туннеля не уходит. Не ожил дом за
// probPosleSbrosa проб после сброса — перезапуск ядра (как «Отключить →
// Подключить»). Порог —
// неудачных проб подряд (шаг ShagSlezhkiZaDomom: около полутора минут).
// Первое лечение — сразу по порогу, следующие — не раньше паузы с
// прошлого, пауза растёт по лестнице до часа: умерший узел дома дёргает
// туннель не чаще раза в час. Сброс и перезапуск после него — одно
// лечение, одна ступень лестницы. Удачная проба лестницу сбрасывает.
//
// Пока не отвечает и сам выход моста (сети нет вовсе), лечение
// бесполезно: не лечим и лестницу не тратим. Мост ожил — лестница с начала,
// лечение на ближайшей неудачной пробе. Молчит мост дольше часа — лечим
// раз в час всё равно: замер моста мог и ошибиться.
const porogLecheniyaDoma = 3

// probPosleSbrosa — сколько неудачных проб после сброса сети ждать до
// перезапуска ядра (шаг ShagSlezhkiZaDomom: до минуты). WireGuard шлёт
// пакет в новое соединение сразу же (keepalive, рукопожатие раз в 5 с, сама
// проба), так что живой мост отвечает на первой же пробе.
const probPosleSbrosa = 2

// srokSbrosa — сколько ждать ответа Clash API на сброс сети ядра.
const srokSbrosa = 5 * time.Second

var lesenkaLecheniyaDoma = []time.Duration{10 * time.Minute, 20 * time.Minute, 40 * time.Minute, time.Hour}

// potolokLecheniyaDoma — верхняя ступень лестницы.
var potolokLecheniyaDoma = lesenkaLecheniyaDoma[len(lesenkaLecheniyaDoma)-1]

// sleduyushchayaPauzaLecheniya — следующая ступень после tekushchaya (0 — первая).
func sleduyushchayaPauzaLecheniya(tekushchaya time.Duration) time.Duration {
	for _, p := range lesenkaLecheniyaDoma {
		if p > tekushchaya {
			return p
		}
	}
	return potolokLecheniyaDoma
}

// lechitDom — лечение «Дома», если пора: подъём связи не идёт, прошлое
// лечение закончено, выход моста отвечает и пауза с прошлого лечения
// вышла. Сперва сброс сети ядра — сразу, в этом шаге, под zamokPodyoma (ни
// подъёма, ни перезапуска рядом). Сброс не вышел (Clash API ответил
// ошибкой или не ответил) или дом не ожил за probPosleSbrosa проб —
// перезапуск ядра в горутине под perezapuski, тем же путём, что при смене
// блока (perezapustitDlyaDoma): «Отключить», нажатое после решения, его
// отменяет.
func (s *Sluzhba) lechitDom(ctx context.Context, neudach int) {
	d := s.dom
	s.zamok.Lock()
	idet := s.podyomIdet
	s.zamok.Unlock()
	if idet {
		return
	}
	d.mu.Lock()
	lechim := d.lechim
	most := d.most
	d.mu.Unlock()
	if lechim {
		return
	}
	if most == nil {
		most = s.mostOtvechaet
	}
	zhiv, teg := most(ctx)
	t := s.chasyDoma()
	d.mu.Lock()
	zamolchal, ozhil := false, false
	if !zhiv {
		if d.mostMolchitS.IsZero() {
			// Сеть пропала после сброса — дом молчит не из-за моста под
			// WireGuard, перезапуск ядра не поможет: сброс забыт.
			d.mostMolchitS, zamolchal = t, true
			d.sbrosiliV, d.neudachPosleSbrosa = time.Time{}, 0
		}
	} else if !d.mostMolchitS.IsZero() {
		d.mostMolchitS, d.pauzaLecheniya, d.kogdaLechili, ozhil = time.Time{}, 0, time.Time{}, true
	}
	otpustit := func() {
		d.mu.Unlock()
		if ozhil {
			log.Printf("дом: мост «%s» снова отвечает — лечу без паузы", teg)
		}
	}
	// Сбросили и ждём: дом не ожил за probPosleSbrosa проб — то же лечение
	// продолжается перезапуском ядра, без новой паузы (и при молчащем мосте:
	// сброс раз в час при молчании дольше часа — тоже лечение, замер моста
	// мог ошибиться).
	if !d.sbrosiliV.IsZero() {
		posle := d.neudachPosleSbrosa
		if posle < probPosleSbrosa {
			otpustit()
			return
		}
		d.sbrosiliV, d.neudachPosleSbrosa = time.Time{}, 0
		d.lechim = true
		perezapustit := d.perezapustit
		otpustit()
		s.lechitPerezapuskom(perezapustit, fmt.Sprintf("сброс сети не помог — перезапускаю ядро (неудачных проб после сброса: %d)", posle))
		return
	}
	if !zhiv {
		ot := d.mostMolchitS
		if d.kogdaLechili.After(ot) {
			ot = d.kogdaLechili
		}
		if t.Sub(ot) < potolokLecheniyaDoma {
			d.mu.Unlock()
			if zamolchal {
				log.Printf("дом: нет связи, и мост «%s» не отвечает — не лечу, пока не вернётся сеть", teg)
			}
			return
		}
	}
	if d.pauzaLecheniya > 0 && t.Before(d.kogdaLechili.Add(d.pauzaLecheniya)) {
		otpustit()
		return
	}
	// Подъём или перезапуск ядра идёт — сбрасывать сеть ядра, которое сейчас
	// гасят или поднимают, незачем: решим на следующей пробе.
	if !s.zamokPodyoma.TryLock() {
		otpustit()
		return
	}
	d.lechim, d.kogdaLechili = true, t
	d.pauzaLecheniya = sleduyushchayaPauzaLecheniya(d.pauzaLecheniya)
	sleduyushchiy := t.Add(d.pauzaLecheniya)
	perezapustit := d.perezapustit
	otpustit()
	log.Printf("дом: нет связи (неудачных проб подряд: %d) — сбрасываю сеть ядра, туннель не трогаю; следующее лечение не раньше %s",
		neudach, sleduyushchiy.Format("15:04"))
	err := s.sbrositSetYadra()
	s.zamokPodyoma.Unlock()
	if err == nil {
		d.mu.Lock()
		d.lechim = false
		d.sbrosiliV, d.neudachPosleSbrosa, d.poslSbros = t, 0, t
		d.mu.Unlock()
		return
	}
	s.lechitPerezapuskom(perezapustit, fmt.Sprintf("сеть ядра не сбросилась (%v) — перезапускаю ядро", err))
}

// lechitPerezapuskom — перезапуск ядра самоизлечения в горутине под
// perezapuski; вызывающий уже поставил d.lechim, здесь он снимается по
// окончании. perezapustit nil — perezapustitDlyaDoma.
func (s *Sluzhba) lechitPerezapuskom(perezapustit func(prichina string, pokolenie uint64), prichina string) {
	d := s.dom
	if perezapustit == nil {
		perezapustit = s.perezapustitDlyaDoma
	}
	pokolenie := s.pokolenieAvtorezhima()
	d.perezapuski.Add(1)
	go func() {
		defer d.perezapuski.Done()
		defer func() {
			d.mu.Lock()
			d.lechim = false
			d.mu.Unlock()
		}()
		perezapustit(prichina, pokolenie)
	}()
}

// chasyDoma — часы самоизлечения (подмена d.seychas в проверках).
func (s *Sluzhba) chasyDoma() time.Time {
	if s.dom != nil && s.dom.seychas != nil {
		return s.dom.seychas()
	}
	return time.Now()
}

// pultYadra — то, чем служба сбрасывает сеть ядра: Clash API ядра
// (*yadro.Yadro) или подмена в проверках.
type pultYadra interface {
	SbrositSet(srok time.Duration) error
}

func (s *Sluzhba) pultDlyaSbrosa() pultYadra {
	s.dom.mu.Lock()
	p := s.dom.pult
	s.dom.mu.Unlock()
	if p != nil {
		return p
	}
	if s.Yadro == nil {
		return nil
	}
	return s.Yadro
}

// yadroRabotaetDlyaDoma — ядро работает (на стенде — подмена rabotaetDlyaStenda).
func (s *Sluzhba) yadroRabotaetDlyaDoma() bool {
	if s.dom != nil && s.dom.rabotaetDlyaStenda != nil {
		return s.dom.rabotaetDlyaStenda()
	}
	return s.Yadro != nil && s.Yadro.Sost() == yadro.Rabotaet
}

// sbrositSetYadra — сброс сети ядра (yadro.SbrositSet: Clash API DELETE
// /connections): ядро закрывает соединения и зовёт InterfaceUpdated у
// эндпоинтов, WireGuard «Дома» открывает соединение моста заново. Tun не
// гаснет, конфиг тот же — годится и для ядра, принятого от прошлой копии.
// Ошибка — сброса не было (ядро ответило не 2xx или не ответило за
// srokSbrosa).
func (s *Sluzhba) sbrositSetYadra() error {
	p := s.pultDlyaSbrosa()
	if p == nil {
		return errors.New("ядра нет")
	}
	return p.SbrositSet(srokSbrosa)
}

// mostOtvechaet — жив ли выход, через который идёт мост «Дом» (detour
// эндпоинта в конфиге на диске): замер ядра через этот выход, как у очереди
// узлов. Не узнать (конфига или эндпоинта нет, Clash API не ответил —
// ядро могло и зависнуть) — «жив»: тогда решает одна проба дома.
func (s *Sluzhba) mostOtvechaet(ctx context.Context) (bool, string) {
	if s.Yadro == nil {
		return true, ""
	}
	syroy, err := os.ReadFile(s.Yadro.PutKonfiga())
	if err != nil {
		return true, ""
	}
	teg := konfig.MostDomaIzKonfiga(syroy)
	if teg == "" {
		return true, ""
	}
	zctx, otmena := context.WithTimeout(ctx, 12*time.Second)
	defer otmena()
	_, err = s.Yadro.ZameritZa(zctx, teg, 6*time.Second)
	var mimo *url.Error
	if err == nil || errors.As(err, &mimo) {
		return true, teg
	}
	return false, teg
}

// posleSna — машина проснулась: сбой до сна не в счёт. Пробы считаются
// заново, а первое лечение после сна не ждёт паузы с прошлого сбоя.
func (d *domSluzhby) posleSna() {
	d.mu.Lock()
	d.neudachPodryad, d.pauzaLecheniya, d.kogdaLechili, d.mostMolchitS = 0, 0, time.Time{}, time.Time{}
	d.sbrosiliV, d.neudachPosleSbrosa = time.Time{}, 0
	d.mu.Unlock()
}

// probaDnsDoma — DNS-запрос к дому (100.77.0.1:53) отсюда, с машины: пакет
// идёт в tun, правило сетей дома ведёт его в «Дом» раньше hijack-dns
// (konfig/dom.go), и ответ приходит только от DNS узла дома через
// WireGuard. Любой ответ на наш запрос (даже «нет такого имени») — связь
// есть. TCP-соединение для этого не годится: tun ядра принимает его сам,
// ещё не дойдя до дома.
func probaDnsDoma(ctx context.Context, server, zona string) error {
	return probaDns(ctx, net.JoinHostPort(server, "53"), zona)
}

// probaDns — тело probaDnsDoma на любом адресе host:port (проверки).
func probaDns(ctx context.Context, adres, zona string) error {
	var dialer net.Dialer
	c, err := dialer.DialContext(ctx, "udp", adres)
	if err != nil {
		return err
	}
	defer c.Close()
	if srok, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(srok)
	}
	id := make([]byte, 2)
	_, _ = rand.Read(id)
	zapros := append([]byte{id[0], id[1], 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0}, imyaDns(zona)...)
	zapros = append(zapros, 0, 6, 0, 1) // SOA, IN
	if _, err := c.Write(zapros); err != nil {
		return err
	}
	buf := make([]byte, 1500)
	for {
		n, err := c.Read(buf)
		if err != nil {
			return err
		}
		if n >= 12 && buf[0] == id[0] && buf[1] == id[1] && buf[2]&0x80 != 0 {
			return nil
		}
	}
}

func imyaDns(imya string) []byte {
	var b []byte
	for _, chast := range strings.Split(strings.Trim(imya, "."), ".") {
		if chast == "" || len(chast) > 63 {
			continue
		}
		b = append(b, byte(len(chast)))
		b = append(b, chast...)
	}
	return append(b, 0)
}

// domStroka — строка окна: «Домашняя сеть: подключено / нет соединения /
// ожидает подтверждения / ключ не совпадает»;
// пусто — сказать нечего (дома нет, ядро стоит, ещё не мерили).
func (s *Sluzhba) domStroka() string {
	if s.dom == nil {
		return ""
	}
	if s.dom.kesh.Blok() != nil {
		s.dom.mu.Lock()
		svyaz := s.dom.svyaz
		s.dom.mu.Unlock()
		switch svyaz {
		case 1:
			return DomNaSvyazi
		case -1:
			return DomNetSvyazi
		}
		return ""
	}
	s.dom.mu.Lock()
	chuzhoy := s.dom.chuzhoy
	s.dom.mu.Unlock()
	if chuzhoy {
		return DomChuzhoyKlyuch
	}
	if s.dom.klyuch.ZhdetPodtverzhdeniya() {
		return DomZhdetPodtverzh
	}
	return ""
}

// domDlyaDiagnostiki — одной строкой: блок, связь, ключ.
func (s *Sluzhba) domDlyaDiagnostiki() string {
	if s.dom == nil {
		return "-"
	}
	b := s.dom.kesh.Blok()
	s.dom.mu.Lock()
	svyaz := s.dom.svyaz
	s.dom.mu.Unlock()
	blok := "net"
	if b != nil {
		blok = fmt.Sprintf("v%d", b.Versiya)
	}
	klyuch := s.dom.klyuch.Sostoyanie()
	if s.dom.klyuch.NuzhnoOtpravit() {
		klyuch = "ne_otpravlen"
	}
	return fmt.Sprintf("blok:%s svyaz:%d klyuch:%s", blok, svyaz, klyuch)
}
