package avtorezhim

import (
	"sync"
	"time"
)

// PamyatPriznaka — сколько признак дома по DNS считается ещё стоящим после
// того, как его видели в последний раз (HomeSign.MEMORY_MILLIS в телефонном
// эталоне, HomeSign.kt). Мерка та же: 45 секунд — самый длинный шаг серии
// перепроверок после смены сети (BURST_STEPS на телефоне — 20с), признак
// обязан пережить один такой шаг вместе с самим заходом. Больше брать
// незачем: смена сети стирает память сразу (см. PriznakPamyat.Zabyt,
// Avtorezhim.Zahod про dovereno), а на той же сети дом никуда не девается.
const PamyatPriznaka = 45 * time.Second

// PriznakPamyat — память DNS-признака дома между заходами (перенос
// HomeSign.stands/rememberHomeSign/forgetHomeSign с телефона).
//
// Одна сводка, где DNS ответил и подмены не нашёл, признак дома не
// отменяет: свежий Wi-Fi мигает (живой замер на телефоне дал 3 из 3 → 0 из
// 3 → 3 из 3 подряд), и без памяти это выглядело бы как «дом моргает» —
// авторежим поднимал бы туннель на секунду и гасил его обратно. Отменяет
// признак только опровержение делом ([PriznakPamyat.Zabyt] при
// TrafikPryamoy == false) или сигнал смены сети ([PriznakPamyat.Zabyt] при
// dovereno==true в Avtorezhim.Zahod).
//
// На телефоне память ключуется объектом Network — здесь такого объекта нет
// (Windows-авторежим не отслеживает сетевые профили, только событие смены,
// см. Sledchik), поэтому ключ один: сигнал dovereno стирает память целиком,
// как если бы сеть сменилась.
//
// nil-приёмник безопасен и ведёт себя как «памяти нет» — так существующие
// тесты и код, собирающие Avtorezhim литералом без поля Priznak, не меняют
// поведения.
type PriznakPamyat struct {
	mu      sync.Mutex
	videliV time.Time // нулевое значение — признак ни разу не видели
}

// NovyyPriznakPamyat — пустая память, признака ещё не видели.
func NovyyPriznakPamyat() *PriznakPamyat { return &PriznakPamyat{} }

// Zapomnit — признак дома видели прямо сейчас.
func (p *PriznakPamyat) Zapomnit(seychas time.Time) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.videliV = seychas
	p.mu.Unlock()
}

// Zabyt — держаться за признак больше не на чем (опровергнут делом или
// сеть, к которой он относился, уже не та).
func (p *PriznakPamyat) Zabyt() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.videliV = time.Time{}
	p.mu.Unlock()
}

// Stoit — стоит ли ещё признак, увиденный в последний раз, если этот заход
// сам его не подтвердил. pamyat<=0 — берётся PamyatPriznaka.
func (p *PriznakPamyat) Stoit(seychas time.Time, pamyat time.Duration) bool {
	if p == nil {
		return false
	}
	if pamyat <= 0 {
		pamyat = PamyatPriznaka
	}
	p.mu.Lock()
	videliV := p.videliV
	p.mu.Unlock()
	if videliV.IsZero() {
		return false
	}
	vozrast := seychas.Sub(videliV)
	return vozrast >= 0 && vozrast < pamyat
}
