package sluzhba

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
	"github.com/HRYNdev/kelevra-desktop/internal/avtozapusk"
	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/ochered"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/pult"
	"github.com/HRYNdev/kelevra-desktop/internal/ustroystvo"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Пульт (договор KelevraServer docs/PULT.md): служба принимает настройки и
// команды мозга из /info, читает подстроечные значения через реестр
// (reestrDesktop) и шлёт серверу X-Pult и X-Sostoyanie. Пакет internal/pult
// держит проверку подписи, слои, испытание и квитанции; здесь — что каждая
// настройка и команда значит для Desktop.
//
// Без пульта (s.pult == nil или ключа нет) каждое значение берётся из кода, и
// поведение то же, что до пульта.

// Ключи выключателей подсистем (vykl.<имя>): true — подсистема молчит.
const (
	vyklZapasnoyPut     = "vykl.zapasnoy_put"
	vyklSverkaPravil    = "vykl.sverka_pravil"
	vyklPravilaIzKesha  = "vykl.pravila_iz_kesha"
	vyklOcheredUzlov    = "vykl.ochered_uzlov"
	vyklUchetTrafika    = "vykl.uchet_trafika"
	vyklSrochnyyZhurnal = "vykl.srochnyy_zhurnal"
	vyklZapisiYadra     = "vykl.zapisi_yadra"
)

// ChastoZhurnal — период журнала до zhurnal.chasto_do (договор: каждые 10 минут).
const ChastoZhurnal = 10 * time.Minute

