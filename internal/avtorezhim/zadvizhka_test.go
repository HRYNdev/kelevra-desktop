package avtorezhim

import "testing"

// TestZadvizhkaOdinochnoyeNablyudeniyeNeMenyaet: одно наблюдение не меняет
// обстановку — это и есть весь смысл гистерезиса.
func TestZadvizhkaOdinochnoyeNablyudeniyeNeMenyaet(t *testing.T) {
	z := NovayaZadvizhka(VneDoma)
	if izm := z.Predlozhit(Doma, false); izm {
		t.Fatal("одно наблюдение сменило обстановку раньше срока")
	}
	if z.Tekushcheye() != VneDoma {
		t.Fatalf("текущая = %v, ждал VneDoma", z.Tekushcheye())
	}
}

// TestZadvizhkaTretyePodtverzhdeniyeMenyaet: ровно на Podtverzhdeniy-м
// одинаковом наблюдении подряд обстановка меняется.
func TestZadvizhkaTretyePodtverzhdeniyeMenyaet(t *testing.T) {
	z := NovayaZadvizhka(VneDoma)
	for i := 1; i < Podtverzhdeniy; i++ {
		if izm := z.Predlozhit(Doma, false); izm {
			t.Fatalf("заход %d сменил обстановку раньше %d-го", i, Podtverzhdeniy)
		}
	}
	if izm := z.Predlozhit(Doma, false); !izm {
		t.Fatalf("на %d-м подряд наблюдении обстановка обязана смениться", Podtverzhdeniy)
	}
	if z.Tekushcheye() != Doma {
		t.Fatalf("текущая = %v, ждал Doma", z.Tekushcheye())
	}
}

// TestZadvizhkaShumOdinNablyudeniyeSbivaetSchyot: одно шумное наблюдение
// посреди набора подтверждений сбрасывает счётчик кандидата — иначе
// гистерезис ловил бы смену обстановки по "2 из 3 любых", а не по трём
// подряд одинаковым.
func TestZadvizhkaShumOdinNablyudeniyeSbivaetSchyot(t *testing.T) {
	z := NovayaZadvizhka(VneDoma)
	z.Predlozhit(Doma, false)       // 1-е подтверждение кандидата Doma
	z.Predlozhit(Neizvestno, false) // шум — кандидат сбрасывается на Neizvestno
	if izm := z.Predlozhit(Doma, false); izm {
		t.Fatal("после шума одно наблюдение Doma не должно сразу переключать (это только 1-е новое подтверждение)")
	}
	if z.Tekushcheye() != VneDoma {
		t.Fatalf("текущая = %v, ждал VneDoma (шум не дал набрать 3 подряд Doma)", z.Tekushcheye())
	}
	if izm := z.Predlozhit(Doma, false); izm {
		t.Fatal("это только 2-е подряд наблюдение Doma после сброса шумом — рано")
	}
	// Теперь три новых подряд Doma с момента шума набраны — задвижка обязана переключиться.
	if izm := z.Predlozhit(Doma, false); !izm {
		t.Fatal("после шума и трёх новых Doma подряд обстановка обязана смениться")
	}
}

// TestZadvizhkaPodtverzhdeniyeTekushchegoNeTratitSchyotchik: наблюдение,
// совпадающее с уже текущей обстановкой, не должно "накапливаться" против
// другого кандидата — счётчик кандидата держится независимо.
func TestZadvizhkaPodtverzhdeniyeTekushchegoNeTratitSchyotchik(t *testing.T) {
	z := NovayaZadvizhka(VneDoma)
	z.Predlozhit(Doma, false)    // 1-е подтверждение кандидата Doma
	z.Predlozhit(VneDoma, false) // подтвердили текущее — не должно засчитаться в пользу Doma
	if izm := z.Predlozhit(Doma, false); izm {
		t.Fatal("после подтверждения текущего обстановка не могла смениться одним наблюдением")
	}
	if z.Tekushcheye() != VneDoma {
		t.Fatalf("текущая = %v, ждал VneDoma", z.Tekushcheye())
	}
}

// TestZadvizhkaDoverennoyeNablyudeniyeMenyaetSrazu: наблюдение, помеченное
// dovereno=true (пришло по доказанному сигналу смены сети), обязано менять
// обстановку с первого раза, минуя Podtverzhdeniy — перенос
// AutoModeGate.offer(trust=true) с телефона (AutoModeGate.kt:53-72: trust
// даёт needed=1).
func TestZadvizhkaDoverennoyeNablyudeniyeMenyaetSrazu(t *testing.T) {
	z := NovayaZadvizhka(VneDoma)
	if izm := z.Predlozhit(Doma, true); !izm {
		t.Fatal("доверенное наблюдение обязано менять обстановку с первого раза")
	}
	if z.Tekushcheye() != Doma {
		t.Fatalf("текущая = %v, ждал Doma", z.Tekushcheye())
	}
}

// TestZadvizhkaNedoverennoyeNablyudeniyeZhdyotPodtverzhdeniy: без dovereno
// (заход страховочного тикера) поведение прежнее — нужны Podtverzhdeniy
// одинаковых наблюдений подряд.
func TestZadvizhkaNedoverennoyeNablyudeniyeZhdyotPodtverzhdeniy(t *testing.T) {
	z := NovayaZadvizhka(VneDoma)
	for i := 1; i < Podtverzhdeniy; i++ {
		if izm := z.Predlozhit(Doma, false); izm {
			t.Fatalf("заход %d сменил обстановку раньше %d-го без доверия", i, Podtverzhdeniy)
		}
	}
	if izm := z.Predlozhit(Doma, false); !izm {
		t.Fatalf("на %d-м подряд наблюдении обстановка обязана смениться", Podtverzhdeniy)
	}
}

func TestSostoyanieString(t *testing.T) {
	cases := map[Sostoyanie]string{
		Doma:       "дома",
		VneDoma:    "вне дома",
		Neizvestno: "неизвестно",
	}
	for s, hochu := range cases {
		if got := s.String(); got != hochu {
			t.Fatalf("%d.String() = %q, хочу %q", s, got, hochu)
		}
	}
}
