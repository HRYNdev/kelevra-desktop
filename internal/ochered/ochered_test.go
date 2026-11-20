package ochered

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type podstava struct {
	vybrano string
	mertvye map[string]bool
	vybory  []string
}

func (p *podstava) Vybrano(string) (string, error) { return p.vybrano, nil }
func (p *podstava) Vybrat(_, u string) error       { p.vybrano = u; p.vybory = append(p.vybory, u); return nil }
func (p *podstava) Zamerit(_ context.Context, u string) (int, error) {
	if p.mertvye[u] {
		return 0, errors.New("не ответил")
	}
	return 100, nil
}

func ochered(p *podstava, t *time.Time) *Ochered {
	return &Ochered{Gruppa: "Соединение", Uzly: []string{"Нидерланды", "Финляндия"}, Ya: p,
		Seychas: func() time.Time { return *t }}
}

const profilDvaUzla = `{"outbounds":[
 {"type":"selector","tag":"Соединение","outbounds":["Нидерланды","Финляндия","Комната"]},
 {"type":"urltest","tag":"Нидерланды","outbounds":["Нидерланды · XHTTP"]},
 {"type":"urltest","tag":"Финляндия","outbounds":["Финляндия · XHTTP","Финляндия · Vision"]},
 {"type":"vless","tag":"Нидерланды · XHTTP"},{"type":"socks","tag":"Комната"}]}`

func TestIzKonfiga(t *testing.T) {
	g, u, ok := IzKonfiga([]byte(profilDvaUzla))
	if !ok || g != "Соединение" || len(u) != 2 || u[0] != "Нидерланды" || u[1] != "Финляндия" {
		t.Fatalf("не та очередь: %q %v %v", g, u, ok)
	}
	odin := `{"outbounds":[{"type":"selector","tag":"Соединение","outbounds":["Нидерланды","Комната"]},
	 {"type":"urltest","tag":"Нидерланды","outbounds":["x"]}]}`
	if _, _, ok := IzKonfiga([]byte(odin)); ok {
		t.Fatal("у профиля с одним узлом очереди быть не должно")
	}
}

// Один провал — не довод; второй подряд — на следующий живой узел.
func TestPerehodPoDvumProvalam(t *testing.T) {
	sey := time.Unix(1000, 0)
	p := &podstava{vybrano: "Нидерланды", mertvye: map[string]bool{"Нидерланды": true}}
	o := ochered(p, &sey)
	if o.Shag(context.Background()) {
		t.Fatal("переключились с первого провала")
	}
	if !o.Shag(context.Background()) || p.vybrano != "Финляндия" {
		t.Fatalf("со второго провала ждал Финляндию, стоим на %q", p.vybrano)
	}
}

// Возврат на первый — только когда он жив непрерывно VozvratCherez.
func TestVozvratPosleZhizni(t *testing.T) {
	sey := time.Unix(1000, 0)
	p := &podstava{vybrano: "Финляндия", mertvye: map[string]bool{}}
	o := ochered(p, &sey)
	if o.Shag(context.Background()) {
		t.Fatal("вернулись сразу, без паузы")
	}
	sey = sey.Add(2 * time.Minute)
	if o.Shag(context.Background()) {
		t.Fatal("вернулись раньше срока")
	}
	p.mertvye["Нидерланды"] = true // мигнул — отсчёт с нуля
	sey = sey.Add(time.Minute)
	o.Shag(context.Background())
	delete(p.mertvye, "Нидерланды")
	sey = sey.Add(time.Minute)
	o.Shag(context.Background())
	sey = sey.Add(3 * time.Minute)
	if o.Shag(context.Background()) {
		t.Fatal("после мигания вернулись, не дождавшись полного срока")
	}
	sey = sey.Add(2 * time.Minute)
	if !o.Shag(context.Background()) || p.vybrano != "Нидерланды" {
		t.Fatalf("ждал возврата на Нидерланды, стоим на %q", p.vybrano)
	}
}

