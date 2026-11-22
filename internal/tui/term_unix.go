//go:build linux || darwin

package tui

import (
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"
)

type winsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

func terminalSize(fd uintptr) (int, int) {
	ws := winsize{}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return 0, 0
	}
	return int(ws.Col), int(ws.Row)
}

var (
	resizeOnce sync.Once
	resizeCh   chan os.Signal
)

func resizeSignal() <-chan os.Signal {
	resizeOnce.Do(func() {
		resizeCh = make(chan os.Signal, 1)
		signal.Notify(resizeCh, syscall.SIGWINCH)
	})
	return resizeCh
}

func enterScreen(w io.Writer) {
	_, _ = io.WriteString(w, "\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H")
}

func leaveScreen(w io.Writer) {
	_, _ = io.WriteString(w, "\x1b[0m\x1b[?25h\x1b[?1049l")
}

// rawMode disables line buffering and echo so q is delivered immediately.
// ISIG stays enabled, keeping Ctrl-C working.
func rawMode(fd uintptr) (func(), bool) {
	var old syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, termGet, uintptr(unsafe.Pointer(&old)))
	if errno != 0 {
		return nil, false
	}
	raw := old
	raw.Lflag &^= termICANON | termECHO
	raw.Cc[termVMIN] = 1
	raw.Cc[termVTIME] = 0
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, termSet, uintptr(unsafe.Pointer(&raw)))
	if errno != 0 {
		return nil, false
	}
	return func() {
		_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, fd, termSet, uintptr(unsafe.Pointer(&old)))
	}, true
}
