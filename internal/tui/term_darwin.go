//go:build darwin

package tui

const (
	termGet    = 0x40487413 // TIOCGETA
	termSet    = 0x80487414 // TIOCSETA
	termICANON = 0x100
	termECHO   = 0x8
	termVMIN   = 16
	termVTIME  = 17
)
