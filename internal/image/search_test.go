package image

import (
	"errors"
	"testing"

	"mins/internal/core"
)

// fakeEval returns a monotone evaluator: q >= threshold passes the 95 gate.
func fakeEval(threshold int, calls *[]int) searchEval {
	return func(q int) (float64, int64, error) {
		if calls != nil {
			*calls = append(*calls, q)
		}
		if q >= threshold {
			return 100, int64(1000 - q), nil
		}
		return 90, int64(1000 - q), nil
	}
}

func fakeRequest(lo, hi, seed, threshold int) (searchRequest, *[]int) {
	var calls []int
	return searchRequest{
		Lo: lo, Hi: hi, Seed: seed, Target: 95, Auto: true,
		Eval:    fakeEval(threshold, &calls),
		Log:     func(string, ...any) {},
		Attempt: func(int, float64, string) {},
	}, &calls
}

func firstCall(calls *[]int) int {
	if len(*calls) == 0 {
		return -1
	}
	return (*calls)[0]
}

func TestSearchWithoutAuto(t *testing.T) {
	req, calls := fakeRequest(0, 95, -1, 70)
	req.Auto = false
	res, err := searchQuality(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Best != 70 || res.Lo != 0 || res.Hi != 95 {
		t.Fatalf("result = %+v, want best 70 in 0..95", res)
	}
	if firstCall(calls) != 47 {
		t.Fatalf("first probe = %d, want midpoint 47", firstCall(calls))
	}
}

func TestSearchWithoutAutoNoPass(t *testing.T) {
	req, _ := fakeRequest(0, 95, -1, 96)
	req.Auto = false
	res, err := searchQuality(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Best != -1 || res.Lo != 0 || res.Hi != 95 {
		t.Fatalf("result = %+v, want no pass in 0..95", res)
	}
}

func TestSearchWithoutAutoDoesNotWiden(t *testing.T) {
	req, calls := fakeRequest(0, 95, -1, 98)
	req.Auto = false
	res, err := searchQuality(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Best != -1 || res.Hi != 95 {
		t.Fatalf("result = %+v, want no pass and no widening", res)
	}
	for _, q := range *calls {
		if q > 95 {
			t.Fatalf("probed q=%d outside the configured range", q)
		}
	}
}

func TestSearchAutoFirstStartsAtMidpoint(t *testing.T) {
	req, calls := fakeRequest(0, 100, -1, 70)
	res, err := searchQuality(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Best != 70 {
		t.Fatalf("best = %d, want 70", res.Best)
	}
	if firstCall(calls) != 50 {
		t.Fatalf("first probe = %d, want midpoint 50", firstCall(calls))
	}
}

func TestSearchAutoSeedPassSearchesBelow(t *testing.T) {
	req, calls := fakeRequest(0, 95, 80, 60)
	res, err := searchQuality(req)
	if err != nil {
		t.Fatal(err)
	}
	// The seed passes, so the search must continue below it and report the
	// lowest passing quality, not the seed.
	if res.Best != 60 {
		t.Fatalf("best = %d, want 60", res.Best)
	}
	if firstCall(calls) != 80 {
		t.Fatalf("first probe = %d, want seed 80", firstCall(calls))
	}
	if len(*calls) < 2 || (*calls)[1] != 39 {
		t.Fatalf("second probe = %v, want midpoint 39 of 0..79", *calls)
	}
}

func TestSearchAutoSeedFailSearchesAbove(t *testing.T) {
	req, calls := fakeRequest(0, 95, 60, 80)
	res, err := searchQuality(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Best != 80 {
		t.Fatalf("best = %d, want 80", res.Best)
	}
	if firstCall(calls) != 60 {
		t.Fatalf("first probe = %d, want seed 60", firstCall(calls))
	}
	if len(*calls) < 2 || (*calls)[1] != 78 {
		t.Fatalf("second probe = %v, want midpoint 78 of 61..95", *calls)
	}
}

func TestSearchAutoSeedAtFloor(t *testing.T) {
	req, calls := fakeRequest(5, 95, 1, 5)
	res, err := searchQuality(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Best != 5 {
		t.Fatalf("best = %d, want floor 5", res.Best)
	}
	if firstCall(calls) != 5 {
		t.Fatalf("first probe = %d, want clamped floor 5", firstCall(calls))
	}
}

func TestSearchAutoSeedAboveRangeFallsBackToMidpoint(t *testing.T) {
	req, calls := fakeRequest(0, 95, 120, 70)
	res, err := searchQuality(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Best != 70 {
		t.Fatalf("best = %d, want 70", res.Best)
	}
	if firstCall(calls) != 47 {
		t.Fatalf("first probe = %d, want midpoint 47", firstCall(calls))
	}
}

func TestSearchAutoWidensInSteps(t *testing.T) {
	req, calls := fakeRequest(0, 87, 80, 90)
	res, err := searchQuality(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Best != 90 {
		t.Fatalf("best = %d, want 90", res.Best)
	}
	if res.Hi != 92 {
		t.Fatalf("hi = %d, want 92 after one 5-unit widening step", res.Hi)
	}
	seen := map[int]bool{}
	for _, q := range *calls {
		seen[q] = true
	}
	if !seen[90] {
		t.Fatalf("widened search did not probe q=90: %v", *calls)
	}
}

func TestSearchAutoWidensTo100AndFails(t *testing.T) {
	req, _ := fakeRequest(0, 87, 80, 101)
	res, err := searchQuality(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Best != -1 {
		t.Fatalf("best = %d, want no pass", res.Best)
	}
	if res.Hi != 100 {
		t.Fatalf("hi = %d, want 100", res.Hi)
	}
}

func TestSearchEncodeError(t *testing.T) {
	req := searchRequest{
		Lo: 0, Hi: 95, Seed: -1, Target: 95,
		Eval: func(q int) (float64, int64, error) {
			return 0, 0, errEncodeFailed
		},
	}
	if _, err := searchQuality(req); !errors.Is(err, errEncodeFailed) {
		t.Fatalf("err = %v, want errEncodeFailed", err)
	}
}

// runAuto mirrors the still-image path: read the shared window, search, then
// fold the result back into it.
func runAuto(t *testing.T, st *core.QState, baseLo, threshold int) (int, []int) {
	t.Helper()
	hi, seed := st.Window(baseLo)
	var calls []int
	res, err := searchQuality(searchRequest{
		Lo: baseLo, Hi: hi, Seed: seed, Target: 95, Auto: true,
		Eval:    fakeEval(threshold, &calls),
		Log:     func(string, ...any) {},
		Attempt: func(int, float64, string) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Best >= 0 {
		st.Record(res.Best)
	} else {
		st.RecordFailure()
	}
	return res.Best, calls
}

// TestSearchAutoTrace replays the documented batch example:
//
//	0-100 -> 87 ; 5-87 -> 86 ; 10-87 -> widen -> 90 ; ...
//	60-90 -> 50 ; 50-90 -> 45
func TestSearchAutoTrace(t *testing.T) {
	st := core.NewQState(0, 100)
	type step struct {
		threshold int
		best      int
		wantHi    int
		wantSeed  int
	}
	steps := []step{
		{87, 87, 87, 5},
		{86, 86, 87, 10},
		{90, 90, 90, 15},
	}
	for qmin := 20; qmin <= 60; qmin += 5 {
		steps = append(steps, step{90, 90, 90, qmin})
	}
	steps = append(steps,
		step{50, 50, 90, 50},
		step{45, 45, 90, 45},
	)

	for i, s := range steps {
		hi, seed := st.Window(0)
		best, calls := runAuto(t, st, 0, s.threshold)
		if best != s.best {
			t.Fatalf("step %d: best = %d, want %d", i, best, s.best)
		}
		if i == 0 && firstCall(&calls) != 50 {
			t.Fatalf("first input: first probe = %d, want midpoint 50", firstCall(&calls))
		}
		if i == 1 && firstCall(&calls) != 5 {
			t.Fatalf("second input: first probe = %d, want qmin 5", firstCall(&calls))
		}
		gotHi, gotSeed := st.Window(0)
		if gotHi != s.wantHi || gotSeed != s.wantSeed {
			t.Fatalf("step %d: window = %d..%d (seed %d), want hi %d seed %d (was %d..%d seed %d)",
				i, gotHi, gotHi, gotSeed, s.wantHi, s.wantSeed, hi, hi, seed)
		}
	}
}

func TestSearchAutoFailureKeepsQMin(t *testing.T) {
	st := core.NewQState(0, 95)
	st.Record(60) // qmin=5, qmax=60
	best, _ := runAuto(t, st, 0, 101)
	if best != -1 {
		t.Fatalf("best = %d, want no pass", best)
	}
	hi, seed := st.Window(0)
	if hi != 100 {
		t.Fatalf("hi = %d, want 100 after failure", hi)
	}
	if seed != 5 {
		t.Fatalf("seed = %d, want qmin unchanged at 5", seed)
	}
}

func TestSearchAutoNeverBelowFloor(t *testing.T) {
	st := core.NewQState(10, 95)
	best, calls := runAuto(t, st, 10, 10)
	if best != 10 {
		t.Fatalf("best = %d, want floor 10", best)
	}
	// The first input still starts at the midpoint of 10..95.
	if firstCall(&calls) != 52 {
		t.Fatalf("first probe = %d, want midpoint 52", firstCall(&calls))
	}
	for _, q := range calls {
		if q < 10 {
			t.Fatalf("probed q=%d below the encoder floor", q)
		}
	}
	_, seed := st.Window(10)
	if seed != 10 {
		t.Fatalf("qmin = %d, want floor 10", seed)
	}
}