// reestrDesktop — все ключи пульта, которые понимает Desktop. Значение по
// умолчанию — нынешняя константа кода (в таблицу оно идёт словами, а берётся
// всегда из кода). Таблица: docs/PULT-KLYUCHI-desktop.md (TestTablicaKlyuchey).
func reestrDesktop() pult.Reestr {
	ms := func(d time.Duration) string { return fmt.Sprint(d.Milliseconds()) }
	s := func(d time.Duration) string { return fmt.Sprint(int64(d / time.Second)) }
	m := func(d time.Duration) string { return fmt.Sprint(int64(d / time.Minute)) }
	vykl := func(imya, chto string) pult.Klyuch {
		return pult.Klyuch{Imya: imya, Tip: pult.Flag, Umolchanie: "false", Gde: "выключатель: " + chto}
	}
	return pult.Reestr{
		// Договор v1, столбец Desktop.
		{Imya: "opros.info_min", Tip: pult.Celoe, Min: 5, Max: 240, Umolchanie: "нет (сводка едет с профилем)", Gde: "лёгкий опрос /info из службы с If-None-Match (SleditZaSvodkoy)"},
		{Imya: "zhurnal.otpravka_min", Tip: pult.Celoe, Min: 10, Max: 1440, Umolchanie: m(PeriodOtpravkiZhurnalov), Gde: "период отправки журнала (PeriodOtpravkiZhurnalov)"},
		{Imya: "zhurnal.chasto_do", Tip: pult.Vremya, Umolchanie: "нет", Gde: "до этого времени журнал каждые 10 мин"},
		{Imya: "obnovleniya.tiho", Tip: pult.Flag, Umolchanie: "true", Gde: "тихая установка найденного обновления (postavitTiho)"},
		{Imya: "obnovleniya.kanal", Tip: pult.Vybor, Varianty: []string{"obychnyy", "proba", "stop"}, Umolchanie: "obychnyy", Gde: "proba — только сборка из пульта, stop — не обновляться; GitHub не спрашивается"},
		{Imya: "funkcii.avtorezhim", Tip: pult.Flag, Umolchanie: "true", Gde: "автомат дома/не дома (выключатель, сразу)"},
		{Imya: klyuchProbnyeYadra, Tip: pult.Flag, Umolchanie: "false", Gde: "брать пробные (prerelease) сборки ядра на сверке ядра (bratProbnyeYadra); выпуск ядра кольцом"},
		{Imya: "kanal.dolgiy_opros", Tip: pult.Flag, Umolchanie: "false", Gde: "долгий опрос …/pult/zhdat (SleditZaDolgimOprosom)"},

		// Периоды и сроки службы.
		{Imya: "profil.period_min", Tip: pult.Celoe, Min: 15, Max: 1440, Umolchanie: "obnovlyat_min из настроек (60), не меньше 15", Gde: "плановое обновление профиля и сводки (ObnovlyatProfil)"},
		{Imya: "obnovleniya.period_proverki_min", Tip: pult.Celoe, Min: 30, Max: 1440, Umolchanie: m(obnovlenie.PeriodFonovoyProverki), Gde: "фоновая проверка обновления (SleditZaObnovleniem)"},
		{Imya: "obnovleniya.srok_proverki_s", Tip: pult.Celoe, Min: 5, Max: 300, Umolchanie: s(srokFonovoyProverkiObnovleniya), Gde: "срок ответа на фоновую проверку (srokFonovoyProverkiObnovleniya)"},
		{Imya: "obnovleniya.srok_knopki_s", Tip: pult.Celoe, Min: 2, Max: 60, Umolchanie: s(srokProverkiObnovleniya), Gde: "срок ответа на «Проверить обновление» (srokProverkiObnovleniya)"},
		{Imya: "obnovleniya.srok_ustanovki_s", Tip: pult.Celoe, Min: 30, Max: 1800, Umolchanie: s(srokUstanovkiObnovleniya), Gde: "срок скачивания сборки (srokUstanovkiObnovleniya)"},
		{Imya: "zhurnal.povtor_posle_otkaza_min", Tip: pult.Celoe, Min: 5, Max: 1440, Umolchanie: m(PovtorPosleOtkaza), Gde: "повтор журнала после оборванной посылки (PovtorPosleOtkaza)"},
		{Imya: "zhurnal.povtor_do_peredachi_min", Tip: pult.Celoe, Min: 1, Max: 60, Umolchanie: m(PovtorPosleOtkazaDoPeredachi), Gde: "повтор журнала после отказа до первого байта"},
		{Imya: "pravila.sverka_min", Tip: pult.Celoe, Min: 5, Max: 1440, Umolchanie: m(ShagSverkiPravil), Gde: "сверка наборов правил (SveryatPravila)"},
		{Imya: "set.ozhidanie_posle_sna_s", Tip: pult.Celoe, Min: 5, Max: 600, Umolchanie: s(srokOzhidaniyaSeti), Gde: "ожидание сети после сна (srokOzhidaniyaSeti)"},

		// Автомат дома/не дома (internal/avtorezhim). Вступают со следующего
		// запуска служителя: при смене служба перезапускает его на той же обстановке.
		{Imya: "avtomat.podtverzhdeniy", Tip: pult.Celoe, Min: 1, Max: 10, Umolchanie: fmt.Sprint(avtorezhim.Podtverzhdeniy), Gde: "подтверждений подряд для смены обстановки (Podtverzhdeniy)"},
		{Imya: "avtomat.podryad_do_prichiny", Tip: pult.Celoe, Min: 1, Max: 20, Umolchanie: fmt.Sprint(avtorezhim.PodryadDoPrichiny), Gde: "слепых заходов до признания в окне (PodryadDoPrichiny)"},
		{Imya: "avtomat.pamyat_priznaka_s", Tip: pult.Celoe, Min: 5, Max: 600, Umolchanie: s(avtorezhim.PamyatPriznaka), Gde: "память DNS-признака дома (PamyatPriznaka)"},
		{Imya: "avtomat.interval_tikera_s", Tip: pult.Celoe, Min: 10, Max: 3600, Umolchanie: s(avtorezhim.IntervalTikeraPoUmolchaniyu), Gde: "страховочный тикер служителя"},
		{Imya: "avtomat.pauza_drebezga_ms", Tip: pult.Celoe, Min: 100, Max: 30000, Umolchanie: ms(avtorezhim.PauzaDrebezga), Gde: "пауза после события смены сети (PauzaDrebezga)"},
		{Imya: "avtomat.pauza_bystrogo_s", Tip: pult.Celoe, Min: 1, Max: 60, Umolchanie: s(avtorezhim.PauzaBystrogoOprosa), Gde: "шаг быстрых опросов после события"},
		{Imya: "avtomat.povtorov_bystrogo", Tip: pult.Celoe, Min: 1, Max: 30, Umolchanie: fmt.Sprint(avtorezhim.PovtorovBystrogoOprosa), Gde: "число быстрых опросов после события"},
		{Imya: "avtomat.dns_domeny", Tip: pult.Spisok, Min: 1, Max: 10, Umolchanie: strings.Join(avtorezhim.DomenyPoUmolchaniyu, ", "), Gde: "контрольные домены DNS-зонда"},
		{Imya: "avtomat.dns_nuzhno", Tip: pult.Celoe, Min: 1, Max: 10, Umolchanie: "2", Gde: "сколько доменов должно быть подменено для признака дома"},
		{Imya: "avtomat.dns_kontrolnyy", Tip: pult.Stroka, Umolchanie: avtorezhim.KontrolnyyDomenPoUmolchaniyu, Gde: "контрольный домен (дома всегда настоящий)"},
		{Imya: "avtomat.dns_taimaut_ms", Tip: pult.Celoe, Min: 500, Max: 15000, Umolchanie: ms(avtorezhim.TaimautPoUmolchaniyu), Gde: "бюджет DNS-зонда"},
		{Imya: "avtomat.domashnie_shlyuzy", Tip: pult.Spisok, Min: 0, Max: 20, Umolchanie: fmt.Sprintf("зашитые (%d шт.)", len(avtorezhim.DomashnieShlyuzyPoUmolchaniyu)), Gde: "MAC домашних шлюзов (опознание дома)"},
		{Imya: "avtomat.knopka_taimaut_s", Tip: pult.Celoe, Min: 2, Max: 60, Umolchanie: s(KnopkaTaimautPoUmolchaniyu), Gde: "заход обстановки по кнопке «Подключиться»"},

		// Очередь узлов своей сети (internal/ochered).
		{Imya: "uzel.porog_otkazov", Tip: pult.Celoe, Min: 1, Max: 20, Umolchanie: fmt.Sprint(ochered.ProvalovDoPerehoda), Gde: "провалов подряд до перехода на следующий узел"},
		{Imya: "uzel.vozvrat_na_pervyy_min", Tip: pult.Celoe, Min: 1, Max: 120, Umolchanie: m(ochered.VozvratCherez), Gde: "сколько первый узел жив до возврата на него"},

		// Выключатели подсистем, выключение которых безопасно.
		vykl(vyklZapasnoyPut, "повтор запросов службы через туннель (zapasnoy_put.go)"),
		vykl(vyklSverkaPravil, "сверка наборов правил с сервером"),
		vykl(vyklPravilaIzKesha, "правила с диска в конфиг ядра (ядро качает само, как до кеша)"),
		vykl(vyklOcheredUzlov, "очередь узлов своей сети"),
		vykl(vyklUchetTrafika, "учёт расхода этой машины (X-Device-Traffic)"),
		vykl(vyklSrochnyyZhurnal, "срочная посылка журнала после неудачи связи"),
		vykl(vyklZapisiYadra, "записи с полями из журнала ядра"),
	}
}

