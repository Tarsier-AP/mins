// Package config validates and normalises the raw command line into the
// resolved settings every processing path uses.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"mins/internal/cli"
	"mins/internal/util"
)

// Config is the fully resolved run configuration.
type Config struct {
	Version string
	Mode    string
	Encoder string
	VidFmt  string
	ACodec  string
	ImgFmt  string
	// ImgEncoder is the user's request; ImgBackend is the resolved native
	// backend. "auto" is settled once by the caller, which knows which
	// tools are available.
	ImgEncoder string
	ImgBackend string
	AvifYUV    string

	TargetVMAF float64
	TargetRaw  string
	CRFMin     int
	CRFMax     int
	QMin       int
	QMax       int
	BRMin      int
	BRMax      int

	AudioMetric     string
	TargetAudio     float64
	AudioTargetRaw  string
	AudioSearch     bool
	AudioBand       string
	AudioBandFilter string
	AudioFmt        string
	VisqolModel     string
	AudioUnit       string
	AudioUnitLong   string
	AudioKey        string
	AudioEps        float64

	WebPMethod int
	AVIFSpeed  int
	JXLEffort  int
	AnimFPS    float64
	AnimFPSRaw string
	AnimWidth  int

	// Fixed encoder/quality settings carried through to reports and the
	// quality-tier encode passes (mins.sh had no flags for these).
	SVTFast         int
	SVTQuality      int
	AOMFast         int
	AOMQuality      int
	NVENCFast       int
	NVENCQuality    int
	NVENCAQStrength int
	PixFmt          string
	AudioBitrate    string

	Start    float64
	End      float64
	StartRaw string
	EndRaw   string
	HasStart bool
	HasEnd   bool

	ATracks string
	STracks string

	FBF                bool
	Workers            int
	WorkersExplicit    bool
	FBFChunkSeconds    float64
	NoTUI              bool
	NoJSON             bool
	Refine             bool
	RefineSeconds      float64
	RefineSecondsRaw   string
	RefineMinFrames    int
	RefineStep         int
	RefineBoundaryCost int
	SkipExisting       bool

	LogFile string
	LogDir  string
	RunID   string

	Positional []string

	QLab      string
	QPHardMin int
	NProc     int

	NeedImage    bool
	NeedVideo    bool
	NeedAudio    bool
	NeedAnim     bool
	NeedVid2Anim bool

	Warnings []string
}

var (
	reUint      = regexp.MustCompile(`^[0-9]+$`)
	reNum       = regexp.MustCompile(`^[0-9]+([.][0-9]+)?$`)
	reSnum      = regexp.MustCompile(`^-?[0-9]+([.][0-9]+)?$`)
	reTracks    = regexp.MustCompile(`^[a-z0-9]+([,][a-z0-9]+)*$`)
	reAudioBand = regexp.MustCompile(`^([0-9]+):([0-9]+)$`)
)

