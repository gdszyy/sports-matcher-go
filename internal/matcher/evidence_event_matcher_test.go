package matcher

import (
	"testing"

	"github.com/gdszyy/sports-matcher/internal/db"
)

func efSR(id, homeID, homeName, awayID, awayName string, start int64) db.SREvent {
	return db.SREvent{ID: id, StartUnix: start, HomeID: homeID, HomeName: homeName, AwayID: awayID, AwayName: awayName}
}

func efTS(id, matchID, homeID, homeName, awayID, awayName string, start int64) db.TSEvent {
	return db.TSEvent{ID: id, MatchID: matchID, MatchTime: start, HomeID: homeID, HomeName: homeName, AwayID: awayID, AwayName: awayName}
}

func efCand(compID string, ev db.TSEvent) EvidenceEventCandidate {
	return EvidenceEventCandidate{CompetitionID: compID, Event: ev, CandidateScore: 0.95, HomeTeamCandidateScore: 0.95, AwayTeamCandidateScore: 0.95, StrongConstraintOK: true}
}

func TestEvidenceEventMatcher_OneToOneConflictResolution(t *testing.T) {
	matcher := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	srEvents := []db.SREvent{
		efSR("sr-1", "A", "Alpha FC", "B", "Beta FC", 1_000),
		efSR("sr-2", "A2", "Alpha FC", "B2", "Beta FC", 1_300),
	}
	candidates := []EvidenceEventCandidate{efCand("comp-1", efTS("ts-row-1", "ts-match-1", "TA", "Alpha FC", "TB", "Beta FC", 1_000))}

	res := matcher.Match(srEvents, candidates, nil, nil, nil)
	matched := 0
	for _, m := range res.Matches {
		if m.Matched {
			matched++
		}
	}
	if matched != 1 {
		t.Fatalf("expected exactly one auto-confirmed match, got %d: %#v", matched, res.Matches)
	}
	if len(res.Eliminated) == 0 {
		t.Fatalf("expected loser edge with conflict explanation")
	}
	found := false
	for _, e := range res.Eliminated {
		if e.Reason == ReasonConflictTSUsed && e.LostToTSMatchID == "ts-match-1" && e.ScoreGap >= 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected TS conflict loser to record lost_to and score gap, got %#v", res.Eliminated)
	}
}

func TestEvidenceEventMatcher_SideReversedMarkedAndPenalized(t *testing.T) {
	matcher := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	srEvents := []db.SREvent{efSR("sr-1", "A", "Home United", "B", "Away City", 2_000)}
	candidates := []EvidenceEventCandidate{efCand("comp-1", efTS("ts-1", "ts-1", "TB", "Away City", "TA", "Home United", 2_000))}

	res := matcher.Match(srEvents, candidates, nil, nil, nil)
	if !res.Matches[0].Matched {
		t.Fatalf("expected reversed strong candidate to be retained as match: %#v", res.Matches[0])
	}
	if !res.Matches[0].SideReversed {
		t.Fatalf("expected SIDE_REVERSED flag")
	}
	if !hasReason(res.Matches[0].ReasonCodes, ReasonSideReversed) {
		t.Fatalf("expected SIDE_REVERSED reason code, got %#v", res.Matches[0].ReasonCodes)
	}
	if res.Matches[0].Score >= 0.95 {
		t.Fatalf("expected reversed candidate to be penalized below perfect score, got %.3f", res.Matches[0].Score)
	}
}

