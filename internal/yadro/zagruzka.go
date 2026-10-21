package yadro

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/obnovlenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpis"
)

// Relizy — откуда приложение берёт ядро. Сборка ядра лежит в релизах этого же
// репозитория (теги core-*), собирает её .github/workflows/core.yml.
//
// Окно 100, а не 20: в этом же репозитории живут релизы приложения, и их уже 31
// против одной сборки ядра. GitHub отдаёт список по дате коммита тега, поэтому
// единственный core-релиз (03.08) провалился на 31-е место — при per_page=20
// приложение переставало его видеть и отвечало «в сборках ядра нет файла».
// Замер 25.08: окно 20 → ядро НЕ найдено, окно 100 → core-v1.14.0-beta.4-1.
const Relizy = "https://api.github.com/repos/HRYNdev/kelevra-desktop/releases?per_page=100"

// ImyaArhiva — ассет под текущую систему.
func ImyaArhiva() string {
	return fmt.Sprintf("sing-box-%s-%s.zip", runtime.GOOS, runtime.GOARCH)
}

// Zagruzit скачивает ядро и кладёт его на место. Пользователю не нужно
// ничего ставить руками: приложение приносит ядро себе само.
func (y *Yadro) Zagruzit(ctx context.Context) error {
	// Файла ядра нет, а прежнее подтверждённое лежит рядом (обрыв замены) —
	// вернуть его, а не качать заново.
	// Только при совпадении хэша с журналом замены.
	y.zamokSmeny.Lock()
	vernul := y.prezhneeIzZhurnala()
	y.zamokSmeny.Unlock()
	if vernul {
		log.Printf("файла ядра не было — вернул прежнее ядро, лежавшее рядом (хэш сошёлся с журналом)")
		return nil
	}
	// Сборки, которые на этой машине не поднялись, не ставятся и заново.
	y.zamokSmeny.Lock()
	otvergnuty := y.chitatVersiyu().Otvergnuty
	y.zamokSmeny.Unlock()
	sborka, err := y.vybratSborku(ctx, &http.Client{Timeout: 20 * time.Second}, obnovlenie.TrebovatPodpis, otvergnuty...)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(y.Papka, 0o755); err != nil {
		return err
	}
	arhiv := filepath.Join(y.Papka, "yadro.zip")
	if err := skachat(ctx, &http.Client{Timeout: 10 * time.Minute}, sborka.Ssylka, arhiv); err != nil {
		return err
	}
	defer os.Remove(arhiv)
	// Ядро запускает служба от системы, поэтому архив ставится только с верной
	// подписью нашего ключа (internal/podpis), до распаковки.
	//
	// Первая установка принимает оба формата подписи: живая сборка
	// core-v1.14.0-beta.4-1 подписана первым (без тега). Версию же ядра
	// запоминаем только по подписи с тегом: тег без подписи ничего не
	// доказывает, а записанная чужая «версия» закрыла бы машине замену.
	podpisSTegom := false
	if sborka.SsylkaPodpisi != "" || obnovlenie.TrebovatPodpis {
		sig, err := skachatPodpis(ctx, &http.Client{Timeout: 30 * time.Second}, sborka.SsylkaPodpisi)
		if err == nil {
			podpisSTegom = podpis.ProveritSTegom(arhiv, ImyaArhiva(), sborka.Teg, sig) == nil
			if !podpisSTegom && obnovlenie.TrebovatPodpis {
				err = podpis.Proverit(arhiv, ImyaArhiva(), sig)
			}
		}
		if err != nil && obnovlenie.TrebovatPodpis {
			log.Printf("подпись ядра %s НЕ подтверждена (ed25519): %v — ядро не поставлено", ImyaArhiva(), err)
			return fmt.Errorf("%w: ядро не поставлено (%v)", obnovlenie.ErrPodpis, err)
		}
		if obnovlenie.TrebovatPodpis {
			if podpisSTegom {
				log.Printf("подпись ядра %s верна (ed25519, тег %s), распаковываю", ImyaArhiva(), sborka.Teg)
			} else {
				log.Printf("подпись ядра %s верна (ed25519, прежний формат без тега), распаковываю", ImyaArhiva())
			}
		}
	}
	y.zamokSmeny.Lock()
	defer y.zamokSmeny.Unlock()
	if err := raspakovat(arhiv, y.Bin); err != nil {
		return err
	}
	if podpisSTegom {
		// Версию поставленного ядра запоминаем сразу: по ней служба потом
		// решает, нужна ли замена (smena.go).
		y.zapisatVersiyu(sborka.Teg)
	}
	return nil
}

// ErrNetNeOtvergnutoy — все подходящие сборки ядра на этом компьютере уже
// не запускались. Текст — для человека.
var ErrNetNeOtvergnutoy = errors.New("Новая сборка ядра Kelevra не запустилась на этом компьютере. Ожидается исправленная сборка.")

// Sborka — один выпуск ядра в релизах: тег и ссылки на архив и его подпись.
type Sborka struct {
	Teg           string
	Ssylka        string
	SsylkaPodpisi string
}

// ssylkaNaYadro — ссылки на архив и подпись самой свежей годной сборки.
func (y *Yadro) ssylkaNaYadro(ctx context.Context) (string, string, error) {
	s, err := y.vybratSborku(ctx, &http.Client{Timeout: 20 * time.Second}, obnovlenie.TrebovatPodpis)
	return s.Ssylka, s.SsylkaPodpisi, err
}

