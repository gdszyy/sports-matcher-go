package matcher

import "testing"

func leSource(name, category, code string, totalEvents int) LeagueEvidenceSource {
	return LeagueEvidenceSource{
		Name:         name,
		CategoryName: category,
		CountryCode:  code,
		Features:     ExtractLeagueFeatures(name),
		TotalEvents:  totalEvents,
		TotalTeams:   totalEvents * 2,
	}
}

func leComp(id, name, country, code string) LeagueEvidenceCompetition {
	return LeagueEvidenceCompetition{
		CompetitionID:   id,
		CompetitionName: name,
		CountryName:     country,
		CountryCode:     code,
		Features:        ExtractLeagueFeatures(name),
	}
}

func leMatches(compID, compName string, count int, score float64) []ResolvedEventMatch {
	out := make([]ResolvedEventMatch, 0, count)
	for i := 0; i < count; i++ {
		suffix := string(rune('a' + i))
		srHomeID := "sr-home-" + suffix
		srAwayID := "sr-away-" + suffix
		tsHomeID := "ts-home-" + suffix
		tsAwayID := "ts-away-" + suffix
		edge := EventEvidenceEdge{
			SREventID:         "sr-event-" + suffix,
			TSMatchID:         "ts-match-" + compID + "-" + suffix,
			TSCompetitionID:   compID,
			TSCompetitionName: compName,
			Score:             score,
			Rule:              RuleEventL1,
			ReasonCodes:       []string{ReasonTimeWindow, ReasonStrongConstraint},
			TimeDiffSec:       0,
			TSHomeID:          tsHomeID,
			TSAwayID:          tsAwayID,
		}
		out = append(out, ResolvedEventMatch{
			EventMatch: EventMatch{
				SREventID:   edge.SREventID,
				SRStartUnix: int64(1000 + i),
				SRHomeID:    srHomeID,
				SRHomeName:  "Home " + suffix,
				SRAwayID:    srAwayID,
				SRAwayName:  "Away " + suffix,
				TSMatchID:   edge.TSMatchID,
				TSMatchTime: int64(1000 + i),
				TSHomeID:    tsHomeID,
				TSHomeName:  "Home " + suffix,
				TSAwayID:    tsAwayID,
				TSAwayName:  "Away " + suffix,
				Matched:     true,
				MatchRule:   RuleEventL1,
				Confidence:  score,
			},
			TSCompetitionID:   compID,
			TSCompetitionName: compName,
			Rule:              RuleEventL1,
			ReasonCodes:       []string{ReasonTimeWindow, ReasonStrongConstraint},
			Score:             score,
			Evidence:          &edge,
		})
	}
	return out
}

func TestLeagueEvidenceAggregator_AutoConfirmedWithAuditFields(t *testing.T) {
	agg := NewLeagueEvidenceAggregator(LeagueEvidenceAggregatorConfig{})
	source := leSource("Premier League", "England", "GB", 5)
	matches := leMatches("epl", "Premier League", 5, 0.96)
	decision := agg.Aggregate(source, matches, []LeagueEvidenceCompetition{leComp("epl", "Premier League", "England", "GB")})
	if decision.Status != LeagueDecisionAutoConfirmed {
		t.Fatalf("expected AUTO_CONFIRMED, got %#v", decision)
	}
	if len(decision.Candidates) != 1 {
		t.Fatalf("expected one candidate, got %d", len(decision.Candidates))
	}
	cand := decision.Candidates[0]
	if cand.Coverage != 1 || cand.TeamCoverage != 1 || cand.VetoReason != "" || len(cand.TopEventExamples) == 0 {
		t.Fatalf("expected complete audit evidence fields, got %#v", cand)
	}
}

func TestLeagueEvidenceAggregator_CrossCountrySerieAVeto(t *testing.T) {
	agg := NewLeagueEvidenceAggregator(LeagueEvidenceAggregatorConfig{})
	source := leSource("Serie A", "Italy", "IT", 6)
	matches := leMatches("bra-serie-a", "Serie A", 6, 0.98)
	decision := agg.Aggregate(source, matches, []LeagueEvidenceCompetition{leComp("bra-serie-a", "Serie A", "Brazil", "BR")})
	if decision.Status != LeagueDecisionRejected || decision.Candidates[0].VetoReason != "country_code_conflict" {
		t.Fatalf("expected cross-country Serie A hard veto, got %#v", decision)
	}
}

func TestLeagueEvidenceAggregator_LigueTierVeto(t *testing.T) {
	agg := NewLeagueEvidenceAggregator(LeagueEvidenceAggregatorConfig{})
	source := leSource("Ligue 1", "France", "FR", 5)
	matches := leMatches("ligue2", "Ligue 2", 5, 0.97)
	decision := agg.Aggregate(source, matches, []LeagueEvidenceCompetition{leComp("ligue2", "Ligue 2", "France", "FR")})
	if decision.Status != LeagueDecisionRejected || decision.Candidates[0].VetoReason != string(VetoTierNumber) {
		t.Fatalf("expected Ligue 1/Ligue 2 tier veto, got %#v", decision)
	}
}

