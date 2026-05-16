package matcher

import (
	"testing"

	"github.com/gdszyy/sports-matcher/internal/db"
)

func tpeMakeCand(compID, tsHomeID, tsHomeName, tsAwayID, tsAwayName string) EvidenceEventCandidate {
	return EvidenceEventCandidate{
		CompetitionID:      compID,
		Event:              db.TSEvent{ID: "m-" + compID, MatchID: "m-" + compID, HomeID: tsHomeID, HomeName: tsHomeName, AwayID: tsAwayID, AwayName: tsAwayName},
		CandidateScore:     0.8,
		StrongConstraintOK: true,
	}
}

func TestEnrichTeamPriors_EmptyCandidates(t *testing.T) {
	out := EnrichTeamPriors(nil, nil, nil, nil)
	if out != nil {
		t.Errorf("expected nil out for nil in, got %v", out)
	}
	out2 := EnrichTeamPriors([]EvidenceEventCandidate{}, nil, nil, nil)
	if len(out2) != 0 {
		t.Errorf("expected len 0, got %d", len(out2))
	}
}

func TestEnrichTeamPriors_EmptySRTeamNames_PriorsStayZero(t *testing.T) {
	cands := []EvidenceEventCandidate{
		tpeMakeCand("c1", "TH", "Alpha United", "TA", "Beta City"),
	}
	out := EnrichTeamPriors(cands, nil, nil, nil)
	if out[0].HomeTeamCandidateScore != 0 || out[0].AwayTeamCandidateScore != 0 {
		t.Errorf("expected priors=0 with empty SR teams, got home=%v away=%v",
			out[0].HomeTeamCandidateScore, out[0].AwayTeamCandidateScore)
	}
}

func TestEnrichTeamPriors_ExactMatch_PriorsHigh(t *testing.T) {
	cands := []EvidenceEventCandidate{
		tpeMakeCand("c1", "TH", "Alpha United", "TA", "Beta City"),
	}
	tsTN := map[string]string{"TH": "Alpha United", "TA": "Beta City"}
	srTN := map[string]string{
		"SR-H": "Alpha United",
		"SR-A": "Beta City",
		"SR-X": "Gamma Town", // 干扰项
	}
	out := EnrichTeamPriors(cands, tsTN, srTN, nil)
	if out[0].HomeTeamCandidateScore <= 0 {
		t.Errorf("expected HomeTeamCandidateScore > 0 with exact match, got %v", out[0].HomeTeamCandidateScore)
	}
	if out[0].AwayTeamCandidateScore <= 0 {
		t.Errorf("expected AwayTeamCandidateScore > 0 with exact match, got %v", out[0].AwayTeamCandidateScore)
	}
	// exact name match 应当显著高于 0.5
	if out[0].HomeTeamCandidateScore < 0.5 {
		t.Errorf("expected HomeTeamCandidateScore >= 0.5 (exact match), got %v", out[0].HomeTeamCandidateScore)
	}
}

func TestEnrichTeamPriors_AliasHitGivesAliasApplyScore(t *testing.T) {
	cands := []EvidenceEventCandidate{
		tpeMakeCand("c1", "TH", "Some Unrelated TS Name", "TA", "Another TS Name"),
	}
	tsTN := map[string]string{"TH": "Some Unrelated TS Name", "TA": "Another TS Name"}
	srTN := map[string]string{
		"SR-H": "Totally Different SR Home",
		"SR-A": "Different SR Away",
	}
	// 注册一对别名：SR-H ↔ TH
	idx := newTeamAliasIndex()
	idx.InjectAlias("SR-H", "TH")
	out := EnrichTeamPriors(cands, tsTN, srTN, idx)
	if out[0].HomeTeamCandidateScore < aliasApplyScore-1e-9 {
		t.Errorf("alias hit should produce score >= aliasApplyScore (%v), got %v", aliasApplyScore, out[0].HomeTeamCandidateScore)
	}
	// AwayTeam 没注册别名，应该是较低的 Jaccard 分
	if out[0].AwayTeamCandidateScore >= aliasApplyScore {
		t.Errorf("non-alias away should be < aliasApplyScore (%v), got %v", aliasApplyScore, out[0].AwayTeamCandidateScore)
	}
}

