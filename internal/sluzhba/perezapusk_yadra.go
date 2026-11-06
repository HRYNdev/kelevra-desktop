package sluzhba

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Перезапускать ли ядро, когда конфиг сменился, — одно правило на все пути.
//
// Каждый перезапуск ядра гасит TUN на 1–30 с. До 07.11.2026 решения были
// разные: ежечасный новый профиль ядро не трогал вовсе (часами на прежнем), а
// приём живого ядра при обновлении приложения на ту же разницу менял ядро
// сразу. Теперь оба пути (и вход по коду, и команда пульта obnovi_profil —
// все идут через SohranitProfil) спрашивают одно: yadro.SravnitKonfigi —
// конфиг, по которому ядро РАБОТАЕТ (снимок konfig_v_rabote.json), против
// того, с которым эта копия подняла бы его сейчас. Классификация (подробно —
// шапка internal/yadro/smysl.go):
//
//	не нужен — имя адаптера и сдвиг адресов вслед за ним, вход службы, наборы
//	           правил, которые ядро перечитывает само (тот же файл), журнал,
//	           порядок;
//	отложить — новые/убранные выходы и их параметры, вход tun (адреса,
//	           маршруты, стек, MTU), DNS, правила маршрутизации; набор,
//	           который ядро читает из встроенного комплекта, когда свежий лёг
//	           в кеш; новое ядро core-*. Ждёт естественного момента
//	           (otlozhennyy_perezapusk.go);
//	сразу    — узел или группа, на которой сидим, из профиля пропали; сменился
//	           вход домой («Дом» сам перезапускает ядро и прежним путём,
//	           prinyatDom); сменился служебный порт ядра.
//
// Перед любым перезапуском на новом конфиге ядро его проверяет
// (`sing-box check`, yadro.Proverit): отверг — рабочее ядро не трогаем,
// остаёмся на прежнем, строка в журнал. Проверить не вышло (нет файла ядра,
// срок) — перезапуск идёт как раньше: неизвестность — не приговор конфигу.

// zhelaemyyKonfig — конфиг, с которым эта копия подняла бы ядро прямо сейчас:
// правила из кеша, имя адаптера из профиля. В памяти, на диск не пишется.
func (s *Sluzhba) zhelaemyyKonfig() ([]byte, error) {
	var v konfig.Vybor
	s.dobavitPravilaIzKesha(&v, false)
	telo, _, err := s.sobratKonfig(v)
	return telo, err
}

// vybranoVYadre — что выбрано сейчас в группах живого ядра (по группам его
// же конфига). Ядро не ответило — группы нет в ответе.
func (s *Sluzhba) vybranoVYadre(rabota []byte) map[string]string {
	if s.vybranoDlyaStenda != nil {
		return s.vybranoDlyaStenda(rabota)
	}
	itog := map[string]string{}
	for _, g := range yadro.GruppyKonfiga(rabota) {
		if u, err := s.Yadro.Vybrano(g); err == nil && u != "" {
			itog[g] = u
		}
	}
	return itog
}

// reshitPoKonfigu — нужен ли перезапуск ядра, работающего по rabota, ради
// novyy. Снимка нет (ядро поднято версией до этого правила) — по отпечатку.
func (s *Sluzhba) reshitPoKonfigu(rabota yadro.KonfigRaboty, novyy []byte) yadro.Reshenie {
	if rabota.Telo == nil {
		return yadro.SravnitPoOtpechatku(rabota.Otpechatok, novyy)
	}
	return yadro.SravnitKonfigi(rabota.Telo, novyy, s.vybranoVYadre(rabota.Telo))
}

// proveritYadrom — точка подмены для проверок пакета: настоящий вызов
// запускает файл ядра, а в проверках на его месте бывает сам тестовый exe
// (опознание по образу) — его запуск с «check» гонял бы проверки по кругу.
var proveritYadrom = func(y *yadro.Yadro, ctx context.Context, telo []byte) error { return y.Proverit(ctx, telo) }

// proveritKonfig — `sing-box check` нашим ядром (или подмена стенда).
func (s *Sluzhba) proveritKonfig(ctx context.Context, telo []byte) error {
	if s.proverkaKonfiga != nil {
		return s.proverkaKonfiga(ctx, telo)
	}
	return proveritYadrom(s.Yadro, ctx, telo)
}