// umeyuDesktop — команды v1, которые Desktop выполняет.
var umeyuDesktop = map[string]bool{
	"prishli_zhurnal":     true,
	"obnovi_profil":       true,
	"pereydi_na_klyuch":   true,
	"sbros_avtomata":      true,
	"proverit_obnovlenie": true,
	"diagnostika":         true,
	"proba":               true,
}

// vykl — выключена ли подсистема пультом.
func (s *Sluzhba) vykl(imya string) bool { return s.pult.FlagIli(imya, false) }

// avtomatRazreshen — выключатель funkcii.avtorezhim (по умолчанию включён).
func (s *Sluzhba) avtomatRazreshen() bool { return s.pult.FlagIli("funkcii.avtorezhim", true) }

// periodProfilya — шаг планового обновления профиля.
func (s *Sluzhba) periodProfilya() time.Duration {
	shag := time.Duration(s.Nastroyki.ObnovlyatMin) * time.Minute
	if shag < 15*time.Minute {
		shag = 15 * time.Minute
	}
	return s.pult.Dlitelnost("profil.period_min", time.Minute, shag)
}

// periodZhurnalov — как часто уходит журнал при удачах.
func (s *Sluzhba) periodZhurnalov(seychas time.Time) time.Duration {
	if do, est := s.pult.Vremya("zhurnal.chasto_do"); est && seychas.Before(do) {
		return ChastoZhurnal
	}
	return s.pult.Dlitelnost("zhurnal.otpravka_min", time.Minute, PeriodOtpravkiZhurnalov)
}

// nachatPult — пульт с диска и заголовки; зовёт Novaya.
func (s *Sluzhba) nachatPult() {
	s.pult = pult.Zagruzit(hranenie.PutPulta(), reestrDesktop(), umeyuDesktop)
	if s.pult.Zapusk() {
		log.Printf("пульт: настройки откатились при запуске")
	}
	if v := s.pult.Versiya(); v > 0 {
		log.Printf("пульт: с диска версия %d%s", v, map[bool]string{true: ", настройки на испытании", false: ""}[s.pult.IspytanieIdet()])
	}
	s.Podpiska.Pult = s.pult.Zagolovok
	s.Podpiska.Sostoyanie = s.sostoyanieDlyaZagolovka
	// Сводка /info с диска: карточка подписки после перезапуска без сети.
	// ETag берётся только вместе с ней: «не менялось» на ETag без сводки
	// оставило бы службу без карточки до первой смены на сервере.
	if b, err := os.ReadFile(hranenie.PutSvodki()); err == nil {
		if sv, err := podpiska.RazobratSvedeniya(b); err == nil {
			etag, _ := os.ReadFile(hranenie.PutEtagaSvodki())
			s.zamok.Lock()
			s.svedeniya = sv
			s.etagSvodki = string(etag)
			s.zamok.Unlock()
		}
	}
}

// prinyatSvodku — свежая сводка /info: на диск и пульт из неё.
func (s *Sluzhba) prinyatSvodku(sv *podpiska.Svedeniya) {
	// Вход домой — своя подпись, свой кеш (dom.go); от пульта не зависит.
	s.prinyatDom(sv)
	if s.pult == nil || sv == nil {
		return
	}
	// ETag — следом за сводкой и только если она легла: на диске они пара,
	// и после перезапуска лёгкий опрос спросит «не менялось ли» (nachatPult).
	if len(sv.Syroe) > 0 {
		if err := hranenie.ZapisatZakryto(hranenie.PutSvodki(), sv.Syroe); err != nil {
			log.Printf("сводка /info не сохранилась на диск: %v", err)
		} else if err := hranenie.ZapisatZakryto(hranenie.PutEtagaSvodki(), []byte(sv.ETag)); err != nil {
			log.Printf("ETag сводки не сохранился на диск: %v", err)
		}
	}
	s.zamok.Lock()
	s.etagSvodki = sv.ETag
	s.zamok.Unlock()
	itog, err := s.pult.Prinyat(sv.Pult, ustroystvo.IdVZagolovke(s.Nastroyki.DeviceID))
	if err != nil {
		log.Printf("%v — держу прежний пульт", err)
		return
	}
	if itog.IzmenilisNastroyki {
		log.Printf("пульт: версия %d, новые настройки на испытании %s", itog.Versiya, pult.SrokIspytaniya)
		s.primenitPult()
	}
	if len(itog.Komandy) > 0 {
		go s.vypolnitKomandy(itog.Komandy)
	}
	if itog.NovoeObnovlenie {
		log.Printf("пульт: назначена сборка %s — проверяю обновление", s.pult.Obnovlenie().Nomer())
		go s.ProveritObnovlenieFonom()
	}
}

