package podpiska

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Сервер своей сети в миниатюре: вход устройства по общему коду и выдача
// профиля по ключу. otvetVhoda — что дверь ответит на POST.
type dverPerehoda struct {
	srv        *httptest.Server
	mu         sync.Mutex
	zaprosy    []string
	xPult      []string
	statusVh   int
	teloVh     string
	konfigPoU  string
	svodkaEtag string
}

func novayaDver(t *testing.T) *dverPerehoda {
	d := &dverPerehoda{statusVh: http.StatusOK, konfigPoU: `{"outbounds":[{"type":"direct"}]}`}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		d.zaprosy = append(d.zaprosy, r.Method+" "+r.URL.Path)
		d.xPult = append(d.xPult, r.Header.Get("X-Pult")+"|"+r.Header.Get("X-Sostoyanie"))
		statusVh, teloVh, konfig, etag := d.statusVh, d.teloVh, d.konfigPoU, d.svodkaEtag
		d.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/k/OBSHCHIY_KOD/ustroystvo":
			w.WriteHeader(statusVh)
			if teloVh == "" {
				teloVh = `{"profil":"` + d.srv.URL + `/u/KLYUCH_USTROYSTVA","slot":"s1"}`
			}
			_, _ = w.Write([]byte(teloVh))
		case r.URL.Path == "/u/KLYUCH_USTROYSTVA":
			_, _ = w.Write([]byte(konfig))
		case strings.HasSuffix(r.URL.Path, "/info"):
			if etag != "" && r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			if etag != "" {
				w.Header().Set("ETag", etag)
			}
			_, _ = w.Write([]byte(`{"name":"x","active":true,"pult":{"dannye":"AA==","podpis":"BB=="}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *dverPerehoda) klient() *Klient {
	return &Klient{DeviceID: "dev-1", Shema: "http", Host: strings.TrimPrefix(d.srv.URL, "http://"),
		Pult: func() string { return "v=1;umeet=1;versiya=4" }, Sostoyanie: func() string { return "avto=vne;tunnel=1" }}
}

func TestPereytiNaKlyuchUdacha(t *testing.T) {
	d := novayaDver(t)
	k := d.klient()
	novyy, uzhe, err := k.PereytiNaKlyuch(context.Background(), d.srv.URL+"/k/OBSHCHIY_KOD", "")
	if err != nil || uzhe {
		t.Fatalf("переход: %v (uzhe=%v)", err, uzhe)
	}
	if novyy != d.srv.URL+"/u/KLYUCH_USTROYSTVA" {
		t.Fatalf("новый адрес %q", novyy)
	}
	if strings.Join(d.zaprosy, ",") != "POST /k/OBSHCHIY_KOD/ustroystvo,GET /u/KLYUCH_USTROYSTVA" {
		t.Fatalf("запросы: %v — ждали вход устройства и проверку конфига по новому адресу", d.zaprosy)
	}
	for _, h := range d.xPult {
		if h != "v=1;umeet=1;versiya=4|avto=vne;tunnel=1" {
			t.Fatalf("X-Pult/X-Sostoyanie не уехали с запросом: %q", h)
		}
	}
}

func TestPereytiNaKlyuchStarayaUstanovkaPoKodu(t *testing.T) {
	d := novayaDver(t)
	novyy, _, err := d.klient().PereytiNaKlyuch(context.Background(), "", "OBSHCHIY_KOD")
	if err != nil || !strings.HasSuffix(novyy, "/u/KLYUCH_USTROYSTVA") {
		t.Fatalf("старая установка (только код): %q %v", novyy, err)
	}
}

func TestPereytiNaKlyuchOtkazy(t *testing.T) {
	sluchai := []struct {
		imya   string
		nastr  func(d *dverPerehoda)
		kratko string
	}{
		{"дверь отказала словами", func(d *dverPerehoda) {
			d.statusVh, d.teloVh = http.StatusForbidden, `{"tekst":"Лимит устройств"}`
		}, "otkaz"},
		{"входа устройства нет", func(d *dverPerehoda) { d.statusVh, d.teloVh = http.StatusNotFound, `{}` }, "404"},
		{"новый адрес не даёт конфиг", func(d *dverPerehoda) { d.konfigPoU = `<html>заглушка</html>` }, "konfig"},
	}
	for _, sl := range sluchai {
		d := novayaDver(t)
		sl.nastr(d)
		novyy, _, err := d.klient().PereytiNaKlyuch(context.Background(), d.srv.URL+"/k/OBSHCHIY_KOD", "")
		var o *OshibkaPerehoda
		if !errors.As(err, &o) || o.Kratko != sl.kratko || novyy != "" {
			t.Errorf("%s: %q %v, ждали oshibka-%s", sl.imya, novyy, err, sl.kratko)
		}
		for _, z := range d.zaprosy {
			if strings.HasPrefix(z, "GET /k/") {
				t.Errorf("%s: откат на старый путь /k/ — команде он не нужен", sl.imya)
			}
		}
	}
}

// 200 без адреса /u/<ключ> — не переход (как в Voyti, adresKlyuchaGoden): старый
// адрес остаётся, конфиг по чужому адресу не запрашивается.
func TestPereytiNaKlyuchTolkoAdresKlyucha(t *testing.T) {
	sluchai := []struct {
		imya   string
		telo   func(d *dverPerehoda) string
		shema  string
		kratko string
		novyy  func(d *dverPerehoda) string
	}{
		{"общий код /k/ вместо ключа", func(d *dverPerehoda) string { return `{"profil":"` + d.srv.URL + `/k/OBSHCHIY_KOD"}` }, "http", "ne_klyuch", nil},
		{"пустой адрес", func(d *dverPerehoda) string { return `{"profil":""}` }, "http", "ne_klyuch", nil},
		{"не ссылка подписки", func(d *dverPerehoda) string { return `{"profil":"` + d.srv.URL + `/u/KLYUCH_USTROYSTVA/info"}` }, "http", "ne_klyuch", nil},
		{"http у клиента на https", func(d *dverPerehoda) string { return `{"profil":"` + d.srv.URL + `/u/KLYUCH_USTROYSTVA"}` }, "https", "ne_klyuch", nil},
		{"хвостовой слэш срезается", func(d *dverPerehoda) string { return `{"profil":"` + d.srv.URL + `/u/KLYUCH_USTROYSTVA/"}` }, "http", "",
			func(d *dverPerehoda) string { return d.srv.URL + "/u/KLYUCH_USTROYSTVA" }},
	}
	for _, sl := range sluchai {
		d := novayaDver(t)
		d.teloVh = sl.telo(d)
		k := d.klient()
		k.Shema = sl.shema
		novyy, uzhe, err := k.PereytiNaKlyuch(context.Background(), d.srv.URL+"/k/OBSHCHIY_KOD", "")
		if sl.kratko == "" {
			if err != nil || uzhe || novyy != sl.novyy(d) {
				t.Errorf("%s: %q %v %v", sl.imya, novyy, uzhe, err)
			}
			continue
		}
		var o *OshibkaPerehoda
		if !errors.As(err, &o) || o.Kratko != sl.kratko || novyy != "" {
			t.Errorf("%s: %q %v, ждали oshibka-%s", sl.imya, novyy, err, sl.kratko)
		}
		for _, z := range d.zaprosy {
			if strings.HasPrefix(z, "GET ") {
				t.Errorf("%s: после негодного адреса запрошен %s", sl.imya, z)
			}
		}
	}
}

func TestPereytiNaKlyuchUzheNaKlyuche(t *testing.T) {
	d := novayaDver(t)
	adres := d.srv.URL + "/u/KLYUCH_USTROYSTVA"
	novyy, uzhe, err := d.klient().PereytiNaKlyuch(context.Background(), adres, "")
	if err != nil || !uzhe || novyy != adres || len(d.zaprosy) != 0 {
		t.Fatalf("адрес уже на ключе: %q %v %v, запросов %d", novyy, uzhe, err, len(d.zaprosy))
	}
	if _, _, err := d.klient().PereytiNaKlyuch(context.Background(), "", ""); err == nil {
		t.Fatal("без кода и адреса переход «удался»")
	}
}

func TestVoytiPosleRazboraVhodaPrezhniy(t *testing.T) {
	// Voyti делит с командой вход устройства — поведение входа по коду прежнее.
	d := novayaDver(t)
	adres, err := d.klient().Voyti(context.Background(), "OBSHCHIY_KOD", []string{strings.TrimPrefix(d.srv.URL, "http://")})
	if err != nil || !strings.HasSuffix(adres, "/u/KLYUCH_USTROYSTVA") {
		t.Fatalf("Voyti: %q %v", adres, err)
	}
}

func TestSvodkaSPultomISyrym(t *testing.T) {
	d := novayaDver(t)
	sv, err := d.klient().SvedeniyaPoAdresu(context.Background(), d.srv.URL+"/k/OBSHCHIY_KOD")
	if err != nil {
		t.Fatal(err)
	}
	if string(sv.Pult) != `{"dannye":"AA==","podpis":"BB=="}` || !strings.Contains(string(sv.Syroe), `"name":"x"`) {
		t.Fatalf("pult=%s syroe=%s", sv.Pult, sv.Syroe)
	}
	s2, err := RazobratSvedeniya(sv.Syroe)
	if err != nil || s2.Imya != "x" || !s2.Aktivna {
		t.Fatal("сводка с диска не разобралась")
	}
}

func TestLegkiyOprosIfNoneMatch(t *testing.T) {
	d := novayaDver(t)
	d.svodkaEtag = `"p7-0123456789abcdef0123"`
	k := d.klient()
	adres := d.srv.URL + "/k/OBSHCHIY_KOD"
	sv, err := k.SvedeniyaEsliNovoe(context.Background(), adres, "")
	if err != nil || sv == nil || sv.ETag != d.svodkaEtag {
		t.Fatalf("без ETag — 200 и ETag сервера дословно, получили %+v %v", sv, err)
	}
	sv2, err := k.SvedeniyaEsliNovoe(context.Background(), adres, sv.ETag)
	if err != nil || sv2 != nil {
		t.Fatalf("повтор ETag сервера — 304 (nil, nil), получили %v %v", sv2, err)
	}
	sv3, err := k.SvedeniyaEsliNovoe(context.Background(), adres, `"pult-7"`)
	if err != nil || sv3 == nil || sv3.Imya != "x" {
		t.Fatalf("чужая метка — 200 со сводкой, получили %v %v", sv3, err)
	}
	sv4, err := k.SvedeniyaPoAdresu(context.Background(), adres)
	if err != nil || sv4.ETag != d.svodkaEtag {
		t.Fatalf("обычная сводка тоже несёт ETag: %+v %v", sv4, err)
	}
}

func TestDolgiyOpros(t *testing.T) {
	var status int
	var put string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		put = r.URL.String()
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "60")
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	k := &Klient{}
	for _, sl := range []struct {
		status int
		est    bool
		oshib  bool
	}{{200, true, false}, {204, false, false}, {304, false, false}, {403, false, true}, {404, false, true}, {429, false, true}} {
		status = sl.status
		est, err := k.ZhdatPult(context.Background(), srv.URL+"/u/KL", 12)
		if est != sl.est || (err != nil) != sl.oshib {
			t.Errorf("ответ %d: est=%v err=%v", sl.status, est, err)
		}
		var oz *OshibkaZhdat
		if sl.oshib && (!errors.As(err, &oz) || oz.Status != sl.status) {
			t.Errorf("ответ %d: ждали OshibkaZhdat с кодом, получили %v", sl.status, err)
		}
		if sl.status == 429 && (oz == nil || oz.RetryAfter != time.Minute) {
			t.Errorf("429: Retry-After не разобран: %+v", oz)
		}
	}
	if put != "/u/KL/pult/zhdat?versiya=12" {
		t.Fatalf("адрес долгого опроса %q", put)
	}
}
