package konfig

// Щуп между моим представлением о конфиге и настоящим ядром.
//
// Зачем он есть. Все остальные тесты здесь проверяют, что конфиг получился
// таким, каким я его задумал. Но ядро — отдельная программа со своим разбором
// полей, и оно уже дважды роняло старт на конфиге, который прошёл все тесты:
// сперва на `override_android_vpn` (поле только для Android), потом на
// `store_selected` (поле выкинуто в sing-box 1.14). Оба раза зелень тестов
// означала лишь «код согласен сам с собой».
//
// Тест зовёт `sing-box check` на настоящем бинаре. Бинаря нет — тест
// пропускается: у человека и в чужой сборке ядра может не быть, и падать
// из-за этого нечестно. Путь можно задать через KELEVRA_YADRO.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// kandidatyYadra — стенд разработки: ядро лежит в .stend/ в корне репозитория.
// Тест запускается с рабочей папкой internal/konfig — до корня ("kd") ровно
// два "../". Было три: путь тихо утекал ВЫШЕ репозитория, в общую папку
// .../repos/, и там, как назло, оказался чужой sing-box от другого стенда —
// щуп молча сверялся не с тем бинарём и всё равно зеленел (найдено 21.08 при
// проверке, что скрытие .stend/sing-box-linux даёт красный: не дало).
func kandidatyYadra() []string {
	return []string{
		"../../.stend/dom/yadro/sing-box",
		"../../.stend/sing-box-linux",
	}
}

func putYadra(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("KELEVRA_YADRO"); p != "" {
		return p
	}
	for _, p := range kandidatyYadra() {
		if a, err := filepath.Abs(p); err == nil {
			if _, err := os.Stat(a); err == nil {
				return a
			}
		}
	}
	return ""
}

// mestaPoiskaYadra — куда именно смотрел putYadra, когда ядра не нашлось.
// Тихий t.Skip неделю стоял поверх пустоты: строка «пропущен» не говорила,
// ГДЕ искали и что стоит положить, чтобы щуп ожил.
func mestaPoiskaYadra() string {
	var mesta []string
	for _, p := range kandidatyYadra() {
		a, err := filepath.Abs(p)
		if err != nil {
			a = p
		}
		mesta = append(mesta, a)
	}
	return "KELEVRA_YADRO (не задана), " + strings.Join(mesta, ", ")
}

func TestYadroPrinimaetGotovyyKonfig(t *testing.T) {
	bin := putYadra(t)
	if bin == "" {
		t.Skipf("настоящего ядра рядом нет — щуп пропущен. Искал: %s", mestaPoiskaYadra())
	}
	// Оба режима: с правами (туннель) и без них (прокси). Падало ядро в своё
	// время именно на одном из них, а не на обоих сразу.
	for imya, v := range map[string]Vybor{
		"с правами":             {Prava: true},
		"без прав":              {Prava: false},
		"без системного прокси": {Prava: false, BezSistemnogoProksi: true},
	} {
		t.Run(imya, func(t *testing.T) {
			gotovyy, _, err := Prigotovit(profil(t), v)
			if err != nil {
				t.Fatal(err)
			}
			put := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(put, gotovyy, 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(bin, "check", "-c", put).CombinedOutput()
			if err != nil {
				t.Fatalf("ядро не приняло конфиг: %v\n%s", err, out)
			}
		})
	}
}
