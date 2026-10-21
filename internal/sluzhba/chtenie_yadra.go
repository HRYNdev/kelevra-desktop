package sluzhba

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Место чтения журнала ядра между перезапусками службы.
//
// Зачем. Смещение в журнале ядра раньше жило только в памяти. Сперва после
// перезапуска службы оно начиналось с нуля, и журнал, уже разобранный прошлой
// копией, разбирался заново — старые отказы ложились в свежее окно и давали
// ложную «беду» (ноутбук 07–08.10). Потом (0.6.80) стало начинаться с конца
// файла: ложной беды нет, но строки, которые ядро дописало, пока служба
// перезапускалась, терялись. Теперь прошлая копия оставляет на диске, где
// остановилась, а следующая продолжает с того же места.
//
// Как узнаём, что файл тот же. Одного размера мало: ядро на каждом своём
// старте откладывает прошлый журнал и заводит новый (yadro.sozdatZhurnalYadra),
// и новый файл может дорасти до старого смещения. Начала файла тоже мало: без
// времени в журнале (log.timestamp выключен) первые строки у разных стартов
// одинаковы. Поэтому файл тот же, только если сошлось всё три:
//
//   - номер файла в файловой системе (NTFS: том + индекс файла; unix: устройство
//     + inode) — прошлый журнал при ротации переименовывается и живёт дальше
//     как .proshlyy со своим номером, новый получает другой. Время создания
//     не годится: Windows переносит его на файл, созданный под тем же именем
//     сразу после переименования прежнего (file tunneling);
//   - отпечаток первых байт файла;
//   - отпечаток хвоста ПЕРЕД смещением: последние байты, которые прошлая копия
//     прочла. В них номера соединений и длительности — у другого файла на том
//     же месте их нет.
//
//   - файл тот же и не короче смещения — читаем с запомненного места;
//   - файл другой или стал короче (ротация, усечение) — всё в нём написано
//     после того, как прошлая копия остановилась, читаем с нуля;
//   - места нет, оно битое или старше окна сводки — как раньше, с конца файла:
//     строки, накопленные за долгое отсутствие службы, легли бы в одно окно и
//     исказили его.

// ImyaMestaChteniya — файл места чтения в папке данных.
const ImyaMestaChteniya = "chtenie_yadra.json"

// dlinaOtpechatka — сколько байт начала файла и хвоста перед смещением идёт
// в отпечатки.
const dlinaOtpechatka = 4096

// mestoChteniya — что лежит в файле места.
type mestoChteniya struct {
	Smeshenie int64 `json:"smeshenie"`
	// Razmer — размер журнала в момент записи места; для разбора руками.
	Razmer int64 `json:"razmer"`
	// Nomer — номер файла в файловой системе (nomerFayla); пусто, если
	// система его не дала, — тогда решают отпечатки.
	Nomer string `json:"nomer,omitempty"`
	// Otpechatok — sha256 первых DlinaOtpechatka байт журнала (не больше
	// смещения: дальше смещения файл ещё мог дописываться).
	Otpechatok      string `json:"otpechatok"`
	DlinaOtpechatka int64  `json:"dlina_otpechatka"`
	// Hvost — sha256 последних DlinaOtpechatka байт перед смещением.
	Hvost string    `json:"hvost"`
	Kogda time.Time `json:"kogda"`
}

// nachat ставит смещение на старте службы: с запомненного места, с нуля или с
// конца файла (см. шапку файла).
func (n *nablyudatelYadra) nachat(seychas time.Time) {
	m, ok := prochestMesto(n.mesto)
	if !ok || seychas.Sub(m.Kogda) > OknoSvodkiYadra || m.Kogda.After(seychas.Add(time.Minute)) {
		n.nachatSKonca()
		return
	}
	f, err := os.Open(n.put)
	if err != nil {
		// Журнала нет — новое ядро заведёт его, и он прочтётся с начала.
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		n.nachatSKonca()
		return
	}
	n.fayl = st
	if totZheFayl(f, st, m) {
		n.smeshenie = m.Smeshenie
		n.razmer = m.Smeshenie
		return
	}
	// Файл другой или усечён: всё в нём — после прошлой копии службы.
	n.smeshenie = 0
	n.razmer = 0
	n.mestoSdvinulos = true
}

