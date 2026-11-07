package sluzhba

// Самоподъём: «физическая сеть есть, туннель мёртв» — перезапустить ядро.
//
// Живой случай 07.11.2026 (ноут, сеть отца, 0.6.103, ядро core-v1.14.1-10.1):
// Wi-Fi выключили в 02:27:19 и включили в 02:28:27. Ядро в 02:28:30 записало
// «updated network environment», а дальше полторы минуты раз в ~30 с
// «lookup node2kv…: context deadline exceeded» — имя своего узла не
// разрешалось, туннель молчал. Служба видела всё: «очередь узлов: ни один
// узел не отвечает», «напрямую … не отвечает, через туннель тоже», «Дом» не
// лечил — правильно, мёртв обычный туннель. Но перезапустить ядро было
// некому: сторож — про падение и зависание процесса, а процесс жил и Clash API
// отвечал. В 02:30:01 человек нажал «Отключить → Подключить» — свежее ядро
// ответило за секунду, «Дом» встал через 6 с.
//
// Эта ступень лестницы самоподъёма делает то же, что человек, но только
// когда сеть доказана без ядра:
//
//   - туннель мёртв: очередь узлов (свежий итог замера текущего узла) или
//     свой замер главной группы («Соединение») ядром не ответил;
//   - физическая сеть есть: TCP до узла напрямую через физический адаптер,
//     мимо TUN и без DNS (internal/fizset). Нет сети — диагноз «нет сети»,
//     ядро не трогаем, ждём. Сеть есть, а узлы напрямую молчат — «узлы
//     недоступны» (узел лежит, оператор режет адрес): перезапуск не лечит,
//     не трогаем;
//   - мёртв подряд при живой сети не меньше ступени лестницы
//     (lestnicaSamopodyoma: 45 с, 2 мин, 5 мин, 15 мин…; после события «сеть
//     вернулась/сменилась» первая ступень — porogPosleSmenySeti, если туннель
//     с тех пор так и не ожил), и свежий замер перед самым перезапуском тоже
//     не ответил (очередь могла как раз перейти на живой узел);
//   - тогда перезапуск через общий perezapustitPodZamkom: под общим замком
//     переходов, под временным запретом мимо туннеля, с проверкой конфига, как
//     у «Дома». Строка журнала: «сеть есть, туннель мёртв N с — перезапускаю
//     ядро».
//
// Лестница сбрасывается после sbrosSamopodyomaPosle живого туннеля и при
// слове человека или автомата (поколение авторежима). Время «мёртв» считается
// только при доказанной сети: минуты без Wi-Fi в счёт не идут, иначе ядро
// перезапускалось бы в первую же секунду после возврата сети, не дав ему
// ожить самому.
//
// С соседями не спорит: сторож поднимает упавшее и зависшее (Clash API
// молчит — здесь это «не знаю», ничего не делаем); «Дом» лечит только при
// живом туннеле (dom_tunnel.go) — после перезапуска здесь он поднимается сам
// вместе с ядром; отложенный перезапуск снимается подъёмом
// (otlozhennyyVypolnen). Занят общий замок — ждём следующего шага.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/fizset"
	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/ochered"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Диагноз канала (поле diagnoz в /api/sostoyanie и в диагностике пульта).
// Окно его не рисует: поле для журнала и пульта.
const (
	DiagnozOk             = "ok"              // туннель отвечает
	DiagnozTunnelMertv    = "tunnel_mertv"    // сеть есть (до узла напрямую есть путь), туннель молчит
	DiagnozUzlyNedostupny = "uzly_nedostupny" // сеть есть, узлы напрямую молчат: узел лежит или оператор режет
	DiagnozNetSeti        = "net_seti"        // физической сети нет
)

