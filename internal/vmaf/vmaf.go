// Package vmaf parses libvmaf's JSON logs.
package vmaf

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Metric is one pooled metric.
type Metric struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Mean float64 `json:"mean"`
}

// FrameMetrics holds per-frame scores.
type FrameMetrics struct {
	FrameNum int                `json:"frameNum"`
	Metrics  map[string]float64 `json:"metrics"`
}

// Log is the subset of libvmaf's JSON output mins uses.
type Log struct {
	PooledMetrics map[string]Metric `json:"pooled_metrics"`
	Frames        []FrameMetrics    `json:"frames"`
}

// Parse reads a libvmaf JSON log.
func Parse(path string) (*Log, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var l Log
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("cannot parse VMAF log: %w", err)
	}
	if _, ok := l.PooledMetrics["vmaf"]; !ok {
		return nil, fmt.Errorf("VMAF log has no pooled vmaf metric")
	}
	return &l, nil
}

// Mean returns the pooled mean and the number of compared frames.
func (l *Log) Mean() (float64, int) {
	return l.PooledMetrics["vmaf"].Mean, len(l.Frames)
}

// SortedFrames returns per-frame vmaf scores ordered by frame number.
func (l *Log) SortedFrames() []FrameMetrics {
	fr := append([]FrameMetrics(nil), l.Frames...)
	sort.Slice(fr, func(i, j int) bool { return fr[i].FrameNum < fr[j].FrameNum })
	return fr
}

// Scores returns the ordered per-frame vmaf values.
func (l *Log) Scores() []float64 {
	fr := l.SortedFrames()
	out := make([]float64, 0, len(fr))
	for _, f := range fr {
		out = append(out, f.Metrics["vmaf"])
	}
	return out
}