func TestLeagueEvidenceAggregator_WomenMenVeto(t *testing.T) {
	agg := NewLeagueEvidenceAggregator(LeagueEvidenceAggregatorConfig{})
	source := leSource("Super League Women", "England", "GB", 4)
	matches := leMatches("men", "Super League", 4, 0.96)
	decision := agg.Aggregate(source, matches, []LeagueEvidenceCompetition{leComp("men", "Super League", "England", "GB")})
	if decision.Status != LeagueDecisionRejected || decision.Candidates[0].VetoReason != string(VetoGender) {
		t.Fatalf("expected Women/Men hard veto, got %#v", decision)
	}
}

func TestLeagueEvidenceAggregator_U19AdultVeto(t *testing.T) {
	agg := NewLeagueEvidenceAggregator(LeagueEvidenceAggregatorConfig{})
	source := leSource("Premier League U19", "England", "GB", 4)
	matches := leMatches("adult", "Premier League", 4, 0.96)
	decision := agg.Aggregate(source, matches, []LeagueEvidenceCompetition{leComp("adult", "Premier League", "England", "GB")})
	if decision.Status != LeagueDecisionRejected || decision.Candidates[0].VetoReason != string(VetoAge) {
		t.Fatalf("expected U19/adult hard veto, got %#v", decision)
	}
}

func TestLeagueEvidenceAggregator_CupLeagueVeto(t *testing.T) {
	agg := NewLeagueEvidenceAggregator(LeagueEvidenceAggregatorConfig{})
	source := leSource("FA Cup", "England", "GB", 4)
	matches := leMatches("league", "Premier League", 4, 0.96)
	decision := agg.Aggregate(source, matches, []LeagueEvidenceCompetition{leComp("league", "Premier League", "England", "GB")})
	if decision.Status != LeagueDecisionRejected || decision.Candidates[0].VetoReason != string(VetoCompetitionType) {
		t.Fatalf("expected Cup/League hard veto, got %#v", decision)
	}
}

func TestLeagueEvidenceAggregator_InsufficientMatchesRequireReview(t *testing.T) {
	agg := NewLeagueEvidenceAggregator(LeagueEvidenceAggregatorConfig{})
	source := leSource("Premier League", "England", "GB", 2)
	matches := leMatches("epl", "Premier League", 2, 0.96)
	decision := agg.Aggregate(source, matches, []LeagueEvidenceCompetition{leComp("epl", "Premier League", "England", "GB")})
	if decision.Status != LeagueDecisionReviewRequired || decision.Reason != "insufficient_high_conf_events" {
		t.Fatalf("expected insufficient matches review, got %#v", decision)
	}
}

func TestLeagueEvidenceAggregator_CandidateGapTooSmallRequiresReview(t *testing.T) {
	agg := NewLeagueEvidenceAggregator(LeagueEvidenceAggregatorConfig{})
	source := leSource("Premier League", "England", "GB", 5)
	matches := append(leMatches("epl-a", "Premier League", 5, 0.96), leMatches("epl-b", "English Premier League", 5, 0.96)...)
	decision := agg.Aggregate(source, matches, []LeagueEvidenceCompetition{
		leComp("epl-a", "Premier League", "England", "GB"),
		leComp("epl-b", "English Premier League", "England", "GB"),
	})
	if decision.Status != LeagueDecisionReviewRequired || decision.Reason != "candidate_gap_below_threshold" {
		t.Fatalf("expected small candidate gap review, got %#v", decision)
	}
}

func TestLeagueEvidenceAggregator_KnownMapLowRCRBecomesSuspect(t *testing.T) {
	agg := NewLeagueEvidenceAggregator(LeagueEvidenceAggregatorConfig{})
	source := leSource("Premier League", "England", "GB", 5)
	matches := leMatches("epl", "Premier League", 5, 0.96)
	decision := agg.AggregateWithKnownMapRCR(source, matches, []LeagueEvidenceCompetition{leComp("epl", "Premier League", "England", "GB")}, 0.20)
	if decision.Status != LeagueDecisionKnownSuspect {
		t.Fatalf("expected KNOWN_SUSPECT, got %#v", decision)
	}
}

func TestLeagueEvidenceAggregator_BigLeagueQuantityBiasBlockedByHardVeto(t *testing.T) {
	agg := NewLeagueEvidenceAggregator(LeagueEvidenceAggregatorConfig{})
	source := leSource("Serie A", "Italy", "IT", 10)
	wrongLarge := leMatches("bra-serie-a", "Serie A", 10, 0.99)
	rightSmall := leMatches("ita-serie-a", "Serie A", 3, 0.94)
	matches := append(wrongLarge, rightSmall...)
	decision := agg.Aggregate(source, matches, []LeagueEvidenceCompetition{
		leComp("bra-serie-a", "Serie A", "Brazil", "BR"),
		leComp("ita-serie-a", "Serie A", "Italy", "IT"),
	})
	if decision.SelectedCompetitionID != "ita-serie-a" {
		t.Fatalf("expected correct country candidate to outrank large cross-country bucket, got %#v", decision)
	}
	for _, cand := range decision.Candidates {
		if cand.CompetitionID == "bra-serie-a" && !cand.HardVeto {
			t.Fatalf("expected large wrong league to be hard-vetoed, got %#v", cand)
		}
	}
}
