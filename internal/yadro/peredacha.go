package yadro

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Передача живого ядра от одной копии приложения к другой.
//
// Зачем. Туннель держит ЯДРО, отдельный процесс: сетевой адаптер принадлежит
// ему и уничтожается драйвером вместе с ним, маршруты прописаны на этом
// адаптере. Оболочка ядру не родитель в том смысле, который важен системе —
// ядро запускается без привязки и переживает смену оболочки само.
//
// А обновляется как раз оболочка. До 11.09.2026 она перед уходом гасила ядро
// (иначе на машине оказывались два хозяина одного адаптера — авария 25.08), и
// замер на стенде 11.09 показал цену: между смертью старой копии в 15:56:49 и
// подъёмом новой в 15:56:57 прошло восемь секунд, всё это время адаптера не
// было, а трафик шёл напрямую мимо обхода. Связь при этом не пропадала, и
// потому дыра была незаметной — худший вид дыры. Хуже того, туннель после
// обновления не поднимался обратно вовсе: автоподключение у людей выключено, а
// авторежим дома считает туннель ненужным.
//
// Правильное лечение — не восстанавливать связь после разрыва, а не рвать её.
// Ядро остаётся жить, новая копия принимает его под управление. Два хозяина не
// появляются, потому что второе ядро просто не запускается.
//
// Эта записка и есть то, по чему преемница узнаёт ядро. Пишется при запуске
// ядра, снимается при штатной остановке. Её наличие при мёртвом ядре не беда:
// опознание (Yadro.Prinyat) проверяет живость отдельно.
type Peredacha struct {
	// PID — номер процесса ядра. Главное, ради чего записка существует:
	// принять можно только процесс, который знаешь по номеру.
	PID int `json:"pid"`
	// Bin — путь к запущенному бинарю ядра. Первая из трёх проверок
	// опознания: номера процессов переиспользуются системой, и без этого
	// принять за своё можно что угодно, занявшее освободившийся номер.
	Bin string `json:"bin"`
	// Api — адрес служебного порта ядра. Вторая проверка и одновременно
	// единственный способ убедиться, что ядро не просто живо как процесс, а
	// действительно работает.
	Api string `json:"api"`
	// Otpechatok — отпечаток конфига, по которому ядро было запущено.
	// Третья проверка. Он же решает, можно ли принять ядро как есть или его
	// конфиг успел устареть и нужна смена.
	Otpechatok string `json:"otpechatok"`
	// Adapter — имя сетевого адаптера туннеля. Не для опознания, а чтобы
	// преемница знала, какое имя занято, и не пыталась поднять своё ядро на
	// том же.
	Adapter string `json:"adapter"`
	// Kogda — когда записка написана. Для журнала и разбора, решения по ней
	// не принимаются: устаревшая записка отсеивается опознанием, а не сроком.
	Kogda time.Time `json:"kogda"`
}

// imyaPeredachi — файл записки. Рядом с остальными следами копии
// (tunnel.json, proksi.json) и по той же причине: переживает жёсткую смерть
// процесса, которую не переживёт ни один defer.
const imyaPeredachi = "peredacha_yadra.json"

// PutPeredachiVPapke — где лежит записка при данной папке приложения.
// Папкой параметром, а не из hranenie: пакет yadro о расположении данных
// приложения не знает и знать не должен, а проверкам нужна своя папка.
func PutPeredachiVPapke(papka string) string {
	return filepath.Join(papka, imyaPeredachi)
}

// OtpechatokKonfiga — отпечаток конфига ядра, лежащего на диске.
func OtpechatokKonfiga(put string) string {
	b, err := os.ReadFile(put)
	if err != nil {
		return ""
	}
	return OtpechatokKonfigaBayt(b)
}

// OtpechatokKonfigaBayt — отпечаток конфига ПО СМЫСЛУ, а не побайтно.
//
// По содержимому, а не по времени правки: конфиг пересобирается на каждом
// подключении, и время менялось бы даже когда ни один байт не изменился —
// тогда любое обновление считалось бы сменой конфига и рвало бы туннель зря.
//
// И с вычеркнутым именем сетевого адаптера. Причина замерена на стенде
// 11.09.2026: ядро работало на «tun126» (имя подобрано, потому что «tun125»
// было занято остатком прошлой попытки), а новая копия собирала конфиг с
// «tun125» из профиля. Побайтно это разные конфиги, по смыслу — один и тот
// же: имя адаптера деталь того, как связь поднята, а не того, как она
// работает. Из-за этой разницы приём ядра каждый раз вырождался в смену, и
// туннель терялся на доли секунды там, где мог не теряться вовсе.
func OtpechatokKonfigaBayt(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	sum := sha256.Sum256(bezImeniAdaptera(b))
	return hex.EncodeToString(sum[:])[:16]
}

