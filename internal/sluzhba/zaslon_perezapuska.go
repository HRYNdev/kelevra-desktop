package sluzhba

// Запрет трафика мимо туннеля (internal/zaslon): владеет им служба.
//
// Что стоит, решает zaslon.Vybrat от входа, который держит служба
// (vhodZaslona): намерение «подключено», решение автомата «дома», строгий
// режим и выключатель из пульта, идёт ли перезапуск ядра:
//
//   - уровень 1 (всем, по умолчанию): к адресам Anthropic только из адреса
//     туннеля — всё время «подключено» вне дома, в том числе пока ядро
//     перезапускается, падает или ещё не поднято после старта службы;
//   - уровень 2 (kanal.strogiy в пульте): всё мимо туннеля закрыто всё время
//     «подключено» вне дома, с момента старта службы;
//   - временный полный запрет на перезапуск ядра — у всех, как раньше, со
//     сроком zaslon.SrokPoUmolchaniyu (60 с).
//
// Решение владельца 07.11.2026: «мне надо держать, чтоб по стандарту ничего на
// сервера Anthropic не уходило, а другим скорее не надо»; про загрузку
// Windows — «если мягко закрывать, типо только мне, чтоб у меня не палилось»:
// запрет живёт в динамической сессии службы и встаёт сразу при её старте, не
// boot-time и не persistent.
//
// Намерение (namerenie):
//   - да: «Подключиться», включённый автомат (и на старте службы, если он
//     включён), живое ядро прошлой копии принято, передача от прошлой копии,
//     автоподключение на старте в ручном режиме (avtopodklyuch.go);
//   - нет: «Отключить», уход копии; полоса «Вручную» — как стоит туннель.
//
// «Дома» — последнее решение автомата, пока автомат включён и разрешён.
//
// Временный запрет перезапуска ставится везде, где служба сама гасит или
// теряет ядро и поднимает его снова (туннель был и должен вернуться), всегда
// под общим замком переходов (perehody.go), кроме паузы сторожа:
//   - perezapustitPodZamkom (perezapusk_yadra.go): «Дом» (dom.go
//     perezapustit), новый конфиг сразу и отложенный перезапуск
//     (perezapustitNaNovom);
//   - podnyatSvyazPosleSmenyVersii, smenitYadroPlavno (priyom_yadra.go);
//   - сторож после падения (storozh_yadra.go): на коротких паузах — с момента
//     падения, вне замка; сам подъём — под замком.
//
// При обновлении exe службы запрет передаётся держателю (--derzhat-zapret,
// zaslon/peredacha.go) и живёт без щели, пока новая копия не поставит свой.
//
// Стенд и проверки (KELEVRA_DIR задан) и KELEVRA_ZASLON=net — запрет никогда
// не ставится по-настоящему: на машине проверяющего сеть не меняется.

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
	"github.com/HRYNdev/kelevra-desktop/internal/tunnel"
	"github.com/HRYNdev/kelevra-desktop/internal/zaslon"
)

// Ключи пульта запрета.
const (
	klyuchStrogiy       = "kanal.strogiy"
	klyuchAnthropicSeti = "kanal.anthropic_seti"
	vyklZapretAnthropic = "vykl.zapret_anthropic"
	ArgDerzhatZapret    = "--derzhat-zapret"
)

// srokGotovnostiDerzh — сколько уходящая копия ждёт метку держателя;
// shagOprosaDerzhatelya — как часто смотрит. Переменные — для проверок.
var (
	srokGotovnostiDerzh   = 5 * time.Second
	shagOprosaDerzhatelya = 100 * time.Millisecond
)

// nachatZaslon — запрет на настоящей системе (WFP на Windows). Намерение на
// старте: передача прошлой копии (обновление), иначе — включён ли автомат.
func (s *Sluzhba) nachatZaslon() {
	sys := zaslon.NovayaSistema()
	if os.Getenv("KELEVRA_DIR") != "" || os.Getenv("KELEVRA_ZASLON") == "net" {
		sys = nil
	}
	s.zaslon = &zaslon.Zaslon{Sys: sys, Vhod: s.vhodZaslona, Dannye: s.dannyeZaslona}
	s.avtorezhimZamok.Lock()
	s.namerenie = s.Nastroyki.Avtorezhim && s.avtomatRazreshen()
	s.avtorezhimZamok.Unlock()
	if p, est := zaslon.ProchestPeredachu(hranenie.Papka(), time.Now()); est {
		s.avtorezhimZamok.Lock()
		s.namerenie, s.zaslonDoma = p.Namerenie, p.Doma
		s.avtorezhimZamok.Unlock()
		log.Printf("запрет мимо туннеля: прошлая копия передала «%s» (намерение %v, дома %v)", p.Politika.Slovami(), p.Namerenie, p.Doma)
	}
}