// primenitPult — то, что действует сразу при смене настроек (выключатели).
// Остальное читается на своей границе (следующий цикл, следующий заход).
func (s *Sluzhba) primenitPult() {
	s.avtorezhimZamok.Lock()
	idet := s.avtorezhimOtmena != nil
	byl := s.avtorezhimOtpechatok
	nado := s.Nastroyki.Avtorezhim
	s.avtorezhimZamok.Unlock()
	switch {
	case !s.avtomatRazreshen():
		if idet {
			log.Printf("пульт: автомат выключен с сервера — останавливаю слежение (выбор человека на диске не трогаю)")
			s.OstanovitAvtorezhim()
		}
	case idet && byl != s.otpechatokAvtomata():
		// Пороги автомата сменились: новый служитель на той же обстановке.
		nachalo := avtorezhim.Neizvestno
		s.avtorezhimZamok.Lock()
		if s.avtorezhimEkz != nil {
			nachalo = s.avtorezhimEkz.Zadvizhka.Tekushcheye()
		}
		s.avtorezhimZamok.Unlock()
		log.Printf("пульт: пороги автомата сменились — перезапускаю слежение на обстановке «%s»", nachalo)
		s.OstanovitAvtorezhim()
		s.zapustitAvtorezhimSNachala(context.Background(), nachalo)
	case !idet && nado:
		log.Printf("пульт: автомат снова разрешён — включаю слежение")
		s.zapustitAvtorezhimSNachala(context.Background(), avtorezhim.Neizvestno)
	}
}

// otpechatokAvtomata — значения ключей avtomat.* одной строкой.
func (s *Sluzhba) otpechatokAvtomata() string {
	var b strings.Builder
	for _, k := range reestrDesktop() {
		if strings.HasPrefix(k.Imya, "avtomat.") {
			if v, est := s.pult.Celoe(k.Imya); est {
				fmt.Fprintf(&b, "%s=%d;", k.Imya, v)
			}
			if v, est := s.pult.Spisok(k.Imya); est {
				fmt.Fprintf(&b, "%s=%v;", k.Imya, v)
			}
			if v := s.pult.StrokaIli(k.Imya, ""); v != "" {
				fmt.Fprintf(&b, "%s=%s;", k.Imya, v)
			}
		}
	}
	return b.String()
}

// nastroitAvtorezhim — пороги автомата из пульта на свежий экземпляр.
// Ключа нет — поле не трогается, и действует константа пакета.
func (s *Sluzhba) nastroitAvtorezhim(a *avtorezhim.Avtorezhim) {
	if s.pult == nil || a == nil {
		return
	}
	if v, est := s.pult.Celoe("avtomat.podtverzhdeniy"); est && a.Zadvizhka != nil {
		a.Zadvizhka.Nuzhno = int(v)
	}
	if v, est := s.pult.Celoe("avtomat.podryad_do_prichiny"); est {
		a.PodryadDoPrichinyN = int(v)
	}
	if v, est := s.pult.Celoe("avtomat.pamyat_priznaka_s"); est {
		a.PamyatPriznakaD = time.Duration(v) * time.Second
	}
	if v, est := s.pult.Spisok("avtomat.domashnie_shlyuzy"); est {
		a.DomashnieShlyuzy = v
	}
	nastroitZond := func(d avtorezhim.DnsProver) {
		z, ok := d.(*avtorezhim.DnsZond)
		if !ok {
			return
		}
		if v, est := s.pult.Spisok("avtomat.dns_domeny"); est {
			z.Domeny = v
		}
		if v, est := s.pult.Celoe("avtomat.dns_nuzhno"); est {
			z.Nuzhno = int(v)
		}
		if v := s.pult.StrokaIli("avtomat.dns_kontrolnyy", ""); v != "" {
			z.KontrolnyyDomen = v
		}
		if v, est := s.pult.Celoe("avtomat.dns_taimaut_ms"); est {
			z.Taimaut = time.Duration(v) * time.Millisecond
		}
	}
	nastroitZond(a.Dns)
	if pryamoy := a.DnsPryamoy; pryamoy != nil {
		a.DnsPryamoy = func(adres, lokalnyy string) avtorezhim.DnsProver {
			d := pryamoy(adres, lokalnyy)
			nastroitZond(d)
			return d
		}
	}
}

// nastroitSluzhitelya — темп служителя из пульта (0 — константа пакета).
func (s *Sluzhba) nastroitSluzhitelya(sl *avtorezhim.Sluzhitel) {
	if s.pult == nil {
		return
	}
	sl.Interval = s.pult.Dlitelnost("avtomat.interval_tikera_s", time.Second, 0)
	sl.Pauza = s.pult.Dlitelnost("avtomat.pauza_drebezga_ms", time.Millisecond, 0)
	sl.PauzaBystrogo = s.pult.Dlitelnost("avtomat.pauza_bystrogo_s", time.Second, 0)
	if v, est := s.pult.Celoe("avtomat.povtorov_bystrogo"); est {
		sl.PovtorovBystrogo = int(v)
	}
}

// --- команды ---

// srokKomandy — сколько ждать одну команду.
const srokKomandy = 3 * time.Minute

// vypolnitKomandy выполняет команды пульта по одной и пишет квитанции.
func (s *Sluzhba) vypolnitKomandy(komandy []pult.Komanda) {
	for _, k := range komandy {
		ctx, otmena := context.WithTimeout(context.Background(), srokKomandy)
		itog := s.vypolnitKomandu(ctx, k)
		otmena()
		log.Printf("пульт: команда %s (%s) — %s", k.ID, k.Chto, itog)
		s.pult.Zavershit(k.ID, itog)
	}
}