// vybratSborku — самая свежая годная сборка ядра из списка релизов.
//
// Свежесть — по версии из тега (versiya_sborki.go), а не по месту в списке:
// GitHub упорядочивает релизы по дате коммита тега. Тег, который не
// разбирается, пропускается с записью в журнал.
//
// Пробные выпуски (prerelease) и черновики пропускаются: пробное ядро не
// должно уходить ни новым установкам, ни на замену у стоящих (до 21.10.2026
// любой подписанный core-* сразу уходил новым установкам). Стенд берёт
// пробные явно — флагом BratProbnye или своим списком (Spisok).
// nuzhnaPodpis — сборки без файла подписи пропускаются.
// propustit — теги, которые не брать (отвергнутые на этой машине).
func (y *Yadro) vybratSborku(ctx context.Context, klient *http.Client, nuzhnaPodpis bool, propustit ...string) (Sborka, error) {
	relizy, err := spisokRelizov(ctx, klient, y.spisok())
	if err != nil {
		return Sborka{}, err
	}
	nuzhen := ImyaArhiva()
	var luchshaya Sborka
	var luchshayaV versiyaSborki
	propushcheno := false
	for _, r := range relizy {
		if !strings.HasPrefix(r.Tag, "core-") || r.Draft {
			continue
		}
		if r.Prerelease && !y.BratProbnye {
			continue
		}
		if slices.Contains(propustit, r.Tag) {
			propushcheno = true
			continue
		}
		var ssylka, ssylkaPodpisi string
		for _, a := range r.Assets {
			switch a.Name {
			case nuzhen:
				ssylka = a.URL
			case nuzhen + podpis.SufiksFayla:
				ssylkaPodpisi = a.URL
			}
		}
		if ssylka == "" {
			continue
		}
		// Сборка ядра без подписи (выложена до подписей) пропускается: берём
		// самую свежую ПОДПИСАННУЮ; новых сборок без подписи не бывает, их
		// подписывает та же сборка (core.yml).
		if nuzhnaPodpis && ssylkaPodpisi == "" {
			continue
		}
		v, err := razobratTeg(r.Tag)
		if err != nil {
			log.Printf("сборка ядра пропущена: %v", err)
			continue
		}
		if luchshaya.Teg == "" || sravnitSborki(v, luchshayaV) > 0 {
			luchshaya, luchshayaV = Sborka{Teg: r.Tag, Ssylka: ssylka, SsylkaPodpisi: ssylkaPodpisi}, v
		}
	}
	if luchshaya.Teg != "" {
		return luchshaya, nil
	}
	if propushcheno {
		return Sborka{}, ErrNetNeOtvergnutoy
	}
	if nuzhnaPodpis {
		return Sborka{}, fmt.Errorf("в сборках ядра нет подписанного файла %s", nuzhen)
	}
	return Sborka{}, fmt.Errorf("в сборках ядра нет файла %s", nuzhen)
}

// relizYadra — релиз из списка GitHub, только нужные поля.
type relizYadra struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// spisokRelizov — список релизов постранично общей функцией
// obnovlenie.SobratStranicy (Link rel="next", тот же сервер, не больше
// obnovlenie.PredelStranicRelizov страниц). Здесь — только слова ошибок ядра.
func spisokRelizov(ctx context.Context, klient *http.Client, adres string) ([]relizYadra, error) {
	relizy, err := obnovlenie.SobratStranicy[relizYadra](ctx, klient, adres, 8<<20)
	if err != nil {
		var status *obnovlenie.OshibkaStatusa
		var razbor *obnovlenie.OshibkaRazbora
		switch {
		case errors.As(err, &status):
			return nil, fmt.Errorf("список сборок ядра недоступен: %w", err)
		case errors.As(err, &razbor):
			return nil, err
		}
		return nil, fmt.Errorf("не дотянулся до списка сборок ядра: %w", err)
	}
	return relizy, nil
}

func skachat(ctx context.Context, klient *http.Client, ssylka, kuda string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ssylka, nil)
	if err != nil {
		return err
	}
	otvet, err := klient.Do(req)
	if err != nil {
		return fmt.Errorf("ядро не скачалось: %w", err)
	}
	defer otvet.Body.Close()
	if otvet.StatusCode != http.StatusOK {
		return fmt.Errorf("ядро не скачалось (ответ %d)", otvet.StatusCode)
	}
	f, err := os.Create(kuda)
	if err != nil {
		return err
	}
	defer f.Close()
	// Потолок в 200 МБ: ядро заведомо меньше, а подменённая ссылка не должна забить диск.
	_, err = io.Copy(f, io.LimitReader(otvet.Body, 200<<20))
	return err
}

// raspakovat достаёт из архива сам бинарь ядра и больше ничего:
// имена внутри архива приходят снаружи, класть их на диск как есть нельзя.
func raspakovat(arhiv, kuda string) error {
	z, err := zip.OpenReader(arhiv)
	if err != nil {
		return fmt.Errorf("архив ядра не читается: %w", err)
	}
	defer z.Close()
	iskomoe := "sing-box"
	if runtime.GOOS == "windows" {
		iskomoe = "sing-box.exe"
	}
	for _, f := range z.File {
		if path.Base(f.Name) != iskomoe || f.FileInfo().IsDir() {
			continue
		}
		vhod, err := f.Open()
		if err != nil {
			return err
		}
		defer vhod.Close()
		vremenny := kuda + ".tmp"
		vyhod, err := os.OpenFile(vremenny, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(vyhod, io.LimitReader(vhod, 200<<20))
		if cerr := vyhod.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(vremenny, kuda)
		}
		if err != nil {
			_ = os.Remove(vremenny) // недописанный файл не оставляем
		}
		return err
	}
	return fmt.Errorf("в архиве нет %s", iskomoe)
}
