package avtorezhim

// FizAdapter — физический адаптер с выходом наружу (FizicheskieAdaptery).
type FizAdapter struct {
	// Indeks — индекс интерфейса Windows; 0 — не известен (стенд на линуксе):
	// сокет к адаптеру не привязывается.
	Indeks uint32
	// IP — свой IPv4 адаптера; пусто — не известен.
	IP string
	// Shlyuz — IPv4 шлюза.
	Shlyuz string
}
