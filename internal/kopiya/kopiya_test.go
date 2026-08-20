package kopiya

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestZhivayaKopiyaNahoditsya(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	papka := t.TempDir()
	if err := Zanyat(papka, server.URL+"/klyuch/", time.Unix(0, 0)); err != nil {
		t.Fatalf("не смог занять: %v", err)
	}
	url, est := Nayti(papka)
	if !est {
		t.Fatal("живая копия должна находиться")
	}
	if url != server.URL+"/klyuch/" {
		t.Fatalf("вернулся чужой адрес: %s", url)
	}
}

func TestMertvayaKopiyaNeNahoditsya(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	adres := server.URL + "/klyuch/"
	server.Close() // копия умерла, метка осталась

	papka := t.TempDir()
	if err := Zanyat(papka, adres, time.Unix(0, 0)); err != nil {
		t.Fatalf("не смог занять: %v", err)
	}
	if _, est := Nayti(papka); est {
		t.Fatal("метка от умершей копии не должна считаться живой")
	}
}

func TestNetMetkiIBityeDannye(t *testing.T) {
	papka := t.TempDir()
	if _, est := Nayti(papka); est {
		t.Fatal("без метки копии быть не должно")
	}
	if err := os.WriteFile(Metka(papka), []byte("{это не json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, est := Nayti(papka); est {
		t.Fatal("битая метка не должна считаться живой копией")
	}
	// Битая метка не должна и запирать приложение: занять её можно поверх.
	if err := Zanyat(papka, "http://127.0.0.1:1/", time.Unix(0, 0)); err != nil {
		t.Fatalf("поверх битой метки записать не смог: %v", err)
	}
}

func TestOsvobodit(t *testing.T) {
	papka := t.TempDir()
	if err := Zanyat(papka, "http://127.0.0.1:1/", time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	Osvobodit(papka)
	if _, err := os.Stat(Metka(papka)); !os.IsNotExist(err) {
		t.Fatal("метка должна пропасть после выхода")
	}
	Osvobodit(papka) // повторное освобождение не должно падать
}

func TestOtvetNeUspehomNeSchitaetsya(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "чужой сервер на этом порту", http.StatusNotFound)
	}))
	defer server.Close()

	papka := t.TempDir()
	if err := Zanyat(papka, server.URL+"/klyuch/", time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if _, est := Nayti(papka); est {
		t.Fatal("404 с чужого сервера не должен считаться нашей копией")
	}
}

func TestMetkaLezhitVPapkeDannyh(t *testing.T) {
	papka := t.TempDir()
	if Metka(papka) != filepath.Join(papka, "zapushcheno.json") {
		t.Fatalf("метка не там, где ожидается: %s", Metka(papka))
	}
}
