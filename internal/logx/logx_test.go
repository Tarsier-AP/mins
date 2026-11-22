package logx

import (
	"bytes"
	"strings"
	"testing"
)

func TestSinkReplacesTerminalWrites(t *testing.T) {
	var out, errOut bytes.Buffer
	l := &Logger{Out: &out, Err: &errOut}
	var got []string
	l.SetSink(func(line string) { got = append(got, line) })

	l.Info("hello %d", 1)
	l.Error("bad %s", "thing")

	if out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("sink still wrote to the terminal: out=%q err=%q", out.String(), errOut.String())
	}
	if len(got) != 2 || got[0] != "[mins] hello 1" || got[1] != "mins: error: bad thing" {
		t.Fatalf("sink got %q", got)
	}
}

func TestNilSinkRestoresTerminalWrites(t *testing.T) {
	var out bytes.Buffer
	l := &Logger{Out: &out, Err: &out}
	l.SetSink(func(string) {})
	l.SetSink(nil)

	l.Info("back")

	if !strings.Contains(out.String(), "[mins] back") {
		t.Fatalf("stdout = %q, want the info line", out.String())
	}
}
