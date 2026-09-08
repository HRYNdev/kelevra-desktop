package kopiya

import (
	"os"
	"testing"
	"time"
)

// Метка называет своего хозяина, и по нему её отличают от чужой.
//
// Беда с живой машины 09.09: копия, поднятая после обновления, стирала метку
// как «оставшуюся от умирающей старой» — а её секундой раньше поставила новая
// служба. Без имени хозяина эти два случая неразличимы, и на машине заводился
// второй хозяин туннеля.
func TestKtoZanyalNazyvaetHozyaina(t *testing.T) {
	papka := t.TempDir()
	if _, _, est := KtoZanyal(papka); est {
		t.Fatal("метки нет, а хозяин нашёлся")
	}
	if err := Zanyat(papka, "http://127.0.0.1:5000/proba/", time.Now()); err != nil {
		t.Fatalf("не записал метку: %v", err)
	}
	url, pid, est := KtoZanyal(papka)
	if !est {
		t.Fatal("метка записана, а хозяина не видно")
	}
	if url != "http://127.0.0.1:5000/proba/" {
		t.Fatalf("адрес %q", url)
	}
	if pid != os.Getpid() {
		t.Fatalf("хозяин %d, а метку писал %d", pid, os.Getpid())
	}
}

// Мёртвый адрес не мешает узнать хозяина: KtoZanyal отвечает на вопрос «чья
// метка», а не «жива ли копия» — иначе на ней нельзя строить решение о том,
// стирать её или нет.
func TestKtoZanyalNeProveryaetZhivost(t *testing.T) {
	papka := t.TempDir()
	if err := Zanyat(papka, "http://127.0.0.1:1/", time.Now()); err != nil {
		t.Fatalf("не записал метку: %v", err)
	}
	if _, _, est := KtoZanyal(papka); !est {
		t.Fatal("мёртвый адрес скрыл хозяина метки")
	}
	if _, est := Nayti(papka); est {
		t.Fatal("Nayti обязана считать мёртвый адрес отсутствием копии")
	}
}
