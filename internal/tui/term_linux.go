//go:build linux

package tui

// termios request numbers and flag/index values are stable on linux; the
// syscall package omits some of them on amd64/386.
const (
	termGet    = 0x5401 // TCGETS
	termSet    = 0x5402 // TCSETS
	termICANON = 0x2
	termECHO   = 0x8
	termVMIN   = 6
	termVTIME  = 5
)
