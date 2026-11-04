// Package cli parses the drop-in mins.sh command line.
package cli

import (
	"fmt"
	"strings"
)

// Version is the program version; Parity names the shell version this
// implementation matches.
const (
	Version = "2.0.0"
	Parity  = "mins.sh 1.17.0"
)

// Options is the raw command line before validation/normalisation.
type Options struct {
	Mode               string
	ModeSet            bool
	Encoder            string
	EncoderSet         bool
	VidFmt             string
	VidFmtSet          bool
	ACodec             string
	ACodecSet          bool
	ImgFmt             string
	ImgFmtSet          bool
	ImgEncoder         string
	ImgEncoderSet      bool
	AvifYUV            string
	AvifYUVSet         bool
	CRFMin             string
	CRFMax             string
	QMin               string
	QMax               string
	QAuto              bool
	BRMin              string
	BRMax              string
	AudioMetric        string
	TargetAudio        string
	AudioSearch        bool
	AudioBand          string
	AudioBandSet       bool
	VisqolModel        string
	AudioFmt           string
	Method             string
	MethodSet          bool
	ASpeed             string
	ASpeedSet          bool
	JXLEffort          string
	JXLEffortSet       bool
	AnimFPS            string
	AnimWidth          string
	Target             string
	Start              string
	End                string
	StartSet           bool
	EndSet             bool
	ATracks            string
	STracks            string
	FBF                bool
	Workers            string
	WorkersExplicit    bool
	FBFChunk           string
	NoTUI              bool
	NoJSON             bool
	Refine             bool
	RefineSeconds      string
	RefineMinFrames    string
	RefineStep         string
	RefineBoundaryCost string
	RefineOptsSet      bool
	SkipExisting       bool
	Log                string
	LogSet             bool
	Positional         []string
	Args               []string
}

// Parse reads argv (without the program name). It returns an error for a
// missing flag value; unknown tokens become positional inputs, matching
// mins.sh's catch-all case.
func Parse(argv []string) (*Options, error) {
	o := &Options{
		Mode: "auto", Encoder: "svt", VidFmt: "mp4", ImgFmt: "webp",
		ImgEncoder: "auto", AvifYUV: "420", QMin: "0", QMax: "95",
		AudioMetric: "visqol",
		AudioBand:   "40:20000", AudioFmt: "auto", Method: "6", ASpeed: "0",
		JXLEffort: "9", AnimFPS: "10", Target: "95", ATracks: "all",
		STracks: "all", BRMin: "32", BRMax: "256", RefineSeconds: "2",
		RefineMinFrames: "1", RefineStep: "2", RefineBoundaryCost: "0",
	}
	o.Args = append([]string(nil), argv...)

	need := func(i int, name string) (string, error) {
		if i+1 >= len(argv) {
			return "", fmt.Errorf("%s requires a value", name)
		}
		return argv[i+1], nil
	}

	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch a {
		case "--mode":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.Mode, o.ModeSet = v, true
			i++
		case "--encoder":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.Encoder, o.EncoderSet = v, true
			i++
		case "--vid-fmt":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.VidFmt, o.VidFmtSet = v, true
			i++
		case "--acodec":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.ACodec, o.ACodecSet = v, true
			i++
		case "--img-fmt":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.ImgFmt, o.ImgFmtSet = v, true
			i++
		case "--img-encoder", "--img-backend":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.ImgEncoder, o.ImgEncoderSet = v, true
			i++
		case "--avif-yuv":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.AvifYUV, o.AvifYUVSet = v, true
			i++
		case "--crf-min", "--cq-min":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.CRFMin = v
			i++
		case "--crf-max", "--cq-max":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.CRFMax = v
			i++
		case "--q-min":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.QMin = v
			i++
		case "--q-max":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.QMax = v
			i++
		case "--q-auto":
			o.QAuto = true
		case "--br-min":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.BRMin = v
			i++
		case "--br-max":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.BRMax = v
			i++
		case "--audio-metric":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.AudioMetric = v
			i++
		case "--target-audio":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.TargetAudio = v
			i++
		case "--audio-search":
			o.AudioSearch = true
		case "--audio-band":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.AudioBand, o.AudioBandSet = v, true
			i++
		case "--visqol-model":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.VisqolModel = v
			i++
		case "--audio-fmt", "--audio-container":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.AudioFmt = v
			i++
		case "--method":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.Method, o.MethodSet = v, true
			i++
		case "--aspeed":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.ASpeed, o.ASpeedSet = v, true
			i++
		case "--jxl-effort":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.JXLEffort, o.JXLEffortSet = v, true
			i++
		case "--anim-fps":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.AnimFPS = v
			i++
		case "--anim-width":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.AnimWidth = v
			i++
		case "--target":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.Target = v
			i++
		case "--start":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.Start, o.StartSet = v, true
			i++
		case "--end":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.End, o.EndSet = v, true
			i++
		case "--atracks", "--audio":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.ATracks = v
			i++
		case "--stracks", "--subs", "--subtitles":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.STracks = v
			i++
		case "--fbf", "--frame-by-frame":
			o.FBF = true
		case "--instances", "--fbf-workers":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.Workers, o.WorkersExplicit = v, true
			i++
		case "--fbf-chunk-seconds", "--fbf-chunk":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.FBFChunk = v
			i++
		case "--no-tui":
			o.NoTUI = true
		case "--no-json", "--no-report":
			o.NoJSON = true
		case "--refine", "--qsplit":
			o.Refine = true
		case "--refine-seconds":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.RefineSeconds, o.RefineOptsSet = v, true
			i++
		case "--refine-min-frames":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.RefineMinFrames, o.RefineOptsSet = v, true
			i++
		case "--refine-step":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.RefineStep, o.RefineOptsSet = v, true
			i++
		case "--refine-boundary-cost":
			v, err := need(i, a)
			if err != nil {
				return nil, err
			}
			o.RefineBoundaryCost, o.RefineOptsSet = v, true
			i++
		case "--skip-existing":
			o.SkipExisting = true
		case "--log":
			if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
				o.Log = argv[i+1]
				i++
			} else {
				o.Log = "auto"
			}
			o.LogSet = true
		case "-h", "--help":
			o.Log = "\x00help"
		case "-v", "--version":
			o.Log = "\x00version"
		default:
			o.Positional = append(o.Positional, a)
		}
	}
	return o, nil
}

