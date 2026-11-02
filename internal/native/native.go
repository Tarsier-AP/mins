// Package native wraps the bundled libwebp/libavif/libjxl command-line
// encoders and decoders, plus the input preparation ffmpeg performs in place
// of ImageMagick.
package native

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"mins/internal/config"
	"mins/internal/ffmpeg"
	"mins/internal/logx"
	"mins/internal/media"
	"mins/internal/tools"
)

// Backend is one native image backend.
type Backend struct {
	Name  string // libwebp | libavif | libjxl
	Fmt   string // webp | avif | jxl
	C     *config.Config
	R     *ffmpeg.Runner
	Tools *tools.Resolver
	Log   *logx.Logger
}

// New returns the backend named by c.ImgBackend.
func New(c *config.Config, r *ffmpeg.Runner, tr *tools.Resolver, log *logx.Logger) *Backend {
	name := c.ImgBackend
	return &Backend{Name: name, Fmt: map[string]string{"libwebp": "webp", "libavif": "avif", "libjxl": "jxl"}[name], C: c, R: r, Tools: tr, Log: log}
}

// Bins returns every executable the backend needs.
func (b *Backend) Bins() []string {
	switch b.Name {
	case "libwebp":
		return []string{"cwebp", "dwebp", "img2webp"}
	case "libavif":
		return []string{"avifenc", "avifdec"}
	case "libjxl":
		return []string{"cjxl", "djxl"}
	}
	return nil
}

// Missing returns the executables not resolvable.
func (b *Backend) Missing() []string {
	var out []string
	for _, name := range b.Bins() {
		if _, err := b.Tools.Path(name); err != nil {
			out = append(out, name)
		}
	}
	return out
}

// Hint describes where the tools normally come from.
func (b *Backend) Hint() string {
	switch b.Name {
	case "libwebp":
		return "the 'webp' package: cwebp, dwebp, img2webp"
	case "libavif":
		return "the 'libavif-bin' package: avifenc, avifdec"
	case "libjxl":
		return "the 'libjxl-tools' package: cjxl, djxl"
	}
	return ""
}

// Version returns the active encoder's version string.
func (b *Backend) Version() string {
	switch b.Name {
	case "libwebp":
		if p, err := b.Tools.Path("cwebp"); err == nil {
			out, _ := exec.Command(p, "-version").CombinedOutput()
			return "cwebp " + strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
		}
	case "libavif":
		if p, err := b.Tools.Path("avifenc"); err == nil {
			out, _ := exec.Command(p, "--version").CombinedOutput()
			for _, line := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(line, "Version:") {
					f := strings.Fields(line)
					if len(f) > 1 {
						return "avifenc " + f[1]
					}
				}
			}
			return "avifenc"
		}
	case "libjxl":
		if p, err := b.Tools.Path("cjxl"); err == nil {
			out, _ := exec.Command(p, "--version").CombinedOutput()
			return strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
		}
	}
	return b.Name
}

// Encode writes one still (one frame) or one animation.
func (b *Backend) Encode(q int, out string, frames []string, delays []int, elog string) (string, error) {
	if len(frames) == 0 {
		return "", fmt.Errorf("no frames to encode")
	}
	var tool string
	var args []string
	switch b.Name {
	case "libwebp":
		if len(frames) == 1 {
			tool = "cwebp"
			p, err := b.Tools.Path(tool)
			if err != nil {
				return tool, err
			}
			args = []string{"-q", strconv.Itoa(q), "-m", strconv.Itoa(b.C.WebPMethod), "-mt", "-metadata", "icc", frames[0], "-o", out}
			return tool, b.run(p, args, elog)
		}
		tool = "img2webp"
		p, err := b.Tools.Path(tool)
		if err != nil {
			return tool, err
		}
		args = []string{"-loop", "0", "-lossy", "-q", strconv.Itoa(q), "-m", strconv.Itoa(b.C.WebPMethod)}
		if len(delays) != len(frames) {
			return tool, fmt.Errorf("%d frame delays for %d frames", len(delays), len(frames))
		}
		for i, f := range frames {
			args = append(args, "-d", strconv.Itoa(delays[i]), f)
		}
		args = append(args, "-o", out)
		return tool, b.run(p, args, elog)
	case "libavif":
		tool = "avifenc"
		p, err := b.Tools.Path(tool)
		if err != nil {
			return tool, err
		}
		args = []string{"-q", strconv.Itoa(q), "--qalpha", "100", "-s", strconv.Itoa(b.C.AVIFSpeed),
			"-y", b.C.AvifYUV, "-j", strconv.Itoa(b.C.NProc)}
		if len(frames) == 1 {
			args = append(args, frames[0])
		} else {
			if len(delays) != len(frames) {
				return tool, fmt.Errorf("%d frame delays for %d frames", len(delays), len(frames))
			}
			args = append(args, "--timescale", "1000", "--repetition-count", "infinite")
			for i, f := range frames {
				args = append(args, "--duration", strconv.Itoa(delays[i]), f)
			}
		}
		args = append(args, out)
		return tool, b.run(p, args, elog)
	case "libjxl":
		tool = "cjxl"
		p, err := b.Tools.Path(tool)
		if err != nil {
			return tool, err
		}
		src := frames[0]
		apng := ""
		if len(frames) > 1 {
			apng = out + ".apng"
			if err := MakeAPNG(apng, delays, frames); err != nil {
				return "apng", err
			}
			src = apng
			defer os.Remove(apng)
		}
		args = []string{src, out, "-q", strconv.Itoa(q), "-e", strconv.Itoa(b.C.JXLEffort),
			"--num_threads=" + strconv.Itoa(b.C.NProc), "--quiet"}
		return tool, b.run(p, args, elog)
	}
	return "", fmt.Errorf("unknown native backend %q", b.Name)
}

