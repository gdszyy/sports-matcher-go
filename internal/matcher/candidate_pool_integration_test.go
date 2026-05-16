package matcher

import (
	"testing"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// TestP1_P3_EndToEnd 验证 Evidence-First 流水线的 P1→P3 接力：
//
//	stub TSEventLoader → CandidatePoolBuilder.Build([]TSCompetitionCandidate)
//	                  → []EvidenceEventCandidate (+ unioned TSTeamNames)
//	                  → EvidenceEventMatcher.MatchTwoRound(srEvents, candidates, ...)
//	                  → ConflictResolutionResult{Matches, Eliminated, Edges, ...}
//
// 这是一个不依赖 DB 的端到端集成测试。它的目的不是覆盖 P3 的内部分支
// （那已由 evidence_event_matcher_test.go 完成），而是锁定 P1/P3 两端的
// 数据契约：CandidatePool 的输出字段（CompetitionID/CandidateScore/
// StrongConstraintOK/...）能被 P3 scoreEdge 与 resolveConflicts 正确消费。
func TestP1_P3_EndToEnd(t *testing.T) {
	tsCompCandidates := []TSCompetitionCandidate{
		{
			Competition:        db.TSCompetition{ID: "comp-primary", Name: "Primary League"},
			LeaguePriorScore:   1.0,
			StrongConstraintOK: true,
		},
		{
			Competition:        db.TSCompetition{ID: "comp-shadow", Name: "Shadow League"},
			LeaguePriorScore:   0.55,
			StrongConstraintOK: true,
		},
	}

	const eventTime = int64(1700000000)
	loader := &stubTSLoader{
		events: map[string][]db.TSEvent{
			"comp-primary": {
				cpMakeTSEvent("m-primary-1", "TS-H", "Alpha United", "TS-A", "Beta City", eventTime),
			},
			"comp-shadow": {
				cpMakeTSEvent("m-shadow-1", "TS-H2", "Alpha United", "TS-A2", "Beta City", eventTime),
			},
		},
		teamNames: map[string]map[string]string{
			"comp-primary": {"TS-H": "Alpha United", "TS-A": "Beta City"},
			"comp-shadow":  {"TS-H2": "Alpha United", "TS-A2": "Beta City"},
		},
	}

	builder := NewCandidatePoolBuilder(loader)
	pool, err := builder.Build(tsCompCandidates, "football")
	if err != nil {
		t.Fatalf("P1 Build error: %v", err)
	}
	if len(pool.Candidates) != 2 {
		t.Fatalf("expected 2 candidates from P1, got %d", len(pool.Candidates))
	}
	if pool.Stats.TotalTSEvents != 2 || pool.Stats.CompetitionsKept != 2 {
		t.Fatalf("unexpected P1 stats: %+v", pool.Stats)
	}
	for _, id := range []string{"TS-H", "TS-A", "TS-H2", "TS-A2"} {
		if _, ok := pool.TSTeamNames[id]; !ok {
			t.Errorf("P1 TSTeamNames missing %s; got %v", id, pool.TSTeamNames)
		}
	}

	srEvents := []db.SREvent{
		{ID: "sr-1", StartUnix: eventTime, HomeID: "SR-H", HomeName: "Alpha United", AwayID: "SR-A", AwayName: "Beta City"},
	}
	srTeamNames := map[string]string{"SR-H": "Alpha United", "SR-A": "Beta City"}

	matcher := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	result := matcher.MatchTwoRound(srEvents, pool.Candidates, srTeamNames, pool.TSTeamNames)

	if len(result.Edges) < 2 {
		t.Fatalf("expected >=2 edges (one per candidate), got %d", len(result.Edges))
	}

	// Per P3 contract: resolveConflicts emits one ResolvedEventMatch per SR,
	// including stubs for unmatched ones. So len(Matches)==len(srEvents) always.
	// We expect exactly 1 of them to have Matched=true.
	var matched []ResolvedEventMatch
	for _, mm := range result.Matches {
		if mm.Matched {
			matched = append(matched, mm)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("expected exactly 1 Matched=true after 1v1 conflict resolution, got %d (Matches=%+v)", len(matched), result.Matches)
	}
	winner := matched[0]
	if winner.TSCompetitionID != "comp-primary" {
		t.Errorf("expected winner from comp-primary, got %s", winner.TSCompetitionID)
	}
	if winner.TSMatchID != "m-primary-1" {
		t.Errorf("expected winner TSMatchID=m-primary-1, got %s", winner.TSMatchID)
	}

	if len(result.Eliminated) == 0 {
		t.Errorf("expected at least 1 elimination (shadow candidate), got 0")
	}
	shadowEliminated := false
	for _, e := range result.Eliminated {
		if e.LoserTSMatchID == "m-shadow-1" {
			shadowEliminated = true
			break
		}
	}
	if !shadowEliminated {
		t.Errorf("expected m-shadow-1 to appear as loser in Eliminated; got %+v", result.Eliminated)
	}
}

// TestP1_P3_VetoCompetitionCarriesThrough 验证 StrongConstraintOK=false &
// reason 非空 的 competition 不会引入虚假赢家：P3 Match() 内部会 continue 跳过，
// 候选边为空，resolveConflicts 输出 NoMatch 占位。
func TestP1_P3_VetoCompetitionCarriesThrough(t *testing.T) {
	const eventTime = int64(1700000000)
	loader := &stubTSLoader{
		events: map[string][]db.TSEvent{
			"comp-veto": {cpMakeTSEvent("m-bad", "TS-H", "Alpha", "TS-A", "Beta", eventTime)},
		},
		teamNames: map[string]map[string]string{
			"comp-veto": {"TS-H": "Alpha", "TS-A": "Beta"},
		},
	}
	builder := NewCandidatePoolBuilder(loader)
	pool, err := builder.Build([]TSCompetitionCandidate{
		{
			Competition:            db.TSCompetition{ID: "comp-veto", Name: "Veto League"},
			LeaguePriorScore:       0.8,
			StrongConstraintOK:     false,
			StrongConstraintReason: "sex mismatch",
		},
	}, "football")
	if err != nil {
		t.Fatalf("P1 Build error: %v", err)
	}
	if pool.Stats.CompetitionsVetoed != 1 {
		t.Errorf("expected CompetitionsVetoed=1, got %d", pool.Stats.CompetitionsVetoed)
	}

	srEvents := []db.SREvent{
		{ID: "sr-1", StartUnix: eventTime, HomeID: "SR-H", HomeName: "Alpha", AwayID: "SR-A", AwayName: "Beta"},
	}
	srTeamNames := map[string]string{"SR-H": "Alpha", "SR-A": "Beta"}

	matcher := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	result := matcher.MatchTwoRound(srEvents, pool.Candidates, srTeamNames, pool.TSTeamNames)

	if len(result.Edges) != 0 {
		t.Errorf("expected 0 edges when only veto candidate exists, got %d", len(result.Edges))
	}
	if len(result.Matches) != len(srEvents) {
		t.Errorf("expected len(Matches)=%d (one stub per SR), got %d", len(srEvents), len(result.Matches))
	}
	for _, mm := range result.Matches {
		if mm.Matched {
			t.Errorf("expected Matched=false for SR=%s under veto, got Matched=true (TS=%s)", mm.SREventID, mm.TSMatchID)
		}
		if mm.Rule != RuleEventNoMatch {
			t.Errorf("expected Rule=NoMatch under veto, got %v", mm.Rule)
		}
	}
}