// Normalize validates o and returns the resolved configuration.
func Normalize(o *cli.Options) (*Config, error) {
	c := &Config{
		Version:         cli.Version,
		Mode:            o.Mode,
		Encoder:         o.Encoder,
		VidFmt:          o.VidFmt,
		ACodec:          o.ACodec,
		ImgFmt:          o.ImgFmt,
		ImgEncoder:      o.ImgEncoder,
		AvifYUV:         o.AvifYUV,
		AudioMetric:     strings.ToLower(o.AudioMetric),
		AudioSearch:     o.AudioSearch,
		AudioBand:       strings.ToLower(o.AudioBand),
		AudioFmt:        strings.ToLower(o.AudioFmt),
		VisqolModel:     o.VisqolModel,
		ATracks:         strings.ToLower(strings.ReplaceAll(o.ATracks, " ", "")),
		STracks:         strings.ToLower(strings.ReplaceAll(o.STracks, " ", "")),
		FBF:             o.FBF,
		WorkersExplicit: o.WorkersExplicit,
		NoTUI:           o.NoTUI,
		NoJSON:          o.NoJSON,
		Refine:          o.Refine,
		SkipExisting:    o.SkipExisting,
		LogFile:         o.Log,
		Positional:      o.Positional,
		NProc:           runtime.NumCPU(),
	}
	if c.NProc < 1 {
		c.NProc = 1
	}

	// Fixed quality-tier settings (mins.sh's SVT_FAST/SVT_QUALITY/... defaults).
	c.SVTFast, c.SVTQuality = 8, 2
	c.AOMFast, c.AOMQuality = 6, 3
	c.NVENCFast, c.NVENCQuality, c.NVENCAQStrength = 4, 7, 8
	c.PixFmt = "yuv420p10le"
	c.AudioBitrate = "256k"
	c.StartRaw, c.EndRaw = o.Start, o.End
	c.TargetRaw = o.Target

	switch c.Mode {
	case "auto", "video", "image", "animate":
	default:
		return nil, fmt.Errorf("unknown mode: %s (use auto, video, image or animate)", c.Mode)
	}
	if c.FBF && (c.Mode == "image" || c.Mode == "animate") {
		return nil, fmt.Errorf("--fbf applies to the AV1 video path only (incompatible with --mode %s)", c.Mode)
	}
	if c.Refine {
		if c.FBF {
			return nil, fmt.Errorf("--refine is incompatible with --fbf (--fbf already gates every frame individually; --refine repairs the usual whole-clip encode)")
		}
		if c.Mode == "image" || c.Mode == "animate" {
			return nil, fmt.Errorf("--refine applies to the AV1 video path only (incompatible with --mode %s)", c.Mode)
		}
	}

	var err error
	if c.RefineSeconds, err = parsePositiveNum(o.RefineSeconds, "--refine-seconds"); err != nil {
		return nil, err
	}
	c.RefineSecondsRaw = o.RefineSeconds
	if c.RefineMinFrames, err = parsePositiveInt(o.RefineMinFrames, "--refine-min-frames", 1); err != nil {
		return nil, err
	}
	if c.RefineStep, err = parsePositiveInt(o.RefineStep, "--refine-step", 1); err != nil {
		return nil, err
	}
	if !reUint.MatchString(o.RefineBoundaryCost) {
		return nil, fmt.Errorf("invalid --refine-boundary-cost '%s' (integer >= 0)", o.RefineBoundaryCost)
	}
	c.RefineBoundaryCost, _ = strconv.Atoi(o.RefineBoundaryCost)

	if o.Workers != "" {
		if !reUint.MatchString(o.Workers) {
			return nil, fmt.Errorf("invalid --instances/--fbf-workers '%s' (positive integer)", o.Workers)
		}
		c.Workers, _ = strconv.Atoi(o.Workers)
		if c.Workers < 1 {
			return nil, fmt.Errorf("--instances/--fbf-workers must be >= 1")
		}
	}
	if o.FBFChunk != "" {
		if c.FBFChunkSeconds, err = parsePositiveNum(o.FBFChunk, "--fbf-chunk-seconds"); err != nil {
			return nil, err
		}
	}
	switch c.Encoder {
	case "svt", "aom", "nvenc":
	default:
		return nil, fmt.Errorf("unknown encoder: %s (use svt, aom or nvenc)", c.Encoder)
	}
	switch c.VidFmt {
	case "mp4", "mkv", "webm":
	default:
		return nil, fmt.Errorf("unknown --vid-fmt: %s (use mp4, mkv or webm)", c.VidFmt)
	}
	switch c.ImgFmt {
	case "webp", "avif", "jxl":
	default:
		return nil, fmt.Errorf("unknown image format: %s (use webp, avif or jxl)", c.ImgFmt)
	}

	// --img-encoder: tool names are accepted as spellings of library names.
	enc := strings.ToLower(c.ImgEncoder)
	switch enc {
	case "imagemagick", "magick":
		return nil, fmt.Errorf("--img-encoder magick is not supported: ImageMagick is not bundled; use auto, libwebp, libavif or libjxl")
	case "cwebp":
		enc = "libwebp"
	case "avifenc":
		enc = "libavif"
	case "cjxl":
		enc = "libjxl"
	}
	switch enc {
	case "auto":
		c.ImgEncoder = "auto"
		c.ImgBackend = "auto"
	case "libwebp", "libavif", "libjxl":
		want := map[string]string{"libwebp": "webp", "libavif": "avif", "libjxl": "jxl"}[enc]
		if o.ImgFmtSet && c.ImgFmt != want {
			return nil, fmt.Errorf("--img-encoder %s writes %s, not %s (drop --img-fmt, or use --img-encoder auto)", enc, want, c.ImgFmt)
		}
		c.ImgFmt = want
		c.ImgEncoder = enc
		c.ImgBackend = enc
	default:
		return nil, fmt.Errorf("unknown --img-encoder: %s (use auto, libwebp, libavif or libjxl)", c.ImgEncoder)
	}
	switch c.AvifYUV {
	case "420", "422", "444", "400":
	default:
		return nil, fmt.Errorf("invalid --avif-yuv '%s' (use 420, 422, 444 or 400)", c.AvifYUV)
	}
	if c.TargetVMAF, err = strconv.ParseFloat(o.Target, 64); err != nil || c.TargetVMAF < 0 {
		return nil, fmt.Errorf("invalid --target '%s'", o.Target)
	}

	switch c.AudioMetric {
	case "visqol", "asdr", "apsnr", "asisdr":
	default:
		return nil, fmt.Errorf("unknown --audio-metric '%s' (use visqol, asisdr, asdr or apsnr)", c.AudioMetric)
	}
	if c.AudioMetric == "visqol" {
		c.AudioUnit, c.AudioUnitLong, c.AudioKey, c.AudioEps = "", " MOS-LQO", "mos", 0.05
	} else {
		c.AudioUnit, c.AudioUnitLong, c.AudioKey, c.AudioEps = "dB", " dB", "db", 1.0
	}

	if c.AudioFmt == "mkv" {
		c.AudioFmt = "mka"
	}
	switch c.AudioFmt {
	case "auto", "ogg", "mp4", "mov", "mka", "webm", "wav":
	default:
		return nil, fmt.Errorf("unknown --audio-fmt '%s' (use auto, ogg, mp4, mov, mka/mkv, webm or wav)", c.AudioFmt)
	}

	if c.AudioBand != "off" {
		m := reAudioBand.FindStringSubmatch(c.AudioBand)
		if m == nil {
			return nil, fmt.Errorf("invalid --audio-band '%s' (use LOW:HIGH in Hz, e.g. 40:20000; 0 skips either end; or 'off')", c.AudioBand)
		}
		lo, _ := strconv.Atoi(m[1])
		hi, _ := strconv.Atoi(m[2])
		if hi >= 24000 {
			return nil, fmt.Errorf("--audio-band high end must be below 24000 Hz (the comparison runs at 48 kHz)")
		}
		if hi != 0 && hi <= lo {
			return nil, fmt.Errorf("--audio-band high end must be above the low end")
		}
		var parts []string
		if lo > 0 {
			parts = append(parts, fmt.Sprintf("highpass=f=%d:poles=2", lo), fmt.Sprintf("highpass=f=%d:poles=2", lo))
		}
		if hi > 0 {
			parts = append(parts, fmt.Sprintf("lowpass=f=%d:poles=2", hi))
		}
		c.AudioBandFilter = strings.Join(parts, ",")
	}

	if !reUint.MatchString(o.BRMin) || !reUint.MatchString(o.BRMax) {
		return nil, fmt.Errorf("invalid --br-min/--br-max (integers, kbps)")
	}
	c.BRMin, _ = strconv.Atoi(o.BRMin)
	c.BRMax, _ = strconv.Atoi(o.BRMax)
	if !(c.BRMax > c.BRMin && c.BRMin > 0) {
		return nil, fmt.Errorf("invalid bitrate bounds (need 0 < br-min < br-max)")
	}

	if o.TargetAudio != "" {
		c.AudioTargetRaw = o.TargetAudio
		v, err := strconv.ParseFloat(o.TargetAudio, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid --target-audio '%s'", o.TargetAudio)
		}
		if c.AudioMetric == "visqol" && (v < 1 || v > 5) {
			return nil, fmt.Errorf("--target-audio '%s' is outside 1..5, the MOS-LQO scale --audio-metric visqol uses (dB targets like 30 belong to --audio-metric asisdr/asdr/apsnr)", o.TargetAudio)
		}
		c.TargetAudio = v
	} else {
		switch c.AudioMetric {
		case "visqol":
			c.TargetAudio = 4.4
			c.AudioTargetRaw = "4.4"
		case "apsnr":
			return nil, fmt.Errorf("--target-audio is required with --audio-metric apsnr; --audio-metric asdr/asisdr has a provisional default if you'd rather not calibrate one yet")
		default:
			c.TargetAudio = 30
			c.AudioTargetRaw = "30"
		}
	}

	// --acodec aliases and container validation.
	ac := strings.ToLower(c.ACodec)
	switch ac {
	case "opus":
		ac = "libopus"
	case "vorbis":
		ac = "libvorbis"
	case "pcm", "pcm16", "pcm_s16":
		ac = "pcm_s16le"
	}
	if ac != "" {
		valid := map[string]bool{
			"mp4:auto": true, "mp4:aac": true, "mp4:ac3": true, "mp4:copy": true,
			"mp4:libopus": true, "mp4:pcm_s16le": true, "mp4:libvorbis": true,
			"mkv:auto": true, "mkv:aac": true, "mkv:ac3": true, "mkv:copy": true,
			"mkv:libopus": true, "mkv:pcm_s16le": true, "mkv:libvorbis": true,
			"webm:auto": true, "webm:libopus": true, "webm:copy": true, "webm:libvorbis": true,
		}
		key := c.VidFmt + ":" + ac
		if !valid[key] {
			switch key {
			case "webm:aac", "webm:ac3", "webm:pcm_s16le":
				return nil, fmt.Errorf("--acodec '%s' is not valid in webm (use libopus, libvorbis, auto, or copy when the source audio is opus/vorbis)", ac)
			default:
				return nil, fmt.Errorf("unknown --acodec '%s' (use auto, copy, aac, libopus/opus, libvorbis/vorbis, ac3 or pcm_s16le/pcm)", ac)
			}
		}
	}
	if ac == "" {
		ac = "auto"
	}
	c.ACodec = ac
	if c.VidFmt == "mp4" && c.ACodec == "libopus" {
		c.Warnings = append(c.Warnings, "warning: libopus-in-mp4 muxes on modern ffmpeg but many TVs/hardware players can't decode it; --vid-fmt mkv is the safe home for opus")
	}
	if c.VidFmt == "mp4" && c.ACodec == "libvorbis" {
		c.Warnings = append(c.Warnings, "warning: libvorbis-in-mp4 muxes but essentially no player supports it; --vid-fmt mkv or webm is the safe home for vorbis. (If this run is actually a standalone audio file rather than a video, ignore this -- --vid-fmt doesn't apply there.)")
	}

	if c.ATracks != "all" && c.ATracks != "none" && !reTracks.MatchString(c.ATracks) {
		return nil, fmt.Errorf("invalid --atracks '%s' (use all, none, or comma-separated 0-based indices / language codes)", c.ATracks)
	}
	if c.STracks != "all" && c.STracks != "none" && !reTracks.MatchString(c.STracks) {
		return nil, fmt.Errorf("invalid --stracks '%s' (use all, none, or comma-separated 0-based indices / language codes)", c.STracks)
	}

	switch c.Mode {
	case "image":
		c.NeedImage = true
	case "video":
		c.NeedVideo = true
	case "animate":
		c.NeedImage = true
		c.NeedVid2Anim = true
		c.NeedAnim = true
	default:
		for _, f := range c.Positional {
			switch util.Classify(f) {
			case "image":
				c.NeedImage = true
			case "video":
				c.NeedVideo = true
			case "audio":
				c.NeedAudio = true
			}
		}
	}

	c.QLab = "crf"
	if c.Encoder == "nvenc" {
		c.QLab = "cq"
	}
	c.QPHardMin = 0
	if c.Encoder == "svt" {
		c.QPHardMin = 1
	}
	if o.CRFMin == "" {
		if c.Encoder == "nvenc" {
			c.CRFMin = 0
		} else {
			c.CRFMin = 15
		}
	} else {
		v, err := strconv.Atoi(o.CRFMin)
		if err != nil {
			return nil, fmt.Errorf("invalid %s bounds (%s-min must be an integer)", c.QLab, c.QLab)
		}
		c.CRFMin = v
	}
	if o.CRFMax == "" {
		if c.Encoder == "nvenc" {
			c.CRFMax = 51
		} else {
			c.CRFMax = 63
		}
	} else {
		v, err := strconv.Atoi(o.CRFMax)
		if err != nil {
			return nil, fmt.Errorf("invalid %s bounds (%s-max must be an integer)", c.QLab, c.QLab)
		}
		c.CRFMax = v
	}
	if c.CRFMin < c.QPHardMin || c.CRFMax <= c.CRFMin {
		return nil, fmt.Errorf("invalid %s bounds (%s-min must be >= %d for --encoder %s)", c.QLab, c.QLab, c.QPHardMin, c.Encoder)
	}

	if o.Start != "" {
		v, err := util.ParseTime(o.Start)
		if err != nil {
			return nil, fmt.Errorf("bad --start '%s' (use HH:MM:SS.mmm)", o.Start)
		}
		c.Start, c.HasStart = v, true
		// mins.sh's parse_time echoes three decimals; every later log line,
		// suffix and -ss argument uses that normalised form, not the flag.
		c.StartRaw = fmt.Sprintf("%.3f", v)
	}
	if o.End != "" {
		v, err := util.ParseTime(o.End)
		if err != nil {
			return nil, fmt.Errorf("bad --end '%s' (use HH:MM:SS.mmm)", o.End)
		}
		c.End, c.HasEnd = v, true
		c.EndRaw = fmt.Sprintf("%.3f", v)
	}
	if c.HasStart && c.HasEnd && c.End <= c.Start {
		return nil, fmt.Errorf("--end must be greater than --start")
	}

	if !reUint.MatchString(o.QMin) || !reUint.MatchString(o.QMax) {
		return nil, fmt.Errorf("invalid --q-min/--q-max (integers)")
	}
	c.QMin, _ = strconv.Atoi(o.QMin)
	c.QMax, _ = strconv.Atoi(o.QMax)
	if !(c.QMax > c.QMin && c.QMax <= 100) {
		return nil, fmt.Errorf("invalid quality bounds (need 0 <= q-min < q-max <= 100)")
	}
	if c.ImgBackend == "libjxl" && c.NeedImage && c.QMax < 5 {
		return nil, fmt.Errorf("--q-max must be >= 5 with --img-encoder libjxl (cjxl can't encode below quality 5)")
	}
	if c.WebPMethod, err = parseRangeInt(o.Method, "--method", 0, 6); err != nil {
		return nil, err
	}
	if c.AVIFSpeed, err = parseRangeInt(o.ASpeed, "--aspeed", 0, 8); err != nil {
		return nil, err
	}
	if c.JXLEffort, err = parseRangeInt(o.JXLEffort, "--jxl-effort", 1, 9); err != nil {
		return nil, err
	}
	if !reNum.MatchString(o.AnimFPS) {
		return nil, fmt.Errorf("invalid --anim-fps '%s'", o.AnimFPS)
	}
	c.AnimFPS, _ = strconv.ParseFloat(o.AnimFPS, 64)
	c.AnimFPSRaw = o.AnimFPS
	if c.AnimFPS < 0.001 {
		return nil, fmt.Errorf("--anim-fps must be > 0")
	}
	if o.AnimWidth != "" {
		if !reUint.MatchString(o.AnimWidth) {
			return nil, fmt.Errorf("invalid --anim-width '%s'", o.AnimWidth)
		}
		c.AnimWidth, _ = strconv.Atoi(o.AnimWidth)
		if c.AnimWidth <= 0 {
			return nil, fmt.Errorf("--anim-width must be > 0")
		}
	}

	// Worker default and fbf chunk default.
	if c.Workers == 0 {
		c.Workers = c.NProc
		if c.Workers > 4 {
			c.Workers = 4
		}
		if c.Workers < 1 {
			c.Workers = 1
		}
	}
	if c.FBF && c.FBFChunkSeconds == 0 {
		c.FBFChunkSeconds = 1
	}

	// Applicability warnings (top-level only).
	c.addWarnings(o)

	// --log resolution.
	if c.LogFile == "auto" {
		if len(c.Positional) > 0 {
			c.LogFile = filepath.Join(filepath.Dir(c.Positional[0]), "mins.log")
		}
	}
	if c.VisqolModel == "" {
		if v := os.Getenv("VISQOL_MODEL"); v != "" {
			c.VisqolModel = v
		}
	}
	return c, nil
}