// vypolnitKomandu — одна команда; итог — слово квитанции.
func (s *Sluzhba) vypolnitKomandu(ctx context.Context, k pult.Komanda) string {
	switch k.Chto {
	case "prishli_zhurnal":
		return itogKomandy(s.otpravitZhurnalySrochno(ctx))
	case "obnovi_profil":
		if s.Nastroyki.Kod == "" && s.Nastroyki.Podpiska == "" {
			return "oshibka-net_profilya"
		}
		return itogKomandy(s.obnovitProfilRaz(ctx))
	case "pereydi_na_klyuch":
		return s.pereytiNaKlyuch(ctx)
	case "sbros_avtomata":
		s.sbrositAvtomat()
		return "ok"
	case "proverit_obnovlenie":
		s.zamok.Lock()
		s.oshibkaTihoy = nil
		s.zamok.Unlock()
		err := s.proveritObnovlenieRaz(ctx)
		if err == nil {
			// Нашлось и ставилось тихо — квитанция по установке (sha256, подпись).
			s.zamok.Lock()
			err = s.oshibkaTihoy
			s.zamok.Unlock()
		}
		return itogKomandy(err)
	case "diagnostika":
		log.Printf("диагностика по просьбе сервера: %s", s.Diagnostika())
		return itogKomandy(s.otpravitZhurnalySrochno(ctx))
	case "proba":
		return s.proba(ctx, k.ParamStroka("cel"))
	}
	return "ne_umeyu"
}

// itogKomandy — ошибка словом квитанции.
func itogKomandy(err error) string {
	if err == nil {
		return "ok"
	}
	var perehod *podpiska.OshibkaPerehoda
	switch {
	case errors.As(err, &perehod):
		return "oshibka-" + perehod.Kratko
	case errors.Is(err, obnovlenie.ErrSha256):
		return "oshibka-hesh"
	case errors.Is(err, obnovlenie.ErrPodpis):
		return "oshibka-podpis"
	case setevoyOtkaz(err):
		return "oshibka-set"
	}
	return "oshibka-otkaz"
}

// prichinaObnovleniya — слово для X-Pult oshibki=obnovlenie:<причина>, общее с
// Box: hesh (sha256 не сошёлся с пультом), podpis (подпись exe), skachat (сеть),
// fayl (прочее).
func prichinaObnovleniya(err error) string {
	switch {
	case errors.Is(err, obnovlenie.ErrSha256):
		return "hesh"
	case errors.Is(err, obnovlenie.ErrPodpis):
		return "podpis"
	case setevoyOtkaz(err):
		return "skachat"
	}
	return "fayl"
}

// pereytiNaKlyuch — команда pereydi_na_klyuch: при удаче новый адрес встаёт
// на место старого в настройках, при отказе ничего не меняется.
func (s *Sluzhba) pereytiNaKlyuch(ctx context.Context) string {
	staryy := s.Nastroyki.Podpiska
	novyy, uzhe, err := s.Podpiska.PereytiNaKlyuch(ctx, staryy, s.Nastroyki.Kod)
	if err != nil {
		log.Printf("пульт: %s", podpiska.SkrytKod(err.Error()))
		return itogKomandy(err)
	}
	if uzhe {
		log.Printf("пульт: подписка и так на своём ключе — переходить некуда")
		return "ok"
	}
	s.Nastroyki.Podpiska = novyy
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		s.Nastroyki.Podpiska = staryy
		log.Printf("пульт: новый адрес подписки не сохранился (%v) — остаюсь на прежнем", err)
		return "oshibka-disk"
	}
	log.Printf("пульт: подписка переведена на свой ключ (%s)", adresDlyaZhurnala(novyy))
	return "ok"
}

// sbrositAvtomat — команда sbros_avtomata: забыть ручной выбор, залипший выбор
// узла и всё, что служитель накопил в памяти (задвижка, слепота, признак).
// Опознание дома на Desktop — зашитые шлюзы, выученного отпечатка нет.
func (s *Sluzhba) sbrositAvtomat() {
	s.avtorezhimZamok.Lock()
	s.Nastroyki.RuchnoyVybor = false
	s.Nastroyki.Uzly = nil
	idet := s.avtorezhimOtmena != nil
	s.avtorezhimZamok.Unlock()
	if err := hranenie.Sohranit(s.Nastroyki); err != nil {
		log.Printf("пульт: сброс автомата не сохранился: %v", err)
	}
	if idet {
		s.OstanovitAvtorezhim()
		s.zapustitAvtorezhimSNachala(context.Background(), avtorezhim.Neizvestno)
	}
	log.Printf("пульт: память автомата сброшена (ручной выбор, выбор узла, задвижка)")
}

// proba — команда proba: param.cel через туннель (вход службы) и мимо него.
// Итог (общий с Box): ok-tun.<мс|net|nepr>-mimo.<мс|net|nepr> — net: не
// прошла, nepr: не мерили (нет входа службы).
func (s *Sluzhba) proba(ctx context.Context, cel string) string {
	if cel == "" {
		return "oshibka-net_celi"
	}
	if !strings.Contains(cel, "://") {
		cel = "https://" + cel
	}
	u, err := url.Parse(cel)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "oshibka-cel"
	}
	zamerit := func(proksi *url.URL) string {
		tr := &http.Transport{Proxy: nil}
		if proksi != nil {
			tr.Proxy = http.ProxyURL(proksi)
		}
		k := &http.Client{Timeout: 10 * time.Second, Transport: tr}
		nachalo := time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, u.String(), nil)
		if err != nil {
			return "x"
		}
		otvet, err := k.Do(req)
		if err != nil {
			return "net"
		}
		otvet.Body.Close()
		return fmt.Sprint(time.Since(nachalo).Milliseconds())
	}
	t := "nepr"
	if vhod := s.adresVhodaSluzhby(); vhod != nil {
		t = zamerit(vhod)
	}
	p := zamerit(nil)
	log.Printf("проба по просьбе сервера до %s: через туннель %s, мимо %s (мс)", u.Host, t, p)
	return "ok-tun." + t + "-mimo." + p
}

