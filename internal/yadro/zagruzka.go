package yadro

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
	ssylka, err := y.ssylkaNaYadro(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(y.Papka, 0o755); err != nil {
		return err
	}
	arhiv := filepath.Join(y.Papka, "yadro.zip")
	if err := skachat(ctx, ssylka, arhiv); err != nil {
		return err
	}
	defer os.Remove(arhiv)
	return raspakovat(arhiv, y.Bin)
}

func (y *Yadro) ssylkaNaYadro(ctx context.Context) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, y.spisok(), nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	otvet, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("не дотянулся до списка сборок ядра: %w", err)
	}
	defer otvet.Body.Close()
	if otvet.StatusCode != http.StatusOK {
		return "", fmt.Errorf("список сборок ядра недоступен (ответ %d)", otvet.StatusCode)
	}
	var relizy []struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(otvet.Body).Decode(&relizy); err != nil {
		return "", err
	}
	nuzhen := ImyaArhiva()
	for _, r := range relizy { // релизы приходят от свежих к старым
		if !strings.HasPrefix(r.Tag, "core-") {
			continue
		}
		for _, a := range r.Assets {
			if a.Name == nuzhen {
				return a.URL, nil
			}
		}
	}
	return "", fmt.Errorf("в сборках ядра нет файла %s", nuzhen)
}

func skachat(ctx context.Context, ssylka, kuda string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ssylka, nil)
	otvet, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
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
		if _, err := io.Copy(vyhod, io.LimitReader(vhod, 200<<20)); err != nil {
			vyhod.Close()
			return err
		}
		vyhod.Close()
		return os.Rename(vremenny, kuda)
	}
	return fmt.Errorf("в архиве нет %s", iskomoe)
}
