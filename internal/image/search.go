package image

import (
	"errors"

	"mins/internal/util"
)

// errEncodeFailed marks an evaluator failure caused by the encoder itself.
// The caller turns it into the usual encodeFail row; other errors are fatal.
var errEncodeFailed = errors.New("image encode failed")

const maxQuality = 100

// widenStep is how far --q-auto grows the upper bound when a file cannot
// reach the target inside the current window.
const widenStep = 5

type searchEval func(q int) (mean float64, size int64, err error)

// searchRequest is the per-input quality search. Auto enables the adaptive
// --q-auto path: the first probe is Seed (or the midpoint when Seed < 0) and
// the upper bound grows in widenStep increments when nothing passes.
type searchRequest struct {
	Lo      int
	Hi      int
	Seed    int // first probe; -1 starts from the midpoint
	Target  float64
	Auto    bool
	Eval    searchEval
	Log     func(format string, args ...any)
	Attempt func(q int, mean float64, verdict string)
}

type searchResult struct {
	Best int // -1 when no candidate passed
	Lo   int
	Hi   int
}

// searchQuality finds the lowest quality that reaches the target with a
// classic binary search. Seed only changes the first probe: the search then
// halves normally and always converges on the lowest passing quality. With
// Auto, a search that finds nothing grows the upper bound by widenStep (up to
// maxQuality) and searches only the newly added slice.
func searchQuality(req searchRequest) (searchResult, error) {
	lo := req.Lo
	hi := req.Hi
	if lo < 0 {
		lo = 0
	}
	if hi > maxQuality {
		hi = maxQuality
	}
	if hi < lo {
		hi = lo
	}
	res := searchResult{Best: -1, Lo: lo, Hi: hi}

	seed := req.Seed
	if seed >= 0 {
		if seed < lo {
			seed = lo
		}
		if seed > hi {
			seed = -1
		}
	}
	if seed >= 0 {
		logf(req, "auto  : starting binary search at q=%d (adaptive q-min)", seed)
	}

	best, _, err := binarySearch(req, lo, hi, seed)
	if err != nil {
		return res, err
	}
	if best >= 0 || !req.Auto {
		res.Best = best
		res.Hi = hi
		return res, nil
	}

	// Nothing passed up to hi. The monotonicity the search relies on means
	// every q <= hi failed too, so only the slice above hi can still pass.
	for hi < maxQuality {
		from := hi + 1
		hi += widenStep
		if hi > maxQuality {
			hi = maxQuality
		}
		logf(req, "auto  : no quality in [%d..%d] reached target; widening to [%d..%d]",
			lo, from-1, from, hi)
		best, _, err = binarySearch(req, from, hi, -1)
		if err != nil {
			return res, err
		}
		if best >= 0 {
			res.Best = best
			res.Hi = hi
			return res, nil
		}
	}
	res.Hi = maxQuality
	return res, nil
}

// binarySearch returns the lowest passing quality, plus the first quality at
// which the search stopped looking. first, when inside lo..hi, is used as the
// initial probe; the remaining steps are the usual halving.
func binarySearch(req searchRequest, lo, hi, first int) (int, int, error) {
	best := -1
	useFirst := first >= lo && first <= hi
	for lo <= hi {
		mid := (lo + hi) / 2
		if useFirst {
			mid = first
			useFirst = false
		}
		pass, err := probe(req, mid)
		if err != nil {
			return -1, lo, err
		}
		if pass {
			best = mid
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}
	return best, lo, nil
}

func probe(req searchRequest, q int) (bool, error) {
	mean, size, err := req.Eval(q)
	if err != nil {
		return false, err
	}
	verdict := "FAIL"
	pass := util.AtLeast(mean, req.Target)
	if pass {
		verdict = "PASS"
	}
	logf(req, "search: q=%d vmaf=%s size=%s %s", q, util.Float(mean), util.HumanSize(size), verdict)
	if req.Attempt != nil {
		req.Attempt(q, mean, verdict)
	}
	return pass, nil
}

func logf(req searchRequest, format string, args ...any) {
	if req.Log != nil {
		req.Log(format, args...)
	}
}