// Diagnostika — сжатый отчёт о машине одной строкой для журнала.
func (s *Sluzhba) Diagnostika() string {
	var chasti []string
	dob := func(k, v string) { chasti = append(chasti, k+"="+v) }
	dob("versiya", podpiska.Versiya)
	dob("pasport", s.SobratPasport())
	if s.SluzhbaWindowsEst != nil {
		dob("sluzhba_windows", fmt.Sprint(s.SluzhbaWindowsEst()))
	}
	// Куда смотрит автозапуск у вошедших пользователей и есть ли там файл:
	// запись на удалённый exe молча лишала значка при входе (ПК Вовы 18–21.10).
	if zapisi, err := avtozapusk.VseZapisi(); err != nil {
		dob("avtozapusk", "?")
	} else {
		dob("avtozapusk", avtozapusk.OpisanieDlyaDiagnostiki(zapisi))
	}
	if s.Yadro != nil {
		dob("yadro", string(s.Yadro.Sost()))
		dob("konfig", yadro.OtpechatokKonfiga(s.Yadro.PutKonfiga()))
	}
	dob("profil", yadro.OtpechatokKonfiga(hranenie.PutProfilya()))
	dob("rezhim_kartiny", fmt.Sprint(s.rezhimKartiny()))
	dob("set", fmt.Sprint(s.estSet()))
	s.avtorezhimZamok.Lock()
	dob("avtomat", fmt.Sprintf("vkl:%v ruchnoy:%v razreshen:%v", s.Nastroyki.Avtorezhim, s.Nastroyki.RuchnoyVybor, s.avtomatRazreshen()))
	if s.avtorezhimEkz != nil {
		dob("obstanovka", s.avtorezhimEkz.Zadvizhka.Tekushcheye().String())
		if p := s.avtorezhimEkz.PrichinaSlepoty(); p != "" {
			dob("slepota", p)
		}
	}
	s.avtorezhimZamok.Unlock()
	s.zamok.Lock()
	dob("uzel", s.aktivnyyUzel)
	dob("otkaz", s.otkaz)
	s.zamok.Unlock()
	dob("pult", fmt.Sprintf("versiya:%d ispytanie:%v", s.pult.Versiya(), s.pult.IspytanieIdet()))
	dob("adres", adresDlyaZhurnala(s.Nastroyki.Podpiska))
	dob("dom", s.domDlyaDiagnostiki())
	return strings.Join(chasti, "; ")
}

// estSet — шлюз на физическом адаптере (как ждёт сеть povtor_seti.go).
func (s *Sluzhba) estSet() bool {
	if s.estSetDlyaStenda != nil {
		return s.estSetDlyaStenda()
	}
	return avtorezhim.EstShlyuz()
}

// --- наблюдение: испытание и пульс ---

// ShagPulta — как часто служба смотрит на испытание и обновляет пульс.
var ShagPulta = 20 * time.Second

// SleditZaPultom — испытание новых настроек и пульс X-Sostoyanie.
func (s *Sluzhba) SleditZaPultom(ctx context.Context) {
	if s.pult == nil {
		return
	}
	s.nablyudatPult()
	t := time.NewTicker(ShagPulta)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.nablyudatPult()
		}
	}
}

// nablyudatPult — один взгляд: пульс и факты испытания.
func (s *Sluzhba) nablyudatPult() {
	set := s.estSet()
	tunnel := s.zashchitaPodnyata()
	sTrafikom := false
	uzel := ""
	if s.Yadro != nil && s.Yadro.Sost() == yadro.Rabotaet {
		// Счётчики ядра нужны только испытанию — без него ядро не дёргаем.
		if t, err := s.trafikDlyaIspytaniya(); err == nil && t != nil {
			vsego := t.VverhBayt + t.VnizBayt
			s.zamok.Lock()
			sTrafikom = s.trafikPulta > 0 && vsego > s.trafikPulta
			s.trafikPulta = vsego
			s.zamok.Unlock()
		}
		if b, err := os.ReadFile(s.Yadro.PutKonfiga()); err == nil {
			if gruppa, _, ok := ochered.IzKonfiga(b); ok {
				uzel, _ = s.Yadro.Vybrano(gruppa)
			}
		}
	}
	s.zamok.Lock()
	nuzhen := tunnel || s.podyomIdet || s.otkaz != ""
	s.zamok.Unlock()
	s.pult.Otmetit(pult.Nablyudenie{Set: set, Nuzhen: nuzhen, STrafikom: tunnel && sTrafikom})
	if s.pult.ProveritIspytanie() {
		s.primenitPult()
	}
	s.obnovitPuls(set, tunnel, uzel)
}

