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
// Перенесено из телефонного эталона исключение «сеть точно сменилась —
// верим первому наблюдению» (kbox_push/.../bg/AutoModeGate.kt:53-72,
// offer(trust=true) → needed=1): наблюдение, доверенное вызывающей
// стороной ([Zadvizhka.Predlozhit] с dovereno=true), меняет обстановку
// с одного раза, минуя [Podtverzhdeniy]. На телефоне доверенным считается
// первое наблюдение сразу после события смены сети (AutoMode.kt:1035,
// trustOnce = networkChanged); здесь то же самое решает вызывающая
// сторона — [Sluzhitel] помечает так наблюдение, пришедшее по [Sledchik],
// а страховочный тикер — нет.
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
//
// dovereno — наблюдение пришло по уже доказанному сигналу смены сети (см.
// комментарий типа), а не от страховочного тикера: тогда обстановка меняется
// сразу, без набора [Podtverzhdeniy] — та же логика, что needed=1 в
// AutoModeGate.offer(trust=true) на телефоне (AutoModeGate.kt:68-72).
//
// Доверие НЕ распространяется на [Neizvestno]: доказанный сигнал смены сети
// доказывает, что сеть шевельнулась, но «не знаю» — это не обстановка, в
// которую можно прыгнуть с одного наблюдения. А приходит Neizvestno как раз
// оттуда, где спешить опаснее всего: молчащий резолвер при роуминге между
// репитером и роутером (см. Nablyudeniye.DnsMolchit, авария 28.08) —
// событие смены сети там доверенное, а наблюдение построено на молчании и
// стоит ровно ничего. Такому наблюдению нужны полные [Podtverzhdeniy]
// подряд, как заходу страховочного тикера.
//
// Возвращает true, если обстановка сменилась именно этим вызовом.
func (z *Zadvizhka) Predlozhit(nablyudeno Sostoyanie, dovereno bool) bool {
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

	nuzhno := Podtverzhdeniy
	if dovereno && nablyudeno != Neizvestno {
		nuzhno = 1
	}
	if z.hitov < nuzhno {
		return false
	}
	z.tekushcheye = nablyudeno
	return true
}
