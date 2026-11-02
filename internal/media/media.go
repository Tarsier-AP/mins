// Package media probes inputs with ffprobe and exposes the facts every
// processing path needs.
package media

import (
	"fmt"
	"strconv"
	"strings"

	"mins/internal/ffmpeg"
)

// VideoInfo describes a video (or animated image) stream.
type VideoInfo struct {
	Width, Height  int
	FPSNum, FPSDen int
	FPS            float64
	Duration       float64
	AudioTracks    int
	SubTracks      int
	HasVideo       bool
	NBFFrames      int // container-declared frame count, 0 if unknown
}

// ProbeVideo gathers the core video metadata.
func ProbeVideo(r *ffmpeg.Runner, input string) (*VideoInfo, error) {
	v := &VideoInfo{}
	res, err := r.Probe("-select_streams", "V:0", "-show_entries", "stream=width,height,r_frame_rate,nb_frames", "-of", "default=nw=1:nk=1", input)
	if err != nil {
		return nil, err
	}
	lines := nonEmptyLines(res)
	if len(lines) >= 2 {
		v.Width, _ = strconv.Atoi(lines[0])
		v.Height, _ = strconv.Atoi(lines[1])
	}
	if len(lines) >= 3 {
		fmt.Sscanf(lines[2], "%d/%d", &v.FPSNum, &v.FPSDen)
	}
	if v.FPSDen > 0 {
		v.FPS = float64(v.FPSNum) / float64(v.FPSDen)
	}
	if len(lines) >= 4 {
		v.NBFFrames, _ = strconv.Atoi(lines[3])
	}
	if v.Width <= 0 || v.Height <= 0 {
		return nil, fmt.Errorf("no readable video stream")
	}
	v.HasVideo = true
	if d, err := Duration(r, input); err == nil {
		v.Duration = d
	}
	if n, err := CountStreams(r, input, "a"); err == nil {
		v.AudioTracks = n
	}
	if n, err := CountStreams(r, input, "s"); err == nil {
		v.SubTracks = n
	}
	return v, nil
}

// Duration returns format duration in seconds.
func Duration(r *ffmpeg.Runner, input string) (float64, error) {
	out, err := r.Probe("-show_entries", "format=duration", "-of", "csv=p=0", input)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(out), 64)
}

