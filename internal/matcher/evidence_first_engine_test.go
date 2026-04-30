package matcher

import (
	"testing"

	"github.com/gdszyy/sports-matcher/internal/db"
)

type p5DummyAdapter struct{}

func (p5DummyAdapter) SourceSide() string                                           { return "sr" }
func (p5DummyAdapter) LoadLeague(tournamentID, sport string) error                  { return nil }
func (p5DummyAdapter) MatchLeague(tsComps []db.TSCompetition) *LeagueMatchResult    { return nil }
func (p5DummyAdapter) LoadEvents(tournamentID string) ([]db.SREvent, error)         { return nil, nil }
func (p5DummyAdapter) LoadTeamNames(tournamentID string) (map[string]string, error) { return nil, nil }
func (p5DummyAdapter) DeriveTeamMappings(events []EventMatch, srcTeamNames, tsTeamNames map[string]string) []TeamMappingResult {
	return nil
}
func (p5DummyAdapter) RunPlayerMatch(teams []TeamMappingResult, sport string, ts *db.TSAdapter) ([]PlayerMatchResult, []TeamMappingResult, []EventMatchResult, bool) {
	return nil, nil, nil, false
}
func (p5DummyAdapter) ConvertEvents(matches []EventMatch) []EventMatchResult { return nil }
func (p5DummyAdapter) ApplyBottomUp(teams []TeamMappingResult, players []PlayerMatchResult, events []EventMatchResult) ([]TeamMappingResult, []EventMatchResult) {
	return teams, events
}

type p5AliasStore struct{ upserts int }

func (s *p5AliasStore) LoadIntoIndex(loader db.AliasIndexLoader, sourceSide string) int { return 0 }
func (s *p5AliasStore) Upsert(sourceSide, srcTeamID, tsTeamID string, confidence float64, sport, competitionID string) error {
	s.upserts++
	return nil
}

func TestEvidenceFirstP5_SafeWriteBackOnlyAutoConfirmed(t *testing.T) {
	store := &p5AliasStore{}
	eng := &UniversalEngine{AliasStore: store}
	decision := LeagueEvidenceDecision{
		Status:                LeagueDecisionAutoConfirmed,
		SelectedCompetitionID: "ts-ok",
		CandidateGap:          0.22,
		Candidates: []LeagueEvidenceCandidate{{
			CompetitionID:      "ts-ok",
			Score:              0.91,
			CandidateGap:       0.22,
			HighConfEvents:     4,
			TwoTeamAnchorScore: 0.80,
		}},
	}
	teams := []TeamMappingResult{{SrcTeamID: "sr-a", TSTeamID: "ts-a", Confidence: 0.92, VoteCount: 2}}
	wb := eng.applyEvidenceFirstWriteBack(p5DummyAdapter{}, "football", decision, teams, EvidenceFirstOptions{AllowWriteBack: true, MinHighConfEvents: 3, MinTwoTeamAnchorRate: 0.60, MinCandidateGap: 0.10, HighConfScore: 0.85})
	if !wb.Allowed || store.upserts != 1 {
		t.Fatalf("expected safe write-back to pass and write one alias, got allowed=%v upserts=%d reasons=%v", wb.Allowed, store.upserts, wb.BlockedReasons)
	}
	if wb.StrongMapUpserted {
		t.Fatalf("Evidence-First must not silently overwrite KnownLeagueMap strong mappings")
	}
}

func TestEvidenceFirstP5_KnownMapSuspectUsesKnownCompetitionOnly(t *testing.T) {
	matches := []ResolvedEventMatch{
		{TSCompetitionID: "ts-other", EventMatch: EventMatch{SREventID: "sr-1", TSMatchID: "m1", Matched: true}},
		{TSCompetitionID: "ts-other", EventMatch: EventMatch{SREventID: "sr-2", TSMatchID: "m2", Matched: true}},
		{TSCompetitionID: "ts-other", EventMatch: EventMatch{SREventID: "sr-3", TSMatchID: "m3", Matched: true}},
		{TSCompetitionID: "ts-other", EventMatch: EventMatch{SREventID: "sr-4", TSMatchID: "m4", Matched: true}},
		{TSCompetitionID: "ts-other", EventMatch: EventMatch{SREventID: "sr-5", TSMatchID: "m5", Matched: true}},
	}
	events := eventMatchesForKnownCompetition(matches, "ts-known")
	rcr := ComputeReverseConfirmRateSR(events)
	if rcr >= defaultLeagueEvidenceKnownRCRMin {
		t.Fatalf("expected known map RCR below suspect threshold, got %.3f", rcr)
	}
}

