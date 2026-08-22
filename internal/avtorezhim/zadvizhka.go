package avtorezhim

// Podtverzhdeniy — сколько одинаковых наблюдений подряд нужно, чтобы
// авторежим поменял обстановку. Тот же приём, что у AutoModeGate в
// телефонном клиенте (kbox_push/.../bg/AutoModeGate.kt): одиночное
// наблюдение — не повод дёргать VPN, DNS может ответить через раз на
// свежем Wi-Fi, а прямая проба — потерять один пакет.
const Podtverzhdeniy = 3

// Zadvizhka — задвижка между «увидел» и «сделал»: не отдаёт новое
// [Zadvizhka.Tekushcheye], пока одно и то же наблюдение не пришло
// [Podtverzhdeniy] раз подряд.
//
// Упрощение против телефонного эталона: там есть исключение «сеть точно
// сменилась — верим первому наблюдению» (AutoModeGate.offer(trust=true)),
// завязанное на событие смены сети. Здесь его нет — решать, насколько
// доверять свежести наблюдения после события [Sledchik], имеет смысл
// вместе с тем, как авторежим реально подключат к proksi/sluzhba, а не
// раньше (см. TODO в пакете).
type Zadvizhka struct {
	tekushcheye Sostoyanie
	kandidat    Sostoyanie
	hitov       int
}

// NovayaZadvizhka заводит задвижку, уже стоящую на nachalo (подтверждений
// для неё не требуется — начальная обстановка считается принятой сразу).
func NovayaZadvizhka(nachalo Sostoyanie) *Zadvizhka {
	return &Zadvizhka{tekushcheye: nachalo, kandidat: nachalo, hitov: Podtverzhdeniy}
}

// Tekushcheye — обстановка, на которой задвижка стоит прямо сейчас.
func (z *Zadvizhka) Tekushcheye() Sostoyanie { return z.tekushcheye }

// Predlozhit — новое наблюдение этого захода.
// Возвращает true, если обстановка сменилась именно этим вызовом.
func (z *Zadvizhka) Predlozhit(nablyudeno Sostoyanie) bool {
	if nablyudeno == z.tekushcheye {
		// Подтвердили то, на чём и так стоим — считаем кандидата тем же,
		// чтобы одиночное отклонение в сторону не начинало набор с середины.
		z.kandidat = nablyudeno
		z.hitov = Podtverzhdeniy
		return false
	}

	if nablyudeno == z.kandidat {
		z.hitov++
	} else {
		z.kandidat = nablyudeno
		z.hitov = 1
	}

	if z.hitov < Podtverzhdeniy {
		return false
	}
	z.tekushcheye = nablyudeno
	return true
}