// konfigRabotyPriPriyome — конфиг живого ядра прошлой копии: снимок, а если
// его нет (ядро поднято версией до снимков) — config.json, каким он был до
// пересборки этой копией или каким лежит сейчас, но только если отпечаток
// сходится с запиской: тогда это тот самый конфиг.
func (s *Sluzhba) konfigRabotyPriPriyome(p yadro.Peredacha) yadro.KonfigRaboty {
	k := yadro.SnimokDlyaOtpechatka(hranenie.PapkaYadra(), p.Otpechatok)
	if k.Telo != nil || p.Otpechatok == "" {
		return k
	}
	s.zamok.Lock()
	doPeresborki := s.konfigDoPeresborki
	s.zamok.Unlock()
	kandidaty := [][]byte{doPeresborki}
	if b, err := os.ReadFile(s.Yadro.PutKonfiga()); err == nil {
		kandidaty = append(kandidaty, b)
	}
	for _, b := range kandidaty {
		if len(b) > 0 && yadro.OtpechatokKonfigaBayt(b) == p.Otpechatok {
			k.Telo = b
			break
		}
	}
	return k
}

// konfigGoden — можно ли гасить рабочее ядро ради telo. false — ядро конфиг
// отвергло: рабочее не трогать. Отвергнутый конфиг запоминается, чтобы
// ежечасный профиль не планировал тот же перезапуск снова.
func (s *Sluzhba) konfigGoden(ctx context.Context, telo []byte, zachem string) bool {
	err := s.proveritKonfig(ctx, telo)
	switch {
	case err == nil:
		return true
	case yadro.KonfigOtvergnut(err):
		log.Printf("%s: ядро не принимает новый конфиг (%v) — рабочее ядро не трогаю, остаюсь на прежнем", zachem, err)
		s.otlozh.zapomnitOtvergnutyy(yadro.OtpechatokKonfigaBayt(telo))
		return false
	default:
		log.Printf("%s: проверить новый конфиг не вышло (%v) — перезапускаю, как раньше", zachem, err)
		return true
	}
}

// konfigGodenDlyaPerezapuska — проверка конфига, на котором поднимется ядро
// после перезапуска ради «Дома» (dom.go perezapustit). Не собрался — решает
// сам подъём (он и скажет причину).
func (s *Sluzhba) konfigGodenDlyaPerezapuska(zachem string) bool {
	telo, err := s.zhelaemyyKonfig()
	if err != nil {
		return true
	}
	return s.konfigGoden(context.Background(), telo, zachem)
}

// reshenieDlyaPriyoma — менять ли живое ядро прошлой копии сразу (true) или
// принять его. Отложенный перезапуск планирует вызывающий, когда ядро
// принято (planirovatPoslePriyoma).
//
// rabota — конфиг живого ядра, если известен (снимок): по нему принявшая
// копия знает, из каких файлов ядро читает наборы правил.
func (s *Sluzhba) reshenieDlyaPriyoma(p yadro.Peredacha) (smenit bool, r yadro.Reshenie, rabota []byte) {
	novyy, err := s.zhelaemyyKonfig()
	if err != nil {
		// Посчитать не вышло — принимаем: оборвать работающую связь из-за
		// неизвестности хуже (так было и с отпечатком).
		return false, yadro.Reshenie{}, nil
	}
	k := s.konfigRabotyPriPriyome(p)
	rabota = k.Telo
	r = s.reshitPoKonfigu(k, novyy)
	log.Printf("приём ядра: перезапуск %s (%s)", r.Nuzhda, r.Slovami())
	if r.Nuzhda != yadro.Srazu {
		return false, r, rabota
	}
	if !s.konfigGoden(context.Background(), novyy, "приём ядра") {
		// Новый конфиг ядро не примет: остаёмся на живом прежнем.
		return false, yadro.Reshenie{}, rabota
	}
	return true, r, rabota
}

// pravilaIzKonfiga — тег → файл для наборов, которые ядро читает с диска
// (route.rule_set type local); nil — конфиг не разобрать.
func pravilaIzKonfiga(telo []byte) map[string]string {
	var d struct {
		Route struct {
			RuleSet []struct {
				Tag  string `json:"tag"`
				Type string `json:"type"`
				Path string `json:"path"`
			} `json:"rule_set"`
		} `json:"route"`
	}
	if len(telo) == 0 || json.Unmarshal(telo, &d) != nil {
		return nil
	}
	itog := map[string]string{}
	for _, n := range d.Route.RuleSet {
		if n.Type == "local" && n.Tag != "" && n.Path != "" {
			itog[n.Tag] = n.Path
		}
	}
	return itog
}