// zaslonPosleStarta — первые правила этой копии, затем отпустить держателя
// прошлой (если он есть): его правила стоят до этой самой строки.
func (s *Sluzhba) zaslonPosleStarta() {
	s.obnovitZaslon("старт службы")
	zaslon.OtpustitDerzhatelya(hranenie.Papka())
}

// vhodZaslona — вход zaslon.Vybrat (без перезапуска: его держит zaslon).
func (s *Sluzhba) vhodZaslona() zaslon.Vhod {
	avtomat := s.avtomatRazreshen()
	s.avtorezhimZamok.Lock()
	v := zaslon.Vhod{
		Namerenie: s.namerenie,
		Doma:      s.zaslonDoma && s.Nastroyki.Avtorezhim && avtomat,
	}
	s.avtorezhimZamok.Unlock()
	v.Strogiy = s.pult.FlagIli(klyuchStrogiy, false)
	v.BezAnthropic = s.vykl(vyklZapretAnthropic)
	return v
}

// dannyeZaslona — адреса Anthropic (пульт или умолчание), свои адреса
// туннеля из профиля с соседями под сдвиг имени, программы для полного
// запрета.
func (s *Sluzhba) dannyeZaslona() zaslon.Dannye {
	var d zaslon.Dannye
	if seti, est := s.pult.Spisok(klyuchAnthropicSeti); est {
		d.Seti = seti
	}
	s.zamok.Lock()
	adresa := append([]string(nil), s.kartina.TunAdresa...)
	s.zamok.Unlock()
	d.Tunnel = zaslon.SetiTunnelya(adresa, tunnel.MaksSdvig, tunnel.SdvinutAdresa)
	d.Programmy = s.programmyMimoZaslona()
	return d
}

// programmyMimoZaslona — кому наружу можно при полном запрете: ядру (до узлов
// VPS и по правилам direct) и самой службе (подписка, сводка, журналы, зонды
// автомата). Файла нет (ядро ещё не скачано) — не называем: WFP не дал бы
// номер программы, и весь набор не встал бы. Скачали — пересборка конфига
// зовёт obnovitZaslon, и ядро появляется в списке.
func (s *Sluzhba) programmyMimoZaslona() []string {
	var p []string
	if s.Yadro != nil && s.Yadro.Bin != "" {
		if _, err := os.Stat(s.Yadro.Bin); err == nil {
			p = append(p, s.Yadro.Bin)
		}
	}
	if exe, err := os.Executable(); err == nil {
		p = append(p, exe)
	}
	return p
}

// obnovitZaslon — вход сменился: правила привести. Не звать из-под s.zamok и
// s.avtorezhimZamok (vhodZaslona берёт их сам).
func (s *Sluzhba) obnovitZaslon(prichina string) {
	_ = s.zaslon.Obnovit(prichina)
}

// ustanovitNamerenie — служба держит (или нет) «подключено».
func (s *Sluzhba) ustanovitNamerenie(da bool, prichina string) {
	s.avtorezhimZamok.Lock()
	bylo := s.namerenie
	s.namerenie = da
	s.avtorezhimZamok.Unlock()
	if bylo != da {
		s.obnovitZaslon(prichina)
	}
}

// ustanovitDoma — последнее решение автомата: «дома» или нет.
func (s *Sluzhba) ustanovitDoma(da bool, prichina string) {
	s.avtorezhimZamok.Lock()
	bylo := s.zaslonDoma
	s.zaslonDoma = da
	s.avtorezhimZamok.Unlock()
	if bylo != da {
		s.obnovitZaslon(prichina)
	}
}

