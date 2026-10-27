package nrpt

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

// Правило ставится и снимается штатно, самой Windows: через PowerShell
// Add-DnsClientNrptRule / Remove-DnsClientNrptRule (модуль DnsClient). Так
// правило получает имя {GUID}, ложится в таблицу и DNS-клиент узнаёт о нём
// сразу — запись в реестр в обход (прежний путь) на стенде Windows не
// подхватила: Get-DnsClientNrptRule и Get-DnsClientNrptPolicy были пусты.
// Описание командлетов: learn.microsoft.com/powershell/module/dnsclient/
// add-dnsclientnrptrule (параметры -Namespace, -NameServers, -Comment).
//
// Своё правило узнаётся по комментарию Kommentariy. Прежние ключи реестра
// KelevraDom* (старая запись, Windows их не читала) снимаются как остатки.

// Zapusk — выполнить сценарий PowerShell; вернуть вывод. Настоящий —
// nrpt_windows.go; проверки подставляют свой, PowerShell машины не трогается.
type Zapusk func(ctx context.Context, scenariy string) (string, error)

// PowerShell — Sistema поверх Add/Remove-DnsClientNrptRule.
type PowerShell struct {
	Zapusk Zapusk
}

const srokVyzova = 45 * time.Second

var (
	guidRe  = regexp.MustCompile(`^\{[0-9A-Fa-f]{8}(-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12}\}$`)
	zonaRe  = regexp.MustCompile(`^\.?[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	reestrP = `HKLM:\SYSTEM\CurrentControlSet\Services\Dnscache\Parameters\DnsPolicyConfig`
)

const nachalo = "$ErrorActionPreference='Stop'; "

func (p *PowerShell) vypolnit(scenariy string) (string, error) {
	ctx, otmena := context.WithTimeout(context.Background(), srokVyzova)
	defer otmena()
	return p.Zapusk(ctx, nachalo+scenariy)
}

func (p *PowerShell) Svoi() ([]string, error) {
	sc := fmt.Sprintf(
		"Get-DnsClientNrptRule | Where-Object { $_.Comment -eq '%s' } | ForEach-Object { $_.Name }; "+
			"Get-ChildItem -LiteralPath '%s' -ErrorAction SilentlyContinue | Where-Object { $_.PSChildName -like '%s*' } | ForEach-Object { $_.PSChildName }",
		Kommentariy, reestrP, Imya)
	out, err := p.vypolnit(sc)
	if err != nil {
		return nil, err
	}
	var imena []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if guidRe.MatchString(l) || strings.HasPrefix(l, Imya) {
			imena = append(imena, l)
		}
	}
	return imena, nil
}

// Postavit — снять прежнее своё правило и добавить новое.
func (p *PowerShell) Postavit(pr Pravilo) error {
	if !zonaRe.MatchString(pr.Zona) {
		return fmt.Errorf("правило NRPT: зона %q недопустима", pr.Zona)
	}
	if net.ParseIP(pr.Server) == nil {
		return fmt.Errorf("правило NRPT: адрес DNS %q недопустим", pr.Server)
	}
	if err := p.Ubrat(Imya); err != nil {
		return err
	}
	sc := fmt.Sprintf("Add-DnsClientNrptRule -Namespace '%s' -NameServers '%s' -Comment '%s' | Out-Null; Clear-DnsClientCache",
		pr.Zona, pr.Server, Kommentariy)
	if _, err := p.vypolnit(sc); err != nil {
		return fmt.Errorf("правило NRPT не создано: %w", err)
	}
	return nil
}

// Ubrat — снять своё правило по имени {GUID}; Imya — все свои. Чужое по
// имени не снимается: правило с другим комментарием пропускается.
func (p *PowerShell) Ubrat(imya string) error {
	imena := []string{imya}
	if imya == Imya {
		var err error
		if imena, err = p.Svoi(); err != nil {
			return fmt.Errorf("правило NRPT не снято: %w", err)
		}
	}
	var chasti []string
	for _, i := range imena {
		switch {
		case guidRe.MatchString(i):
			chasti = append(chasti, fmt.Sprintf(
				"$r = Get-DnsClientNrptRule -Name '%s' -ErrorAction SilentlyContinue; "+
					"if ($r -and $r.Comment -eq '%s') { Remove-DnsClientNrptRule -Name '%s' -Force }", i, Kommentariy, i))
		case strings.HasPrefix(i, Imya) && !strings.ContainsAny(i, `'"\/;$`+"`"+` `):
			chasti = append(chasti, fmt.Sprintf("Remove-Item -LiteralPath '%s\\%s' -Recurse -Force -ErrorAction SilentlyContinue", reestrP, i))
		default:
			return fmt.Errorf("чужое правило NRPT %q не трогаю", i)
		}
	}
	if len(chasti) == 0 {
		return nil
	}
	if _, err := p.vypolnit(strings.Join(chasti, "; ") + "; Clear-DnsClientCache"); err != nil {
		return fmt.Errorf("правило NRPT не снято: %w", err)
	}
	return nil
}