// planirovatPosleProyoma — ядро принято, а конфиг его устарел не срочно:
// перезапуск отложен до естественного момента.
func (s *Sluzhba) planirovatPoslePriyoma(r yadro.Reshenie) {
	if r.Nuzhda == yadro.Otlozhit {
		s.otlozhitPerezapusk(prichinaKonfig, r.Slovami(), "")
	}
}

// posleNovogoProfilya — профиль сменился (ежечасно, код, пульт) при
// работающем ядре: решение по одному правилу.
func (s *Sluzhba) posleNovogoProfilya() {
	if s.Yadro == nil {
		return
	}
	kakRabotaet := s.Yadro.KonfigVRabote
	if s.otlozh.konfigVRabote != nil {
		kakRabotaet = s.otlozh.konfigVRabote
	}
	rabota, rabotaet := kakRabotaet()
	if !rabotaet {
		// Ядро стоит — новый конфиг возьмёт ближайший подъём.
		return
	}
	novyy, err := s.zhelaemyyKonfig()
	if err != nil {
		return
	}
	r := s.reshitPoKonfigu(rabota, novyy)
	switch r.Nuzhda {
	case yadro.NeNuzhen:
		s.otlozh.snyat(prichinaKonfig)
	case yadro.Otlozhit:
		s.otlozhitPerezapusk(prichinaKonfig, "новый профиль: "+r.Slovami(), yadro.OtpechatokKonfigaBayt(novyy))
	case yadro.Srazu:
		log.Printf("новый профиль: %s — перезапускаю ядро сразу", r.Slovami())
		prichina := "новый профиль: " + r.Slovami()
		if s.otlozh.perezapustit != nil {
			s.otlozh.perezapustit(prichina)
			return
		}
		pokolenie := s.pokolenieAvtorezhima()
		go s.perezapustitNaNovom(context.Background(), prichina, pokolenie)
	}
}

// perezapustitNaNovom — перезапуск ядра ради нового конфига или нового ядра:
// проверка конфига ядром, затем как у «Дома» (dom.go perezapustit) — трафик
// мимо туннеля закрыт, ядро погашено, поднято тем же подъёмом, что у кнопки
// (с заменой ядра, если новое отложено), запрет снят по итогу.
//
// Занято (идёт подъём или другой перезапуск), связь не поднята, решение о
// связи сменилось — ничего не делает: sdelano=false, отложенное остаётся до
// следующего момента (или его снимет чужой подъём). Ядро отвергло новый
// конфиг — sdelano=true: ждать нечего, рабочее ядро не тронуто.
func (s *Sluzhba) perezapustitNaNovom(ctx context.Context, prichina string, pokolenie uint64) (sdelano bool) {
	if !s.zashchitaPodnyata() {
		return false
	}
	if !s.zamokPodyoma.TryLock() {
		log.Printf("перезапуск ядра (%s): идёт подъём или другой перезапуск — позже", prichina)
		return false
	}
	defer s.zamokPodyoma.Unlock()
	s.zamok.Lock()
	idet := s.podyomIdet
	s.zamok.Unlock()
	if idet || !s.zashchitaPodnyata() || s.pokolenieAvtorezhima() != pokolenie {
		return false
	}
	telo, err := s.zhelaemyyKonfig()
	if err == nil && !s.konfigGoden(ctx, telo, "перезапуск ядра ("+prichina+")") {
		s.otlozh.snyat(prichinaKonfig)
		s.otlozh.snyat(prichinaYadro)
		return true
	}
	log.Printf("перезапускаю ядро: %s", prichina)
	s.zakrytMimoTunnelya()
	_ = s.Yadro.Ostanovit()
	if s.pokolenieAvtorezhima() != pokolenie {
		log.Printf("перезапуск ядра: пока гасил, решение о связи сменилось — не поднимаю")
		s.otkrytMimoTunnelya("решение о связи сменилось, ядро не поднимаю")
		return true
	}
	err = s.podnyatZashchituPodZamkom(ctx)
	if err != nil {
		log.Printf("перезапуск ядра: связь не поднялась: %v", err)
	}
	s.otkrytMimoTunnelyaPoItogu(err)
	return true
}
