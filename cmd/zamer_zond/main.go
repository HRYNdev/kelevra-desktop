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
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/avtorezhim"
)

func main() {
	rezhim := flag.String("rezhim", "", "dns | adapter | avtorezhim")
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
	default:
		fmt.Fprintln(os.Stderr, "нужен -rezhim dns|adapter|avtorezhim")
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
		n, izmenilos, t := a.Zahod(ctx, true)
		tek = t
		fmt.Printf("заход %d: ZondSlep=%v DnsPriznakDoma=%v TrafikPryamoy=%v izmenilos=%v tekushcheye=%v\n",
			i, n.ZondSlep, n.DnsPriznakDoma, trafikStr(n.TrafikPryamoy), izmenilos, t)
	}
	fmt.Printf("итог: tekushcheye=%v\n", tek)
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