// Help returns the --help text.
func Help() string {
	return `mins ` + Version + `

Usage: mins <input>... [options]

Mixed batch: video files -> <name>.av1.<vid-fmt>, images -> <name>.webp,
<name>.avif or <name>.jxl via --img-fmt (an input already carrying the target
extension becomes <name>.min.<ext>). Both paths gate on mean VMAF >= target:
videos search the highest CRF/CQ that passes, images the lowest quality that
passes.

All media tools are bundled inside this executable and extracted to a
per-user cache on first run, so no system FFmpeg, ImageMagick or codec
package is required. ImageMagick is deliberately not used: images are read
by the bundled FFmpeg (and libheif for HEIC) and written by the reference
encoders -- libwebp (cwebp/img2webp), libavif (avifenc) and libjxl (cjxl).
The matching native decoder (dwebp, avifdec, djxl) reads candidates back for
the VMAF gate. --img-encoder magick is therefore rejected.

  --mode           auto (default; by extension), video, image, or animate
                   (animate: video input -> animated webp/avif/jxl)
  --encoder        video: libsvtav1 (default), libaom-av1, or nvenc (av1_nvenc)
  --vid-fmt        video: mp4 (default), mkv, or webm container.
  --acodec         video: auto (default), copy, aac, libopus (alias opus),
                   libvorbis (alias vorbis), ac3, or pcm_s16le (alias pcm)
  --img-fmt        image / animate: webp (default), avif, or jxl
  --img-encoder    image / animate: auto (default), libwebp, libavif or libjxl.
                   Aliases: cwebp, avifenc, cjxl. A native choice implies its
                   format and rejects a contradicting --img-fmt. ImageMagick
                   (magick/imagemagick) is not supported.
  --avif-yuv FMT   image / animate (libavif): 420 (default), 422, 444 or 400
  --crf-min        video: lower CRF/CQ bound (default: 15 svt/aom, 0 nvenc)
  --crf-max        video: upper CRF/CQ bound (default: 63 svt/aom, 51 nvenc)
  --cq-min         alias of --crf-min
  --cq-max         alias of --crf-max
  --q-min          image / animate: lower quality bound (default: 0)
  --q-max          image / animate: upper quality bound (default: 95)
  --q-auto         image / animate: adapt the binary search window across
                   inputs. The first input starts at the midpoint of the full
                   range; later inputs probe the shared q-min first (raised by
                   at most 5 per passing input). The upper bound keeps the
                   largest passing quality and grows in steps of 5, up to 100,
                   when nothing passes. The lowest passing q is always used.
                   Works for a single input.
  --audio-band     audio / --audio-search: (asisdr/asdr/apsnr only) LOW:HIGH Hz
  --audio-search   video: search each kept audio track's bitrate
  --br-min N       audio / --audio-search: lower bitrate bound, kbps (32)
  --br-max N       audio / --audio-search: upper bitrate bound, kbps (256)
  --audio-metric   audio: visqol (default), asisdr, asdr, or apsnr
  --visqol-model PATH
                   visqol: similarity-to-quality SVR model (bundled by default)
  --target-audio N audio quality gate (visqol default 4.4 MOS-LQO; asdr/asisdr
                   default 30 dB; apsnr requires it)
  --audio-fmt      audio: auto (default), ogg, mp4, mov, mka (alias mkv), webm,
                   or wav
  --method N       image / animate (webp): libwebp effort 0-6 (default 6)
  --aspeed N       image / animate (avif): aom speed 0-8 (default 0)
  --jxl-effort N   image / animate (jxl): libjxl effort 1-9 (default 9)
  --anim-fps N     animate: frames per second pulled from the source (10)
  --anim-width N   animate: scale extracted frames to width N
  --target N       VMAF gate: mean must be >= N (default: 95)
  --start TIME     video / animate: trim window start, [[HH:]MM:]SS.mmm
  --end TIME       video / animate: trim window end (exclusive)
  --atracks SPEC   video: audio tracks to keep: all, none, or 0-based
                   indices / ISO language codes (alias: --audio)
  --stracks SPEC   video: subtitle tracks to keep: all, none, or indices /
                   language codes (alias: --subs)
  --fbf            video: frame-by-frame VMAF gate (alias: --frame-by-frame)
  --instances N    fbf / batch: parallel workers (default: min(nproc,4))
  --fbf-chunk-seconds N
                   fbf: seconds of frames kept on disk at a time (default: 1)
  --no-tui         disable the live dashboard
  --refine         video: repair fragments below target (alias: --qsplit)
  --refine-seconds N
                   refine: longest fragment, seconds (default: 2)
  --refine-min-frames N
                   refine: shortest fragment, frames (default: 1)
  --refine-step N  refine: CRF/CQ lowered by N per attempt (default: 2)
  --refine-boundary-cost N
                   refine: cost of an extra fragment boundary, frames (0)
  --no-json        don't write JSON reports (alias: --no-report)
  --skip-existing  skip files whose computed output already exists
  --log FILE       append a run log to FILE; 'auto' -> <first input's dir>/mins.log
  -v, --version    print version and exit
  -h, --help       print this help and exit
`
}
