package perf

import (
	"testing"
	"time"
)

func TestTimerDisabled(t *testing.T) {
	timer := New(false)
	timer.Start("test")
	time.Sleep(10 * time.Millisecond)
	timer.Stop("test")
	if timer.Elapsed("test") != 0 {
		t.Error("disabled timer should return 0")
	}
}

func TestTimerEnabled(t *testing.T) {
	timer := New(true)
	timer.Start("test")
	time.Sleep(50 * time.Millisecond)
	timer.Stop("test")
	elapsed := timer.Elapsed("test")
	if elapsed < 40 || elapsed > 100 {
		t.Errorf("expected ~50ms, got %dms", elapsed)
	}
}

func TestTimerMultipleStages(t *testing.T) {
	timer := New(true)
	timer.Start("stage1")
	time.Sleep(20 * time.Millisecond)
	timer.Stop("stage1")

	timer.Start("stage2")
	time.Sleep(30 * time.Millisecond)
	timer.Stop("stage2")

	if timer.Elapsed("stage1") < 15 {
		t.Errorf("stage1 should be >= 15ms, got %d", timer.Elapsed("stage1"))
	}
	if timer.Elapsed("stage2") < 25 {
		t.Errorf("stage2 should be >= 25ms, got %d", timer.Elapsed("stage2"))
	}
	if timer.Elapsed("missing") != 0 {
		t.Errorf("missing stage should be 0, got %d", timer.Elapsed("missing"))
	}
}
