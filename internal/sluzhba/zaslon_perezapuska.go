package sluzhba

// Трафик мимо туннеля на время перезапуска ядра (internal/zaslon).
//
// Когда служба САМА гасит ядро, чтобы поднять снова, туннеля нет от 2 до 30
// секунд, и всё, что программы открывают в эти секунды, уходит напрямую через
// физический адаптер (живой случай: ноут в Крыму, запросы Claude ушли через
// местного провайдера, ответ 403 «unsupported region»). На это время исходящее
// закрыто, кроме ядра, самой службы, петли, DHCP и локальной сети; туннель
// поднят — открыто. Дольше zaslon.SrokPoUmolchaniyu (60 с) запрет не держится.
//
// Где ставится (плановый перезапуск, туннель был и должен вернуться):
//   - perezapustit (dom.go): самоизлечение «Дома» и смена блока дома — в том
//     числе когда блок сменился по ежечасной сводке (obnovitProfilRaz →
//     ObnovitSvedeniya → prinyatDom → perepodnyatDlyaDoma) и когда подъём
//     внутри перезапуска ставит новое ядро и откатывает его (smena_yadra.go);
//   - podnyatSvyazPosleSmenyVersii (priyom_yadra.go): прошлая копия держала
//     туннель и оборвала его обновлением.
//
// Где НЕ ставится, нарочно: «Отключить» человека, авторежим «дома»
// (OpustitZashchitu), уход копии новее или службе Windows — это выключение,
// трафик должен идти напрямую; OpustitZashchitu снимает и стоящий запрет.
// Обычный подъём из выключенного («Подключиться», авторежим «вне дома») —
// не перезапуск: туннеля до него не было.
//
// Стенд и проверки (KELEVRA_DIR задан) и KELEVRA_ZASLON=net — запрет никогда
// не ставится по-настоящему: на машине проверяющего сеть не меняется.

import (
	"fmt"
	"os"

	"github.com/HRYNdev/kelevra-desktop/internal/zaslon"
)

// nachatZaslon — запрет на настоящей системе (WFP на Windows).
func (s *Sluzhba) nachatZaslon() {
	sys := zaslon.NovayaSistema()
	if os.Getenv("KELEVRA_DIR") != "" || os.Getenv("KELEVRA_ZASLON") == "net" {
		sys = nil
	}
	s.zaslon = &zaslon.Zaslon{Sys: sys, Programmy: s.programmyMimoZaslona}
}

// programmyMimoZaslona — кому наружу можно и при запрете: ядру (до узлов
// VPS) и самой службе (подписка, сводка, журналы).
func (s *Sluzhba) programmyMimoZaslona() []string {
	var p []string
	if s.Yadro != nil && s.Yadro.Bin != "" {
		p = append(p, s.Yadro.Bin)
	}
	if exe, err := os.Executable(); err == nil {
		p = append(p, exe)
	}
	return p
}

// zakrytMimoTunnelya — перед тем как служба сама погасит ядро ради подъёма.
// Не встал запрет — перезапуск идёт как раньше (строка в журнале — у zaslon).
func (s *Sluzhba) zakrytMimoTunnelya() {
	s.zaslon.Zakryt()
}

// otkrytMimoTunnelya — перезапуск кончился без туннеля (или связь опущена).
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
