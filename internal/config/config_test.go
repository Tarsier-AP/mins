package config

import (
	"strings"
	"testing"

	"mins/internal/cli"
)

func parse(t *testing.T, args ...string) *Config {
	t.Helper()
	o, err := cli.Parse(args)
	if err != nil {
		t.Fatalf("cli.Parse: %v", err)
	}
	c, err := Normalize(o)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	return c
}

func TestDefaults(t *testing.T) {
	c := parse(t, "in.mp4")
	if c.Mode != "auto" || c.Encoder != "svt" || c.VidFmt != "mp4" {
		t.Errorf("mode/encoder/container: %s/%s/%s", c.Mode, c.Encoder, c.VidFmt)
	}
	if c.TargetVMAF != 95 || c.TargetRaw != "95" {
		t.Errorf("target: %v/%q", c.TargetVMAF, c.TargetRaw)
	}
	if c.ImgFmt != "webp" || c.ImgEncoder != "auto" || c.ImgBackend != "auto" {
		t.Errorf("image: %s/%s/%s", c.ImgFmt, c.ImgEncoder, c.ImgBackend)
	}
	if c.CRFMin != 15 || c.CRFMax != 63 || c.QLab != "crf" || c.QPHardMin != 1 {
		t.Errorf("crf: %d..%d %s hard=%d", c.CRFMin, c.CRFMax, c.QLab, c.QPHardMin)
	}
	if c.QMin != 0 || c.QMax != 95 || c.WebPMethod != 6 || c.AVIFSpeed != 0 || c.JXLEffort != 9 {
		t.Errorf("image quality defaults: %d..%d m=%d s=%d e=%d", c.QMin, c.QMax, c.WebPMethod, c.AVIFSpeed, c.JXLEffort)
	}
	if c.AudioMetric != "visqol" || c.TargetAudio != 4.4 || c.AudioTargetRaw != "4.4" {
		t.Errorf("audio: %s %v/%q", c.AudioMetric, c.TargetAudio, c.AudioTargetRaw)
	}
	if c.PixFmt != "yuv420p10le" || c.AudioBitrate != "256k" {
		t.Errorf("fixed settings: %s/%s", c.PixFmt, c.AudioBitrate)
	}
	if c.AnimFPS != 10 || c.AnimFPSRaw != "10" {
		t.Errorf("anim fps: %v/%q", c.AnimFPS, c.AnimFPSRaw)
	}
}

func TestNativeEncoderImpliesFormat(t *testing.T) {
	c := parse(t, "--img-encoder", "avifenc", "in.png")
	if c.ImgEncoder != "libavif" || c.ImgBackend != "libavif" || c.ImgFmt != "avif" {
		t.Errorf("avifenc: %s/%s/%s", c.ImgEncoder, c.ImgBackend, c.ImgFmt)
	}
	if _, err := Normalize(mustParse(t, "--img-encoder", "libavif", "--img-fmt", "webp", "in.png")); err == nil {
		t.Fatal("expected a contradiction error")
	}
	if _, err := Normalize(mustParse(t, "--img-encoder", "magick", "in.png")); err == nil ||
		!strings.Contains(err.Error(), "ImageMagick is not bundled") {
		t.Fatalf("magick error = %v", err)
	}
}

func TestNvencBounds(t *testing.T) {
	c := parse(t, "--encoder", "nvenc", "in.mp4")
	if c.CRFMin != 0 || c.CRFMax != 51 || c.QLab != "cq" || c.QPHardMin != 0 {
		t.Errorf("nvenc: %d..%d %s hard=%d", c.CRFMin, c.CRFMax, c.QLab, c.QPHardMin)
	}
}

func TestAudioMetrics(t *testing.T) {
	c := parse(t, "--audio-metric", "asisdr", "in.flac")
	if c.TargetAudio != 30 || c.AudioTargetRaw != "30" || c.AudioUnit != "dB" {
		t.Errorf("asisdr defaults: %v/%q/%q", c.TargetAudio, c.AudioTargetRaw, c.AudioUnit)
	}
	c = parse(t, "--audio-metric", "visqol", "--target-audio", "4.2", "in.flac")
	if c.TargetAudio != 4.2 || c.AudioTargetRaw != "4.2" {
		t.Errorf("explicit target: %v/%q", c.TargetAudio, c.AudioTargetRaw)
	}
	if _, err := Normalize(mustParse(t, "--audio-metric", "apsnr", "in.flac")); err == nil {
		t.Fatal("apsnr should require --target-audio")
	}
}

func TestTrimValidation(t *testing.T) {
	c := parse(t, "--start", "1:02.5", "--end", "1:10", "in.mp4")
	if c.Start != 62.5 || c.End != 70 || c.StartRaw != "62.500" || c.EndRaw != "70.000" {
		t.Errorf("trim: %v..%v raw %q..%q", c.Start, c.End, c.StartRaw, c.EndRaw)
	}
	if _, err := Normalize(mustParse(t, "--start", "10", "--end", "5", "in.mp4")); err == nil {
		t.Fatal("expected --end <= --start error")
	}
}

func TestJXLQualityFloor(t *testing.T) {
	if _, err := Normalize(mustParse(t, "--img-encoder", "cjxl", "--q-max", "4", "in.png")); err == nil {
		t.Fatal("expected the libjxl q-max floor error")
	}
}

func mustParse(t *testing.T, args ...string) *cli.Options {
	t.Helper()
	o, err := cli.Parse(args)
	if err != nil {
		t.Fatalf("cli.Parse: %v", err)
	}
	return o
}
