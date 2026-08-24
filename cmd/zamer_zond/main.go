// Замерочный вход для internal/avtorezhim: дёргает НАСТОЯЩИЙ код пакета
// (DnsZond, SetevoyAdapter, Avtorezhim) на живой сети, а не самодельную
// имитацию — тот же приём, что cmd/zamer_konfig для internal/konfig.
// Используется stend/zond_doma.sh.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
)

func main() {
	rezhim := flag.String("rezhim", "", "dns | adapter | avtorezhim | sluzhitel")
	resolver := flag.String("resolver", "", "AdresResolvera для DnsZond, вида host:port (пусто — системный резолвер)")
	lokalny := flag.String("lokalny", "", "LokalnyAdres для DnsZond (IP физического адаптера)")
	taimaut := flag.Duration("taimaut", 3*time.Second, "DnsZond.Taimaut — публичный резолвер по WAN бывает медленнее LAN")
	slep := flag.Bool("slep", false, "avtorezhim: симулировать «адрес адаптера неизвестен» вместо настоящего SetevoyAdapter")
	zahodov := flag.Int("zahodov", avtorezhim.Podtverzhdeniy, "avtorezhim: сколько заходов подряд сделать")
	flag.Parse()

	switch *rezhim {
	case "dns":
		zamerDns(*resolver, *lokalny, *taimaut)
	case "adapter":
		zamerAdapter()
	case "avtorezhim":
		zamerAvtorezhim(*slep, *zahodov)
	case "sluzhitel":
		zamerSluzhitel()
	default:
		fmt.Fprintln(os.Stderr, "нужен -rezhim dns|adapter|avtorezhim|sluzhitel")
		os.Exit(2)
	}
}

func zamerDns(resolver, lokalny string, taimaut time.Duration) {
	z := avtorezhim.NovyyDnsZond()
	z.AdresResolvera = resolver
	z.LokalnyAdres = lokalny
	z.Taimaut = taimaut
	ctx, otmena := context.WithTimeout(context.Background(), taimaut+2*time.Second)
	defer otmena()
	doma, err := z.DomaPoDns(ctx)
	fmt.Printf("resolver=%q lokalny=%q doma=%v err=%v\n", resolver, lokalny, doma, err)
	if err != nil {
		os.Exit(1)
	}
}

func zamerAdapter() {
	dns, lokalny, err := avtorezhim.SetevoyAdapter()
	fmt.Printf("dns=%q lokalny=%q err=%v\n", dns, lokalny, err)
	if err != nil {
		os.Exit(1)
	}
}

func zamerAvtorezhim(slep bool, zahodov int) {
	a := &avtorezhim.Avtorezhim{
		Trafik:        avtorezhim.NovyyPryamoyZond(),
		Zadvizhka:     avtorezhim.NovayaZadvizhka(avtorezhim.VneDoma),
		TunnelPodnyat: func() bool { return true }, // сценарий полной защиты: TUN-вход поднят
	}
	if slep {
		a.SetevoyAdres = func() (string, string, error) {
			return "", "", fmt.Errorf("симуляция: адрес адаптера неизвестен")
		}
	} else {
		a.SetevoyAdres = avtorezhim.SetevoyAdapter // настоящий код, не мок
	}

	ctx := context.Background()
	var tek avtorezhim.Sostoyanie
	for i := 0; i < zahodov; i++ {
		n, izmenilos, t := a.Zahod(ctx, true, false)
		tek = t
		fmt.Printf("заход %d: ZondSlep=%v DnsPriznakDoma=%v TrafikPryamoy=%v izmenilos=%v tekushcheye=%v\n",
			i, n.ZondSlep, n.DnsPriznakDoma, trafikStr(n.TrafikPryamoy), izmenilos, t)
	}
	fmt.Printf("итог: tekushcheye=%v\n", tek)
}

// ruchnoySledchik — Sledchik, событие в который шлёт сам замер (без
// настоящей ОС), чтобы измерить именно логику Sluzhitel/Zadvizhka, а не
// скорость самих зондов (та измерена отдельно режимами dns/adapter).
type ruchnoySledchik struct{ sobytiya chan struct{} }

func (r *ruchnoySledchik) Sobytiya() <-chan struct{} { return r.sobytiya }
func (r *ruchnoySledchik) Stop()                     {}

// perekluchaemyyDns — DNS-зонд с ответом, который замер переключает между
// "не дома" и "дома" по ходу дела (под мьютексом — читает его фоновый цикл
// Sluzhitel.Krutit из другой горутины).
type perekluchaemyyDns struct {
	mu   sync.Mutex
	doma bool
}

func (d *perekluchaemyyDns) DomaPoDns(ctx context.Context) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.doma, nil
}

func (d *perekluchaemyyDns) ustanovit(doma bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.doma = doma
}

// vsegdaProshedshiyTrafik — прямой зонд, который всегда отвечает "прошёл".
type vsegdaProshedshiyTrafik struct{}

func (vsegdaProshedshiyTrafik) Proshel(ctx context.Context) (bool, bool) { return true, true }

// zamerSluzhitel измеряет ЖИВЫМ прогоном (реальный Sluzhitel.Krutit, реальные
// таймеры PauzaDrebezga/time.After), сколько проходит от события смены сети
// до смены вердикта Avtorezhim — то есть от [Sledchik.Sobytiya] до вызова
// Sluzhitel.Kolbek. DNS/трафик — подставные (их собственная скорость уже
// измерена режимами dns/adapter), здесь под замером ровно то, что чинили:
// Zadvizhka + доверие событию смены сети.
func zamerSluzhitel() {
	dns := &perekluchaemyyDns{doma: false}
	a := &avtorezhim.Avtorezhim{
		Dns:       dns,
		Trafik:    vsegdaProshedshiyTrafik{},
		Zadvizhka: avtorezhim.NovayaZadvizhka(avtorezhim.VneDoma),
	}
	sl := &ruchnoySledchik{sobytiya: make(chan struct{})}

	var mu sync.Mutex
	var start time.Time
	promezhutok := make(chan time.Duration, 1)

	sluzh := &avtorezhim.Sluzhitel{
		Avtorezhim: a,
		Sledchik:   sl,
		Interval:   time.Hour, // страховочный тикер тут не при чём — гасим на "никогда"
		Kolbek: func(ctx context.Context, s avtorezhim.Sostoyanie) {
			mu.Lock()
			d := time.Since(start)
			mu.Unlock()
			promezhutok <- d
			fmt.Printf("Kolbek: обстановка -> %v, прошло %v с события\n", s, d)
		},
	}

	ctx, otmena := context.WithCancel(context.Background())
	defer otmena()
	gotovo := make(chan struct{})
	go func() { sluzh.Krutit(ctx); close(gotovo) }()

	time.Sleep(50 * time.Millisecond) // дать стартовому заходу отработать (VneDoma == VneDoma, без смены)

	dns.ustanovit(true) // сеть теперь "дома"
	mu.Lock()
	start = time.Now()
	mu.Unlock()
	sl.sobytiya <- struct{}{} // РЕАЛЬНОЕ событие смены сети

	select {
	case d := <-promezhutok:
		fmt.Printf("итог: событие смены сети -> смена вердикта = %v\n", d)
	case <-time.After(10 * time.Second):
		fmt.Println("итог: не дождались смены вердикта за 10с")
		otmena()
		<-gotovo
		os.Exit(1)
	}

	otmena()
	<-gotovo
}

func trafikStr(p *bool) string {
	if p == nil {
		return "не проверяли"
	}
	if *p {
		return "прошёл"
	}
	return "не прошёл"
}
