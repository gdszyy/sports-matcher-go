package matcher

import (
	"testing"
	"time"
)

// TestBudgetExceeded_NoLimit 验证 MaxRuntime=0 时永远返回 false。
func TestBudgetExceeded_NoLimit(t *testing.T) {
	e := &UniversalEngine{MaxRuntime: 0}
	if e.budgetExceeded(time.Now().Add(-time.Hour)) {
		t.Errorf("MaxRuntime=0 should never exceed (永不超时)")
	}
}

// TestBudgetExceeded_WithLimit 验证 MaxRuntime>0 时按 elapsed 判定。
func TestBudgetExceeded_WithLimit(t *testing.T) {
	e := &UniversalEngine{MaxRuntime: 100 * time.Millisecond}
	t0 := time.Now()
	if e.budgetExceeded(t0) {
		t.Errorf("just started, should not exceed")
	}
	time.Sleep(120 * time.Millisecond)
	if !e.budgetExceeded(t0) {
		t.Errorf("after 120ms with 100ms budget should exceed")
	}
}

// TestMarkTruncated 验证 markTruncated 把 Stats 两个字段都写上。
func TestMarkTruncated(t *testing.T) {
	r := &UniversalMatchResult{}
	markTruncated(r, "p1")
	if !r.Stats.Truncated {
		t.Errorf("Truncated should be true")
	}
	if r.Stats.TruncatedStage != "p1" {
		t.Errorf("TruncatedStage want p1 got %q", r.Stats.TruncatedStage)
	}
}
