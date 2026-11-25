package core

import (
	"strings"
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

func TestClaimOutputRefusesSecondWriter(t *testing.T) {
	// Distinct temp paths so the process-wide map stays clean for other tests.
	a := t.TempDir() + "/same.webp"
	if err := claimOutput(a, "a/in.png"); err != nil {
		t.Fatal(err)
	}
	err := claimOutput(a, "b/in.png")
	if err == nil {
		t.Fatal("second claim of the same output was allowed")
	}
	if !strings.Contains(err.Error(), "a/in.png") || !strings.Contains(err.Error(), "b/in.png") {
		t.Errorf("error should name both inputs: %v", err)
	}
	if err := claimOutput(t.TempDir()+"/other.webp", "c/in.png"); err != nil {
		t.Errorf("unrelated output refused: %v", err)
	}
}

func TestClaimOutputIsRaceFree(t *testing.T) {
	target := t.TempDir() + "/race.webp"
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if claimOutput(target, "in"+string(rune('a'+i%26))+".png") == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d claims succeeded, want exactly 1", wins)
	}
}