// Ручной выбор человека и «не узел» в селекторе — не наше дело.
func TestNeVmeshivaemsya(t *testing.T) {
	sey := time.Unix(1000, 0)
	p := &podstava{vybrano: "Нидерланды", mertvye: map[string]bool{"Нидерланды": true}}
	o := ochered(p, &sey)
	o.Ruchnoy = func() bool { return true }
	o.Shag(context.Background())
	o.Shag(context.Background())
	if len(p.vybory) != 0 {
		t.Fatal("переключили при ручном выборе")
	}
	o.Ruchnoy = nil
	p.vybrano = "Комната"
	o.Shag(context.Background())
	o.Shag(context.Background())
	if len(p.vybory) != 0 {
		t.Fatal("увели с комнаты")
	}
}

// Ни один узел не отвечает — остаёмся, где были.
func TestVseMertvy(t *testing.T) {
	sey := time.Unix(1000, 0)
	p := &podstava{vybrano: "Нидерланды", mertvye: map[string]bool{"Нидерланды": true, "Финляндия": true}}
	o := ochered(p, &sey)
	o.Shag(context.Background())
	if o.Shag(context.Background()) || len(p.vybory) != 0 {
		t.Fatalf("переключили на мёртвый: %v", p.vybory)
	}
}

type opozdanie struct{ podstava }

func (p *opozdanie) Vybrat(g, u string) error { p.podstava.Vybrat(g, u); return errors.New("context deadline exceeded") }

// Ядро переключило, но ответило таймаутом — узел встал, считаем переход состоявшимся.
func TestVybratSOpozdaniem(t *testing.T) {
	sey := time.Unix(1000, 0)
	p := &opozdanie{podstava{vybrano: "Нидерланды", mertvye: map[string]bool{"Нидерланды": true}}}
	o := &Ochered{Gruppa: "Соединение", Uzly: []string{"Нидерланды", "Финляндия"}, Ya: p, Seychas: func() time.Time { return sey }}
	o.Shag(context.Background())
	if !o.Shag(context.Background()) || p.vybrano != "Финляндия" {
		t.Fatalf("переход с опоздавшим ответом не засчитан: %q", p.vybrano)
	}
}

// zhurnal — строки, которые очередь пишет в журнал.
type zhurnal struct{ stroki []string }

func (z *zhurnal) log(f string, a ...any) { z.stroki = append(z.stroki, fmt.Sprintf(f, a...)) }

func (z *zhurnal) skolko(podstroka string) int {
	n := 0
	for _, s := range z.stroki {
		if strings.Contains(s, podstroka) {
			n++
		}
	}
	return n
}

// Ни один узел не отвечает час подряд: строка «ни один» с причинами — при входе в серию, потом
// не чаще NapominatVseMertvy с числом шагов, и одна строка, когда узел ожил. Решения те же:
// никуда не переключаемся, пока все мертвы.
func TestVseMertvyZhurnalSeriey(t *testing.T) {
	sey := time.Unix(1000, 0)
	p := &podstava{vybrano: "Нидерланды", mertvye: map[string]bool{"Нидерланды": true, "Финляндия": true}}
	o := ochered(p, &sey)
	z := &zhurnal{}
	o.Log = z.log

	shagov := 0
	for proshlo := time.Duration(0); proshlo <= time.Hour; proshlo += 34 * time.Second {
		sey = time.Unix(1000, 0).Add(proshlo)
		if o.Shag(context.Background()) {
			t.Fatal("переключились, когда все мертвы")
		}
		shagov++
	}
	if n := z.skolko("ни один узел не отвечает:"); n != 1 {
		t.Fatalf("строк о входе в серию %d, ждал 1:\n%s", n, strings.Join(z.stroki, "\n"))
	}
	if !strings.Contains(z.stroki[1], "«Нидерланды» — не ответил") || !strings.Contains(z.stroki[1], "«Финляндия» — не ответил") {
		t.Fatalf("в строке о серии нет причин по узлам: %q", z.stroki[1])
	}
	napominaniy := z.skolko("всё ещё не отвечает")
	if napominaniy < 5 || napominaniy > 6 {
		t.Fatalf("напоминаний за час %d, ждал 5–6 (раз в %s):\n%s", napominaniy, NapominatVseMertvy, strings.Join(z.stroki, "\n"))
	}
	if len(z.stroki) > 10 {
		t.Fatalf("за %d шагов %d строк — журнал снова шумит:\n%s", shagov, len(z.stroki), strings.Join(z.stroki, "\n"))
	}

	delete(p.mertvye, "Нидерланды")
	sey = sey.Add(34 * time.Second)
	o.Shag(context.Background())
	if z.skolko("узлы снова отвечают") != 1 {
		t.Fatalf("нет строки о выходе из серии:\n%s", strings.Join(z.stroki, "\n"))
	}
	n := len(z.stroki)
	sey = sey.Add(34 * time.Second)
	o.Shag(context.Background())
	if len(z.stroki) != n {
		t.Fatalf("живой узел пишет лишнее: %v", z.stroki[n:])
	}
	if len(p.vybory) != 0 {
		t.Fatalf("очередь переключала: %v", p.vybory)
	}
	t.Logf("журнал за час:\n%s", strings.Join(z.stroki, "\n"))
}