// trafikDlyaIspytaniya — счётчики ядра, только пока идёт испытание (без него
// ядро лишний раз не спрашиваем); nil — мерить нечего.
func (s *Sluzhba) trafikDlyaIspytaniya() (*yadro.Trafik, error) {
	if !s.pult.IspytanieIdet() {
		return nil, nil
	}
	return s.Yadro.Trafik()
}

// obnovitPuls собирает X-Sostoyanie: avto=<doma|vne|neizv|ruchnoy|pauza|vykl>;
// tunnel=0|1;set=0|1;perehod=<unix последней смены avto/tunnel>;uzel=<имя, url-кодировка>.
func (s *Sluzhba) obnovitPuls(set, tunnel bool, uzel string) {
	avto := "pauza"
	s.avtorezhimZamok.Lock()
	switch {
	case !s.avtomatRazreshen():
		avto = "vykl"
	case s.Nastroyki.Avtorezhim && s.avtorezhimEkz != nil:
		switch s.avtorezhimEkz.Zadvizhka.Tekushcheye() {
		case avtorezhim.Doma:
			avto = "doma"
		case avtorezhim.VneDoma:
			avto = "vne"
		default:
			avto = "neizv"
		}
	case s.Nastroyki.RuchnoyVybor:
		avto = "ruchnoy"
	case s.Nastroyki.Avtorezhim:
		// Автомат включён, а служитель ещё не встал (первые секунды после
		// старта, см. PervyyPuls): обстановка не известна, но это не пауза.
		avto = "neizv"
	}
	s.avtorezhimZamok.Unlock()
	b2i := map[bool]string{true: "1", false: "0"}
	klyuch := avto + b2i[tunnel]
	s.zamok.Lock()
	defer s.zamok.Unlock()
	if klyuch != s.pulsKlyuch {
		s.pulsKlyuch = klyuch
		s.pulsPerehod = time.Now()
	}
	s.aktivnyyUzel = uzel
	s.puls = fmt.Sprintf("avto=%s;tunnel=%s;set=%s;perehod=%d", avto, b2i[tunnel], b2i[set], s.pulsPerehod.Unix())
	if uzel != "" {
		s.puls += ";uzel=" + url.QueryEscape(uzel)
	}
}

// PervyyPuls собирает пульс до первого запроса к серверу. Зовёт cmd/kelevra
// при старте службы — после приёма живого ядра и ДО подключения и обновления
// профиля.
//
// Беда 21.10 (ПК Вовы, 0.6.84-proba): первый /info ушёл через 2 с после
// старта, а первый пульс собирает SleditZaPultom в своей горутине — она
// стартует позже, и заголовок X-Sostoyanie уехал пустым (то есть не уехал
// вовсе). Следующий запрос к выдаче у Desktop без opros.info_min и долгого
// опроса — обновление профиля через час, и всё это время в pulsy.json
// устройства не было. Узел здесь не известен (ядро ещё не спрашивали) —
// его допишет первый же взгляд SleditZaPultom.
func (s *Sluzhba) PervyyPuls() {
	if s.pult == nil {
		return
	}
	s.obnovitPuls(s.estSet(), s.zashchitaPodnyata(), "")
}

// sostoyanieDlyaZagolovka — последний собранный пульс (X-Sostoyanie).
func (s *Sluzhba) sostoyanieDlyaZagolovka() string {
	s.zamok.Lock()
	defer s.zamok.Unlock()
	return s.puls
}

// PultVyhod — служба уходит штатно: следующий запуск испытание пульта не
// считает падением (зовёт cmd/kelevra при выходе).
func (s *Sluzhba) PultVyhod() { s.pult.Vyhod() }

// otmetitNuzhenTunnel — подъём связи начат: туннель нужен (для испытания).
func (s *Sluzhba) otmetitNuzhenTunnel() {
	s.pult.Otmetit(pult.Nablyudenie{Nuzhen: true})
}

// --- связь: лёгкий и долгий опрос ---

// adresPodpiski — адрес подписки для /info и долгого опроса.
func (s *Sluzhba) adresPodpiski() string {
	if s.Nastroyki.Podpiska != "" {
		return s.Nastroyki.Podpiska
	}
	if s.Nastroyki.Kod == "" {
		return ""
	}
	shema, host := s.Podpiska.Shema, s.Podpiska.Host
	if shema == "" {
		shema = "https"
	}
	if host == "" {
		host = podpiska.Host
	}
	return fmt.Sprintf("%s://%s/k/%s", shema, host, s.Nastroyki.Kod)
}

// ObnovitSvedeniyaLegko — /info с If-None-Match: 304 — ничего не делать.
func (s *Sluzhba) ObnovitSvedeniyaLegko(ctx context.Context) error {
	adres := s.adresPodpiski()
	if adres == "" {
		return nil
	}
	s.zamok.Lock()
	etag := s.etagSvodki
	s.zamok.Unlock()
	sv, err := s.Podpiska.SvedeniyaEsliNovoe(ctx, adres, etag)
	if err != nil || sv == nil {
		return err
	}
	s.prinyatSvedeniya(ctx, sv)
	return nil
}

