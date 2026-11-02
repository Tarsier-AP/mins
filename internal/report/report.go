// Package report writes the JSON reports mins.sh produced.
package report

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

// F is a float that marshals like Python's json.dump does: integral values
// keep a trailing .0 (95 -> 95.0), matching mins.sh's reports.
type F float64

// MarshalJSON implements json.Marshaler.
func (f F) MarshalJSON() ([]byte, error) {
	s := strconv.FormatFloat(float64(f), 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return []byte(s), nil
}

// Trim is the trim block shared by video-shaped reports.
type Trim struct {
	Start    string `json:"start"`
	End      string `json:"end"`
	Duration string `json:"duration"`
}

// AudioInfo is the audio block of a video report.
type AudioInfo struct {
	Codec    string `json:"codec"`
	Bitrate  string `json:"bitrate"`
	Tracks   int    `json:"tracks"`
	Kept     int    `json:"kept"`
	Select   string `json:"select"`
	Resolved string `json:"resolved"`
}

// SubInfo is the subtitles block of a video report.
type SubInfo struct {
	Policy   string `json:"policy"`
	Select   string `json:"select"`
	Streams  int    `json:"streams"`
	Kept     int    `json:"kept"`
	Dropped  int    `json:"dropped"`
	Resolved string `json:"resolved"`
}

// Video is the <name>.vmaf.json schema.
type Video struct {
	Input       string          `json:"input"`
	Output      string          `json:"output"`
	Mode        string          `json:"mode"`
	Encoder     string          `json:"encoder"`
	Container   string          `json:"container"`
	TargetVMAF  F               `json:"target_vmaf"`
	MeanVMAF    F               `json:"mean_vmaf"`
	Passed      bool            `json:"passed"`
	CRF         *int            `json:"crf,omitempty"`
	CQ          *int            `json:"cq,omitempty"`
	Trim        Trim            `json:"trim"`
	TierQuality json.RawMessage `json:"tier_quality_settings"`
	PixFmt      string          `json:"pix_fmt"`
	Audio       AudioInfo       `json:"audio"`
	Subtitles   SubInfo         `json:"subtitles"`
	SizeBytes   int64           `json:"size_bytes"`
	Refine      *Refine         `json:"refine,omitempty"`
	Refined     *bool           `json:"refined,omitempty"`
}

// Image is the still-image report schema.
type Image struct {
	Input      string `json:"input"`
	Output     string `json:"output"`
	Mode       string `json:"mode"`
	Encoder    string `json:"encoder"`
	Quality    int    `json:"quality"`
	WebPMethod *int   `json:"webp_method,omitempty"`
	AvifSpeed  *int   `json:"avif_speed,omitempty"`
	AvifYUV    string `json:"avif_yuv,omitempty"`
	JXLEffort  *int   `json:"jxl_effort,omitempty"`
	TargetVMAF F      `json:"target_vmaf"`
	MeanVMAF   F      `json:"mean_vmaf"`
	Passed     bool   `json:"passed"`
	Dimensions string `json:"dimensions"`
	Animated   bool   `json:"animated"`
	Frames     int    `json:"frames"`
	SizeBytes  int64  `json:"size_bytes"`
}

// VideoToImage is the --mode animate report schema.
type VideoToImage struct {
	Input            string `json:"input"`
	Output           string `json:"output"`
	Mode             string `json:"mode"`
	Encoder          string `json:"encoder"`
	Quality          int    `json:"quality"`
	WebPMethod       *int   `json:"webp_method,omitempty"`
	AvifSpeed        *int   `json:"avif_speed,omitempty"`
	AvifYUV          string `json:"avif_yuv,omitempty"`
	JXLEffort        *int   `json:"jxl_effort,omitempty"`
	TargetVMAF       F      `json:"target_vmaf"`
	MeanVMAF         F      `json:"mean_vmaf"`
	Passed           bool   `json:"passed"`
	SourceDimensions string `json:"source_dimensions"`
	AnimFPS          F      `json:"anim_fps"`
	AnimWidth        *int   `json:"anim_width"`
	Animated         bool   `json:"animated"`
	Frames           int    `json:"frames"`
	Trim             Trim   `json:"trim"`
	SizeBytes        int64  `json:"size_bytes"`
}

// AudioReport is the <name>.audio.json schema.
type AudioReport struct {
	Input       string  `json:"input"`
	Output      string  `json:"output"`
	Mode        string  `json:"mode"`
	Codec       string  `json:"codec"`
	Container   string  `json:"container"`
	BitrateKbps string  `json:"bitrate_kbps"`
	Metric      string  `json:"metric"`
	TargetMOS   *F      `json:"target_mos,omitempty"`
	MeanMOS     *string `json:"mean_mos,omitempty"`
	TargetDB    *F      `json:"target_db,omitempty"`
	MeanDB      *string `json:"mean_db,omitempty"`
	Passed      bool    `json:"passed"`
	SizeBytes   int64   `json:"size_bytes"`
	VisqolModel string  `json:"visqol_model,omitempty"`
}

// FBFFrame is one frame row in the frame-by-frame report.
type FBFFrame struct {
	Frame         int   `json:"frame"`
	CRF           int   `json:"crf"`
	VMAF          F     `json:"vmaf"`
	SizeBytes     int64 `json:"size_bytes"`
	TargetReached bool  `json:"target_reached"`
}

// CRFRange is a min/max pair.
type CRFRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// FBF is the <name>.fbf.vmaf.json schema.
type FBF struct {
	Input                 string     `json:"input"`
	Output                string     `json:"output"`
	Mode                  string     `json:"mode"`
	Encoder               string     `json:"encoder"`
	Container             string     `json:"container"`
	TargetVMAF            F          `json:"target_vmaf"`
	MinVMAF               F          `json:"min_vmaf"`
	MeanVMAF              F          `json:"mean_vmaf"`
	Passed                bool       `json:"passed"`
	CRFRange              CRFRange   `json:"crf_range"`
	Trim                  Trim       `json:"trim"`
	FrameCount            int        `json:"frame_count"`
	FramesBelowTarget     int        `json:"frames_below_target"`
	FramesBelowTargetList []int      `json:"frames_below_target_list"`
	WorstFrame            FBFFrame   `json:"worst_frame"`
	SizeBytes             int64      `json:"size_bytes"`
	Frames                []FBFFrame `json:"frames"`
}

// RefineAttempt is one quality attempt for a fragment.
type RefineAttempt struct {
	CRF  int `json:"crf"`
	VMAF F   `json:"vmaf"`
}

// RefineFragment is one fragment row in the refine block.
type RefineFragment struct {
	Index           int             `json:"index"`
	FirstFrame      int             `json:"first_frame"`
	LastFrame       int             `json:"last_frame"`
	Frames          int             `json:"frames"`
	Planned         string          `json:"planned"`
	UsualEncodeVMAF F               `json:"usual_encode_vmaf"`
	CRF             int             `json:"crf"`
	VMAF            F               `json:"vmaf"`
	MinFrameVMAF    F               `json:"min_frame_vmaf"`
	TargetReached   bool            `json:"target_reached"`
	SizeBytes       int64           `json:"size_bytes"`
	Attempts        []RefineAttempt `json:"attempts"`
}

// UsualEncode describes the unrefined encode.
type UsualEncode struct {
	CRF       int   `json:"crf"`
	MeanVMAF  F     `json:"mean_vmaf"`
	SizeBytes int64 `json:"size_bytes"`
}

// MergedEncode describes the accepted merged encode.
type MergedEncode struct {
	MeanVMAF  F     `json:"mean_vmaf"`
	SizeBytes int64 `json:"size_bytes"`
}

// Refine is the refine block of a video report.
type Refine struct {
	Applied                  bool             `json:"applied"`
	Reason                   string           `json:"reason"`
	TargetVMAF               F                `json:"target_vmaf"`
	UsualEncode              UsualEncode      `json:"usual_encode"`
	Frames                   int              `json:"frames"`
	MaxFragmentFrames        int              `json:"max_fragment_frames"`
	MinFragmentFrames        int              `json:"min_fragment_frames"`
	CRFStep                  int              `json:"crf_step"`
	FragmentCount            int              `json:"fragment_count"`
	FragmentsBelowTarget     int              `json:"fragments_below_target"`
	FragmentsBelowTargetList []int            `json:"fragments_below_target_list"`
	MergedEncode             *MergedEncode    `json:"merged_encode,omitempty"`
	CRFRange                 *CRFRange        `json:"crf_range,omitempty"`
	Fragments                []RefineFragment `json:"fragments"`
}

// Write serialises v to path with two-space indentation and no trailing
// newline, matching Python's json.dump(indent=2).
func Write(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
