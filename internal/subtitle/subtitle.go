// Package subtitle applies --stracks and the per-container subtitle policy.
package subtitle

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"mins/internal/config"
	"mins/internal/ffmpeg"
	"mins/internal/logx"
	"mins/internal/media"
)

// Selection is the resolved subtitle track set.
type Selection struct {
	SMAP    []string
	SARGS   []string
	SKeep   int
	SDrop   int
	Desc    string
	HasSubs int
	Policy  string
}

// Kind classifies a subtitle codec.
func Kind(codec string) string {
	switch codec {
	case "subrip", "srt", "ass", "ssa", "webvtt", "mov_text", "text", "sami", "microdvd",
		"subviewer", "subviewer1", "vplayer", "realtext", "stl", "pjs", "jacosub", "mpsub", "tedcaptions":
		return "text"
	case "dvd_subtitle", "dvb_subtitle", "hdmv_pgs_subtitle", "xsub", "dvb_teletext":
		return "bitmap"
	}
	return "other"
}

// Resolve applies --stracks to input.
func Resolve(r *ffmpeg.Runner, log *logx.Logger, c *config.Config, input string) (*Selection, error) {
	sel := &Selection{}
	switch c.VidFmt {
	case "mp4":
		sel.Policy = "mov_text"
	case "webm":
		sel.Policy = "webvtt"
	default:
		sel.Policy = "copy"
	}
	tracks, err := media.SubTracks(r, input)
	if err != nil {
		return nil, err
	}
	n := len(tracks)
	sel.HasSubs = n
	if c.STracks == "none" {
		return sel, nil
	}
	var keep []int
	if c.STracks == "all" {
		for i := 0; i < n; i++ {
			keep = append(keep, i)
		}
	} else {
		avail := ""
		for i, t := range tracks {
			if i > 0 {
				avail += " "
			}
			avail += fmt.Sprintf("%d:%s", i, t.Lang)
		}
		for _, tok := range strings.Split(c.STracks, ",") {
			if tok == "" {
				continue
			}
			if idx, err := strconv.Atoi(tok); err == nil {
				if idx >= n {
					return nil, fmt.Errorf("subtitle track %d out of range in %s (available: %s)", idx, filepath.Base(input), noneIfEmpty(avail))
				}
				keep = append(keep, idx)
				continue
			}
			found := false
			for i, t := range tracks {
				if strings.EqualFold(t.Lang, tok) {
					keep = append(keep, i)
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("--stracks: '%s' matches no subtitle stream in %s (available: %s)", tok, filepath.Base(input), noneIfEmpty(avail))
			}
		}
	}
	seen := map[int]bool{}
	outIdx := 0
	for _, k := range keep {
		if seen[k] {
			continue
		}
		seen[k] = true
		t := tracks[k]
		kind := Kind(t.Codec)
		if kind == "text" && c.VidFmt != "mkv" && (t.Codec == "ass" || t.Codec == "ssa") {
			log.Info("warning: subtitle #%d (%s) is %s; converting for %s drops styling (use --vid-fmt mkv to keep it verbatim)", k, t.Lang, t.Codec, c.VidFmt)
		}
		switch {
		case c.VidFmt == "mkv":
			sel.SMAP = append(sel.SMAP, "-map", fmt.Sprintf("0:s:%d", k))
			sel.SARGS = append(sel.SARGS, fmt.Sprintf("-c:s:%d", outIdx), "copy")
			sel.Desc += comma(sel.Desc) + fmt.Sprintf("%d:%s", k, t.Lang)
			outIdx++
		case c.VidFmt == "mp4" && kind == "text":
			sel.SMAP = append(sel.SMAP, "-map", fmt.Sprintf("0:s:%d", k))
			if t.Codec == "mov_text" {
				sel.SARGS = append(sel.SARGS, fmt.Sprintf("-c:s:%d", outIdx), "copy")
			} else {
				sel.SARGS = append(sel.SARGS, fmt.Sprintf("-c:s:%d", outIdx), "mov_text")
			}
			sel.Desc += comma(sel.Desc) + fmt.Sprintf("%d:%s", k, t.Lang)
			outIdx++
		case c.VidFmt == "webm" && kind == "text":
			sel.SMAP = append(sel.SMAP, "-map", fmt.Sprintf("0:s:%d", k))
			if t.Codec == "webvtt" {
				sel.SARGS = append(sel.SARGS, fmt.Sprintf("-c:s:%d", outIdx), "copy")
			} else {
				sel.SARGS = append(sel.SARGS, fmt.Sprintf("-c:s:%d", outIdx), "webvtt")
			}
			sel.Desc += comma(sel.Desc) + fmt.Sprintf("%d:%s", k, t.Lang)
			outIdx++
		default:
			sel.SDrop++
			log.Info("warning: dropping subtitle #%d (%s, %s) — %s cannot store it; use --vid-fmt mkv to keep it", k, t.Lang, t.Codec, c.VidFmt)
		}
	}
	sel.SKeep = outIdx
	if sel.SKeep > 0 && c.VidFmt == "mkv" {
		sel.SMAP = append(sel.SMAP, "-map", "0:t?")
		sel.SARGS = append(sel.SARGS, "-c:t", "copy")
	}
	return sel, nil
}

func comma(s string) string {
	if s == "" {
		return ""
	}
	return ","
}

func noneIfEmpty(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