// zapomnitMesto кладёт нынешнее место чтения на диск. Ошибки молча: место —
// удобство, а не работа; без него следующая копия начнёт с конца файла.
func (n *nablyudatelYadra) zapomnitMesto(seychas time.Time) {
	if n.mesto == "" || n.seredinaStroki {
		// Смещение посреди строки (старт с конца файла, ничего ещё не
		// прочитано) — не место, с которого можно продолжать.
		return
	}
	f, err := os.Open(n.put)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return
	}
	// Открыт уже другой файл (ядро только что перезапущено) — место
	// относится к прежнему; запишем на следующем заходе, когда прочтём новый.
	if n.fayl != nil && !os.SameFile(n.fayl, st) {
		return
	}
	dlina := n.smeshenie
	if dlina > dlinaOtpechatka {
		dlina = dlinaOtpechatka
	}
	otp, err := otpechatok(f, 0, dlina)
	if err != nil {
		return
	}
	hvost, err := otpechatok(f, n.smeshenie-dlina, dlina)
	if err != nil {
		return
	}
	b, err := json.Marshal(mestoChteniya{
		Smeshenie:       n.smeshenie,
		Razmer:          st.Size(),
		Nomer:           nomerFayla(f, st),
		Otpechatok:      otp,
		DlinaOtpechatka: dlina,
		Hvost:           hvost,
		Kogda:           seychas,
	})
	if err != nil {
		return
	}
	vremennyy := n.mesto + ".pishem"
	if err := os.WriteFile(vremennyy, b, 0o644); err != nil {
		return
	}
	if err := os.Rename(vremennyy, n.mesto); err != nil {
		_ = os.Remove(vremennyy)
		return
	}
	n.mestoSdvinulos = false
	n.mestoZapisano = seychas
}

// totZheFayl — открытый журнал тот же, что описан в месте (см. шапку).
func totZheFayl(f *os.File, st os.FileInfo, m mestoChteniya) bool {
	if st.Size() < m.Smeshenie || m.DlinaOtpechatka > m.Smeshenie {
		return false
	}
	if nomer := nomerFayla(f, st); nomer != "" && m.Nomer != "" && nomer != m.Nomer {
		return false
	}
	if otp, err := otpechatok(f, 0, m.DlinaOtpechatka); err != nil || otp != m.Otpechatok {
		return false
	}
	hvost, err := otpechatok(f, m.Smeshenie-m.DlinaOtpechatka, m.DlinaOtpechatka)
	return err == nil && hvost == m.Hvost
}

// prochestMesto — место с диска; нет файла или он битый — ok=false.
func prochestMesto(put string) (mestoChteniya, bool) {
	var m mestoChteniya
	if put == "" {
		return m, false
	}
	b, err := os.ReadFile(filepath.Clean(put))
	if err != nil {
		return m, false
	}
	if err := json.Unmarshal(b, &m); err != nil || m.Smeshenie < 0 || m.DlinaOtpechatka < 0 || m.Kogda.IsZero() {
		return m, false
	}
	return m, true
}

// otpechatok — sha256 dlina байт файла с позиции otkuda.
func otpechatok(f *os.File, otkuda, dlina int64) (string, error) {
	b := make([]byte, dlina)
	// Короткое чтение — ошибка: файл короче, чем был при записи места, и
	// дополненный нулями отпечаток не должен случайно совпасть.
	if dlina > 0 {
		if _, err := io.ReadFull(io.NewSectionReader(f, otkuda, dlina), b); err != nil {
			return "", err
		}
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
