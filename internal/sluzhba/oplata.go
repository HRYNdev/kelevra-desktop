package sluzhba

// Оплата подписки в окне (internal/podpiska/oplata.go, KelevraServer
// docs/OPLATA.md). Образец поведения — Android-клиент (OplataSostoyanie.kt):
//
//   - /oplata спрашивается, когда окно на экране: окно зовёт /api/oplata при
//     показе и раз в минуту (при ждущей заявке — каждые 5 секунд), служба
//     решает, пора ли (ShagOprosaOplaty: час, при ждущей заявке 30 секунд);
//   - без окна — только ради напоминаний: значок в трее зовёт /api/napominanie,
//     служба спрашивает сервер не чаще ShagOprosaOplatyFon. Своих фоновых работ
//     у службы нет;
//   - ходит тем же клиентом, что /info (s.Podpiska: заголовки устройства,
//     запасной путь через туннель);
//   - сбой сети и 429 держат прежнее; 404 — оплаты нет; не ответ оплаты —
//     адрес оплату не умеет, до перезапуска службы не спрашиваем.

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"rsc.io/qr"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

// oplataSost — состояние оплаты службы. Нулевое значение готово к работе.
type oplataSost struct {
	zamok sync.Mutex
	// info — последний ответ платящему; nil — не знаем или оплаты нет.
	info *podpiska.Oplata
	// baza — для какого адреса подписки info; kogda — когда спрашивали.
	baza  string
	kogda time.Time
	// neUmeet — адрес отдал не ответ оплаты: до перезапуска не спрашиваем.
	neUmeet string
	// otpravka — итог «Я оплатил»: "" | "idet" | "uzhe" | "net_svyazi".
	otpravka string
	// opros — один заход за раз (второй окно получает прежнее).
	opros sync.Mutex
}

// otvetOplaty — то, что видит окно.
type otvetOplaty struct {
	Est      bool   `json:"est"`
	Cena     int    `json:"cena,omitempty"`
	Ssylka   string `json:"ssylka,omitempty"`
	Nomer    string `json:"nomer,omitempty"`
	SrokUnix int64  `json:"srok_unix,omitempty"`
	// Srok — "obychnyj" | "skoro" | "konchilsya" (podpiska.SostoyanieSroka).
	Srok     string `json:"srok,omitempty"`
	Zhdet    bool   `json:"zhdet,omitempty"`
	Otpravka string `json:"otpravka,omitempty"`
	// Istoriya — подтверждённые оплаты, свежие первыми.
	Istoriya []podpiska.ZapisOplaty `json:"istoriya,omitempty"`
}

// bazaOplaty — база /oplata для нынешней подписки; "" — кода нет.
func (s *Sluzhba) bazaOplaty() string {
	if s.Podpiska == nil {
		return ""
	}
	return s.Podpiska.BazaOplaty(s.Nastroyki.Podpiska, s.Nastroyki.Kod)
}

// otvetOplatySeychas — снимок для окна.
func (s *Sluzhba) otvetOplatySeychas() otvetOplaty {
	o := &s.oplata
	o.zamok.Lock()
	defer o.zamok.Unlock()
	if o.info == nil || !o.info.Platit || o.baza != s.bazaOplaty() {
		return otvetOplaty{}
	}
	i := o.info
	return otvetOplaty{
		Est: true, Cena: i.Cena, Ssylka: i.Ssylka, Nomer: i.Nomer, SrokUnix: i.SrokUnix,
		Srok: podpiska.SostoyanieSroka(i.SrokUnix, time.Now()), Zhdet: i.Zhdet, Otpravka: o.otpravka,
		Istoriya: i.Istoriya,
	}
}

// obnovitOplatu — спросить /oplata, если пора или sila. fon — спрашивает не
// окно, а значок ради напоминаний: шаг ShagOprosaOplatyFon.
func (s *Sluzhba) obnovitOplatu(ctx context.Context, sila, fon bool) {
	o := &s.oplata
	if !o.opros.TryLock() {
		return
	}
	defer o.opros.Unlock()
	baza := s.bazaOplaty()
	o.zamok.Lock()
	if baza != o.baza {
		o.info, o.baza, o.kogda, o.otpravka = nil, baza, time.Time{}, ""
	}
	shag := podpiska.ShagOprosaOplaty(o.info != nil && o.info.Zhdet)
	if fon {
		shag = podpiska.ShagOprosaOplatyFon
	}
	pora := baza != "" && baza != o.neUmeet &&
		(sila || o.kogda.IsZero() || time.Since(o.kogda) >= shag)
	o.zamok.Unlock()
	if !pora {
		return
	}
	info, err := s.Podpiska.Oplata(ctx, baza)
	o.zamok.Lock()
	defer o.zamok.Unlock()
	if o.baza != baza {
		return // подписку сменили, пока спрашивали
	}
	var koda *podpiska.OshibkaKoda
	switch {
	case err == nil:
		o.info, o.kogda = platyashchemu(info), time.Now()
	case errors.As(err, &koda):
		o.info, o.kogda = nil, time.Now()
	case errors.Is(err, podpiska.ErrNeOplata):
		log.Printf("оплата: дверь не отвечает про оплату — не спрашиваю до перезапуска")
		o.info, o.neUmeet = nil, baza
	default:
		// Сбой сети и 429: держим прежнее, спросим при следующем заходе окна.
		log.Printf("оплата не прочитана: %s", podpiska.SkrytKod(err.Error()))
	}
}

func platyashchemu(i *podpiska.Oplata) *podpiska.Oplata {
	if i == nil || !i.Platit {
		return nil
	}
	return i
}

