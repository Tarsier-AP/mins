package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFKeepsPythonFloatShape(t *testing.T) {
	cases := map[F]string{95: "95.0", 4.4: "4.4", 95.017503: "95.017503", 0: "0.0"}
	for in, want := range cases {
		got, err := in.MarshalJSON()
		if err != nil || string(got) != want {
			t.Errorf("F(%v) = %s, %v; want %s", float64(in), got, err, want)
		}
	}
}

func TestWriteMatchesPythonJSONDump(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.json")
	crf := 0
	doc := &Video{
		Input: "in.mp4", Output: "/tmp/out.mp4", Mode: "video",
		Encoder: "svt-av1", Container: "mp4", TargetVMAF: 95,
		MeanVMAF: 95.5, Passed: true, CRF: &crf,
		Trim:        Trim{Start: "", End: "", Duration: "1.000"},
		TierQuality: []byte(`{"svt":{"preset":8}}`), PixFmt: "yuv420p10le",
		Audio: AudioInfo{Codec: "copy"}, Subtitles: SubInfo{Policy: "none"},
		SizeBytes: 1234,
	}
	if err := Write(path, doc); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"target_vmaf": 95.0`,
		`"mean_vmaf": 95.5`,
		`"crf": 0`,
		`"tier_quality_settings": {`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.HasSuffix(s, "\n") {
		t.Error("report has a trailing newline; Python's json.dump does not")
	}
}