func TestEnrichTeamPriors_PreservesOtherFields(t *testing.T) {
	cands := []EvidenceEventCandidate{
		{
			CompetitionID:          "c1",
			CompetitionName:        "League A",
			Event:                  db.TSEvent{ID: "m", MatchID: "m", HomeID: "TH", HomeName: "Alpha", AwayID: "TA", AwayName: "Beta"},
			CandidateScore:         0.95,
			HomeTeamCandidateScore: 0.0, // 待填充
			AwayTeamCandidateScore: 0.0,
			StrongConstraintOK:     true,
			StrongConstraintReason: "",
		},
	}
	tsTN := map[string]string{"TH": "Alpha", "TA": "Beta"}
	srTN := map[string]string{"SR-H": "Alpha", "SR-A": "Beta"}
	out := EnrichTeamPriors(cands, tsTN, srTN, nil)
	if out[0].CompetitionID != "c1" || out[0].CompetitionName != "League A" ||
		out[0].CandidateScore != 0.95 || !out[0].StrongConstraintOK {
		t.Errorf("EnrichTeamPriors corrupted non-prior fields: %+v", out[0])
	}
	if out[0].HomeTeamCandidateScore == 0 || out[0].AwayTeamCandidateScore == 0 {
		t.Errorf("expected priors filled, got home=%v away=%v",
			out[0].HomeTeamCandidateScore, out[0].AwayTeamCandidateScore)
	}
}

func TestEnrichTeamPriors_NilAliasIndex(t *testing.T) {
	// nil aliasIdx 应当被内部 fallback 成空索引，不 panic
	cands := []EvidenceEventCandidate{tpeMakeCand("c1", "TH", "Alpha", "TA", "Beta")}
	srTN := map[string]string{"SR-H": "Alpha"}
	out := EnrichTeamPriors(cands, nil, srTN, nil)
	if out[0].HomeTeamCandidateScore <= 0 {
		t.Errorf("expected non-zero HomeTeamCandidateScore even with nil aliasIdx")
	}
}

// TestP1_P2_P3_EndToEnd 验证完整流水线 P1→P2→P3：
// P1 拿到候选池 → P2 填充球队先验 → P3 打分消解。
func TestP1_P2_P3_EndToEnd(t *testing.T) {
	const eventTime = int64(1700000000)
	loader := &stubTSLoader{
		events: map[string][]db.TSEvent{
			"comp-primary": {cpMakeTSEvent("m-primary", "TS-H", "Alpha United", "TS-A", "Beta City", eventTime)},
		},
		teamNames: map[string]map[string]string{
			"comp-primary": {"TS-H": "Alpha United", "TS-A": "Beta City"},
		},
	}
	// P1
	pool, err := NewCandidatePoolBuilder(loader).Build([]TSCompetitionCandidate{
		{Competition: db.TSCompetition{ID: "comp-primary", Name: "Primary"}, LeaguePriorScore: 1.0, StrongConstraintOK: true},
	}, "football")
	if err != nil {
		t.Fatalf("P1 Build error: %v", err)
	}
	if pool.Candidates[0].HomeTeamCandidateScore != 0 || pool.Candidates[0].AwayTeamCandidateScore != 0 {
		t.Fatalf("P1 must leave team priors=0 (got %+v)", pool.Candidates[0])
	}
	// P2
	srTeamNames := map[string]string{"SR-H": "Alpha United", "SR-A": "Beta City"}
	enriched := EnrichTeamPriors(pool.Candidates, pool.TSTeamNames, srTeamNames, nil)
	if enriched[0].HomeTeamCandidateScore == 0 || enriched[0].AwayTeamCandidateScore == 0 {
		t.Fatalf("P2 should fill priors, got %+v", enriched[0])
	}
	homeBefore := enriched[0].HomeTeamCandidateScore
	awayBefore := enriched[0].AwayTeamCandidateScore
	// P3
	srEvents := []db.SREvent{{ID: "sr-1", StartUnix: eventTime, HomeID: "SR-H", HomeName: "Alpha United", AwayID: "SR-A", AwayName: "Beta City"}}
	result := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{}).MatchTwoRound(
		srEvents, enriched, srTeamNames, pool.TSTeamNames,
	)
	if len(result.Edges) < 1 {
		t.Fatalf("expected >=1 edge from P3, got 0")
	}
	// P3 不应该把 candidate 的 prior 字段改掉
	if enriched[0].HomeTeamCandidateScore != homeBefore || enriched[0].AwayTeamCandidateScore != awayBefore {
		t.Errorf("P3 should not mutate candidate priors")
	}
	// 该 SR 应被匹配上
	var matched []ResolvedEventMatch
	for _, mm := range result.Matches {
		if mm.Matched {
			matched = append(matched, mm)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 matched SR event, got %d", len(matched))
	}
	if matched[0].TSMatchID != "m-primary" {
		t.Errorf("expected winner TS=m-primary, got %s", matched[0].TSMatchID)
	}
}
