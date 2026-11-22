//go:build windows

package tui

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procGetConsoleMode             = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
)

const enableVirtualTerminalProcessing = 0x0004

type coord struct {
	X, Y int16
}

type smallRect struct {
	Left, Top, Right, Bottom int16
}

type consoleScreenBufferInfo struct {
	Size              coord
	CursorPosition    coord
	Attributes        uint16
	Window            smallRect
	MaximumWindowSize coord
}

func terminalSize(fd uintptr) (int, int) {
	var info consoleScreenBufferInfo
	r, _, _ := procGetConsoleScreenBufferInfo.Call(fd, uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0, 0
	}
	return int(info.Window.Right-info.Window.Left) + 1, int(info.Window.Bottom-info.Window.Top) + 1
}

func resizeSignal() <-chan os.Signal { return nil }

func enableVT(w io.Writer) {
	f, ok := w.(*os.File)
	if !ok {
		return
	}
	var mode uint32
	fd := f.Fd()
	if r, _, _ := procGetConsoleMode.Call(fd, uintptr(unsafe.Pointer(&mode))); r == 0 {
		return
	}
	_, _, _ = procSetConsoleMode.Call(fd, uintptr(mode|enableVirtualTerminalProcessing))
}

func enterScreen(w io.Writer) {
	enableVT(os.Stdout)
	enableVT(os.Stderr)
	_, _ = io.WriteString(w, "\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H")
}

func leaveScreen(w io.Writer) {
	_, _ = io.WriteString(w, "\x1b[0m\x1b[?25h\x1b[?1049l")
}

// rawMode disables line input and echo so q is delivered immediately.
// Processed input stays enabled, keeping Ctrl-C working.
func rawMode(fd uintptr) (func(), bool) {
	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(fd, uintptr(unsafe.Pointer(&mode))); r == 0 {
		return nil, false
	}
	const enableLineInput, enableEchoInput = 0x0002, 0x0004
	raw := mode &^ (enableLineInput | enableEchoInput)
	if r, _, _ := procSetConsoleMode.Call(fd, uintptr(raw)); r == 0 {
		return nil, false
	}
	return func() {
		_, _, _ = procSetConsoleMode.Call(fd, uintptr(mode))
	}, true
}