// Пороги самоподъёма; проверки подменяют.
var (
	// lestnicaSamopodyoma — сколько туннель мёртв подряд при живой сети
	// перед очередным перезапуском; последняя ступень повторяется.
	lestnicaSamopodyoma = []time.Duration{45 * time.Second, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute}
	// porogPosleSmenySeti — первая ступень, если сеть только что вернулась
	// или сменилась (смена сети — частый повод) и туннель с тех пор не ожил.
	porogPosleSmenySeti = 30 * time.Second
	// oknoSmenySeti — сколько после смены сети действует porogPosleSmenySeti.
	oknoSmenySeti = 5 * time.Minute
	// sbrosSamopodyomaPosle — туннель жив столько подряд — лестница сначала.
	sbrosSamopodyomaPosle = 10 * time.Minute
	// shagSamopodyoma — шаг слежки.
	shagSamopodyoma = 15 * time.Second
	// svezhestMetkiOcheredi — итог очереди узлов не старше этого заменяет свой
	// замер (очередь мерит раз в 30 с).
	svezhestMetkiOcheredi = 35 * time.Second
	// zamerZhivogoRaz — живой туннель своим замером мерим не чаще этого
	// (мёртвый — каждый шаг).
	zamerZhivogoRaz = 30 * time.Second
)

// samopodyomSost — состояние ступени самоподъёма.
type samopodyomSost struct {
	mu sync.Mutex
	// mertvS — с какого шага туннель мёртв подряд при доказанной сети.
	mertvS time.Time
	// zhivS — с какого шага туннель жив подряд.
	zhivS time.Time
	// smenaSeti — когда физическая сеть вернулась или сменилась; снимается,
	// когда туннель ожил.
	smenaSeti time.Time
	// pid — ядро, на котором идёт счёт; сменилось — счёт «мёртв» заново.
	pid string
	// stupen — перезапусков подряд в этой серии.
	stupen int
	// pokolenie — поколение авторежима при последнем перезапуске: сменилось
	// (человек или автомат сказали своё) — лестница сначала.
	pokolenie uint64
	// diagnoz — последний диагноз (Diagnoz*), пусто — не известен.
	diagnoz string
	// skazano — о каком состоянии уже сказано в журнале (без повторов).
	skazano string
	// zamerZhiv, zamerKogda — последний свой замер туннеля.
	zamerZhiv  bool
	zamerKogda time.Time

	// Подмены для проверок; nil — настоящие.
	seychas      func() time.Time
	gotov        func() bool
	pidYadra     func() string
	tunnel       func(ctx context.Context, svezhiy bool) (zhiv, izvestno bool)
	fiz          func(ctx context.Context) fizset.Rezultat
	perezapustit func(ctx context.Context, prichina string) bool
}

func (st *samopodyomSost) chasy() time.Time {
	if st.seychas != nil {
		return st.seychas()
	}
	return time.Now()
}

// Diagnoz — последний диагноз канала (Diagnoz*); пусто — не известен
// (туннель не поднят, ступень выключена, ещё не мерили).
func (s *Sluzhba) Diagnoz() string {
	st := &s.samo
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.diagnoz
}

// otmetitSmenuSeti — физическая сеть вернулась после пропажи или сменилась
// (слежка отложенного перезапуска, otlozhennyy_perezapusk.go).
func (s *Sluzhba) otmetitSmenuSeti() {
	st := &s.samo
	t := st.chasy()
	st.mu.Lock()
	st.smenaSeti = t
	st.mu.Unlock()
}

// VestiSamopodyom — слежка ступени, пока жив ctx (cmd/kelevra).
func (s *Sluzhba) VestiSamopodyom(ctx context.Context) {
	t := time.NewTicker(shagSamopodyoma)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.shagSamopodyoma(ctx)
		}
	}
}

