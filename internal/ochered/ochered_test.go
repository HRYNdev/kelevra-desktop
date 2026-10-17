package ochered

import (
	"context"
	"errors"
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
