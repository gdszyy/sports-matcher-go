package matcher

import (
	"testing"
)

// 编译期常量断言：suspectConfidenceMultiplier 在合理区间
func TestSuspectConfidenceMultiplier_InRange(t *testing.T) {
	if suspectConfidenceMultiplier <= 0 || suspectConfidenceMultiplier >= 1 {
		t.Errorf("suspectConfidenceMultiplier should be in (0,1), got %v", suspectConfidenceMultiplier)
	}
}

// 验证 RuleLeagueSuspect 常量值符合期望
func TestRuleLeagueSuspect_ConstantValue(t *testing.T) {
	if RuleLeagueSuspect != "LEAGUE_SUSPECT" {
		t.Errorf("RuleLeagueSuspect = %q, want LEAGUE_SUSPECT", RuleLeagueSuspect)
	}
}

// applySuspectDowngradeIfNeeded 把 SUSPECT 降级逻辑抽出来便于单测。
// 注意：本函数与 runLeagueEvidenceFirst 内嵌的判定逻辑保持等价。
func applySuspectDowngradeIfNeeded(league *LeagueMatchResult, edgesCount int) (downgraded bool) {
	if edgesCount != 0 || league == nil || !league.Matched || league.MatchRule == RuleLeagueKnown {
		return false
	}
	league.Confidence *= suspectConfidenceMultiplier
	league.MatchRule = RuleLeagueSuspect
	return true
}

func TestSuspectDowngrade_TriggersOnEdgesZero(t *testing.T) {
	l := &LeagueMatchResult{Matched: true, MatchRule: RuleLeagueNameHi, Confidence: 0.890}
	if !applySuspectDowngradeIfNeeded(l, 0) {
		t.Error("expected downgrade when edges=0")
	}
	if l.MatchRule != RuleLeagueSuspect {
		t.Errorf("rule = %v, want LEAGUE_SUSPECT", l.MatchRule)
	}
	if l.Confidence > 0.5 {
		t.Errorf("confidence should be downgraded, got %v", l.Confidence)
	}
}

func TestSuspectDowngrade_SkipsKnownMap(t *testing.T) {
	l := &LeagueMatchResult{Matched: true, MatchRule: RuleLeagueKnown, Confidence: 1.0}
	if applySuspectDowngradeIfNeeded(l, 0) {
		t.Error("KnownMap should NOT downgrade on edges=0 (TS data may be missing)")
	}
	if l.MatchRule != RuleLeagueKnown {
		t.Errorf("KnownMap rule should remain, got %v", l.MatchRule)
	}
	if l.Confidence != 1.0 {
		t.Errorf("KnownMap confidence should remain, got %v", l.Confidence)
	}
}

func TestSuspectDowngrade_SkipsWhenEdgesPositive(t *testing.T) {
	l := &LeagueMatchResult{Matched: true, MatchRule: RuleLeagueNameHi, Confidence: 0.908}
	if applySuspectDowngradeIfNeeded(l, 347) {
		t.Error("should NOT downgrade when edges > 0")
	}
	if l.MatchRule != RuleLeagueNameHi {
		t.Errorf("rule should remain NameHi, got %v", l.MatchRule)
	}
}

func TestSuspectDowngrade_SkipsUnmatched(t *testing.T) {
	l := &LeagueMatchResult{Matched: false, MatchRule: RuleLeagueNoMatch, Confidence: 0.0}
	if applySuspectDowngradeIfNeeded(l, 0) {
		t.Error("should NOT downgrade unmatched league")
	}
}

func TestSuspectDowngrade_NilLeague(t *testing.T) {
	if applySuspectDowngradeIfNeeded(nil, 0) {
		t.Error("nil league should not panic, returns false")
	}
}
