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
	// perezapuski — идущие перезапуски ядра после смены блока (ждут проверки).
	perezapuski sync.WaitGroup
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
	}
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
	if !s.zashchitaPodnyata() {
		return
	}
	s.zamok.Lock()
	idet := s.podyomIdet
	s.zamok.Unlock()
	if idet {
		log.Printf("дом: связь сейчас поднимается — блок возьмёт этот подъём")
		return
	}
	pokolenie := s.pokolenieAvtorezhima()
	log.Printf("дом: вход домой сменился при поднятом туннеле — перезапускаю ядро на новом конфиге")
	_ = s.Yadro.Ostanovit()
	if s.pokolenieAvtorezhima() != pokolenie {
		log.Printf("дом: пока гасил ядро, решение о связи сменилось — не поднимаю")
		return
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		log.Printf("дом: связь после перезапуска ядра не поднялась: %v", err)
	}
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
	t := time.NewTicker(shag)
	defer t.Stop()
	for {
		s.otpravitKlyuchDomaEsliPora(ctx)
		s.sprositSvodkuDlyaDoma(ctx)
		s.proveritSvyazDoma(ctx)
		s.soglasovatNrpt()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
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
		d.svyaz = 0
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
	d.mu.Lock()
	bylo := d.svyaz
	d.svyaz = itog
	d.mu.Unlock()
	if bylo != itog {
		log.Printf("дом: %s", map[int]string{1: "на связи", -1: "нет связи"}[itog])
	}
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