// shagSamopodyoma — один шаг: замер, диагноз, при нужде перезапуск.
func (s *Sluzhba) shagSamopodyoma(ctx context.Context) {
	st := &s.samo
	if s.vykl(vyklSamopodyom) || !s.samopodyomGotov() {
		st.mu.Lock()
		st.mertvS, st.zhivS, st.diagnoz, st.skazano = time.Time{}, time.Time{}, "", ""
		st.mu.Unlock()
		return
	}
	pid := s.pidDlyaSamopodyoma()
	pokolenie := s.pokolenieAvtorezhima()
	st.mu.Lock()
	if pid != st.pid {
		// Новое ядро (кнопка, сторож, «Дом», этот же перезапуск): счёт
		// заново; лестница — нет, её сбрасывает только живой туннель.
		st.pid, st.mertvS, st.zhivS, st.zamerKogda = pid, time.Time{}, time.Time{}, time.Time{}
	}
	if st.stupen > 0 && pokolenie != st.pokolenie {
		st.stupen = 0
	}
	st.mu.Unlock()

	zhiv, izvestno := s.tunnelSamopodyoma(ctx, false)
	if !izvestno || ctx.Err() != nil {
		return
	}
	t := st.chasy()
	if zhiv {
		st.mu.Lock()
		st.mertvS, st.smenaSeti, st.diagnoz = time.Time{}, time.Time{}, DiagnozOk
		if st.zhivS.IsZero() {
			st.zhivS = t
		}
		sbros := st.stupen > 0 && t.Sub(st.zhivS) >= sbrosSamopodyomaPosle
		if sbros {
			st.stupen = 0
		}
		bylo := st.skazano
		st.skazano = DiagnozOk
		st.mu.Unlock()
		if bylo != "" && bylo != DiagnozOk {
			log.Printf("самоподъём: туннель ожил")
		}
		if sbros {
			log.Printf("самоподъём: туннель жив %s — лестница перезапусков сначала", sbrosSamopodyomaPosle)
		}
		return
	}

	// Туннель молчит — есть ли физическая сеть.
	r := s.fizProba(ctx)
	if ctx.Err() != nil {
		return
	}
	t = st.chasy()
	st.mu.Lock()
	st.zhivS = time.Time{}
	switch r.Itog {
	case fizset.NetSeti, fizset.UzlyNedostupny:
		d := DiagnozNetSeti
		if r.Itog == fizset.UzlyNedostupny {
			d = DiagnozUzlyNedostupny
		}
		// Без доказанной сети время «мёртв» не идёт.
		st.mertvS, st.diagnoz = time.Time{}, d
		skazat := st.skazano != d
		st.skazano = d
		st.mu.Unlock()
		if skazat {
			if d == DiagnozNetSeti {
				log.Printf("самоподъём: туннель молчит, физической сети нет (%s) — ядро не трогаю, жду сеть", r.Opisanie)
			} else {
				log.Printf("самоподъём: туннель молчит; сеть есть, но узлы напрямую не отвечают (%s) — перезапуск не поможет, ядро не трогаю", r.Opisanie)
			}
		}
		return
	}
	st.diagnoz = DiagnozTunnelMertv
	if st.mertvS.IsZero() {
		st.mertvS = t
	}
	porog, otschet := st.porogLocked(t)
	mertv, mertvVsego := t.Sub(otschet), t.Sub(st.mertvS)
	stupen := st.stupen
	skazat := st.skazano != DiagnozTunnelMertv
	st.skazano = DiagnozTunnelMertv
	st.mu.Unlock()
	if mertv < porog {
		if skazat {
			log.Printf("самоподъём: сеть есть (%s), а туннель молчит — перезапущу ядро, если не оживёт за %d с", r.Opisanie, int(porog/time.Second))
		}
		return
	}
	// Перед самым перезапуском — свежий замер: очередь узлов могла как раз
	// перейти на живой узел.
	zhiv, izvestno = s.tunnelSamopodyoma(ctx, true)
	if !izvestno || ctx.Err() != nil {
		return
	}
	if zhiv {
		st.mu.Lock()
		st.mertvS, st.diagnoz, st.skazano = time.Time{}, DiagnozOk, DiagnozOk
		st.zhivS = st.chasy()
		st.mu.Unlock()
		log.Printf("самоподъём: туннель ожил перед самым перезапуском — ядро не трогаю")
		return
	}
	prichina := fmt.Sprintf("сеть есть, туннель мёртв %d с — перезапускаю ядро (ступень %d, %s)", int(mertvVsego/time.Second), stupen+1, r.Opisanie)
	if !s.perezapustitSamopodyomom(ctx, prichina, pokolenie) {
		return
	}
	st.mu.Lock()
	st.stupen++
	st.pokolenie = pokolenie
	st.mertvS, st.smenaSeti, st.zamerKogda = time.Time{}, time.Time{}, time.Time{}
	st.pid = "" // следующий шаг увидит новое ядро и начнёт счёт с него
	st.mu.Unlock()
}