func parsePositiveNum(s, name string) (float64, error) {
	if !reNum.MatchString(s) {
		return 0, fmt.Errorf("invalid %s '%s' (positive number)", name, s)
	}
	v, _ := strconv.ParseFloat(s, 64)
	if v < 0.001 {
		return 0, fmt.Errorf("%s must be > 0", name)
	}
	return v, nil
}

func parsePositiveInt(s, name string, min int) (int, error) {
	if !reUint.MatchString(s) {
		return 0, fmt.Errorf("invalid %s '%s' (positive integer)", name, s)
	}
	v, _ := strconv.Atoi(s)
	if v < min {
		return 0, fmt.Errorf("%s must be >= %d", name, min)
	}
	return v, nil
}

func parseRangeInt(s, name string, lo, hi int) (int, error) {
	if !reUint.MatchString(s) {
		return 0, fmt.Errorf("invalid %s '%s'", name, s)
	}
	v, _ := strconv.Atoi(s)
	if v < lo || v > hi {
		return 0, fmt.Errorf("%s must be %d..%d", name, lo, hi)
	}
	return v, nil
}

func (c *Config) addWarnings(o *cli.Options) {
	if c.NeedImage && !c.NeedVid2Anim && (c.HasStart || c.HasEnd) {
		c.Warnings = append(c.Warnings, "warning: --start/--end apply to video (or --mode animate) only; ignored for image inputs")
	}
	if c.NeedImage && !c.NeedVideo {
		if o.EncoderSet {
			c.Warnings = append(c.Warnings, "warning: --encoder applies to the AV1 video path only; ignored here")
		}
		if o.VidFmtSet {
			c.Warnings = append(c.Warnings, "warning: --vid-fmt applies to the AV1 video path only; ignored here")
		}
		if o.ACodecSet && !c.NeedAudio {
			c.Warnings = append(c.Warnings, "warning: --acodec applies to the AV1 video path only; ignored here")
		}
		if c.ATracks != "all" {
			c.Warnings = append(c.Warnings, "warning: --atracks applies to the AV1 video path only; ignored here")
		}
		if c.STracks != "all" {
			c.Warnings = append(c.Warnings, "warning: --stracks applies to the AV1 video path only; ignored here")
		}
		if c.ImgFmt != "avif" && o.ASpeedSet {
			c.Warnings = append(c.Warnings, "warning: --aspeed applies to avif only; ignored for "+c.ImgFmt)
		}
		if c.ImgFmt != "webp" && o.MethodSet {
			c.Warnings = append(c.Warnings, "warning: --method applies to webp only; ignored for "+c.ImgFmt)
		}
		if c.ImgFmt != "jxl" && o.JXLEffortSet {
			c.Warnings = append(c.Warnings, "warning: --jxl-effort applies to jxl only; ignored for "+c.ImgFmt)
		}
	}
	if !c.NeedVid2Anim && o.AnimWidth != "" {
		c.Warnings = append(c.Warnings, "warning: --anim-width applies to --mode animate only; ignored here")
	}
	if c.FBF && !c.NeedVideo {
		c.Warnings = append(c.Warnings, "warning: --fbf applies to video inputs only; no video inputs in this batch")
	}
	if c.Refine && !c.NeedVideo {
		c.Warnings = append(c.Warnings, "warning: --refine applies to video inputs only; no video inputs in this batch")
	}
	if !c.Refine && o.RefineOptsSet {
		c.Warnings = append(c.Warnings, "warning: --refine-seconds/--refine-min-frames/--refine-step/--refine-boundary-cost only matter together with --refine; ignored here")
	}
	if !c.NeedImage && o.ImgFmtSet {
		c.Warnings = append(c.Warnings, "warning: --img-fmt applies to image inputs only; ignored here")
	}
	if !c.NeedImage && o.ImgEncoderSet {
		c.Warnings = append(c.Warnings, "warning: --img-encoder applies to image inputs only; ignored here")
	}
	if c.NeedImage && c.ImgBackend != "libavif" && !(c.ImgBackend == "auto" && c.ImgFmt == "avif") && o.AvifYUVSet {
		c.Warnings = append(c.Warnings, "warning: --avif-yuv applies to --img-encoder libavif only; ignored here")
	}
	if c.NeedAudio && (c.HasStart || c.HasEnd) {
		c.Warnings = append(c.Warnings, "warning: --start/--end don't apply to standalone audio inputs; ignored here")
	}
	if c.AudioSearch && !c.NeedVideo {
		c.Warnings = append(c.Warnings, "warning: --audio-search applies to video inputs only (standalone audio inputs always search); ignored here")
	}
	if !c.NeedAudio && !(c.AudioSearch && c.NeedVideo) {
		if c.BRMin != 32 || c.BRMax != 256 {
			c.Warnings = append(c.Warnings, "warning: --br-min/--br-max apply to standalone audio inputs (or --audio-search) only; ignored here")
		}
		if c.AudioMetric != "visqol" {
			c.Warnings = append(c.Warnings, "warning: --audio-metric applies to standalone audio inputs (or --audio-search) only; ignored here")
		}
	}
	if (c.NeedAudio || (c.AudioSearch && c.NeedVideo)) && c.AudioMetric == "visqol" {
		if o.AudioBandSet {
			c.Warnings = append(c.Warnings, "warning: --audio-band has no effect with --audio-metric visqol (ViSQOL picks its own band); ignored")
		}
		if c.TargetAudio >= 4.75 {
			c.Warnings = append(c.Warnings, fmt.Sprintf("warning: --target-audio %s is at or above the ~4.75 ceiling of ViSQOL's audio mode; the search will most likely never pass and fall back to --br-max", o.TargetAudio))
		}
	}
	if !c.NeedAudio && c.AudioFmt != "auto" {
		c.Warnings = append(c.Warnings, "warning: --audio-fmt applies to standalone audio inputs only; ignored here")
	}
	if c.Encoder == "nvenc" && c.NeedVideo && c.Workers > 1 {
		c.Warnings = append(c.Warnings, fmt.Sprintf("warning: --instances %d with --encoder nvenc runs that many concurrent av1_nvenc sessions; many consumer GPUs cap concurrent NVENC sessions — extra instances will simply fail if that cap is exceeded", c.Workers))
	}
}
