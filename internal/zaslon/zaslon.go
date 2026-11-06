// Пакет zaslon: на время, пока служба сама перезапускает ядро, трафик мимо
// туннеля не выпускается (fail-closed).
//
// Зачем. Ядро держит туннель (адаптер tun125, auto_route). Когда служба сама
// гасит ядро и поднимает снова (самоизлечение «Дома», смена блока дома,
// подъём после смены версии), адаптера нет от 2 до 30 секунд, и всё, что
// программы открывают в эти секунды, уходит напрямую через физический
// адаптер. Живой случай: ноут в Крыму, запросы Claude ушли через местного
// провайдера, и Anthropic ответил 403 «unsupported region» — ровно между
// «останавливаю ядро» 17:43:16 и «поднят туннель» 17:43:18.
//
// Как. На время перезапуска ставится запрет всего исходящего, кроме самого
// ядра (ему надо достучаться до узлов), самой службы, петли, DHCP и
// локальной сети. Туннель поднят — запрет снят. Настоящий запрет — фильтры
// WFP в динамической сессии (wfp_windows.go): умерла служба — фильтры
// исчезли сами, вечной блокировки интернета быть не может.
//
// Предохранитель по времени: запрет снимается сам не позже Srok (60 с), даже
// если ядро не поднялось. Лучше утечка, чем человек без интернета.
// Запрет не встал (WFP недоступен, ошибка) — перезапуск идёт как раньше.
//
// На «Отключить» человека и на выход из туннеля (дома) запрет не ставится:
// решает вызывающий (internal/sluzhba), здесь только «закрыть» и «открыть».
package zaslon

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// SrokPoUmolchaniyu — дольше этого запрет не держится никогда.
const SrokPoUmolchaniyu = 60 * time.Second

// Sistema — где живёт запрет. Настоящая — WFP (wfp_windows.go); проверки
// подставляют свою: на машине, где идут проверки, сеть не трогается.
type Sistema interface {
	// Zakryt — закрыть всё исходящее (IPv4 и IPv6), кроме программ
	// programmy (полные пути к exe), петли, DHCP и локальной сети.
	Zakryt(programmy []string) error
	// Otkryt — снять запрет. Повтор на снятом — не ошибка.
	Otkryt() error
}

// Zaslon — держит запрет на время перезапуска ядра. Нулевой Sys (и nil
// *Zaslon) — ничего не делать: не Windows, стенд, проверки.
type Zaslon struct {
	Sys Sistema
	// Programmy — чьё исходящее пропускать (ядро и сама служба); зовётся на
	// каждом закрытии: путь к ядру мог смениться.
	Programmy func() []string
	// Srok — предохранитель; 0 — SrokPoUmolchaniyu.
	Srok time.Duration

	mu      sync.Mutex
	zakryt  bool
	nachalo time.Time
	nomer   uint64 // растёт на каждом закрытии: старый таймер не трогает новый запрет
	taymer  *time.Timer
}

func (z *Zaslon) srok() time.Duration {
	if z.Srok > 0 {
		return z.Srok
	}
	return SrokPoUmolchaniyu
}

// Zakryt ставит запрет перед тем, как служба сама погасит ядро. true —
// запрет стоит (поставлен сейчас или уже стоял); false — не ставится (нет
// системы) или не встал (ошибка — строка в журнал, перезапуск идёт как
// раньше). Повтор при стоящем запрете срок не продлевает.
func (z *Zaslon) Zakryt() bool {
	if z == nil || z.Sys == nil {
		return false
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.zakryt {
		return true
	}
	var programmy []string
	if z.Programmy != nil {
		programmy = z.Programmy()
	}
	if err := z.Sys.Zakryt(programmy); err != nil {
		log.Printf("перезапуск ядра: закрыть трафик мимо туннеля не вышло (%v) — перезапускаю как раньше", err)
		// Половина фильтров могла встать: снимаем всё, что есть.
		_ = z.Sys.Otkryt()
		return false
	}
	z.zakryt, z.nachalo = true, time.Now()
	z.nomer++
	nomer := z.nomer
	srok := z.srok()
	z.taymer = time.AfterFunc(srok, func() { z.poSroku(nomer, srok) })
	log.Printf("перезапуск ядра: трафик мимо туннеля закрыт до подъёма (не дольше %s)", sekundy(srok))
	return true
}

// poSroku — предохранитель: запрет того же закрытия всё ещё стоит.
func (z *Zaslon) poSroku(nomer uint64, srok time.Duration) {
	z.mu.Lock()
	defer z.mu.Unlock()
	if !z.zakryt || z.nomer != nomer {
		return
	}
	z.snyat()
	log.Printf("перезапуск ядра: трафик мимо туннеля открыт по сроку %s, туннель не поднялся", sekundy(srok))
}

// OtkrytPodnyat — туннель поднят: запрет снять.
func (z *Zaslon) OtkrytPodnyat() {
	if z == nil || z.Sys == nil {
		return
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if !z.zakryt {
		return
	}
	proshlo := time.Since(z.nachalo)
	z.snyat()
	log.Printf("перезапуск ядра: трафик мимо туннеля открыт: туннель поднят за %s", sekundy(proshlo))
}

// Otkryt — перезапуск кончился без туннеля (подъём не удался, решение о
// связи сменилось, связь опущена): запрет снять, prichina — в журнал.
func (z *Zaslon) Otkryt(prichina string) {
	if z == nil || z.Sys == nil {
		return
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if !z.zakryt {
		return
	}
	proshlo := time.Since(z.nachalo)
	z.snyat()
	log.Printf("перезапуск ядра: трафик мимо туннеля открыт через %s: %s", sekundy(proshlo), prichina)
}

// Zakryto — стоит ли запрет сейчас (для проверок и диагностики).
func (z *Zaslon) Zakryto() bool {
	if z == nil {
		return false
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.zakryt
}

// snyat — под z.mu.
func (z *Zaslon) snyat() {
	if z.taymer != nil {
		z.taymer.Stop()
		z.taymer = nil
	}
	z.zakryt = false
	if err := z.Sys.Otkryt(); err != nil {
		log.Printf("перезапуск ядра: запрет трафика мимо туннеля снят с ошибкой: %v — повторю через 2 с", err)
		// Фильтры могли остаться: без повтора человек сидел бы без сети до
		// смерти службы. Повтор — только если за это время не встал новый запрет
		// (его снимет свой путь).
		time.AfterFunc(2*time.Second, func() {
			z.mu.Lock()
			defer z.mu.Unlock()
			if z.zakryt {
				return
			}
			if err := z.Sys.Otkryt(); err != nil {
				log.Printf("перезапуск ядра: повторное снятие запрета не вышло: %v", err)
			} else {
				log.Printf("перезапуск ядра: запрет снят со второй попытки")
			}
		})
	}
}

// sekundy — «1,8 с», «60 с»: десятичная запятая, как пишут люди.
func sekundy(d time.Duration) string {
	if d%time.Second == 0 {
		return fmt.Sprintf("%d с", int64(d/time.Second))
	}
	return strings.Replace(fmt.Sprintf("%.1f с", d.Seconds()), ".", ",", 1)
}
