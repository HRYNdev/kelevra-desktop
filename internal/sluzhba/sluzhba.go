// Пакет sluzhba: вся работа приложения, выставленная наружу как маленький
// локальный HTTP API, и интерфейс поверх него.
//
// Интерфейс — обычная страница в окне приложения. Такой стык даёт две вещи:
// окно можно проверить в браузере на сервере, где Windows нет, а логика
// проверяется без окна вовсе.
package sluzhba

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/avtozapusk"
	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/kopiya"
	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/prava"
	"github.com/HRYNdev/kelevra-desktop/internal/proksi"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

//go:embed oblik/*
var oblik embed.FS

// Sluzhba — приложение целиком, без оболочки окна.
type Sluzhba struct {
	Nastroyki *hranenie.Nastroyki
	Yadro     *yadro.Yadro
	Podpiska  *podpiska.Klient

	// Adres — свой же URL окна (с ключом). Нужен, чтобы вернуть метку копии
	// на место, если человек откажет в правах администратора.
	Adres string

	// poprositPrava — точка подмены для стенда: настоящее окно UAC на linux
	// не показать, а порядок «снять метку → спросить → вернуть при отказе»
	// проверять надо именно тут (гонка 23.08, см. polnayaZashchita).
	poprositPrava func() error
	// vyhod — как эта копия уходит после согласия на права. По умолчанию
	// os.Exit(0); на стенде подменяется, иначе тест убил бы сам себя.
	vyhod func()

	zamok      sync.Mutex
	svedeniya  *podpiska.Svedeniya
	klyuch     string
	kachaemBin bool // идёт скачивание ядра
	kartina    konfig.Kartina

	// Авторежим (переключение защиты по смене сети) живёт под своим замком,
	// отдельным от zamok выше: запуск/остановка служителя не должны ждать
	// того же замка, что держат при пересборке конфига или скачивании ядра.
	avtorezhimZamok  sync.Mutex
	avtorezhimOtmena context.CancelFunc     // не nil, пока служитель крутится
	avtorezhimEkz    *avtorezhim.Avtorezhim // тот же экземпляр — источник обстановки для /api/sostoyanie
}

// Novaya собирает службу на настоящих путях приложения.
// KELEVRA_PODPISKA и KELEVRA_SHEMA переопределяют сервер подписки — это нужно
// только для проверки приложения на стенде, у пользователя они не заданы.
func Novaya() (*Sluzhba, error) {
	n, err := hranenie.Zagruzit()
	if err != nil {
		return nil, err
	}
	if err := hranenie.Sohranit(n); err != nil { // закрепляем device_id при первом запуске
		return nil, err
	}
	s := &Sluzhba{
		Nastroyki: n,
		Yadro:     &yadro.Yadro{Bin: hranenie.PutYadra(), Papka: hranenie.PapkaYadra()},
		Podpiska:  &podpiska.Klient{DeviceID: n.DeviceID, Host: os.Getenv("KELEVRA_PODPISKA"), Shema: os.Getenv("KELEVRA_SHEMA")},
		klyuch:    sluchaynyy(),
	}
	// Профиль мог остаться с прошлого запуска: пересобираем его под нынешние
	// права, чтобы состояние в окне было правдой ещё до первого нажатия.
	_ = s.PerestroitKonfig()
	return s, nil
}

// PerestroitKonfig готовит рабочий конфиг ядра из профиля, который прислал
// сервер: убирает поля, работающие только на телефоне, и выбирает режим по
// правам. Без этого ядро на компьютере не стартует вообще.
func (s *Sluzhba) PerestroitKonfig() error { return s.perestroit(konfig.Vybor{}) }