// porogLocked — ступень лестницы и от какого момента её считать. После
// смены сети на первой ступени — porogPosleSmenySeti от смены (или от начала
// «мёртв», если позже). Зовётся под st.mu.
func (st *samopodyomSost) porogLocked(t time.Time) (porog time.Duration, otschet time.Time) {
	i := st.stupen
	if i >= len(lestnicaSamopodyoma) {
		i = len(lestnicaSamopodyoma) - 1
	}
	porog, otschet = lestnicaSamopodyoma[i], st.mertvS
	if st.stupen == 0 && !st.smenaSeti.IsZero() && t.Sub(st.smenaSeti) <= oknoSmenySeti && porogPosleSmenySeti < porog {
		ot := st.smenaSeti
		if st.mertvS.After(ot) {
			ot = st.mertvS
		}
		// Сработает раньше: от смены сети (или начала «мёртв») прошло
		// porogPosleSmenySeti — это меньше, чем осталось до обычной ступени.
		if t.Sub(ot)-porogPosleSmenySeti >= t.Sub(st.mertvS)-porog {
			return porogPosleSmenySeti, ot
		}
	}
	return porog, otschet
}

// samopodyomGotov — туннель поднят, ядро работает, подъём не идёт, служба не
// уходит.
func (s *Sluzhba) samopodyomGotov() bool {
	if s.samo.gotov != nil {
		return s.samo.gotov()
	}
	if s.Yadro == nil || s.Yadro.Sost() != yadro.Rabotaet || s.uhodit.Load() {
		return false
	}
	s.zamok.Lock()
	idet := s.podyomIdet
	s.zamok.Unlock()
	return !idet
}

func (s *Sluzhba) pidDlyaSamopodyoma() string {
	if s.samo.pidYadra != nil {
		return s.samo.pidYadra()
	}
	if s.Yadro == nil {
		return ""
	}
	return s.Yadro.PID()
}

// fizProba — проба физической сети (fizset) по узлам из конфига ядра.
func (s *Sluzhba) fizProba(ctx context.Context) fizset.Rezultat {
	if s.samo.fiz != nil {
		return s.samo.fiz(ctx)
	}
	var uzly []string
	if s.Yadro != nil {
		if syroy, err := os.ReadFile(s.Yadro.PutKonfiga()); err == nil {
			uzly = fizset.CeliIzKonfiga(syroy)
		}
	}
	return fizset.Proba{Uzly: uzly}.Proverit(ctx)
}

// tunnelSamopodyoma — жив ли обычный туннель. svezhiy=false — годится
// свежий итог очереди узлов и недавний свой замер живого; true — только
// свой замер сейчас. izvestno=false — судить нечем (Clash API молчит — это
// дело сторожа; в «Соединении» не узел, а комната).
func (s *Sluzhba) tunnelSamopodyoma(ctx context.Context, svezhiy bool) (zhiv, izvestno bool) {
	st := &s.samo
	if st.tunnel != nil {
		return st.tunnel(ctx, svezhiy)
	}
	t := st.chasy()
	if !svezhiy {
		s.zamok.Lock()
		oz, ok := s.ocheredZhiv, s.ocheredKogda
		s.zamok.Unlock()
		if !ok.IsZero() && s.chasyDoma().Sub(ok) <= svezhestMetkiOcheredi {
			return oz, true
		}
		st.mu.Lock()
		nedavno := st.zamerZhiv && !st.zamerKogda.IsZero() && t.Sub(st.zamerKogda) < zamerZhivogoRaz
		st.mu.Unlock()
		if nedavno {
			return true, true
		}
	}
	if s.Yadro == nil {
		return false, false
	}
	syroy, err := os.ReadFile(s.Yadro.PutKonfiga())
	if err != nil {
		return false, false
	}
	gruppa, mozhno := glavnayaGruppa(syroy)
	if gruppa == "" {
		return false, false
	}
	vybrano, err := s.Yadro.Vybrano(gruppa)
	if err != nil || !mozhno(vybrano) {
		return false, false
	}
	zctx, otmena := context.WithTimeout(ctx, 12*time.Second)
	_, err = s.Yadro.ZameritZa(zctx, gruppa, 6*time.Second)
	otmena()
	var mimo *url.Error
	if errors.As(err, &mimo) || ctx.Err() != nil {
		return false, false
	}
	zhiv = err == nil
	st.mu.Lock()
	st.zamerZhiv, st.zamerKogda = zhiv, st.chasy()
	st.mu.Unlock()
	return zhiv, true
}

