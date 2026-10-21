package sluzhba

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/podpis"
	"github.com/HRYNdev/kelevra-desktop/internal/yadro"
)

// stendZamenyYadra — стенд лестницы, у которого на месте стоит ядро "staroe",
// а в подменном списке сборок лежит подписанная core-v2-1 с ядром "novoe".
// Сборка уже отложена фоновой сверкой.
func stendZamenyYadra(t *testing.T) *Sluzhba {
	t.Helper()
	s := stendSPravami(t)
	if err := os.WriteFile(s.Yadro.Bin, []byte("staroe"), 0o755); err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	staryKlyuch := podpis.KlyuchHex
	podpis.KlyuchHex = hex.EncodeToString(pub)
	t.Cleanup(func() { podpis.KlyuchHex = staryKlyuch })

	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create(filepath.Base(s.Yadro.Bin))
	w.Write([]byte("novoe"))
	z.Close()
	arhiv := b.Bytes()
	tmp := filepath.Join(t.TempDir(), "a.zip")
	os.WriteFile(tmp, arhiv, 0o644)
	sig, err := podpis.PodpisatSTegom(base64.StdEncoding.EncodeToString(priv.Seed()), yadro.ImyaArhiva(), "core-v2-1", tmp)
	if err != nil {
		t.Fatal(err)
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/spisok":
			fmt.Fprintf(w, `[{"tag_name":"core-v2-1","assets":[{"name":%q,"browser_download_url":"%s/a"},{"name":%q,"browser_download_url":"%s/s"}]}]`,
				yadro.ImyaArhiva(), srv.URL, yadro.ImyaArhiva()+".sig", srv.URL)
		case "/a":
			w.Write(arhiv)
		case "/s":
			fmt.Fprint(w, sig)
		}
	}))
	t.Cleanup(srv.Close)
	s.Yadro.Spisok = srv.URL + "/spisok"
	if err := s.prigotovitYadroRaz(context.Background()); err != nil {
		t.Fatalf("сверка ядра: %v", err)
	}
	return s
}