func TestEvidenceFirstP5_AmbiguousCandidateGapBlocksWriteBack(t *testing.T) {
	decision := LeagueEvidenceDecision{
		Status:       LeagueDecisionReviewRequired,
		CandidateGap: 0.02,
		Reason:       "candidate_gap_below_threshold",
		Candidates: []LeagueEvidenceCandidate{
			{CompetitionID: "ts-a", Score: 0.88, CandidateGap: 0.02, HighConfEvents: 5, TwoTeamAnchorScore: 0.90},
			{CompetitionID: "ts-b", Score: 0.86, CandidateGap: 0.00, HighConfEvents: 5, TwoTeamAnchorScore: 0.88},
		},
	}
	wb := (&UniversalEngine{}).applyEvidenceFirstWriteBack(p5DummyAdapter{}, "football", decision, nil, EvidenceFirstOptions{AllowWriteBack: true, MinHighConfEvents: 3, MinTwoTeamAnchorRate: 0.60, MinCandidateGap: 0.10, HighConfScore: 0.85})
	if wb.Allowed || !containsString(wb.BlockedReasons, "decision_not_auto_confirmed") || !containsString(wb.BlockedReasons, "candidate_gap_below_threshold") {
		t.Fatalf("expected ambiguous candidate gap to block write-back, got allowed=%v reasons=%v", wb.Allowed, wb.BlockedReasons)
	}
}

func TestEvidenceFirstP5_ReviewRequiredCarriesAuditEvidence(t *testing.T) {
	result := &EvidenceFirstResult{
		SourceSide:   "sr",
		TournamentID: "sr:tournament:review",
		Sport:        "football",
		Source:       leSource("Review League", "England", "ENG", 5),
		Decision:     LeagueEvidenceDecision{Status: LeagueDecisionReviewRequired, Reason: "insufficient_high_conf_events", Candidates: []LeagueEvidenceCandidate{{CompetitionID: "ts-review", CompetitionName: "Review League", HighConfEvents: 1, TopEventExamples: []LeagueEventExample{{SREventID: "sr-1", TSMatchID: "ts-1", Score: 0.91}}}}},
		KnownMap:     EvidenceFirstKnownMapStatus{Checked: true, Status: ValidationStatusOK, RCR: 0.8},
		WriteBack:    EvidenceFirstWriteBackResult{Enabled: false, Reason: "write_back_disabled"},
		Competitions: []LeagueEvidenceCompetition{leComp("ts-review", "Review League", "England", "ENG")},
		Events:       leMatches("ts-review", "Review League", 2, 0.82),
		Teams:        []TeamMappingResult{{SrcTeamID: "sr-a", SrcTeamName: "A", TSTeamID: "ts-a", TSTeamName: "A"}},
	}
	review := buildEvidenceFirstReview(result, EvidenceFirstOptions{}, 0, 5, 1, 2, 2)
	if review.Decision.Status != LeagueDecisionReviewRequired || len(review.Decision.Candidates) == 0 || len(review.ResolvedMatches) == 0 || len(review.TeamMappings) == 0 {
		t.Fatalf("review output lacks required audit evidence: %+v", review)
	}
	if len(review.SelectedEvents) == 0 || review.SelectedEvents[0].SREventID == "" || review.SelectedEvents[0].TSMatchID == "" {
		t.Fatalf("review output must include top event examples for manual judgment: %+v", review.SelectedEvents)
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
