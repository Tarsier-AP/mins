package escape

import "testing"

func TestFilter(t *testing.T) {
	cases := map[string]string{
		"/tmp/plain.json":      "/tmp/plain.json",
		"/tmp/a:b.json":        `/tmp/a\\:b.json`,
		`/tmp/a'b.json`:        `/tmp/a\\\'b.json`,
		`/tmp/a\b.json`:        `/tmp/a\\\\b.json`,
		"/tmp/a[b],c;d.json":   `/tmp/a\[b\]\,c\;d.json`,
		"/tmp/with space.json": "/tmp/with space.json",
	}
	for in, want := range cases {
		if got := Filter(in); got != want {
			t.Errorf("Filter(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConcat(t *testing.T) {
	if got := Concat("/tmp/it's here.mp4"); got != `/tmp/it'\''s here.mp4` {
		t.Errorf("Concat quote = %q", got)
	}
	if got := Concat("/tmp/plain.mp4"); got != "/tmp/plain.mp4" {
		t.Errorf("Concat plain = %q", got)
	}
}
