//go:build linux

package zaslon

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Запрет на Linux — своя таблица nftables (inet kelevra_zaslon) на хуке output:
// те же фильтры, что WFP на Windows (filtry.go), в том же порядке весов.
// Первое совпавшее правило решает (accept/drop), поэтому правила идут по
// убыванию веса — это и есть «наибольший вес побеждает» из WFP.
//
// Таблица ставится одной транзакцией nft -f: «создать, удалить, создать
// заново» — не встало, стоит прежняя. Процесс её не держит: после падения
// службы она остаётся, а следующий старт ставит свою (или снимает).
// Аварийно снять руками: sudo nft delete table inet kelevra_zaslon
// (или kelevra --snyat-zapret).
const tablicaNft = "kelevra_zaslon"

type nftSistema struct{ nft string }

// NovayaSistema — nftables есть и мы root (служба systemd); иначе запрета нет.
func NovayaSistema() Sistema {
	if os.Geteuid() != 0 {
		return nil
	}
	p, err := exec.LookPath("nft")
	if err != nil {
		log.Printf("запрет мимо туннеля: nft не найден (%v) — запрета на этой машине нет", err)
		return nil
	}
	return &nftSistema{nft: p}
}

func (n *nftSistema) zapustit(skript string) error {
	cmd := exec.Command(n.nft, "-f", "-")
	cmd.Stdin = strings.NewReader(skript)
	var vyvod bytes.Buffer
	cmd.Stdout, cmd.Stderr = &vyvod, &vyvod
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("nft: %w: %s", err, strings.TrimSpace(vyvod.String()))
	}
	return nil
}

func (n *nftSistema) Primenit(filtry []Filtr) error { return n.zapustit(SkriptNft(filtry)) }

func (n *nftSistema) Snyat() error {
	// «Объявить и удалить» — удаление не падает, если таблицы нет.
	return n.zapustit(fmt.Sprintf("table inet %s\ndelete table inet %s\n", tablicaNft, tablicaNft))
}

// SkriptNft — сценарий nft для набора фильтров (чистая функция, проверяется тестом).
func SkriptNft(filtry []Filtr) string {
	po := append([]Filtr(nil), filtry...)
	sort.SliceStable(po, func(i, j int) bool { return po[i].Ves > po[j].Ves })
	var b strings.Builder
	fmt.Fprintf(&b, "table inet %s\ndelete table inet %s\n", tablicaNft, tablicaNft)
	fmt.Fprintf(&b, "table inet %s {\n\tchain vyhod {\n\t\ttype filter hook output priority filter; policy accept;\n", tablicaNft)
	for _, f := range po {
		fmt.Fprintf(&b, "\t\t%s\n", pravilo(f))
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

func pravilo(f Filtr) string {
	sem, ip := "ipv4", "ip"
	if f.Semeystvo == V6 {
		sem, ip = "ipv6", "ip6"
	}
	chasti := []string{"meta nfproto " + sem}
	po := map[Pole][]Uslovie{}
	var poryadok []Pole
	for _, u := range f.Usloviya {
		if _, est := po[u.Pole]; !est {
			poryadok = append(poryadok, u.Pole)
		}
		po[u.Pole] = append(po[u.Pole], u)
	}
	icmp := false
	for _, u := range po[PoleProtokol] {
		if u.Chislo == protokolIcmpV6 {
			icmp = true
		}
	}
	for _, pole := range poryadok {
		us := po[pole]
		switch pole {
		case PoleUdalennyy:
			chasti = append(chasti, ip+" daddr "+mnozhestvo(us, true))
		case PoleLokalnyy:
			chasti = append(chasti, ip+" saddr "+mnozhestvo(us, true))
		case PolePetlya:
			chasti = append(chasti, `oifname "lo"`)
		case PoleProtokol:
			chasti = append(chasti, "meta l4proto "+mnozhestvo(us, false))
		case PoleLokalnyyPort:
			// У ICMP «локальный порт» в WFP — тип сообщения (NDP).
			if icmp {
				chasti = append(chasti, "icmpv6 type "+mnozhestvo(us, false))
			} else {
				chasti = append(chasti, "th sport "+mnozhestvo(us, false))
			}
		case PoleUdalennyyPort:
			chasti = append(chasti, "th dport "+mnozhestvo(us, false))
		case PoleProgramma:
			// По пути программы nftables не различает. Программы в фильтрах —
			// ядро и служба, обе от root: разрешаем сокеты root. Это шире, чем
			// на Windows, и только для полного запрета (строгий режим).
			chasti = append(chasti, "meta skuid 0")
		}
	}
	verdikt := "accept"
	if f.Zapret {
		verdikt = "drop"
	}
	imya := strings.NewReplacer(`"`, "", `\`, "").Replace(f.Imya)
	return fmt.Sprintf("%s %s comment \"%s\"", strings.Join(chasti, " "), verdikt, imya)
}

func mnozhestvo(us []Uslovie, seti bool) string {
	var z []string
	vidano := map[string]bool{}
	for _, u := range us {
		s := fmt.Sprint(u.Chislo)
		if seti {
			s = u.Set.Masked().String()
		}
		if !vidano[s] {
			vidano[s] = true
			z = append(z, s)
		}
	}
	return "{ " + strings.Join(z, ", ") + " }"
}
