//go:build windows

package hranenie

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func sddlFayla(t *testing.T, put string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(put, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("не прочитать права %s: %v", put, err)
	}
	return sd.String()
}

// Пароль в общей папке после закрытия читают только система и администраторы:
// ни группы «Пользователи» (BU), ни «Все» (WD), ни прав, унаследованных от папки.
func TestZakrytFaylTolkoSistemaIAdminy(t *testing.T) {
	put := filepath.Join(t.TempDir(), "clash_sekret")
	if err := os.WriteFile(put, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Вернуть наследование, чтобы TempDir убрался без прав администратора.
		_ = windows.SetNamedSecurityInfo(put, windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, nil, nil)
	})
	if err := zakrytFayl(put); err != nil {
		t.Fatal(err)
	}
	s := sddlFayla(t, put)
	if !strings.HasPrefix(s, "D:P") {
		t.Fatalf("права наследуются от папки: %s", s)
	}
	for _, chuzhoy := range []string{";BU)", ";WD)", ";AU)", ";IU)"} {
		if strings.Contains(s, chuzhoy) {
			t.Fatalf("в правах остался %s: %s", chuzhoy, s)
		}
	}
	if !strings.Contains(s, ";SY)") || !strings.Contains(s, ";BA)") {
		t.Fatalf("нет системы или администраторов: %s", s)
	}
}

func TestVObshcheyPapke(t *testing.T) {
	t.Setenv("PROGRAMDATA", `C:\ProgramData`)
	sluchai := map[string]bool{
		`C:\ProgramData\Kelevra\clash_sekret`:           true,
		`c:\programdata\kelevra\yadro\config.json`:      true,
		`C:\ProgramData\Kelevra`:                        false,
		`C:\ProgramData\KelevraDrugaya\clash_sekret`:    false,
		`C:\Users\x\AppData\Local\Kelevra\clash_sekret`: false,
	}
	for put, zhdu := range sluchai {
		if got := vObshcheyPapke(put); got != zhdu {
			t.Errorf("vObshcheyPapke(%q) = %v, ждал %v", put, got, zhdu)
		}
	}
}

// Вне общей папки (стенд с KELEVRA_DIR, профиль человека) права не трогаются:
// процесс без администратора иначе сам потерял бы доступ к своему паролю.
func TestSekretVneObshcheyPapkiChitaetsya(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())
	a := SekretClash()
	if a == "" {
		t.Fatal("пароль не создан")
	}
	if b := SekretClash(); b != a {
		t.Fatalf("второй вызов не прочитал свой же пароль: %q и %q", a, b)
	}
}
