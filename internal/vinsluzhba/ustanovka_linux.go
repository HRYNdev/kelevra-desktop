//go:build linux

package vinsluzhba

import (
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// PutYarlyka — ярлык в меню приложений (окно Kelevra).
const PutYarlyka = "/usr/share/applications/kelevra.desktop"

// PutZnachka — значок для меню, автозапуска и уведомлений (Icon=kelevra).
const PutZnachka = "/usr/share/icons/hicolor/256x256/apps/kelevra.png"

//go:embed znachok-256.png
var znachokPNG []byte

const tekstYarlyka = `[Desktop Entry]
Type=Application
Name=Kelevra
Comment=Kelevra
Exec=/opt/kelevra/kelevra
Icon=kelevra
Terminal=false
Categories=Network;
`

// UstanovitPolnostyu — всё, ради чего человека спрашивают пароль администратора
// (pkexec): общая папка данных, перенос данных из его профиля, копия программы
// в /opt/kelevra, ярлык в меню и служба systemd. Порядок тот же, что у Windows
// (ustanovka_windows.go): папка и данные — до старта службы.
func UstanovitPolnostyu(putExe string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("установке службы нужны права администратора")
	}
	obshchaya := hranenie.PapkaObshchaya()
	if err := os.MkdirAll(obshchaya, 0o755); err != nil {
		return fmt.Errorf("не создать общую папку %s: %w", obshchaya, err)
	}
	// Запись — только root: конфиг ядра из этой папки читает служба от root,
	// открытая запись дала бы любому на машине увести весь её трафик.
	if err := os.Chmod(obshchaya, 0o755); err != nil {
		log.Printf("ВНИМАНИЕ: права на общую папку не выставлены: %v", err)
	}
	if staraya := staraya(); staraya != "" {
		if err := hranenie.Perenesti(staraya, obshchaya); err != nil {
			log.Printf("ВНИМАНИЕ: данные не перенеслись из %s: %v", staraya, err)
		}
	}
	if err := hranenie.OtmetitPodklyuchitPosleUstanovki(); err != nil {
		log.Printf("флаг подключения после установки не записан: %v", err)
	}
	est, _ := Ustanovlena()
	if est {
		_ = Ostanovit()
	}
	if err := skopirovat(putExe, PutUstanovki); err != nil {
		return fmt.Errorf("не положить программу в %s: %w", KatalogUstanovki, err)
	}
	if err := os.MkdirAll(filepath.Dir(PutZnachka), 0o755); err == nil {
		if err := os.WriteFile(PutZnachka, znachokPNG, 0o644); err != nil {
			log.Printf("значок не записан: %v", err)
		}
	}
	if err := os.WriteFile(PutYarlyka, []byte(tekstYarlyka), 0o644); err != nil {
		log.Printf("ярлык в меню не записан: %v", err)
	}
	return Ustanovit(PutUstanovki)
}

// UdalitPolnostyu снимает службу и ярлык. Данные и программу не трогает.
func UdalitPolnostyu() error {
	_ = os.Remove(PutYarlyka)
	return Udalit()
}

// staraya — папка данных человека, запустившего установку (до службы данные
// жили в его профиле). pkexec и sudo оставляют его uid в окружении.
func staraya() string {
	for _, k := range []string{"PKEXEC_UID", "SUDO_UID"} {
		uid := os.Getenv(k)
		if uid == "" {
			continue
		}
		u, err := user.LookupId(uid)
		if err != nil || u.HomeDir == "" {
			continue
		}
		put := filepath.Join(u.HomeDir, ".local", "share", "kelevra")
		if st, err := os.Stat(put); err == nil && st.IsDir() {
			return put
		}
	}
	return ""
}

// skopirovat кладёт программу на место через временный файл и переименование:
// запущенный файл службы (если она была) заменяется целиком, а не дописывается.
func skopirovat(otkuda, kuda string) error {
	if same, err := odinFayl(otkuda, kuda); err == nil && same {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(kuda), 0o755); err != nil {
		return err
	}
	src, err := os.Open(otkuda)
	if err != nil {
		return err
	}
	defer src.Close()
	vrem := kuda + ".novyy"
	dst, err := os.OpenFile(vrem, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(vrem)
		return err
	}
	if err := dst.Close(); err != nil {
		os.Remove(vrem)
		return err
	}
	return os.Rename(vrem, kuda)
}

func odinFayl(a, b string) (bool, error) {
	sa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(sa, sb), nil
}
