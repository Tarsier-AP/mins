package core

import (
	"sync"
	"testing"
)

func TestQStateWindow(t *testing.T) {
	st := NewQState(0, 95)
	hi, seed := st.Window(0)
	if hi != 95 || seed != -1 {
		t.Fatalf("first window = %d seed %d, want 95 / midpoint", hi, seed)
	}

	st.Record(80) // first success: qmax=80, qmin=5
	hi, seed = st.Window(0)
	if hi != 80 || seed != 5 {
		t.Fatalf("window = %d seed %d, want 80 / 5", hi, seed)
	}

	st.Record(90) // larger result raises qmax; qmin=10
	hi, seed = st.Window(0)
	if hi != 90 || seed != 10 {
		t.Fatalf("window = %d seed %d, want 90 / 10", hi, seed)
	}

	st.Record(20) // smaller result keeps qmax, qmin follows q*
	hi, seed = st.Window(0)
	if hi != 90 || seed != 15 {
		t.Fatalf("window = %d seed %d, want 90 / 15", hi, seed)
	}
}

func TestQStateFloor(t *testing.T) {
	st := NewQState(10, 95)
	st.Record(10)
	hi, seed := st.Window(10)
	if hi != 10 {
		t.Fatalf("hi = %d, want 10", hi)
	}
	if seed != 10 {
		t.Fatalf("seed = %d, want floor 10", seed)
	}
}

func TestQStateFailure(t *testing.T) {
	st := NewQState(0, 90)
	st.Record(50) // qmin=5, qmax=50
	st.RecordFailure()
	hi, seed := st.Window(0)
	if hi != 100 {
		t.Fatalf("hi = %d, want 100", hi)
	}
	if seed != 5 {
		t.Fatalf("seed = %d, want qmin unchanged at 5", seed)
	}
}

func TestQStateConcurrent(t *testing.T) {
	st := NewQState(0, 95)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(q int) {
			defer wg.Done()
			st.Window(0)
			st.Record(q)
			st.Window(5)
		}(i)
	}
	wg.Wait()

	hi, seed := st.Window(0)
	if hi < 0 || hi > MaxQuality {
		t.Fatalf("hi = %d outside 0..%d", hi, MaxQuality)
	}
	if seed < 0 || seed > hi {
		t.Fatalf("seed = %d outside 0..%d", seed, hi)
	}
}
