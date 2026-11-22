//go:build !linux && !darwin && !windows

package tui

import (
	"io"
	"os"
)

func terminalSize(fd uintptr) (int, int) { return 0, 0 }

func resizeSignal() <-chan os.Signal { return nil }

func enterScreen(w io.Writer) {
	_, _ = io.WriteString(w, "\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H")
}

func leaveScreen(w io.Writer) {
	_, _ = io.WriteString(w, "\x1b[0m\x1b[?25h\x1b[?1049l")
}

func rawMode(fd uintptr) (func(), bool) { return nil, false }