// oplatil — «Я оплатил»: POST /oplatil, итог — в otpravka и info.
func (s *Sluzhba) oplatil(ctx context.Context) {
	o := &s.oplata
	baza := s.bazaOplaty()
	o.zamok.Lock()
	if o.otpravka == "idet" {
		o.zamok.Unlock()
		return
	}
	o.otpravka = "idet"
	o.zamok.Unlock()

	itog := "net_svyazi"
	var info *podpiska.Oplata
	var err error
	if baza != "" {
		info, err = s.Podpiska.Oplatil(ctx, baza)
		switch {
		case err == nil:
			itog = ""
		case errors.Is(err, podpiska.ErrChasto):
			itog = "uzhe"
		}
	}
	if err != nil {
		log.Printf("«Я оплатил» не отправлено: %s", podpiska.SkrytKod(err.Error()))
	} else if baza != "" {
		log.Printf("«Я оплатил»: заявка отправлена")
	}
	o.zamok.Lock()
	defer o.zamok.Unlock()
	o.otpravka = itog
	if err == nil && baza == o.baza {
		o.info, o.kogda = platyashchemu(info), time.Now()
	}
}

// oplataRuchka — GET /api/oplata[?sila=1]: окно на экране; спросить сервер,
// если пора, и отдать состояние. ?zabyt=1 — шторка закрыта: сбросить итог
// отправки.
func (s *Sluzhba) oplataRuchka(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("zabyt") == "1" {
		s.oplata.zamok.Lock()
		if s.oplata.otpravka != "idet" {
			s.oplata.otpravka = ""
		}
		s.oplata.zamok.Unlock()
	} else {
		ctx, otmena := context.WithTimeout(r.Context(), 25*time.Second)
		s.obnovitOplatu(ctx, r.URL.Query().Get("sila") == "1", false)
		otmena()
	}
	otdat(w, s.otvetOplatySeychas(), nil)
}

// otvetNapominaniya — что значок в трее показывает пузырём; пусто — нечего.
type otvetNapominaniya struct {
	Klyuch string `json:"klyuch,omitempty"`
	Tekst  string `json:"tekst,omitempty"`
}

// napominanieSeychas — напоминание об оплате, которое ещё не показывали.
// Окно на экране — напоминание уже видно на карточке подписки: помечаем
// показанным и пузырём не дублируем (то же правило, что у пузыря обновления,
// povestitEsliNovaya).
func (s *Sluzhba) napominanieSeychas(seychas time.Time) otvetNapominaniya {
	o := &s.oplata
	o.zamok.Lock()
	var info *podpiska.Oplata
	if o.baza == s.bazaOplaty() {
		info = o.info
	}
	o.zamok.Unlock()
	klyuch, tekst := podpiska.NapominanieOplaty(info, seychas)
	if klyuch == "" || s.Nastroyki == nil || klyuch == s.Nastroyki.NapomnenoOplaty {
		return otvetNapominaniya{}
	}
	if s.oknoOtkryto() {
		s.otmetitNapominanie(klyuch)
		return otvetNapominaniya{}
	}
	return otvetNapominaniya{Klyuch: klyuch, Tekst: tekst}
}

// otmetitNapominanie — напоминание показано; на диске, чтобы перезапуск не
// повторил его.
func (s *Sluzhba) otmetitNapominanie(klyuch string) {
	if klyuch == "" || len(klyuch) > 64 || s.Nastroyki == nil || s.Nastroyki.NapomnenoOplaty == klyuch {
		return
	}
	s.Nastroyki.NapomnenoOplaty = klyuch
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		log.Printf("не сохранил отметку о напоминании об оплате: %v", err)
	}
}

// napominanieRuchka — значок в трее (cmd/kelevra/napominanie_oplaty.go):
// GET /api/napominanie — что показать (сервер спрашивается не чаще
// ShagOprosaOplatyFon); POST /api/napominanie?klyuch=… — показано.
func (s *Sluzhba) napominanieRuchka(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.otmetitNapominanie(r.URL.Query().Get("klyuch"))
		otdat(w, otvetNapominaniya{}, nil)
		return
	}
	ctx, otmena := context.WithTimeout(r.Context(), 25*time.Second)
	s.obnovitOplatu(ctx, false, true)
	otmena()
	otdat(w, s.napominanieSeychas(time.Now()), nil)
}

// oplatilRuchka — POST /api/oplatil.
func (s *Sluzhba) oplatilRuchka(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, otmena := context.WithTimeout(r.Context(), 30*time.Second)
	defer otmena()
	s.oplatil(ctx)
	otdat(w, s.otvetOplatySeychas(), nil)
}

// oplataQrRuchka — GET /api/oplata_qr: QR-код ссылки на перевод (SVG),
// собранный на месте — без сети. Ссылки нет — 404.
func (s *Sluzhba) oplataQrRuchka(w http.ResponseWriter, r *http.Request) {
	ssylka := s.otvetOplatySeychas().Ssylka
	if ssylka == "" {
		http.NotFound(w, r)
		return
	}
	svg, err := qrSVG(ssylka)
	if err != nil {
		log.Printf("оплата: QR не собрался: %v", err)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(svg))
}

// qrSVG — QR-код текста картинкой SVG: чёрные модули на белом, поле в 4 модуля.
func qrSVG(tekst string) (string, error) {
	c, err := qr.Encode(tekst, qr.M)
	if err != nil {
		return "", err
	}
	const pole = 4
	n := c.Size + 2*pole
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `)
	b.WriteString(strconv.Itoa(n) + " " + strconv.Itoa(n))
	b.WriteString(`" shape-rendering="crispEdges"><rect width="100%" height="100%" fill="#fff"/><path fill="#000" d="`)
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if c.Black(x, y) {
				b.WriteString("M" + strconv.Itoa(x+pole) + " " + strconv.Itoa(y+pole) + "h1v1h-1z")
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}