func (b *Backend) run(path string, args []string, elog string) error {
	f, err := os.Create(elog)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.Command(path, args...)
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// DecodeStill decodes a still candidate to PNG.
func (b *Backend) DecodeStill(in, out, elog string) error {
	var name string
	var args []string
	switch b.Name {
	case "libwebp":
		name, args = "dwebp", []string{in, "-o", out}
	case "libavif":
		name, args = "avifdec", []string{"-j", strconv.Itoa(b.C.NProc), "-d", "8", in, out}
	case "libjxl":
		name, args = "djxl", []string{in, out}
	default:
		return fmt.Errorf("unknown native backend %q", b.Name)
	}
	p, err := b.Tools.Path(name)
	if err != nil {
		return err
	}
	return b.run(p, args, elog)
}

// DecodeFrames explodes an encoded animation into dir/f-%06d.png.
func (b *Backend) DecodeFrames(in, dir string, want int, elog string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_ = os.Remove(elog)
	appendLog := func(s string) {
		f, _ := os.OpenFile(elog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if f != nil {
			fmt.Fprint(f, s)
			f.Close()
		}
	}
	count := func() int { return countPNGs(dir) }
	switch b.Name {
	case "libwebp":
		// libwebp's own dumper first: ffmpeg's VP8 decoder produces
		// different RGB, and the gate must see what mins.sh saw.
		if p, err := b.Tools.Path("anim_dump"); err == nil {
			tmp := dir + ".dump"
			_ = os.RemoveAll(tmp)
			_ = os.MkdirAll(tmp, 0o755)
			cmd := exec.Command(p, "-folder", tmp, "-prefix", "f-", in)
			out, derr := cmd.CombinedOutput()
			// anim_dump narrates every frame on success ("Decoding file:
			// ..."); mins.sh only ever surfaced the frame-count mismatch,
			// so keep its chatter out of the gate error tail.
			if derr != nil {
				appendLog(string(out))
			}
			renameDump(tmp, dir)
			_ = os.RemoveAll(tmp)
		}
		if count() != want {
			clearPNGs(dir)
			b.ffmpegDecode(in, dir, appendLog)
		}
	case "libavif":
		b.ffmpegDecode(in, dir, appendLog)
		if count() != want {
			clearPNGs(dir)
			p, err := b.Tools.Path("avifdec")
			if err != nil {
				return err
			}
			for i := 0; i < want; i++ {
				out := filepath.Join(dir, fmt.Sprintf("f-%06d.png", i))
				if err := b.run(p, []string{"-j", strconv.Itoa(b.C.NProc), "-d", "8", "--index", strconv.Itoa(i), in, out}, elog); err != nil {
					return err
				}
			}
		}
	case "libjxl":
		p, err := b.Tools.Path("djxl")
		if err != nil {
			return err
		}
		anim := filepath.Join(dir, "anim.png")
		if err := b.run(p, []string{in, anim}, elog); err != nil {
			return err
		}
		cmd := exec.Command(b.R.FFmpeg, "-hide_banner", "-nostdin", "-y", "-loglevel", "error",
			"-i", anim, "-fps_mode", "passthrough", "-start_number", "0", filepath.Join(dir, "f-%06d.png"))
		out, _ := cmd.CombinedOutput()
		appendLog(string(out))
		_ = os.Remove(anim)
	default:
		return fmt.Errorf("unknown native backend %q", b.Name)
	}
	if got := count(); got != want {
		// mins.sh's nat_decode_frames writes this line into the decode log,
		// which the caller tails on failure.
		appendLog(fmt.Sprintf("mins: decoded %d frame(s), expected %d\n", got, want))
		return fmt.Errorf("decoded %d frame(s), expected %d", got, want)
	}
	return nil
}

func (b *Backend) ffmpegDecode(in, dir string, appendLog func(string)) {
	cmd := exec.Command(b.R.FFmpeg, "-hide_banner", "-nostdin", "-y", "-loglevel", "error",
		"-i", in, "-fps_mode", "passthrough", "-start_number", "0", filepath.Join(dir, "f-%06d.png"))
	out, _ := cmd.CombinedOutput()
	appendLog(string(out))
}

func countPNGs(dir string) int {
	m, _ := filepath.Glob(filepath.Join(dir, "f-*.png"))
	return len(m)
}

func clearPNGs(dir string) {
	m, _ := filepath.Glob(filepath.Join(dir, "f-*.png"))
	for _, f := range m {
		_ = os.Remove(f)
	}
}

func renameDump(src, dst string) {
	m, _ := filepath.Glob(filepath.Join(src, "f-*.png"))
	sort.Strings(m)
	for i, f := range m {
		_ = os.Rename(f, filepath.Join(dst, fmt.Sprintf("f-%06d.png", i)))
	}
}

// PrepareStill converts any supported still input to an 8-bit PNG.
func (b *Backend) PrepareStill(input, out, elog string) error {
	ext := strings.ToLower(filepath.Ext(input))
	if ext == ".heic" || ext == ".heif" {
		if p, err := b.Tools.Path("heif-convert"); err == nil {
			// heif-convert names a multi-image output <out>-1.png; a
			// single image lands exactly on out.
			if err := b.run(p, []string{input, out}, elog); err == nil {
				if _, err := os.Stat(out); err == nil {
					return nil
				}
			}
		}
	}
	// WebP goes through libwebp's own decoder: ffmpeg's native VP8 path
	// produces different RGB (PSNR ~27 against libwebp), which would shift
	// the gate's reference away from what mins.sh measured.
	if ext == ".webp" {
		if p, err := b.Tools.Path("dwebp"); err == nil {
			if err := b.run(p, []string{input, "-o", out}, elog); err == nil {
				if _, err := os.Stat(out); err == nil {
					return nil
				}
			}
		}
	}
	pix := "rgb24"
	if media.HasAlpha(b.R, input) {
		pix = "rgba"
	}
	args := []string{"-y", "-loglevel", "error", "-i", input, "-frames:v", "1",
		"-vf", "format=" + pix, "-f", "image2", "-c:v", "png", out}
	return b.ffmpegCmd(args, elog)
}

// PrepareAnim extracts every frame as a PNG plus a per-frame delay list.
func (b *Backend) PrepareAnim(input, dir string, elog string) ([]string, []int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	// WebP animations use libwebp's dumper for the same reason stills use
	// dwebp: ffmpeg's decode is not pixel-identical to libwebp/magick.
	if strings.EqualFold(filepath.Ext(input), ".webp") {
		if p, err := b.Tools.Path("anim_dump"); err == nil {
			tmp := dir + ".dump"
			_ = os.RemoveAll(tmp)
			_ = os.MkdirAll(tmp, 0o755)
			cmd := exec.Command(p, "-folder", tmp, "-prefix", "f-", input)
			out, _ := cmd.CombinedOutput()
			_ = os.WriteFile(elog, out, 0o644)
			renameDump(tmp, dir)
			_ = os.RemoveAll(tmp)
			if frames, _ := filepath.Glob(filepath.Join(dir, "f-*.png")); len(frames) > 0 {
				sort.Strings(frames)
				return frames, b.frameDelays(input, len(frames)), nil
			}
			clearPNGs(dir)
		}
	}
	args := []string{"-y", "-loglevel", "error", "-i", input, "-fps_mode", "passthrough",
		"-start_number", "0", filepath.Join(dir, "f-%06d.png")}
	if err := b.ffmpegCmd(args, elog); err != nil {
		return nil, nil, err
	}
	frames, _ := filepath.Glob(filepath.Join(dir, "f-*.png"))
	sort.Strings(frames)
	if len(frames) == 0 {
		return nil, nil, fmt.Errorf("no frames extracted")
	}
	return frames, b.frameDelays(input, len(frames)), nil
}

func (b *Backend) frameDelays(input string, n int) []int {
	delays, err := media.FrameDelaysMS(b.R, input)
	if err != nil || len(delays) < n {
		for len(delays) < n {
			delays = append(delays, 100)
		}
	}
	return delays[:n]
}

func (b *Backend) ffmpegCmd(args []string, elog string) error {
	f, err := os.Create(elog)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.Command(b.R.FFmpeg, append([]string{"-hide_banner", "-nostdin"}, args...)...)
	cmd.Stdout = f
	cmd.Stderr = f
	return cmd.Run()
}

// FlattenFile composites an image over mid-gray and writes RGB PNG.
//
// The overlay is forced to blend in RGB: its default yuv420 blend would
// round-trip the whole image through limited-range YUV and shift every pixel,
// which moves the VMAF gate by ~0.1. gray50 is 0x7f7f7f in ImageMagick, and
// mins.sh's gate used exactly that, so match it.
func (b *Backend) FlattenFile(in, out, elog string) error {
	w, h, err := pngSize(in)
	if err != nil {
		// Unknown format: let ffmpeg decide with a plain rgb conversion.
		return b.ffmpegCmd([]string{"-y", "-loglevel", "error", "-i", in, "-frames:v", "1",
			"-vf", "format=rgb24", "-f", "image2", "-c:v", "png", out}, elog)
	}
	filter := "[1:v]format=rgba[bg];[0:v]format=rgba[fg];" +
		"[bg][fg]overlay=shortest=1:format=rgb,format=rgb24"
	args := []string{"-y", "-loglevel", "error", "-i", in, "-f", "lavfi", "-i",
		fmt.Sprintf("color=c=0x7f7f7f:s=%dx%d:d=1", w, h), "-filter_complex", filter,
		"-frames:v", "1", "-f", "image2", "-c:v", "png", out}
	return b.ffmpegCmd(args, elog)
}

// FlattenDir composites every f-*.png in inDir into outDir.
func (b *Backend) FlattenDir(inDir, outDir, elog string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	frames, _ := filepath.Glob(filepath.Join(inDir, "f-*.png"))
	sort.Strings(frames)
	for _, f := range frames {
		dst := filepath.Join(outDir, filepath.Base(f))
		if err := b.FlattenFile(f, dst, elog); err != nil {
			return err
		}
	}
	return nil
}

func pngSize(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// Preflight performs a real encode/decode round trip.
func (b *Backend) Preflight(animated bool, work string) error {
	if miss := b.Missing(); len(miss) > 0 {
		return fmt.Errorf("--img-encoder %s: not found: %s (%s)", b.Name, strings.Join(miss, " "), b.Hint())
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	src := filepath.Join(work, "probe.png")
	if err := b.ffmpegCmd([]string{"-y", "-loglevel", "error", "-f", "lavfi", "-i",
		"gradients=s=64x64:c0=white:c1=black", "-frames:v", "1", src}, filepath.Join(work, "prep.log")); err != nil {
		return fmt.Errorf("%s preflight: could not write a probe PNG: %w", b.Name, err)
	}
	out := filepath.Join(work, "probe."+b.Fmt)
	elog := filepath.Join(work, "enc.log")
	if _, err := b.Encode(50, out, []string{src}, nil, elog); err != nil {
		return fmt.Errorf("%s preflight failed: a still %s could not be written (is it recent enough?): %w", b.Name, b.Fmt, err)
	}
	dec := filepath.Join(work, "probe_dec.png")
	if err := b.DecodeStill(out, dec, filepath.Join(work, "dec.log")); err != nil {
		return fmt.Errorf("%s preflight failed: a still %s could not be read back: %w", b.Name, b.Fmt, err)
	}
	if animated {
		a0 := filepath.Join(work, "a0.png")
		a1 := filepath.Join(work, "a1.png")
		_ = b.ffmpegCmd([]string{"-y", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=red:s=32x32", "-frames:v", "1", a0}, elog)
		_ = b.ffmpegCmd([]string{"-y", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=blue:s=32x32", "-frames:v", "1", a1}, elog)
		aout := filepath.Join(work, "a."+b.Fmt)
		if _, err := b.Encode(50, aout, []string{a0, a1}, []int{100, 100}, elog); err != nil {
			return fmt.Errorf("animated %s preflight failed: a 2-frame %s could not be written: %w", b.Name, b.Fmt, err)
		}
		if err := b.DecodeFrames(aout, filepath.Join(work, "af"), 2, filepath.Join(work, "dec.log")); err != nil {
			return fmt.Errorf("animated %s preflight failed: a 2-frame %s could not be read back as 2 frames: %w", b.Name, b.Fmt, err)
		}
	}
	return nil
}

// MakeAPNG assembles same-sized PNG frames into an APNG with per-frame delays.
func MakeAPNG(out string, delays []int, frames []string) error {
	if len(delays) != len(frames) {
		return fmt.Errorf("apng: %d delays for %d frames", len(delays), len(frames))
	}
	sig := []byte("\x89PNG\r\n\x1a\n")
	var res bytes.Buffer
	res.Write(sig)
	seq := 0
	var ihdr0 []byte
	keep := map[string]bool{"PLTE": true, "tRNS": true, "iCCP": true, "sRGB": true, "gAMA": true, "cHRM": true, "cICP": true}
	for k, path := range frames {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(data[:8], sig) {
			return fmt.Errorf("apng: not a PNG: %s", path)
		}
		chunks := parseChunks(data[8:])
		var ihdr []byte
		var idat []byte
		for _, ch := range chunks {
			if ch.typ == "IHDR" {
				ihdr = ch.data
			}
			if ch.typ == "IDAT" {
				idat = append(idat, ch.data...)
			}
		}
		if ihdr == nil {
			return fmt.Errorf("apng: no IHDR in %s", path)
		}
		w := binary.BigEndian.Uint32(ihdr[0:4])
		h := binary.BigEndian.Uint32(ihdr[4:8])
		if k == 0 {
			ihdr0 = ihdr
			writeChunk(&res, "IHDR", ihdr)
			for _, ch := range chunks {
				if keep[ch.typ] {
					writeChunk(&res, ch.typ, ch.data)
				}
			}
			var actl bytes.Buffer
			binary.Write(&actl, binary.BigEndian, uint32(len(frames)))
			binary.Write(&actl, binary.BigEndian, uint32(0))
			writeChunk(&res, "acTL", actl.Bytes())
		} else if !bytes.Equal(ihdr, ihdr0) {
			return fmt.Errorf("apng: frame %d has a different PNG header than frame 0", k)
		}
		d := delays[k]
		if d < 1 {
			d = 1
		}
		var fctl bytes.Buffer
		binary.Write(&fctl, binary.BigEndian, uint32(seq))
		binary.Write(&fctl, binary.BigEndian, w)
		binary.Write(&fctl, binary.BigEndian, h)
		binary.Write(&fctl, binary.BigEndian, uint32(0))
		binary.Write(&fctl, binary.BigEndian, uint32(0))
		binary.Write(&fctl, binary.BigEndian, uint16(d))
		binary.Write(&fctl, binary.BigEndian, uint16(1000))
		fctl.WriteByte(0)
		fctl.WriteByte(0)
		writeChunk(&res, "fcTL", fctl.Bytes())
		seq++
		if k == 0 {
			writeChunk(&res, "IDAT", idat)
		} else {
			var fdat bytes.Buffer
			binary.Write(&fdat, binary.BigEndian, uint32(seq))
			fdat.Write(idat)
			writeChunk(&res, "fdAT", fdat.Bytes())
			seq++
		}
	}
	writeChunk(&res, "IEND", nil)
	return os.WriteFile(out, res.Bytes(), 0o644)
}

type pngChunk struct {
	typ  string
	data []byte
}

func parseChunks(b []byte) []pngChunk {
	var out []pngChunk
	for len(b) >= 12 {
		n := int(binary.BigEndian.Uint32(b[0:4]))
		if len(b) < 12+n {
			break
		}
		typ := string(b[4:8])
		data := append([]byte(nil), b[8:8+n]...)
		out = append(out, pngChunk{typ, data})
		b = b[12+n:]
	}
	return out
}

func writeChunk(buf *bytes.Buffer, typ string, data []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(data)))
	buf.Write(n[:])
	buf.WriteString(typ)
	buf.Write(data)
	crc := crc32.NewIEEE()
	crc.Write([]byte(typ))
	crc.Write(data)
	var c [4]byte
	binary.BigEndian.PutUint32(c[:], crc.Sum32())
	buf.Write(c[:])
}
