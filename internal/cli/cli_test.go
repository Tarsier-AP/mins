package cli

import "testing"

func TestDefaults(t *testing.T) {
	o, err := Parse([]string{"in.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{
		"mode": o.Mode, "encoder": o.Encoder, "vid-fmt": o.VidFmt,
		"img-fmt": o.ImgFmt, "img-encoder": o.ImgEncoder, "avif-yuv": o.AvifYUV,
		"q-min": o.QMin, "q-max": o.QMax, "audio-metric": o.AudioMetric,
		"audio-band": o.AudioBand, "audio-fmt": o.AudioFmt, "method": o.Method,
		"aspeed": o.ASpeed, "jxl-effort": o.JXLEffort, "anim-fps": o.AnimFPS,
		"target": o.Target, "atracks": o.ATracks, "stracks": o.STracks,
		"br-min": o.BRMin, "br-max": o.BRMax,
	}
	want := map[string]string{
		"mode": "auto", "encoder": "svt", "vid-fmt": "mp4", "img-fmt": "webp",
		"img-encoder": "auto", "avif-yuv": "420", "q-min": "0", "q-max": "95",
		"audio-metric": "visqol", "audio-band": "40:20000", "audio-fmt": "auto",
		"method": "6", "aspeed": "0", "jxl-effort": "9", "anim-fps": "10",
		"target": "95", "atracks": "all", "stracks": "all", "br-min": "32",
		"br-max": "256",
	}
	for k, got := range checks {
		if got != want[k] {
			t.Errorf("default %s = %q, want %q", k, got, want[k])
		}
	}
	if len(o.Positional) != 1 || o.Positional[0] != "in.mp4" {
		t.Errorf("positional = %v", o.Positional)
	}
}

func TestAliasesAndValues(t *testing.T) {
	o, err := Parse([]string{
		"--cq-min", "20", "--cq-max", "40", "--audio", "0,eng", "--subs", "none",
		"--frame-by-frame", "--instances", "3", "--img-encoder", "cwebp",
		"a.mkv", "b.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.CRFMin != "20" || o.CRFMax != "40" {
		t.Errorf("cq aliases: %q/%q", o.CRFMin, o.CRFMax)
	}
	if o.ATracks != "0,eng" || o.STracks != "none" {
		t.Errorf("track aliases: %q/%q", o.ATracks, o.STracks)
	}
	if !o.FBF || o.Workers != "3" {
		t.Errorf("fbf/workers: %v/%q", o.FBF, o.Workers)
	}
	if o.ImgEncoder != "cwebp" || !o.ImgEncoderSet {
		t.Errorf("img encoder: %q set=%v", o.ImgEncoder, o.ImgEncoderSet)
	}
	if len(o.Positional) != 2 || o.Positional[0] != "a.mkv" || o.Positional[1] != "b.png" {
		t.Errorf("positional = %v", o.Positional)
	}
}

func TestMissingValue(t *testing.T) {
	if _, err := Parse([]string{"--target"}); err == nil {
		t.Fatal("expected an error for a missing value")
	}
}

func TestQAuto(t *testing.T) {
	o, err := Parse([]string{"--q-auto", "a.jpg", "b.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.QAuto {
		t.Fatal("--q-auto did not set QAuto")
	}
	if len(o.Positional) != 2 || o.Positional[0] != "a.jpg" || o.Positional[1] != "b.jpg" {
		t.Errorf("positional = %v", o.Positional)
	}
}

func TestHelpAndVersionMarkers(t *testing.T) {
	o, _ := Parse([]string{"--help"})
	if o.Log != "\x00help" {
		t.Errorf("help marker = %q", o.Log)
	}
	o, _ = Parse([]string{"--version"})
	if o.Log != "\x00version" {
		t.Errorf("version marker = %q", o.Log)
	}
}
