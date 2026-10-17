package sluzhba

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// Честный статус: окно говорит, что идёт через Kelevra, только пока связь
// реально поднята. Неполного режима (прокси, «Частично») с 0.6.77 нет.

func TestObyomZashchityTablitsa(t *testing.T) {
	k := konfig.Kartina{Rezhim: konfig.Tunnel, Zametka: konfig.ZametkaVes}
	sluchai := []struct {
		imya    string
		sost    string
		zametka string
		zachem  string
	}{
		{"туннель поднят", string(yadro.Rabotaet), konfig.ZametkaVes, "заметка обязана дойти до окна"},
		{"защита опущена", string(yadro.Stoit), "", "у опущенной связи нечего описывать"},
		{"ядро сломалось", string(yadro.Slomalos), "", "заметка поверх беды сказала бы, что что-то работает"},
		{"ядро поднимается", string(yadro.Podnimaem), "", "пока ядро не ответило, говорить рано"},
	}
	for _, s := range sluchai {
		t.Run(s.imya, func(t *testing.T) {
			if got := obyomZashchity(s.sost, k); got != s.zametka {
				t.Fatalf("заметка = %q, ждали %q — %s", got, s.zametka, s.zachem)
			}
		})
	}
}

// Имена полей в JSON — договор с окном, и ломается он молча: окно прочитает
// undefined и нарисует не то состояние. Бьёмся о сырой JSON, а не о
// структуру Go. Полей неполного режима в договоре больше нет.
func TestImenaPoleyNuzhnyhPravIOtkazaNeMenyayutsya(t *testing.T) {
	b, err := json.Marshal(otvetSostoyaniya{NuzhnyPrava: true, Otkaz: OshibkaAdapterZanyat.Error()})
	if err != nil {
		t.Fatal(err)
	}
	var syroy map[string]any
	if err := json.Unmarshal(b, &syroy); err != nil {
		t.Fatal(err)
	}
	if v, _ := syroy["nuzhny_prava"].(bool); !v {
		t.Fatalf("в ответе нет поля nuzhny_prava=true (index.html читает именно его): %s", b)
	}
	if v, _ := syroy["otkaz"].(string); v != OshibkaAdapterZanyat.Error() {
		t.Fatalf("в ответе нет поля otkaz: %s", b)
	}
	for _, pole := range []string{"chastichnaya", "pochemu_chastichnaya", "mozhno_tun", "ruchnoy_proksi"} {
		if _, est := syroy[pole]; est {
			t.Fatalf("в договоре с окном осталось поле неполного режима %q: %s", pole, b)
		}
	}
}

// Отказ подъёма не висит над поднимающимся или работающим ядром: окно
// показывает его только в состоянии «нет подключения».
func TestOtkazNeVidenPokaYadroRabotaet(t *testing.T) {
	s := stend(t)
	s.zamok.Lock()
	s.otkaz = OshibkaTunnelNePodnyalsya.Error()
	s.zamok.Unlock()
	w := httptest.NewRecorder()
	s.Obsluzhit().ServeHTTP(w, httptest.NewRequest("GET", "/"+s.klyuch+"/api/sostoyanie", nil))
	var o otvetSostoyaniya
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatal(err)
	}
	if o.Sost != "stoit" || o.Otkaz != OshibkaTunnelNePodnyalsya.Error() {
		t.Fatalf("ядро стоит: sost=%q otkaz=%q — причина обязана дойти до окна", o.Sost, o.Otkaz)
	}
}
