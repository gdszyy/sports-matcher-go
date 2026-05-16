package matcher

import (
	"testing"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// TestScoreEdge_TimePrefilter_SkipsFarCandidate 验证：
// 远期候选（>30 天）且无球队 ID 锚点 → scoreEdge 直接返回 false，
// 不再做 4 次 NameSim 计算。这是 v1.7 性能优化的核心。
func TestScoreEdge_TimePrefilter_SkipsFarCandidate(t *testing.T) {
	srEvents := []db.SREvent{
		{ID: "sr-1", StartUnix: 1_700_000_000, HomeID: "SR-H", HomeName: "Alpha", AwayID: "SR-A", AwayName: "Beta"},
	}
	// TS 比赛时间 = SR + 31 天，超出 l5MaxTimeDiff
	farTime := int64(1_700_000_000 + 31*24*3600)
	candidates := []EvidenceEventCandidate{
		{
			CompetitionID:      "c1",
			Event:              db.TSEvent{ID: "m-far", MatchID: "m-far", MatchTime: farTime, HomeID: "TS-H", HomeName: "Alpha", AwayID: "TS-A", AwayName: "Beta"},
			CandidateScore:     1.0,
			StrongConstraintOK: true,
		},
	}
	srTN := map[string]string{"SR-H": "Alpha", "SR-A": "Beta"}
	tsTN := map[string]string{"TS-H": "Alpha", "TS-A": "Beta"}

	m := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	result := m.MatchTwoRound(srEvents, candidates, srTN, tsTN)
	// 预筛击中：没有任何 edge
	if len(result.Edges) != 0 {
		t.Errorf("expected 0 edges (prefilter skipped far candidate w/o anchor), got %d", len(result.Edges))
	}
	// 所有 SR 都是 NoMatch stub
	for _, mm := range result.Matches {
		if mm.Matched {
			t.Errorf("expected Matched=false, got %+v", mm)
		}
	}
}

// TestScoreEdge_TimePrefilter_KeepsCloseCandidate 反面：
// 近期候选（< 30 天）应不被预筛 cut，正常进入 NameSim 阶段。
func TestScoreEdge_TimePrefilter_KeepsCloseCandidate(t *testing.T) {
	srEvents := []db.SREvent{
		{ID: "sr-1", StartUnix: 1_700_000_000, HomeID: "SR-H", HomeName: "Alpha", AwayID: "SR-A", AwayName: "Beta"},
	}
	candidates := []EvidenceEventCandidate{
		{
			CompetitionID:      "c1",
			Event:              db.TSEvent{ID: "m-near", MatchID: "m-near", MatchTime: 1_700_000_000, HomeID: "TS-H", HomeName: "Alpha", AwayID: "TS-A", AwayName: "Beta"},
			CandidateScore:     1.0,
			StrongConstraintOK: true,
		},
	}
	srTN := map[string]string{"SR-H": "Alpha", "SR-A": "Beta"}
	tsTN := map[string]string{"TS-H": "Alpha", "TS-A": "Beta"}

	m := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	result := m.MatchTwoRound(srEvents, candidates, srTN, tsTN)
	if len(result.Edges) == 0 {
		t.Errorf("expected at least 1 edge for near candidate, got 0 (prefilter incorrectly skipped)")
	}
	var matched int
	for _, mm := range result.Matches {
		if mm.Matched {
			matched++
		}
	}
	if matched != 1 {
		t.Errorf("expected 1 matched, got %d", matched)
	}
}

// TestScoreEdge_TimePrefilter_KeepsFarCandidateIfTeamAnchored 验证：
// 远期候选（>30 天）但有球队 ID 锚点 → 不被预筛 cut，能走 L4b 兜底。
// 这是预筛的安全性关键 —— 不可以丢失 L4b 路径。
func TestScoreEdge_TimePrefilter_KeepsFarCandidateIfTeamAnchored(t *testing.T) {
	srEvents := []db.SREvent{
		{ID: "sr-1", StartUnix: 1_700_000_000, HomeID: "SR-H", HomeName: "DifferentName1", AwayID: "SR-A", AwayName: "DifferentName2"},
	}
	farTime := int64(1_700_000_000 + 31*24*3600)
	candidates := []EvidenceEventCandidate{
		{
			CompetitionID:      "c1",
			Event:              db.TSEvent{ID: "m-far-anchored", MatchID: "m-far-anchored", MatchTime: farTime, HomeID: "TS-H", HomeName: "OtherName1", AwayID: "TS-A", AwayName: "OtherName2"},
			CandidateScore:     1.0,
			StrongConstraintOK: true,
		},
	}
	srTN := map[string]string{"SR-H": "DifferentName1", "SR-A": "DifferentName2"}
	tsTN := map[string]string{"TS-H": "OtherName1", "TS-A": "OtherName2"}

	// 构造 teamIDMap：SR-H↔TS-H, SR-A↔TS-A
	teamIDMap := map[string]string{"SR-H": "TS-H", "SR-A": "TS-A"}

	m := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	// 直接调 Match（绕开 MatchTwoRound，省得第一轮先把 teamIDMap 算掉）
	result := m.Match(srEvents, candidates, srTN, tsTN, teamIDMap)
	// 应该至少产生一条 edge（即使名称对不上，球队 ID 锚点能撑住 L4b）
	if len(result.Edges) == 0 {
		t.Errorf("expected edge to survive prefilter due to team anchor; got 0 (prefilter dropped L4b path!)")
	}
}
