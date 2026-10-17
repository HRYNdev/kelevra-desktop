package hranenie

import "testing"

// Стенд и проверки (KELEVRA_DIR) не должны видеть НАСТОЯЩИЕ папки машины:
// ни метку живой службы Windows, ни папку человека. 17.10 проверка сторожа
// окна на ПК нашла живую службу в общей папке и «перевела окно» на неё.
func TestPriKelevraDirNastoyashchiePapkiNeVidny(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())
	if p := PapkaSluzhbyWindows(); p != "" {
		t.Fatalf("при KELEVRA_DIR папка службы Windows = %q — проверка найдёт настоящую службу", p)
	}
	if p := PapkaLichnaya(); p != "" {
		t.Fatalf("при KELEVRA_DIR папка человека = %q — уборка тронет настоящие файлы", p)
	}
}

// Флаг «подключиться после установки» снимается при первом же чтении.
func TestFlagPodklyuchitOdnorazovyy(t *testing.T) {
	t.Setenv("KELEVRA_DIR", t.TempDir())
	if ZabratFlagPodklyuchit() {
		t.Fatal("флага не ставили, а он есть")
	}
	if err := OtmetitPodklyuchitPosleUstanovki(); err != nil {
		t.Fatal(err)
	}
	if !ZabratFlagPodklyuchit() {
		t.Fatal("флаг поставлен, а не прочитан")
	}
	if ZabratFlagPodklyuchit() {
		t.Fatal("флаг прочитан дважды — служба подключалась бы на каждом старте")
	}
}