// ImyaAdapteraIzKonfiga — под каким именем ядро поднимает сетевой адаптер.
//
// Берём из самого конфига, а не из того, кто его собирал: конфиг — это ровно
// то, с чем ядро стартует, и разойтись с ним имя не может. Попытка носить имя
// отдельным полем 11.09.2026 дала пустую строку в записке всякий раз, когда
// имя не подбиралось, а бралось из профиля.
func ImyaAdapteraIzKonfiga(put string) string {
	b, err := os.ReadFile(put)
	if err != nil {
		return ""
	}
	var derevo struct {
		Inbounds []struct {
			InterfaceName string `json:"interface_name"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(b, &derevo); err != nil {
		return ""
	}
	for _, v := range derevo.Inbounds {
		if v.InterfaceName != "" {
			return v.InterfaceName
		}
	}
	return ""
}

// bezImeniAdaptera — тот же конфиг, но со стёртым interface_name.
//
// Разбором JSON, а не заменой по строке: имя адаптера встречается в конфиге
// один раз, но подстрока «tun125» может попасться и в чужом значении, а
// испортить чужое поле ради отпечатка — значит получить отпечаток не того
// конфига. Не разобралось — отдаём как есть: хуже побайтного сравнения не
// будет, оно и было до 11.09.2026.
func bezImeniAdaptera(b []byte) []byte {
	var derevo map[string]any
	if err := json.Unmarshal(b, &derevo); err != nil {
		return b
	}
	vhody, _ := derevo["inbounds"].([]any)
	tronuli := false
	for _, v := range vhody {
		vhod, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if _, est := vhod["interface_name"]; est {
			vhod["interface_name"] = ""
			tronuli = true
		}
	}
	if !tronuli {
		return b
	}
	// Ключи map в Go сериализуются по алфавиту, поэтому один и тот же конфиг
	// всегда даёт одни и те же байты — на этом и держится сравнение.
	rovnyy, err := json.Marshal(derevo)
	if err != nil {
		return b
	}
	return rovnyy
}

// ZapisatPeredachu кладёт записку рядом с данными приложения.
//
// Ошибка записи не должна мешать работе: записка — подстраховка для будущей
// копии, а не часть защиты. Не записалась — следующая копия просто поднимет
// своё ядро, как делала всегда до 11.09.2026.
func ZapisatPeredachu(papka string, p Peredacha) {
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	if err := os.MkdirAll(papka, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(PutPeredachiVPapke(papka), b, 0o600)
}

// ProchestPeredachu — записка прошлой копии, если она есть.
func ProchestPeredachu(papka string) (Peredacha, bool) {
	b, err := os.ReadFile(PutPeredachiVPapke(papka))
	if err != nil {
		return Peredacha{}, false
	}
	var p Peredacha
	if err := json.Unmarshal(b, &p); err != nil {
		return Peredacha{}, false
	}
	if p.PID <= 0 {
		return Peredacha{}, false
	}
	return p, true
}

// UbratPeredachu снимает записку. Зовётся при штатной остановке ядра: ядра
// больше нет, и принимать преемнице нечего.
func UbratPeredachu(papka string) { _ = os.Remove(PutPeredachiVPapke(papka)) }

// PogasitChuzhoe гасит процесс ядра по номеру, не принимая его под управление.
//
// Нужно ровно для одного случая: ядро живо, но эта копия управлять им не
// может (записка не сошлась, бинарь не наш, порт занят кем-то ещё). Оставить
// его — значит оставить на машине связь без хозяина: она работает, а человек
// не может её ни увидеть, ни выключить.
//
// Отдельно от Yadro.Ostanovit намеренно: тот гасит СВОЁ ядро и снимает
// записку, а здесь мы гасим чужое и о своём состоянии речи не идёт.
func PogasitChuzhoe(pid int) {
	if pid <= 0 {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	if err := proc.Kill(); err != nil {
		log.Printf("не смог погасить ядро без хозяина (pid %d): %v", pid, err)
	}
}

// ZhivoPoZapiske — отвечает ли служебный порт ядра, названного в записке.
//
// Отдельно от Yadro.Zhivo и намеренно дёшево: это спрашивают в самом начале
// запуска, ДО того как собран Yadro и вообще что-либо решено. Ответ «да»
// означает, что живой адаптер туннеля — не след аварии, а работающая связь
// прошлой копии, и трогать её нельзя (см. uborkaSledaTunnelya в cmd/kelevra).
func ZhivoPoZapiske(papka string) (Peredacha, bool) {
	p, est := ProchestPeredachu(papka)
	if !est {
		return Peredacha{}, false
	}
	adres := p.Api
	if adres == "" {
		adres = ApiAdres
	}
	klient := &http.Client{Timeout: 2 * time.Second}
	otvet, err := klient.Get("http://" + adres + "/version")
	if err != nil {
		return p, false
	}
	defer otvet.Body.Close()
	return p, otvet.StatusCode == http.StatusOK
}