// perestroit принимает отступления от обычной сборки конфига (dop.Prava
// подставляется тут же, по факту прав на машине — вызывающему коду задавать
// его незачем). BezSistemnogoProksi и BezSetevyhPravil в dop складываются, а
// не заменяют друг друга: PodnyatZashchitu может взвести оба по очереди на
// одном и том же подключении, и второй отказ не должен откатывать первую
// подстраховку.
func (s *Sluzhba) perestroit(dop konfig.Vybor) error {
	syroy, err := os.ReadFile(hranenie.PutProfilya())
	if err != nil {
		return err
	}
	dop.Prava = prava.Est()
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
	log.Printf("конфиг собран: режим %s, права %v, туннель в профиле %v, Clash API %s%s",
		k.Rezhim, prava.Est(), k.EstTunnel, k.ClashAdres, zametka(k.Zametka))
	return nil
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
	s.Adres = url
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
	m.HandleFunc(pref+"/api/podklyuchit", s.podklyuchit)
	m.HandleFunc(pref+"/api/otklyuchit", s.otklyuchit)
	m.HandleFunc(pref+"/api/polnaya_zashchita", s.polnayaZashchita)
	m.HandleFunc(pref+"/api/avtozapusk", s.avtozapuskRuchka)
	m.HandleFunc(pref+"/api/avtorezhim", s.avtorezhimRuchka)
	m.HandleFunc(pref+"/api/uzly", s.uzly)
	m.HandleFunc(pref+"/api/vybrat", s.vybrat)
	m.HandleFunc(pref+"/api/zamerit", s.zamerit)
	m.HandleFunc(pref+"/api/zhurnal", s.zhurnal)
	m.HandleFunc(pref+"/api/obnovlenie", s.obnovlenieRuchka)
	return m
}

// srokProverkiObnovleniya — сколько ждём ответа GitHub на нажатие «Проверить
// обновление» в окне. Короче обычного (obnovlenie идёт в фоне при старте) —
// тут человек стоит и смотрит на подпись «Проверяем…».
const srokProverkiObnovleniya = 6 * time.Second

type otvetObnovleniya struct {
	Tekushchaya string `json:"tekushchaya"`
	Novaya      string `json:"novaya,omitempty"`
	Beda        string `json:"beda,omitempty"`
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
		o.Beda = "не удалось проверить"
	} else if n != nil {
		o.Novaya = n.Versiya
	}
	otdat(w, o, nil)
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
// списка (Вова, снимок 21.08) — здесь показываем ту же конфигурацию, только
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
	// человеку (Вова, 20.08). До 23.08 её печатала шапка; на телефоне
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
	Rezhim     string `json:"rezhim,omitempty"`     // tunnel | proksi
	Zametka    string `json:"zametka,omitempty"`    // почему режим такой
	MozhnoTun  bool   `json:"mozhno_tun,omitempty"` // туннель в профиле есть, а прав нет
	Prava      bool   `json:"prava"`                // запущены ли мы администратором
	// RuchnoyProksi — система отказалась настроить прокси сама, адрес придётся вписать руками.
	RuchnoyProksi bool `json:"ruchnoy_proksi,omitempty"`
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
}

func (s *Sluzhba) sostoyanie(w http.ResponseWriter, r *http.Request) {
	o := otvetSostoyaniya{
		Versiya:  podpiska.Versiya,
		Sost:     string(s.Yadro.Sost()),
		KodEst:   s.Nastroyki.Kod != "",
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
	}
	k := s.kartina
	s.zamok.Unlock()
	o.Rezhim = string(k.Rezhim)
	// Zametka описывает ОБЪЁМ защиты («защищены только браузеры» и соседи,
	// internal/konfig/konfig.go) — правда, только пока защита реально
	// поднята (API ядра отвечает). k.kartina не чистится, когда защита
	// опущена — ни ручным тумблером (OpustitZashchitu), ни авторежимом
	// (avtorezhimKolbek), — так что без этого условия окно продолжало бы
	// врать про объём защиты, которой в этот момент нет вовсе. Заметка
	// авторежима (зачем защита опущена) — отдельный текст, живёт в JS
	// (zametkaAvtorezhima, oblik/index.html) и этой правки не касается.
	if o.Sost == string(yadro.Rabotaet) {
		o.Zametka = k.Zametka
	}
	o.Prava = prava.Est()
	o.MozhnoTun = k.EstTunnel && !o.Prava
	o.RuchnoyProksi = k.RuchnoyProksi
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
	}
	s.avtorezhimZamok.Unlock()
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
	s.Nastroyki.Avtorezhim = vhod.Vklyuchit
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		otdat(w, nil, err)
		return
	}
	if vhod.Vklyuchit {
		s.ZapustitAvtorezhim(context.Background())
	} else {
		s.OstanovitAvtorezhim()
	}
	otdat(w, map[string]any{"gotovo": true}, nil)
}