func TestEvidenceEventMatcher_DTWOffsetRecalls24hShift(t *testing.T) {
	matcher := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{UseDTW: true})
	day := int64(24 * 3600)
	srEvents := []db.SREvent{
		efSR("sr-1", "A", "Alpha", "B", "Beta", 10_000),
		efSR("sr-2", "C", "Gamma", "D", "Delta", 20_000),
		efSR("sr-3", "E", "Epsilon", "F", "Zeta", 30_000),
	}
	candidates := []EvidenceEventCandidate{
		efCand("comp-1", efTS("ts-1", "ts-1", "TA", "Alpha", "TB", "Beta", 10_000+day)),
		efCand("comp-1", efTS("ts-2", "ts-2", "TC", "Gamma", "TD", "Delta", 20_000+day)),
		efCand("comp-1", efTS("ts-3", "ts-3", "TE", "Epsilon", "TF", "Zeta", 30_000+day)),
	}

	res := matcher.Match(srEvents, candidates, nil, nil, nil)
	if !res.DTWApplied || res.DTWOffsetSec != day {
		t.Fatalf("expected +24h DTW offset, got applied=%v offset=%d", res.DTWApplied, res.DTWOffsetSec)
	}
	for _, m := range res.Matches {
		if !m.Matched || m.TimeDiffSec != 0 || !hasReason(m.ReasonCodes, ReasonDTWOffset) {
			t.Fatalf("expected DTW-corrected exact match, got %#v", m)
		}
	}
}

func TestEvidenceEventMatcher_SameTeamsMultipleMatchesNoReuse(t *testing.T) {
	matcher := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	day := int64(24 * 3600)
	srEvents := []db.SREvent{
		efSR("sr-1", "A", "Arsenal", "B", "Chelsea", 100_000),
		efSR("sr-2", "A", "Arsenal", "B", "Chelsea", 100_000+day),
	}
	candidates := []EvidenceEventCandidate{
		efCand("comp-epl", efTS("ts-1", "ts-1", "TA", "Arsenal", "TB", "Chelsea", 100_000)),
		efCand("comp-epl", efTS("ts-2", "ts-2", "TA", "Arsenal", "TB", "Chelsea", 100_000+day)),
	}

	res := matcher.Match(srEvents, candidates, nil, nil, nil)
	seen := map[string]bool{}
	for _, m := range res.Matches {
		if !m.Matched {
			t.Fatalf("expected both same-team fixtures to match, got %#v", res.Matches)
		}
		if seen[m.TSMatchID] {
			t.Fatalf("TS match reused: %#v", res.Matches)
		}
		seen[m.TSMatchID] = true
	}
}

func TestEvidenceEventMatcher_MultiCompetitionCandidatePool(t *testing.T) {
	matcher := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	srEvents := []db.SREvent{efSR("sr-1", "A", "Lions", "B", "Tigers", 50_000)}
	candidates := []EvidenceEventCandidate{
		efCand("wrong-comp", efTS("ts-wrong", "ts-wrong", "TX", "Lions", "TY", "Panthers", 50_000)),
		efCand("right-comp", efTS("ts-right", "ts-right", "TA", "Lions", "TB", "Tigers", 50_030)),
	}

	res := matcher.Match(srEvents, candidates, nil, nil, nil)
	if !res.Matches[0].Matched || res.Matches[0].TSCompetitionID != "right-comp" || res.Matches[0].TSMatchID != "ts-right" {
		t.Fatalf("expected correct competition candidate, got %#v", res.Matches[0])
	}
}

func TestEvidenceEventMatcher_L4bTeamIDFallback(t *testing.T) {
	matcher := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	srEvents := []db.SREvent{efSR("sr-1", "A", "Completely Different Home", "B", "Completely Different Away", 70_000)}
	candidates := []EvidenceEventCandidate{efCand("comp-1", efTS("ts-1", "ts-1", "TA", "Unrelated Name One", "TB", "Unrelated Name Two", 70_000+72*3600))}
	teamIDMap := map[string]string{"A": "TA", "B": "TB"}

	res := matcher.Match(srEvents, candidates, nil, nil, teamIDMap)
	if !res.Matches[0].Matched || res.Matches[0].MatchRule != RuleEventL4b {
		t.Fatalf("expected L4b team ID fallback match, got %#v", res.Matches[0])
	}
	if !hasReason(res.Matches[0].ReasonCodes, ReasonTeamIDFallback) {
		t.Fatalf("expected TEAM_ID_FALLBACK reason, got %#v", res.Matches[0].ReasonCodes)
	}
}

func hasReason(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}