// zakrytMimoTunnelya — перед тем как служба сама погасит ядро ради подъёма.
// Не встал запрет — перезапуск идёт как раньше (строка в журнале — у zaslon).
func (s *Sluzhba) zakrytMimoTunnelya() {
	s.zaslon.Zakryt()
}

// otkrytMimoTunnelya — перезапуск кончился без туннеля (или связь опущена).
// Свой запрет (уровень 1, строгий) при этом остаётся по своим правилам.
func (s *Sluzhba) otkrytMimoTunnelya(prichina string) {
	s.zaslon.Otkryt(prichina)
}

// otkrytMimoTunnelyaPoItogu — по итогу подъёма: поднят — «туннель поднят за
// N с», нет — с причиной.
func (s *Sluzhba) otkrytMimoTunnelyaPoItogu(err error) {
	switch {
	case err == nil && s.zashchitaPodnyata():
		s.zaslon.OtkrytPodnyat()
	case err == nil:
		s.zaslon.Otkryt("ядро не работает")
	default:
		s.zaslon.Otkryt(fmt.Sprintf("связь не поднялась (%v)", err))
	}
}

// zapretDlyaOkna — какой запрет стоит («anthropic», «polnyy» или пусто): поле
// /api/sostoyanie и диагностики, не надпись.
func (s *Sluzhba) zapretDlyaOkna() string {
	return s.zaslon.Primeneno().Kratko()
}

// peredatZapretNovoyKopii — перед уходом после обновления: правила, что стоят,
// и намерение — в передачу; держатель (новый exe с --derzhat-zapret) ставит их
// в своей сессии, и только после его метки эта копия уходит. Держатель не
// ответил — снимаем его: щель как до передачи (уход + подъём новой копии).
//
// versiya — версия встающего exe. Старее этой — держатель не запускается:
// сборка без флага --derzhat-zapret поняла бы его как обычный запуск окна.
func (s *Sluzhba) peredatZapretNovoyKopii(put, versiya string) {
	p := s.zaslon.Primeneno()
	papka := hranenie.Papka()
	s.avtorezhimZamok.Lock()
	namerenie, doma := s.namerenie, s.zaslonDoma
	s.avtorezhimZamok.Unlock()
	seychas := time.Now()
	zaslon.UbratGotovnost(papka)
	if err := zaslon.ZapisatPeredachu(papka, zaslon.Peredacha{
		Politika: p, Namerenie: namerenie, Doma: doma,
		Kogda: seychas, Do: seychas.Add(zaslon.SrokDerzhatelya), PID: os.Getpid(),
	}); err != nil {
		log.Printf("запрет мимо туннеля: передачу новой копии не записал (%v) — до её подъёма запрета не будет", err)
		return
	}
	if p.Pusto() {
		// Держать нечего; новой копии остаётся намерение.
		return
	}
	if versiya != "" && obnovlenie.Sravnit(versiya, podpiska.Versiya) < 0 {
		log.Printf("запрет мимо туннеля: встаёт версия %s старее этой (%s) — держателя не запускаю, до подъёма новой копии запрета не будет", versiya, podpiska.Versiya)
		return
	}
	zapustit := s.zapustitDerzhatelya
	if zapustit == nil {
		zapustit = zapustitDerzhatelyaBoevoy
	}
	snyat, err := zapustit(put)
	if err != nil {
		log.Printf("запрет мимо туннеля: держатель не запущен (%v) — до подъёма новой копии запрета не будет", err)
		return
	}
	if !zaslon.ZhdatGotovnosti(papka, srokGotovnostiDerzh, shagOprosaDerzhatelya) {
		log.Printf("запрет мимо туннеля: держатель за %s не встал — снимаю его, до подъёма новой копии запрета не будет", srokGotovnostiDerzh)
		snyat()
		return
	}
	log.Printf("запрет мимо туннеля: «%s» держит держатель до подъёма новой копии (не дольше %s)", p.Slovami(), zaslon.SrokDerzhatelya)
}

// zapustitDerzhatelyaBoevoy — новый exe с --derzhat-zapret. Возвращает, как
// его снять, если он не встал (незнакомый флаг у старой сборки и т.п.).
func zapustitDerzhatelyaBoevoy(put string) (func(), error) {
	cmd := exec.Command(put, ArgDerzhatZapret)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() { _ = cmd.Wait() }()
	return func() { _ = cmd.Process.Kill() }, nil
}