// ZapustitAvtorezhim поднимает фонового служителя авторежима (see
// internal/avtorezhim.Sluzhitel), если он ещё не крутится. Идемпотентно:
// повторный вызов, пока служитель уже работает, ничего не делает — так
// вызвать можно и из ручки /api/avtorezhim, и один раз при старте службы
// (cmd/kelevra/main.go), не заботясь, кто из них подоспел первым.
func (s *Sluzhba) ZapustitAvtorezhim(roditelskiy context.Context) {
	s.avtorezhimZamok.Lock()
	defer s.avtorezhimZamok.Unlock()
	if s.avtorezhimOtmena != nil {
		return
	}
	ctx, otmena := context.WithCancel(roditelskiy)
	s.avtorezhimOtmena = otmena
	s.avtorezhimEkz = avtorezhim.Novyy()
	s.avtorezhimEkz.TunnelPodnyat = s.tunnelPodnyat
	sluzh := &avtorezhim.Sluzhitel{
		Avtorezhim: s.avtorezhimEkz,
		Sledchik:   avtorezhim.NovySledchik(),
		Kolbek:     s.avtorezhimKolbek,
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
	s.avtorezhimOtmena()
	s.avtorezhimOtmena = nil
	s.avtorezhimEkz = nil
}

// tunnelPodnyat — стоит ли сейчас НАШ туннель на пути зондов авторежима.
// Именно туннель, а не «защита вообще»: в прокси-режиме ядро прописано
// системным прокси, а зонды авторежима ходят мимо системного прокси
// (net.Resolver и net.Dialer его не читают) — там они мерят настоящую сеть
// и слепыми не становятся.
// Зачем признак нужен — см. avtorezhim.Nablyudeniye.ZondSlep: в туннеле
// зонды видят подмену нашего же fakeip и решают «дома» где угодно.
func (s *Sluzhba) tunnelPodnyat() bool {
	if s.Yadro == nil || s.Yadro.Sost() != yadro.Rabotaet {
		return false
	}
	s.zamok.Lock()
	rezhim := s.kartina.Rezhim
	s.zamok.Unlock()
	return rezhim == konfig.Tunnel
}

// avtorezhimKolbek — что делать при реальной смене обстановки: дома —
// опустить защиту (обход уже делает роутер), вне дома — поднять (нужен
// полный туннель). Neizvestno нарочно не делает ничего — неизвестность не
// повод дёргать чужой туннель.
func (s *Sluzhba) avtorezhimKolbek(ctx context.Context, sost avtorezhim.Sostoyanie) {
	switch sost {
	case avtorezhim.Doma:
		log.Printf("авторежим: обстановка «дома» — опускаю защиту")
		if err := s.OpustitZashchitu(); err != nil {
			log.Printf("авторежим: не опустил защиту: %v", err)
		}
	case avtorezhim.VneDoma:
		log.Printf("авторежим: обстановка «вне дома» — поднимаю защиту")
		if err := s.PodnyatZashchitu(ctx); err != nil {
			log.Printf("авторежим: не поднял защиту: %v", err)
		}
	default:
		// Neizvestno — ничего не делаем нарочно, см. комментарий выше.
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
	profil, err := s.Podpiska.Konfig(ctx, kod)
	if err != nil {
		log.Printf("сервер подписки не дал профиль: %v", err)
		otdat(w, nil, err)
		return
	}
	log.Printf("профиль получен с сервера подписки: %d байт", len(profil))
	if err := s.SohranitProfil(profil); err != nil {
		otdat(w, nil, err)
		return
	}
	s.Nastroyki.Kod = kod
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		otdat(w, nil, err)
		return
	}
	if sv, err := s.Podpiska.Svedeniya(ctx, kod); err == nil {
		s.zamok.Lock()
		s.svedeniya = sv
		s.zamok.Unlock()
	}
	otdat(w, map[string]any{"gotovo": true}, nil)
}

// PodnyatZashchitu поднимает ядро, а если ядра на машине ещё нет — сначала
// приносит его сам. Общее тело для ручки «Подключить» (podklyuchit) и для
// авторежима (вызывается из avtorezhimKolbek, когда обстановка стала «вне
// дома») — раньше это было только внутри HTTP-обработчика, и авторежиму
// подключиться было некуда.
//
// ctx — основа для собственных таймаутов метода (15 минут на скачивание
// ядра, 70 секунд на его старт); HTTP-обработчик передаёт context.Background()
// — так же, как было устроено до выноса метода, чтобы поведение ручки не
// изменилось ни на йоту.
func (s *Sluzhba) PodnyatZashchitu(ctx context.Context) error {
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
	// Права могли появиться (человек перезапустил приложение администратором) —
	// пересобираем конфиг перед стартом, иначе режим останется вчерашним.
	vybor := konfig.Vybor{}
	if err := s.perestroit(vybor); err != nil {
		log.Printf("не подготовил конфиг: %v", err)
		return fmt.Errorf("не подготовил конфиг: %w", err)
	}
	zctx, otmena := context.WithTimeout(ctx, 70*time.Second)
	defer otmena()
	err := s.Yadro.Zapustit(zctx)
	// Отказ системы настроить прокси ядро считает поводом упасть. Человеку от
	// этого одна беда: связи нет вообще. Поднимаем ядро без просьбы к системе
	// и говорим адрес прокси прямо в окне.
	//
	// 23.08: сеть подстраховки не ловила ровно тот случай, ради которого её
	// сделали. На Linux ядро падает строкой «initialize system proxy», а на
	// WINDOWS — «start inbound/mixed[mixed-in]: set system proxy:
	// InternetSetOption(ProxySettingsChanged): winapi error #12009» (замер под
	// wine, stend/proksi.sh сценарий 4). Слова «initialize» там нет, и вся
	// подстраховка на Windows молча простаивала: вместо половинной защиты с
	// запиской «пропишите прокси руками» человек получал «ядро упало при
	// старте» и связь никакую. Сверяем по общей части — «system proxy».
	if err != nil && strings.Contains(err.Error(), "system proxy") {
		log.Printf("система не дала настроить прокси, поднимаю ядро без этой просьбы")
		vybor.BezSistemnogoProksi = true
		if e := s.perestroit(vybor); e == nil {
			zctx2, otmena2 := context.WithTimeout(ctx, 70*time.Second)
			defer otmena2()
			err = s.Yadro.Zapustit(zctx2)
		}
	}
	// Второй такой же отказ, найден 23.08 замером настоящего ядра
	// (.stend/sing-box-linux) на боевом профиле (22 route.rule_set, качаются
	// с subkv.chickenkiller.com detour:"direct" — мимо туннеля). Источник
	// правил жив, кеш пуст → 3.3с и порт открыт. Источник мёртв (connection
	// refused) или молчит (i/o timeout), кеш пуст → ядро НЕ открывает порт
	// вовсе, а падает целиком за 0.4с и за 5.2с соответственно строкой
	// «initialize rule-set[N]: initial rule-set: ...». Наполненный кеш беду
	// прячет (0.04с) — значит бьёт она по первому запуску и по слабой сети,
	// проваленному DNS или провайдеру, который режет домен правил. Тот же
	// приём, что и с system proxy выше: пересобираем конфиг без сетевых
	// правил (весь трафик через туннель — konfig.Vybor.BezSetevyhPravil) и
	// поднимаем ядро снова, вместо того чтобы оставить человека вовсе без связи.
	if err != nil && strings.Contains(err.Error(), "initialize rule-set") {
		log.Printf("источник правил маршрутизации недоступен, поднимаю ядро без них (весь трафик через VPN)")
		vybor.BezSetevyhPravil = true
		if e := s.perestroit(vybor); e == nil {
			zctx3, otmena3 := context.WithTimeout(ctx, 70*time.Second)
			defer otmena3()
			err = s.Yadro.Zapustit(zctx3)
		}
	}
	// Зелёный поверх пустоты. До 23.08 «ядро поднялось без ошибки» само по
	// себе считалось доказательством, что системный прокси в реестре стоит:
	// проверка Stoit/Postavit висела ТОЛЬКО внутри ветки-подстраховки, то
	// есть срабатывала лишь когда ядро упало ГРОМКО, строкой «system proxy».
	// Молчаливый отказ — ядро стартовало, порт слушает, а записи в реестре
	// нет или она не наша — не ловил никто: окно показывало зелёную защиту,
	// метка «прокси поставили мы» писалась по предположению, а трафик шёл
	// мимо. Проверяем не молчание ядра, а сам реестр, и на любом успешном
	// подъёме, а не только после известной поломки.
	if err == nil {
		s.zamok.Lock()
		proksiRezhim := s.kartina.Rezhim == konfig.Proksi && s.kartina.ProksiAdres != ""
		adres, estTunnel := s.kartina.ProksiAdres, s.kartina.EstTunnel
		s.zamok.Unlock()
		if proksiRezhim {
			stoit := proksi.Stoit(adres)
			if !stoit {
				// Ядро либо не просили (BezSistemnogoProksi), либо оно
				// промолчало. Тот же ключ реестра от лица своего процесса
				// пишут рабочие VPN-клиенты — пробуем сами.
				stoit = proksi.Postavit(adres)
			}
			s.zamok.Lock()
			s.kartina.RuchnoyProksi = !stoit
			if stoit {
				s.kartina.Zametka = konfig.ZametkaProksiRezhima(estTunnel)
			} else {
				s.kartina.Zametka = fmt.Sprintf(konfig.ZametkaRuchnoyProksi, adres)
			}
			s.zamok.Unlock()
			log.Printf("системный прокси в реестре: стоит=%v (адрес %s)", stoit, adres)
		}
	}
	if err != nil {
		// Ядро могло прописать системный прокси ещё до того, как упало или
		// не ответило за 70 секунд (падение при старте, таймаут API) — запись
		// в реестре остаётся висеть, а Ostanovit() её не снимает. Тот же баг,
		// что был на «Отключить» и закрытии окна (Вова, 20.08), только на
		// неудачном подключении.
		proksi.Snyat()
	} else {
		// Узел, выбранный в окне ДО этого нажатия (или на прошлом сеансе),
		// применяем прямо сейчас: раньше выбор до подключения был декорацией —
		// список стоял пустым, а нажать было нечего.
		s.primenitSohranennyeUzly()

		// Метка на диске: успешный подъём защиты означает, что системный
		// прокси на s.kartina.ProksiAdres реально стоит в реестре — его
		// поставило либо само ядро (обычный путь), либо мы сами чуть выше
		// (проверка Stoit/Postavit). Жёсткая смерть процесса (Диспетчер
		// задач, выключение питания) не даёт службе снять его штатно —
		// метка переживает эту смерть и даёт следующему запуску окна
		// (cmd/kelevra/main.go) снять чужой для него, но доказанно наш
		// прокси самому. RuchnoyProksi=true — противоположный случай: адрес
		// в реестре не стоит, человеку показана записка «впишите руками»,
		// снимать нечего.
		s.zamok.Lock()
		adres, pishemMetku := s.kartina.ProksiAdres, s.kartina.Rezhim == konfig.Proksi && !s.kartina.RuchnoyProksi && s.kartina.ProksiAdres != ""
		s.zamok.Unlock()
		if pishemMetku {
			proksi.Otmetit(adres)
		}
	}
	return err
}

func (s *Sluzhba) podklyuchit(w http.ResponseWriter, r *http.Request) {
	err := s.PodnyatZashchitu(context.Background())
	otdat(w, map[string]any{"gotovo": true}, err)
}

// OpustitZashchitu останавливает ядро и снимает системный прокси. Общее тело
// для ручки «Отключить» (otklyuchit) и для авторежима (когда обстановка
// стала «дома»). Снимаем прокси безусловно: ядро могло умереть само ещё до
// вызова, запись в реестре всё равно висит.
func (s *Sluzhba) OpustitZashchitu() error {
	err := s.Yadro.Ostanovit()
	proksi.Snyat()
	return err
}

func (s *Sluzhba) otklyuchit(w http.ResponseWriter, r *http.Request) {
	log.Printf("человек нажал «Отключить»")
	err := s.OpustitZashchitu()
	otdat(w, map[string]any{"gotovo": true}, err)
}

// ObnovlyatProfil перекачивает конфиг по расписанию, как это делает мобильный клиент.
func (s *Sluzhba) ObnovlyatProfil(ctx context.Context) {
	shag := time.Duration(s.Nastroyki.ObnovlyatMin) * time.Minute
	if shag < 15*time.Minute {
		shag = 15 * time.Minute
	}
	t := time.NewTicker(shag)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.Nastroyki.Kod == "" {
				continue
			}
			k, err := s.Podpiska.Konfig(ctx, s.Nastroyki.Kod)
			if err != nil {
				log.Printf("плановое обновление профиля не удалось: %v", err)
				continue // сеть могла лечь; работаем на прежнем конфиге
			}
			if err := s.SohranitProfil(k); err != nil {
				log.Printf("плановое обновление профиля не сохранилось: %v", err)
			}
		}
	}
}

