package tui

import "sync"

var (
	activeMu sync.Mutex
	active   = map[*Dashboard]struct{}{}
)

func register(d *Dashboard) {
	activeMu.Lock()
	active[d] = struct{}{}
	activeMu.Unlock()
}

func unregister(d *Dashboard) {
	activeMu.Lock()
	delete(active, d)
	activeMu.Unlock()
}
