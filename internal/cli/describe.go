package cli

import (
	"encoding/json"
	"fmt"
)

// Flag is one option's machine description. It carries facts the parser and
// config actually enforce; user-facing wording lives in mins-gui.
type Flag struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	Kind    string   `json:"kind"` // bool, number, enum, text, path
	Default string   `json:"default,omitempty"`
	Values  []string `json:"values,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
}

// Surface is the whole --describe payload.
type Surface struct {
	Version   string         `json:"version"`
	Flags     []Flag         `json:"flags"`
	ExitCodes map[string]int `json:"exit_codes"`
}

func lim(v float64) *float64 { return &v }

// harnessFlags are set by the frontend itself, not by a form control.
var harnessFlags = map[string]bool{
	"help": true, "version": true, "describe": true, "events": true, "no-tui": true,
}

var surface = Surface{
	Version: Version,
	ExitCodes: map[string]int{
		"ok": 0, "failures": 1, "usage": 2, "interrupted": 130,
	},
	Flags: []Flag{
		{Name: "mode", Kind: "enum", Default: "auto", Values: []string{"auto", "video", "image", "animate"}},
		{Name: "encoder", Kind: "enum", Default: "svt", Values: []string{"svt", "aom", "nvenc"}},
		{Name: "vid-fmt", Kind: "enum", Default: "mp4", Values: []string{"mp4", "mkv", "webm"}},
		{Name: "acodec", Kind: "enum", Default: "auto",
			Values: []string{"auto", "copy", "aac", "libopus", "libvorbis", "ac3", "pcm_s16le"}},
		{Name: "img-fmt", Kind: "enum", Default: "webp", Values: []string{"webp", "avif", "jxl"}},
		{Name: "img-encoder", Kind: "enum", Default: "auto", Aliases: []string{"img-backend"},
			Values: []string{"auto", "libwebp", "libavif", "libjxl"}},
		{Name: "avif-yuv", Kind: "enum", Default: "420", Values: []string{"420", "422", "444", "400"}},
		{Name: "crf-min", Kind: "number", Aliases: []string{"cq-min"}},
		{Name: "crf-max", Kind: "number", Aliases: []string{"cq-max"}},
		{Name: "q-min", Kind: "number", Default: "0", Min: lim(0), Max: lim(100)},
		{Name: "q-max", Kind: "number", Default: "95", Min: lim(0), Max: lim(100)},
		{Name: "q-auto", Kind: "bool"},
		{Name: "br-min", Kind: "number", Default: "32", Min: lim(1)},
		{Name: "br-max", Kind: "number", Default: "256", Min: lim(1)},
		{Name: "audio-metric", Kind: "enum", Default: "visqol",
			Values: []string{"visqol", "asisdr", "asdr", "apsnr"}},
		{Name: "target-audio", Kind: "number", Min: lim(0)},
		{Name: "audio-search", Kind: "bool"},
		{Name: "audio-band", Kind: "text", Default: "40:20000"},
		{Name: "visqol-model", Kind: "path"},
		{Name: "audio-fmt", Kind: "enum", Default: "auto", Aliases: []string{"audio-container"},
			Values: []string{"auto", "ogg", "mp4", "mov", "mka", "webm", "wav"}},
		{Name: "method", Kind: "number", Default: "6", Min: lim(0), Max: lim(6)},
		{Name: "aspeed", Kind: "number", Default: "0", Min: lim(0), Max: lim(8)},
		{Name: "jxl-effort", Kind: "number", Default: "9", Min: lim(1), Max: lim(9)},
		{Name: "anim-fps", Kind: "number", Default: "10", Min: lim(0)},
		{Name: "anim-width", Kind: "number", Min: lim(1)},
		{Name: "target", Kind: "number", Default: "95", Min: lim(0), Max: lim(100)},
		{Name: "start", Kind: "number", Min: lim(0)},
		{Name: "end", Kind: "number", Min: lim(0)},
		{Name: "atracks", Kind: "text", Default: "all", Aliases: []string{"audio"}},
		{Name: "stracks", Kind: "text", Default: "all", Aliases: []string{"subs", "subtitles"}},
		{Name: "fbf", Kind: "bool", Aliases: []string{"frame-by-frame"}},
		{Name: "instances", Kind: "number", Min: lim(1), Aliases: []string{"fbf-workers"}},
		{Name: "fbf-chunk-seconds", Kind: "number", Default: "1", Min: lim(0), Aliases: []string{"fbf-chunk"}},
		{Name: "refine", Kind: "bool", Aliases: []string{"qsplit"}},
		{Name: "refine-seconds", Kind: "number", Default: "2", Min: lim(0)},
		{Name: "refine-min-frames", Kind: "number", Default: "1", Min: lim(1)},
		{Name: "refine-step", Kind: "number", Default: "2", Min: lim(1)},
		{Name: "refine-boundary-cost", Kind: "number", Default: "0", Min: lim(0)},
		{Name: "no-json", Kind: "bool", Aliases: []string{"no-report"}},
		{Name: "skip-existing", Kind: "bool"},
		{Name: "log", Kind: "path"},
		{Name: "out-dir", Kind: "path"},
	},
}

// Describe renders the --describe payload as indented JSON.
func Describe() (string, error) {
	b, err := json.MarshalIndent(surface, "", "  ")
	if err != nil {
		return "", fmt.Errorf("describe: %w", err)
	}
	return string(b) + "\n", nil
}