// polnayaZashchita просит у Windows права администратора: без них ядро не
// поднимет туннель. При согласии человека приложение перезапускается уже с
// правами, а эта копия уходит — две копии на машине не нужны.
func (s *Sluzhba) polnayaZashchita(w http.ResponseWriter, r *http.Request) {
	if prava.Est() {
		otdat(w, map[string]any{"gotovo": true}, nil)
		return
	}
	// Метка копии снимается ДО окна UAC, а не после него. ShellExecuteW
	// возвращается уже с запущенной копией, и та копия первым делом смотрит
	// метку: увидев ещё живую службу, она решает «приложение уже работает» и
	// открывает окно на адрес, который умрёт через мгновение. Раньше это
	// кончалось вторым окном-трупом, теперь (со сторожем окна) кончилось бы
	// тем, что приложение у человека просто исчезает с экрана.
	papka := hranenie.Papka()
	kopiya.Osvobodit(papka)
	if err := s.sprositPrava(); err != nil {
		// Человек нажал «Нет»: эта копия остаётся работать — значит и метка
		// обязана вернуться. Без неё следующий двойной щелчок поднял бы
		// второе ядро на уже занятые порты.
		if s.Adres != "" {
			if err := kopiya.Zanyat(papka, s.Adres, time.Now()); err != nil {
				log.Printf("метка копии не вернулась после отказа в правах: %v", err)
			}
		}
		otdat(w, nil, err)
		return
	}
	otdat(w, map[string]any{"gotovo": true, "perezapusk": true}, nil)
	log.Printf("человек согласился на права администратора, перезапускаюсь")
	go func() {
		time.Sleep(300 * time.Millisecond) // дать ответу уйти в окно
		_ = s.Yadro.Ostanovit()            // ядро старой копии гасим сами
		if s.vyhod != nil {
			s.vyhod()
			return
		}
		os.Exit(0)
	}()
}

// sprositPrava — настоящее окно UAC либо подмена со стенда.
func (s *Sluzhba) sprositPrava() error {
	if s.poprositPrava != nil {
		return s.poprositPrava()
	}
	return prava.Poprosit()
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