// glavnayaGruppa — группа обычного туннеля в конфиге ядра: селектор очереди
// узлов, иначе первый селектор, иначе первая группа urltest не «Дома».
// mozhno — годится ли выбранное в ней для суждения о туннеле: локальная
// комната (выход на 127.0.0.1) и direct — нет, перезапуск ядра их не лечит.
func glavnayaGruppa(syroy []byte) (gruppa string, mozhno func(vybrano string) bool) {
	var d struct {
		Outbounds []struct {
			Type   string `json:"type"`
			Tag    string `json:"tag"`
			Server string `json:"server"`
		} `json:"outbounds"`
	}
	if json.Unmarshal(syroy, &d) != nil {
		return "", nil
	}
	tipy := map[string]string{}
	servery := map[string]string{}
	var selector, urltest string
	for _, o := range d.Outbounds {
		tip := strings.ToLower(o.Type)
		tipy[o.Tag], servery[o.Tag] = tip, o.Server
		switch {
		case tip == "selector" && selector == "":
			selector = o.Tag
		case tip == "urltest" && urltest == "" && o.Tag != konfig.TegMostaDoma:
			urltest = o.Tag
		}
	}
	if g, _, ok := ochered.IzKonfiga(syroy); ok {
		gruppa = g
	} else if selector != "" {
		gruppa = selector
	} else {
		gruppa = urltest
	}
	mozhno = func(v string) bool {
		if v == "" {
			return true
		}
		switch tipy[v] {
		case "direct", "block", "dns":
			return false
		}
		sv := strings.ToLower(servery[v])
		return !(sv == "localhost" || strings.HasPrefix(sv, "127.") || sv == "::1")
	}
	return gruppa, mozhno
}

// perezapustitSamopodyomom — перезапуск ядра ступенью самоподъёма: общий
// замок переходов (занят — false, следующий шаг попробует снова), проверка
// конфига ядром (как у «Дома»), общий perezapustitPodZamkom. true — шаг
// лестницы сделан (в том числе когда ядро отвергло конфиг: молотить
// проверкой каждые 15 с незачем).
func (s *Sluzhba) perezapustitSamopodyomom(ctx context.Context, prichina string, pokolenie uint64) bool {
	if s.samo.perezapustit != nil {
		return s.samo.perezapustit(ctx, prichina)
	}
	if !s.zamokPodyoma.TryLock() {
		log.Printf("самоподъём: идёт подъём или другой перезапуск ядра — позже")
		return false
	}
	defer s.zamokPodyoma.Unlock()
	s.zamok.Lock()
	idet := s.podyomIdet
	s.zamok.Unlock()
	if s.uhodit.Load() || idet || !s.zashchitaPodnyata() || s.pokolenieAvtorezhima() != pokolenie {
		return false
	}
	if !s.konfigGodenDlyaPerezapuska("самоподъём") {
		return true
	}
	log.Printf("самоподъём: %s", prichina)
	_ = s.perezapustitPodZamkom(ctx, "самоподъём", pokolenie)
	return true
}