// Нет шлюза по умолчанию — пишем «у компьютера нет сети», а не «узлы не отвечают»; сеть
// вернулась, а узлы всё ещё мертвы — это уже другая серия, о ней своя строка.
func TestVseMertvyBezSeti(t *testing.T) {
	sey := time.Unix(1000, 0)
	p := &podstava{vybrano: "Нидерланды", mertvye: map[string]bool{"Нидерланды": true, "Финляндия": true}}
	o := ochered(p, &sey)
	z := &zhurnal{}
	o.Log = z.log
	set := false
	o.EstSet = func() bool { return set }

	for i := 0; i < 5; i++ {
		sey = sey.Add(34 * time.Second)
		o.Shag(context.Background())
	}
	if z.skolko("у компьютера нет сети") != 1 || z.skolko("ни один узел") != 0 {
		t.Fatalf("без сети ждал одну строку «нет сети» и ни одной «ни один узел»:\n%s", strings.Join(z.stroki, "\n"))
	}
	set = true
	sey = sey.Add(34 * time.Second)
	o.Shag(context.Background())
	if z.skolko("ни один узел не отвечает:") != 1 {
		t.Fatalf("сеть есть, узлы мертвы — ждал строку «ни один»:\n%s", strings.Join(z.stroki, "\n"))
	}
	p.mertvye = map[string]bool{}
	sey = sey.Add(34 * time.Second)
	o.Shag(context.Background())
	if z.skolko("узлы снова отвечают") != 1 {
		t.Fatalf("нет строки о выходе:\n%s", strings.Join(z.stroki, "\n"))
	}
}

// Причины Clash API — по-русски; ошибка срока шага — своя; длинное обрезано в одну строку.
func TestPrichinaZamera(t *testing.T) {
	sluchai := map[string]error{
		"нет ответа за срок замера":             errors.New("Timeout"),
		"соединение через узел не установилось": errors.New("An error occurred in the delay test"),
		"замер не уложился в срок шага":         fmt.Errorf("Get: %w", context.DeadlineExceeded),
		"узел не ответил":                       errors.New("узел не ответил"),
	}
	for zhdu, err := range sluchai {
		if got := prichinaZamera(err); got != zhdu {
			t.Errorf("%v: %q, ждал %q", err, got, zhdu)
		}
	}
	dlinnaya := prichinaZamera(errors.New(strings.Repeat("а", 300) + "\nвторая строка"))
	if strings.Contains(dlinnaya, "\n") || len([]rune(dlinnaya)) > 121 {
		t.Fatalf("причина не в одну короткую строку: %q", dlinnaya)
	}
}

// Провал текущего узла (до порога) — строка с причиной.
func TestProvalSPrichinoy(t *testing.T) {
	sey := time.Unix(1000, 0)
	p := &podstava{vybrano: "Нидерланды", mertvye: map[string]bool{"Нидерланды": true}}
	o := ochered(p, &sey)
	z := &zhurnal{}
	o.Log = z.log
	o.Shag(context.Background())
	if len(z.stroki) != 1 || !strings.HasSuffix(z.stroki[0], "(1 из 2): не ответил") {
		t.Fatalf("строка провала без причины: %v", z.stroki)
	}
}