// SleditZaSvodkoy — лёгкий опрос /info раз в opros.info_min. Ключа нет — молчит
// (сводка едет с профилем, как до пульта) и раз в минуту смотрит, не появился ли.
func (s *Sluzhba) SleditZaSvodkoy(ctx context.Context) {
	if s.pult == nil {
		return
	}
	for {
		zhdat := time.Minute
		if v, est := s.pult.Celoe("opros.info_min"); est {
			zhdat = time.Duration(v) * time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(zhdat):
		}
		if _, est := s.pult.Celoe("opros.info_min"); !est {
			continue
		}
		octx, otmena := context.WithTimeout(ctx, 30*time.Second)
		if err := s.ObnovitSvedeniyaLegko(octx); err != nil && !setevoyOtkaz(err) {
			log.Printf("лёгкий опрос сводки: %v", err)
		}
		otmena()
	}
}

// SleditZaDolgimOprosom — долгий опрос …/pult/zhdat при kanal.dolgiy_opros.
func (s *Sluzhba) SleditZaDolgimOprosom(ctx context.Context) {
	if s.pult == nil {
		return
	}
	pauza := time.Minute
	for {
		if ctx.Err() != nil {
			return
		}
		adres := s.adresPodpiski()
		if !s.pult.FlagIli("kanal.dolgiy_opros", false) || adres == "" {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
			continue
		}
		zctx, otmena := context.WithTimeout(ctx, 70*time.Second)
		est, err := s.Podpiska.ZhdatPult(zctx, adres, s.pult.Versiya())
		otmena()
		if err != nil && ctx.Err() == nil {
			zhdat, rastit := pauzaDolgogo(err, pauza)
			if rastit && pauza < 15*time.Minute {
				pauza *= 2
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(zhdat):
			}
			continue
		}
		pauza = time.Minute
		if est {
			octx, otmena := context.WithTimeout(ctx, 30*time.Second)
			if err := s.ObnovitSvedeniyaLegko(octx); err != nil {
				log.Printf("сводка после долгого опроса: %v", err)
			}
			otmena()
		}
	}
}

// pauzaDolgogo — сколько ждать после неудачного долгого опроса (коды выдачи —
// docs/PULT.md, «Провод»): 403 (мозг этому устройству не включил) — 10 мин;
// 429 — Retry-After (нет — 60 с); 404/405 — 30 мин; сеть и прочее — растущая
// pauza (rastit=true).
func pauzaDolgogo(err error, pauza time.Duration) (zhdat time.Duration, rastit bool) {
	var oz *podpiska.OshibkaZhdat
	if errors.As(err, &oz) {
		switch oz.Status {
		case http.StatusForbidden:
			return 10 * time.Minute, false
		case http.StatusTooManyRequests:
			if oz.RetryAfter > 0 {
				return oz.RetryAfter, false
			}
			return time.Minute, false
		case http.StatusNotFound, http.StatusMethodNotAllowed:
			return 30 * time.Minute, false
		}
	}
	return pauza, true
}

// --- обновление по пульту ---

// naytiObnovlenie — что ставить: сборка из пульта главнее GitHub; канал proba
// и stop GitHub не спрашивают (stop не обновляется вовсе).
func (s *Sluzhba) naytiObnovlenie(ctx context.Context, klient *http.Client) (*obnovlenie.Novaya, error) {
	kanal := s.pult.StrokaIli("obnovleniya.kanal", "obychnyy")
	if kanal == "stop" {
		return nil, nil
	}
	// http — только стенду (KELEVRA_SHEMA=http, как у подписки); в бою https.
	shemaOk := func(u string) bool {
		return strings.HasPrefix(u, "https://") || os.Getenv("KELEVRA_SHEMA") == "http" && strings.HasPrefix(u, "http://")
	}
	if o := s.pult.Obnovlenie(); o != nil && shemaOk(o.URL) && o.Sha256 != "" {
		if nomer := o.Nomer(); nomer != "" && obnovlenie.Sravnit(nomer, podpiska.Versiya) > 0 {
			return &obnovlenie.Novaya{Versiya: nomer, Ssylka: o.URL, Razmer: o.Razmer,
				SsylkaPodpisi: o.URL + ".sig", Sha256: o.Sha256}, nil
		}
	}
	if kanal == "proba" {
		return nil, nil
	}
	adres := obnovlenie.SpisokReliza
	if svoy := os.Getenv("KELEVRA_RELIZY"); svoy != "" {
		adres = svoy // стенд
	}
	return obnovlenie.Proverit(ctx, klient, adres, podpiska.Versiya)
}

// tablicaKlyuchey — docs/PULT-KLYUCHI-desktop.md из реестра (см. TestTablicaKlyuchey).
func tablicaKlyuchey() string {
	zagolovok := "# Ключи пульта Kelevra Desktop\n\n" +
		"Создано из реестра клиента (`internal/sluzhba/pult.go`, `reestrDesktop`); править реестр, затем\n" +
		"`KELEVRA_OBNOVIT_KLYUCHI=1 go test ./internal/sluzhba -run TestTablicaKlyuchey`. Договор — KelevraServer\n" +
		"`docs/PULT.md`. Ключа нет в пульте — действует значение из кода (столбец «По умолчанию»), поведение прежнее.\n" +
		"Незнакомый ключ, не тот тип или выход за границы — ключ не принимается и уходит в `X-Pult` словом `oshibki`.\n\n" +
		fmt.Sprintf("`umeet=%d`. Команды: ", pult.Umeet)
	var komandy []string
	for k := range umeyuDesktop {
		komandy = append(komandy, "`"+k+"`")
	}
	sort.Strings(komandy)
	zagolovok += strings.Join(komandy, ", ") + ".\n"
	return reestrDesktop().Tablica(zagolovok)
}
