package audio

import (
	"math"
	"testing"
)

func TestRound4(t *testing.T) {
	cases := map[float64]float64{
		4.73154: 4.7315,
		4.73155: 4.7316,
		4.732:   4.732,
		0:       0,
	}
	for in, want := range cases {
		if got := round4(in); got != want {
			t.Errorf("round4(%v) = %v, want %v", in, got, want)
		}
	}
	if v := round4(math.Inf(1)); !math.IsInf(v, 1) {
		t.Errorf("round4(+inf) = %v", v)
	}
	if v := round4(math.NaN()); !math.IsNaN(v) {
		t.Errorf("round4(nan) = %v", v)
	}
}

func TestScoreFormatting(t *testing.T) {
	cases := map[float64]string{
		4.7315:       "4.7315",
		4.732:        "4.732",
		0:            "0",
		math.Inf(1):  "inf",
		math.Inf(-1): "-inf",
	}
	for in, want := range cases {
		if got := Score(in); got != want {
			t.Errorf("Score(%v) = %q, want %q", in, got, want)
		}
	}
}