func chtoVYadre(t *testing.T, s *Sluzhba) string {
	t.Helper()
	b, err := os.ReadFile(s.Yadro.Bin)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Подключение ставит отложенную сборку до запуска ядра; поднялось — сборка
// закреплена.
func TestPodklyuchenieStavitNovoeYadro(t *testing.T) {
	s := stendZamenyYadra(t)
	if got := chtoVYadre(t, s); got != "staroe" {
		t.Fatalf("сверка тронула стоящее ядро: %q", got)
	}
	var zapuskalos []string
	s.zapustitYadro = func(ctx context.Context) error {
		zapuskalos = append(zapuskalos, chtoVYadre(t, s))
		return nil
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("связь не поднялась: %v", err)
	}
	if len(zapuskalos) != 1 || zapuskalos[0] != "novoe" {
		t.Fatalf("запускалось ядро %v, ждал одно новое", zapuskalos)
	}
	if v := s.Yadro.Versiya(); v != "core-v2-1" {
		t.Fatalf("версия ядра %q", v)
	}
	if _, err := os.Stat(s.Yadro.Bin + ".prezhnee"); err == nil {
		t.Fatal("после удачного подъёма осталось прежнее ядро")
	}
}

// Новое ядро упало на старте — служба возвращает прежнее, поднимает связь на
// нём и больше эту сборку не ставит.
func TestNovoeYadroNePodnyalosOtkat(t *testing.T) {
	s := stendZamenyYadra(t)
	var zapuskalos []string
	s.zapustitYadro = func(ctx context.Context) error {
		chto := chtoVYadre(t, s)
		zapuskalos = append(zapuskalos, chto)
		if chto == "novoe" {
			return errors.New("ядро упало при старте: FATAL[0000] decode config: unknown field")
		}
		return nil
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("на прежнем ядре связь не поднялась: %v", err)
	}
	if len(zapuskalos) != 2 || zapuskalos[0] != "novoe" || zapuskalos[1] != "staroe" {
		t.Fatalf("запускалось %v, ждал новое, затем прежнее", zapuskalos)
	}
	if got := chtoVYadre(t, s); got != "staroe" {
		t.Fatalf("после отката на месте %q", got)
	}
	// Следующее подключение и следующая сверка отвергнутую сборку не берут.
	zapuskalos = nil
	_ = s.Yadro.Ostanovit()
	if err := s.prigotovitYadroRaz(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(zapuskalos) != 1 || zapuskalos[0] != "staroe" {
		t.Fatalf("отвергнутая сборка снова в деле: %v", zapuskalos)
	}
}

// Без прав администратора ядро не меняется (и связь не поднимается).
func TestBezPravYadroNeMenyaetsya(t *testing.T) {
	s := stendZamenyYadra(t)
	s.pravaDlyaStenda = func() bool { return false }
	if err := s.PodnyatZashchitu(context.Background()); !errors.Is(err, OshibkaNetPrav) {
		t.Fatalf("ошибка %v, ждал OshibkaNetPrav", err)
	}
	if got := chtoVYadre(t, s); got != "staroe" {
		t.Fatalf("без прав ядро сменилось: %q", got)
	}
}

// Винится только отказ самого запуска ядра.
func TestNovoeYadroVinovatoTolkoZaZapusk(t *testing.T) {
	ctx := context.Background()
	zapusk := oshibkaZapuskaYadra{errors.New("ядро упало при старте: FATAL[0000] decode config: unknown field")}
	for _, sluchay := range []struct {
		imya  string
		err   error
		hochu bool
	}{
		{"упало на старте", zapusk, true},
		{"туннель не создан ядром", otkazPodyoma{OshibkaTunnelNePodnyalsya, zapusk}, true},
		{"нет конфига", errors.New("конфигурация не подготовлена: open profil.json: no such file"), false},
		{"ядро качается", errors.New("ядро уже качается"), false},
		{"нет прав", OshibkaNetPrav, false},
		{"адаптер занят до запуска", OshibkaAdapterZanyat, false},
		{"адаптер занят при запуске", otkazPodyoma{OshibkaAdapterZanyat, oshibkaZapuskaYadra{errors.New(oshibkaZanyatogoAdaptera)}}, false},
	} {
		if got := novoeYadroVinovato(ctx, sluchay.err); got != sluchay.hochu {
			t.Errorf("%s: винится %v, ждал %v", sluchay.imya, got, sluchay.hochu)
		}
	}
	otmenen, otmena := context.WithCancel(ctx)
	otmena()
	if novoeYadroVinovato(otmenen, zapusk) {
		t.Error("отменённый подъём винит новое ядро")
	}
}

// Подъём сорвался не на запуске ядра (нет конфига) — новое ядро не
// откатывается и не отвергается; следующий подъём продолжает пробу.
func TestOtkazNeNaZapuskeNeOtkatyvaet(t *testing.T) {
	s := stendZamenyYadra(t)
	zapuskov := 0
	s.zapustitYadro = func(ctx context.Context) error { zapuskov++; return nil }
	profil, err := os.ReadFile(hranenie.PutProfilya())
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(hranenie.PutProfilya())
	if err := s.PodnyatZashchitu(context.Background()); err == nil {
		t.Fatal("без профиля связь поднялась")
	}
	if zapuskov != 0 {
		t.Fatalf("ядро запускалось %d раз без конфига", zapuskov)
	}
	if got := chtoVYadre(t, s); got != "novoe" {
		t.Fatalf("новое ядро откачено за чужую ошибку: на месте %q", got)
	}
	if err := os.WriteFile(hranenie.PutProfilya(), profil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatalf("второй подъём: %v", err)
	}
	if got := chtoVYadre(t, s); got != "novoe" || s.Yadro.Versiya() != "core-v2-1" {
		t.Fatalf("проба не подтвердилась: %q, %q", got, s.Yadro.Versiya())
	}
	if _, err := os.Stat(s.Yadro.Bin + ".prezhnee"); err == nil {
		t.Fatal("после подтверждения осталось прежнее ядро")
	}
}

// От отката до конца повторного подъёма замок замены держится.
func TestZamokDerzhitsyaDoPovtornogoPodyoma(t *testing.T) {
	s := stendZamenyYadra(t)
	zamokSvoboden := true
	s.zapustitYadro = func(ctx context.Context) error {
		if chtoVYadre(t, s) == "novoe" {
			return errors.New("ядро упало при старте: FATAL[0000] decode config: unknown field")
		}
		if s.zamokSmenyYadra.TryLock() {
			s.zamokSmenyYadra.Unlock()
		} else {
			zamokSvoboden = false
		}
		return nil
	}
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatal(err)
	}
	if zamokSvoboden {
		t.Fatal("во время повторного подъёма на прежнем ядре замок замены был свободен")
	}
}

type kluchKto struct{}

// Ревью, круг 3 (и круг 2 в обратном порядке): подъём A держит пробу, подъём
// B приходит, пока исхода нет. B обязан ждать: новое ядро на пробе
// запускает только держатель, и подтвердить пробу чужим запуском нельзя.
// Новое ядро падает у A — откат на прежнее, B потом поднимается на нём.
func TestRev3ChuzhoyZapuskPodtverzhdaetProbu(t *testing.T) {
	s := stendZamenyYadra(t)
	var mu sync.Mutex
	var poryadok []string
	aVnutri := make(chan struct{})
	razreshitA := make(chan struct{})
	s.zapustitYadro = func(ctx context.Context) error {
		kto, _ := ctx.Value(kluchKto{}).(string)
		chto := chtoVYadre(t, s)
		mu.Lock()
		poryadok = append(poryadok, kto+":"+chto)
		mu.Unlock()
		if kto == "A" && chto == "novoe" {
			close(aVnutri)
			<-razreshitA
			return errors.New("ядро упало при старте: FATAL bad core")
		}
		if chto == "novoe" {
			return errors.New("ядро упало при старте: FATAL bad core")
		}
		return nil
	}
	ctxA := context.WithValue(context.Background(), kluchKto{}, "A")
	ctxB := context.WithValue(context.Background(), kluchKto{}, "B")
	doneA := make(chan error, 1)
	go func() { doneA <- s.PodnyatZashchitu(ctxA) }()
	<-aVnutri
	doneB := make(chan error, 1)
	go func() { doneB <- s.PodnyatZashchitu(ctxB) }()
	select {
	case err := <-doneB:
		t.Fatalf("B закончил подъём (%v), не дождавшись исхода пробы A", err)
	case <-time.After(300 * time.Millisecond):
	}
	mu.Lock()
	for _, z := range poryadok {
		if strings.HasPrefix(z, "B:") {
			mu.Unlock()
			t.Fatalf("B запускал ядро во время пробы A: %v", poryadok)
		}
	}
	mu.Unlock()
	close(razreshitA)
	errA, errB := <-doneA, <-doneB
	if errA != nil || errB != nil {
		t.Fatalf("A=%v B=%v (%v)", errA, errB, poryadok)
	}
	if got := chtoVYadre(t, s); got != "staroe" {
		t.Fatalf("на месте %q, ждал откат на прежнее (%v)", got, poryadok)
	}
	if s.Yadro.Versiya() == "core-v2-1" {
		t.Fatal("упавшая сборка подтверждена")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(poryadok) != 3 || poryadok[0] != "A:novoe" || poryadok[1] != "A:staroe" || poryadok[2] != "B:staroe" {
		t.Fatalf("порядок запусков %v, ждал A:novoe, A:staroe, B:staroe", poryadok)
	}
}

// Чужая ошибка на пробе (нет профиля) попыток не тратит (перенесено из ревью).
func TestChuzhayaOshibkaNeTratitPopytki(t *testing.T) {
	s := stendZamenyYadra(t)
	s.zapustitYadro = func(ctx context.Context) error { return nil }
	profil, _ := os.ReadFile(hranenie.PutProfilya())
	os.Remove(hranenie.PutProfilya())
	for i := 0; i < 5; i++ {
		if err := s.PodnyatZashchitu(context.Background()); err == nil {
			t.Fatal("без профиля связь поднялась")
		}
	}
	os.WriteFile(hranenie.PutProfilya(), profil, 0o600)
	if err := s.PodnyatZashchitu(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := chtoVYadre(t, s); got != "novoe" || s.Yadro.Versiya() != "core-v2-1" {
		t.Fatalf("сборка сгорела на чужих ошибках: в ядре %q, версия %q", got, s.Yadro.Versiya())
	}
}

// «Отключить» посреди старта нового ядра (перенесено из ревью): не отказ
// ядра — ни отката, ни повторного подъёма, сборка не отвергается.
func TestOtklyuchitVoVremyaProby(t *testing.T) {
	s := stendZamenyYadra(t)
	var zapuskalos []string
	s.zapustitYadro = func(ctx context.Context) error {
		zapuskalos = append(zapuskalos, chtoVYadre(t, s))
		return fmt.Errorf("ядро: %w", yadro.ErrOstanovleno)
	}
	_ = s.PodnyatZashchitu(context.Background())
	if len(zapuskalos) != 1 || zapuskalos[0] != "novoe" {
		t.Fatalf("запускалось %v, ждал одно новое без повтора", zapuskalos)
	}
	if got := chtoVYadre(t, s); got != "novoe" {
		t.Fatalf("откат за «Отключить»: на месте %q", got)
	}
}

// Новое ядро упало, а человек тем временем нажал «Отключить»: откат есть,
// повторного подъёма против его воли — нет, сборка не отвергается.
func TestOtkatBezPodyomaPosleOtklyuchit(t *testing.T) {
	s := stendZamenyYadra(t)
	var zapuskalos []string
	s.zapustitYadro = func(ctx context.Context) error {
		zapuskalos = append(zapuskalos, chtoVYadre(t, s))
		s.avtorezhimZamok.Lock()
		s.avtorezhimPokolenie++
		s.avtorezhimZamok.Unlock()
		return errors.New("ядро упало при старте: FATAL bad core")
	}
	_ = s.PodnyatZashchitu(context.Background())
	if len(zapuskalos) != 1 {
		t.Fatalf("запускалось %v, ждал только новое", zapuskalos)
	}
	if got := chtoVYadre(t, s); got != "staroe" {
		t.Fatalf("на месте %q, ждал откат", got)
	}
	if err := s.prigotovitYadroRaz(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Yadro.Papka, "novoe.zip")); err != nil {
		t.Fatal("сборка отвергнута, хотя на прежнем ядре связь не проверялась")
	}
}
