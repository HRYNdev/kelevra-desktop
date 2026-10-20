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

// obshchayaPapkaStenda — общая папка (%PROGRAMDATA%\Kelevra) во временном
// каталоге: Papka() смотрит в неё, как у службы Windows. Права закрытых
// файлов в конце возвращаются к наследованию, иначе TempDir не убрать без
// прав администратора (владелец файла права менять может всегда).
func obshchayaPapkaStenda(t *testing.T) string {
	t.Helper()
	koren := t.TempDir()
	t.Setenv("KELEVRA_DIR", "")
	t.Setenv("PROGRAMDATA", koren)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	papka := filepath.Join(koren, "Kelevra")
	if err := os.MkdirAll(papka, 0o755); err != nil {
		t.Fatal(err)
	}
	if p := Papka(); p != papka {
		t.Fatalf("Papka() = %q, ждал общую %q", p, papka)
	}
	kakSluzhba(t)
	t.Cleanup(func() {
		_ = filepath.Walk(papka, func(put string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				_ = windows.SetNamedSecurityInfo(put, windows.SE_FILE_OBJECT,
					windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, nil, nil)
			}
			return nil
		})
	})
	return papka
}

// sidProverki — учётная запись, от которой идёт проверка.
var sidProverki string

// kakSluzhba подставляет в закрытые права, кроме системы и администраторов,
// учётную запись проверки. В бою файлы пишет служба (система) или копия с
// правами администратора, у обеих полный доступ по своим строкам; проверка же
// идёт без повышения, и по-боевому закрытый файл не смогла бы ни
// перезаписать, ни переименовать. Пользователи ПК (BU, AU, WD) по-прежнему
// не получают ничего — это proveritZakryt и сторожит.
func kakSluzhba(t *testing.T) {
	t.Helper()
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("не узнать учётную запись проверки: %v", err)
	}
	sidProverki = tu.User.Sid.String()
	bylo := sddlTolkoSistema
	sddlTolkoSistema = bylo + "(A;;FA;;;" + sidProverki + ")"
	t.Cleanup(func() { sddlTolkoSistema = bylo })
}

func proveritZakryt(t *testing.T, put string) {
	t.Helper()
	s := sddlFayla(t, put)
	if !strings.HasPrefix(s, "D:P") {
		t.Fatalf("%s: права наследуются от папки: %s", filepath.Base(put), s)
	}
	for _, chuzhoy := range []string{";BU)", ";WD)", ";AU)", ";IU)"} {
		if strings.Contains(s, chuzhoy) {
			t.Fatalf("%s: в правах остался %s: %s", filepath.Base(put), chuzhoy, s)
		}
	}
	if !strings.Contains(s, ";SY)") || !strings.Contains(s, ";BA)") {
		t.Fatalf("%s: нет системы или администраторов: %s", filepath.Base(put), s)
	}
	// Ни одного разрешения сверх боевых двух и подставленной учётной записи
	// проверки (kakSluzhba): ни унаследованных от папки, ни владельцу.
	if n := strings.Count(s, "(A;"); n != 3 || !strings.Contains(s, ";"+sidProverki+")") {
		t.Fatalf("%s: разрешений %d, ждал система, администраторы и учётная запись проверки: %s", filepath.Base(put), n, s)
	}
}

// Настройки в общей папке пишутся сразу закрытыми — и при первой записи, и при
// перезаписи (переименование сохраняет права временного файла, а он
// создаётся с правами папки).
func TestSohranitVObshcheyPapkeZakryvaet(t *testing.T) {
	papka := obshchayaPapkaStenda(t)
	n := &Nastroyki{Kod: "KOD", DeviceID: "x", ObnovlyatMin: 60}
	if err := Sohranit(n); err != nil {
		t.Fatalf("Sohranit: %v", err)
	}
	proveritZakryt(t, filepath.Join(papka, "nastroyki.json"))
	n.Kod = "KOD2"
	if err := Sohranit(n); err != nil {
		t.Fatalf("повторный Sohranit: %v", err)
	}
	proveritZakryt(t, filepath.Join(papka, "nastroyki.json"))
	if err := ZapisatZakryto(PutProfilya(), []byte("{}")); err != nil {
		t.Fatalf("профиль: %v", err)
	}
	proveritZakryt(t, PutProfilya())
}

// Файлы, оставленные прежними версиями с правами папки, закрываются при
// старте службы (Zagruzit), остальные файлы общей папки не трогаются: журнал
// и метку адреса службы читает окно без прав администратора.
func TestZagruzitZakryvaetStaroeLichnoe(t *testing.T) {
	papka := obshchayaPapkaStenda(t)
	for _, imya := range []string{"nastroyki.json", "profil.json", "clash_sekret", "kelevra.log"} {
		telo := "{}"
		if imya == "clash_sekret" {
			telo = strings.Repeat("ab", 32)
		}
		if err := os.WriteFile(filepath.Join(papka, imya), []byte(telo), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if s := sddlFayla(t, filepath.Join(papka, "nastroyki.json")); strings.HasPrefix(s, "D:P") {
		t.Fatalf("исходный файл уже закрыт — проверять нечего: %s", s)
	}
	if _, err := Zagruzit(); err != nil {
		t.Fatalf("Zagruzit: %v", err)
	}
	for _, imya := range []string{"nastroyki.json", "profil.json", "clash_sekret"} {
		proveritZakryt(t, filepath.Join(papka, imya))
	}
	if s := sddlFayla(t, filepath.Join(papka, "kelevra.log")); strings.HasPrefix(s, "D:P") {
		t.Fatalf("журнал закрыт, а его читает окно: %s", s)
	}
}

// Перенос данных из профиля человека в общую папку кладёт код доступа и
// профиль сразу закрытыми, прочее — с правами папки.
func TestPerenestiZakryvaetLichnoe(t *testing.T) {
	papka := obshchayaPapkaStenda(t)
	staraya := t.TempDir()
	for _, imya := range []string{"nastroyki.json", "profil.json", "otpravka_zhurnalov.json"} {
		if err := os.WriteFile(filepath.Join(staraya, imya), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Perenesti(staraya, papka); err != nil {
		t.Fatalf("Perenesti: %v", err)
	}
	proveritZakryt(t, filepath.Join(papka, "nastroyki.json"))
	proveritZakryt(t, filepath.Join(papka, "profil.json"))
	if s := sddlFayla(t, filepath.Join(papka, "otpravka_zhurnalov.json")); strings.HasPrefix(s, "D:P") {
		t.Fatalf("служебный файл закрыт без нужды: %s", s)
	}
}

// Смена старого пароля в общей папке пишет новый тоже закрытым.
func TestSmenitStaryySekretVObshcheyPapkeZakryt(t *testing.T) {
	papka := obshchayaPapkaStenda(t)
	put := filepath.Join(papka, "clash_sekret")
	if err := os.WriteFile(put, []byte(strings.Repeat("ab", 32)), 0o644); err != nil {
		t.Fatal(err)
	}
	if smenen, err := SmenitStaryySekret(); err != nil || !smenen {
		t.Fatalf("не сменён: smenen=%v err=%v", smenen, err)
	}
	proveritZakryt(t, put)
}