// DurationRaw returns ffprobe's duration text unchanged (mins.sh kept it
// verbatim for trim reports and clamping).
func DurationRaw(r *ffmpeg.Runner, input string) (string, error) {
	out, err := r.Probe("-show_entries", "format=duration", "-of", "csv=p=0", input)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CountStreams counts streams of a type (a, s, v).
func CountStreams(r *ffmpeg.Runner, input, kind string) (int, error) {
	out, err := r.Probe("-select_streams", kind, "-show_entries", "stream=index", "-of", "csv=p=0", input)
	if err != nil {
		return 0, err
	}
	return len(nonEmptyLines(out)), nil
}

// AudioTrack describes one audio stream.
type AudioTrack struct {
	Index int
	Lang  string
	Codec string
}

// AudioTracks lists audio streams with language and codec.
func AudioTracks(r *ffmpeg.Runner, input string) ([]AudioTrack, error) {
	out, err := r.Probe("-select_streams", "a", "-show_entries", "stream=index:stream_tags=language", "-of", "csv=p=0", input)
	if err != nil {
		return nil, err
	}
	var tracks []AudioTrack
	for _, line := range nonEmptyLines(out) {
		parts := strings.Split(line, ",")
		t := AudioTrack{Lang: "und"}
		if len(parts) > 1 {
			t.Lang = parts[1]
		}
		t.Index = len(tracks)
		tracks = append(tracks, t)
	}
	return tracks, nil
}

// AudioCodecs returns codec_name for each audio stream in order.
func AudioCodecs(r *ffmpeg.Runner, input string) ([]string, error) {
	out, err := r.Probe("-select_streams", "a", "-show_entries", "stream=codec_name", "-of", "csv=p=0", input)
	if err != nil {
		return nil, err
	}
	var codecs []string
	for _, line := range nonEmptyLines(out) {
		codecs = append(codecs, strings.Split(line, ",")[0])
	}
	return codecs, nil
}

// SubTrack describes one subtitle stream.
type SubTrack struct {
	Index int
	Lang  string
	Codec string
}

// SubTracks lists subtitle streams.
func SubTracks(r *ffmpeg.Runner, input string) ([]SubTrack, error) {
	out, err := r.Probe("-select_streams", "s", "-show_entries", "stream=index,codec_name:stream_tags=language", "-of", "csv=p=0", input)
	if err != nil {
		return nil, err
	}
	var tracks []SubTrack
	for _, line := range nonEmptyLines(out) {
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		t := SubTrack{Codec: parts[1], Lang: "und", Index: len(tracks)}
		if len(parts) > 2 {
			t.Lang = parts[2]
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}

// FrameCount returns the number of decoded frames of the first video stream.
func FrameCount(r *ffmpeg.Runner, input string) (int, error) {
	out, err := r.Probe("-select_streams", "V:0", "-count_frames", "-show_entries", "stream=nb_read_frames", "-of", "csv=p=0", input)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

// PacketCount returns nb_read_packets for the first video stream.
func PacketCount(r *ffmpeg.Runner, input string) (int, error) {
	out, err := r.Probe("-select_streams", "V:0", "-count_packets", "-show_entries", "stream=nb_read_packets", "-of", "csv=p=0", input)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

// FrameDelaysMS returns per-frame display durations in milliseconds. Missing
// durations fall back to the nominal frame rate.
func FrameDelaysMS(r *ffmpeg.Runner, input string) ([]int, error) {
	out, err := r.Probe("-select_streams", "V:0", "-show_entries", "frame=pkt_duration_time", "-of", "csv=p=0", input)
	if err != nil {
		return nil, err
	}
	info, _ := ProbeVideo(r, input)
	fallback := 100.0
	if info != nil && info.FPS > 0 {
		fallback = 1000 / info.FPS
	}
	var delays []int
	for _, line := range nonEmptyLines(out) {
		v, err := strconv.ParseFloat(line, 64)
		if err != nil || v <= 0 {
			v = fallback / 1000
		}
		ms := int(v*1000 + 0.5)
		if ms <= 0 {
			ms = 100
		}
		delays = append(delays, ms)
	}
	return delays, nil
}

// ImageInfo describes a still or animated image input.
type ImageInfo struct {
	Width, Height int
	Frames        int
	DelaysMS      []int
	Animated      bool
}

// ProbeImage gathers dimensions, frame count and delays.
func ProbeImage(r *ffmpeg.Runner, input string) (*ImageInfo, error) {
	res, err := r.Probe("-select_streams", "V:0", "-show_entries", "stream=width,height", "-of", "csv=p=0", input)
	if err != nil {
		return nil, err
	}
	lines := nonEmptyLines(res)
	if len(lines) == 0 {
		return nil, fmt.Errorf("no image stream")
	}
	parts := strings.Split(lines[0], ",")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid image dimensions")
	}
	ii := &ImageInfo{}
	ii.Width, _ = strconv.Atoi(parts[0])
	ii.Height, _ = strconv.Atoi(parts[1])
	if n, err := FrameCount(r, input); err == nil && n > 0 {
		ii.Frames = n
	} else {
		ii.Frames = 1
	}
	ii.Animated = ii.Frames > 1
	if ii.Animated {
		if d, err := FrameDelaysMS(r, input); err == nil && len(d) > 0 {
			ii.DelaysMS = d
		}
	}
	return ii, nil
}

// HasAlpha reports whether a PNG pix_fmt carries an alpha channel.
func HasAlpha(r *ffmpeg.Runner, input string) bool {
	out, err := r.Probe("-select_streams", "V:0", "-show_entries", "stream=pix_fmt", "-of", "csv=p=0", input)
	if err != nil {
		return false
	}
	pf := strings.TrimSpace(out)
	return strings.Contains(pf, "a") && (strings.HasPrefix(pf, "rgba") || strings.HasPrefix(pf, "yuva") ||
		strings.HasPrefix(pf, "gbrap") || strings.HasPrefix(pf, "pal8") || strings.HasPrefix(pf, "ya"))
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
