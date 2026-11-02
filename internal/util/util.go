// Package util holds small pure helpers shared across mins.
package util

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseTime converts a [[HH:]MM:]SS.mmm timestamp to seconds. It mirrors
// mins.sh's parse_time: seconds and minutes must be < 60, the hour field
// must be a plain integer, and at most three fields are accepted.
func ParseTime(s string) (float64, error) {
	s = strings.TrimSpace(s)
	p := strings.Split(s, ":")
	if len(p) > 3 || p[len(p)-1] == "" {
		return 0, fmt.Errorf("bad time %q", s)
	}
	secs, err := strconv.ParseFloat(p[len(p)-1], 64)
	if err != nil || secs >= 60 || secs < 0 {
		return 0, fmt.Errorf("bad time %q", s)
	}
	if len(p) >= 2 {
		m, err := strconv.ParseFloat(p[len(p)-2], 64)
		if err != nil || m >= 60 || m < 0 {
			return 0, fmt.Errorf("bad time %q", s)
		}
		secs += m * 60
	}
	if len(p) == 3 {
		if strings.Contains(p[0], ".") {
			return 0, fmt.Errorf("bad time %q", s)
		}
		h, err := strconv.Atoi(p[0])
		if err != nil || h < 0 {
			return 0, fmt.Errorf("bad time %q", s)
		}
		secs += float64(h) * 3600
	}
	return secs, nil
}

// AtLeast reports a >= b. Non-finite values never pass.
func AtLeast(a, b float64) bool {
	return a >= b
}

// HumanSize renders a byte count the way numfmt --to=iec --suffix=B did:
// values below 10 in their unit keep one decimal, larger ones are whole.
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	suffix := "KMGTPE"[exp]
	v := float64(n) / float64(div)
	// GNU numfmt rounds up (ceiling), not to nearest: 15447 bytes is 16KB,
	// not 15KB. One decimal below 10, whole numbers at or above it.
	r := math.Ceil(v*10) / 10
	var s string
	if r < 10 {
		s = fmt.Sprintf("%.1f", r)
	} else {
		s = fmt.Sprintf("%.0f", math.Ceil(v))
	}
	if dec := decimalPoint(); dec != "." {
		s = strings.Replace(s, ".", dec, 1)
	}
	return s + string(suffix) + "B"
}

// decimalPoint returns the decimal separator GNU numfmt would use under the
// current locale. mins.sh sized files with `numfmt --to=iec`, so the log and
// summary show e.g. "3,5KB" on a ru_RU/de_DE system. Go has no locale support,
// so this approximates it from LC_ALL/LC_NUMERIC/LANG.
func decimalPoint() string {
	loc := ""
	for _, k := range []string{"LC_ALL", "LC_NUMERIC", "LANG"} {
		if v := os.Getenv(k); v != "" {
			loc = v
			break
		}
	}
	if loc == "" || loc == "C" || loc == "POSIX" {
		return "."
	}
	lang := strings.ToLower(loc)
	if i := strings.IndexAny(lang, "_.@"); i >= 0 {
		lang = lang[:i]
	}
	switch lang {
	case "af", "az", "be", "bg", "bs", "ca", "cs", "da", "de", "el", "es",
		"et", "eu", "fi", "fr", "hr", "hu", "hy", "id", "is", "it", "ka",
		"kk", "lt", "lv", "mk", "mn", "nb", "nl", "nn", "pl", "pt", "ro",
		"ru", "sk", "sl", "sq", "sr", "sv", "tr", "uk", "uz", "vi":
		return ","
	}
	return "."
}

// Float renders a float the way Python's str()/print() does for the values
// mins deals with: shortest round-trip form, but always with a decimal point
// for integral values (95 -> "95.0").
func Float(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// ExtKind classifies an extension (without the dot, any case).
func ExtKind(ext string) string {
	switch strings.ToLower(ext) {
	case "jpg", "jpeg", "jpe", "png", "gif", "tif", "tiff", "bmp", "webp", "avif", "jxl", "heic", "heif",
		"cr2", "cr3", "nef", "nrw", "arw", "dng", "rw2", "orf", "raf", "srw":
		return "image"
	case "srt", "ass", "ssa", "vtt", "sub", "idx", "sup", "smi", "sami":
		return "subtitle"
	case "mp3", "flac", "wav", "wave", "aiff", "aif", "aifc", "m4a", "aac", "ogg", "oga", "opus", "wma", "ape", "wv", "mka", "alac":
		return "audio"
	case "mp4", "m4v", "mkv", "webm", "mov", "avi", "wmv", "flv", "ts", "m2ts", "mts", "mpg", "mpeg", "3gp", "ogv", "vob", "mxf":
		return "video"
	}
	return ""
}

// StripURLSuffix removes a trailing ?query/#fragment from the final path
// component when a known extension sits before it.
func StripURLSuffix(p string) string {
	dir, b := filepath.Split(p)
	if !strings.Contains(b, ".") || !strings.ContainsAny(b, "?#") {
		return p
	}
	// If what follows the last dot is itself a known extension, leave it.
	if ExtKind(extOf(b)) != "" {
		return p
	}
	pre := b
	if i := strings.IndexAny(pre, "?#"); i >= 0 {
		pre = pre[:i]
	}
	if strings.Contains(pre, ".") && ExtKind(extOf(pre)) != "" {
		b = pre
	}
	return dir + b
}

func extOf(s string) string {
	i := strings.LastIndex(s, ".")
	if i < 0 {
		return ""
	}
	return s[i+1:]
}

// InStem returns the bare name without directory, URL suffix, or extension.
func InStem(p string) string {
	b := filepath.Base(StripURLSuffix(p))
	if i := strings.LastIndex(b, "."); i > 0 {
		return b[:i]
	}
	return b
}

// InExt returns the extension with any URL suffix ignored.
func InExt(p string) string {
	s := StripURLSuffix(p)
	i := strings.LastIndex(s, ".")
	if i < 0 || i < strings.LastIndexAny(s, string(filepath.Separator)) {
		return ""
	}
	return s[i+1:]
}

// Classify maps a path to image, video, audio or subtitle.
func Classify(p string) string {
	if k := ExtKind(InExt(p)); k != "" {
		return k
	}
	return "video"
}

// SafeTag turns a stem into the character set mins.sh used for log names.
func SafeTag(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// FormatFloat renders a float with a fixed number of decimals, avoiding
// exponent notation for the values mins deals with.
func FormatFloat(v float64, prec int) string {
	return strconv.FormatFloat(v, 'f', prec, 64)
}
