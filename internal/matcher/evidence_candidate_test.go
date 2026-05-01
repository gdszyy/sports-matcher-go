package matcher

import (
	"context"
	"testing"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

type mockEvidenceQuerier struct {
	teams          []EvidenceQueriedTeam
	events         []EvidenceQueriedEvent
	lastTeamQuery  EvidenceTeamCandidateQuery
	lastEventQuery EvidenceEventCandidateQuery
}

func (m *mockEvidenceQuerier) QueryTeamCandidates(_ context.Context, q EvidenceTeamCandidateQuery) ([]EvidenceQueriedTeam, error) {
	m.lastTeamQuery = q
	return append([]EvidenceQueriedTeam(nil), m.teams...), nil
}

func (m *mockEvidenceQuerier) QueryEventCandidates(_ context.Context, q EvidenceEventCandidateQuery) ([]EvidenceQueriedEvent, error) {
	m.lastEventQuery = q
	return append([]EvidenceQueriedEvent(nil), m.events...), nil
}

func newTestGenerator(q *mockEvidenceQuerier, opts EvidenceCandidateOptions) *EvidenceCandidateGenerator {
	opts.Sport = "football"
	opts.SourceSide = "sr"
	if opts.LeagueName == "" {
		opts.LeagueName = "Premier League"
	}
	if opts.LeagueCountry == "" {
		opts.LeagueCountry = "England"
	}
	return NewEvidenceCandidateGenerator(q, opts)
}

func reasonSet(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

func TestEvidenceCandidateGenerator_AliasHitRanksFirstAndEmitsReason(t *testing.T) {
	alias := newTeamAliasIndex()
	alias.InjectAlias("sr-ars", "ts-ars")
	q := &mockEvidenceQuerier{teams: []EvidenceQueriedTeam{
		{TeamID: "ts-other", TeamName: "Arsenal", CompetitionID: "c1", CompetitionName: "Premier League", Country: "England", RecentMatch: true, Source: "name"},
		{TeamID: "ts-ars", TeamName: "North London FC", CompetitionID: "c1", CompetitionName: "Premier League", Country: "England", Source: "alias"},
	}}
	gen := newTestGenerator(q, EvidenceCandidateOptions{TeamAliasIndex: alias, TeamTopK: 8})
	res, err := gen.Generate(context.Background(), []EvidenceSourceTeam{{ID: "sr-ars", Name: "Arsenal"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cands := res.TeamsBySourceTeamID["sr-ars"]
	if len(cands) == 0 || cands[0].TSTeamID != "ts-ars" {
		t.Fatalf("expected alias candidate first, got %#v", cands)
	}
	if cands[0].Score != 1 || !reasonSet(cands[0].ReasonCodes, ReasonAliasHit) {
		t.Fatalf("expected ALIAS_HIT score=1, got %#v", cands[0])
	}
	if len(q.lastTeamQuery.AliasTeamIDs) != 1 || q.lastTeamQuery.AliasTeamIDs[0] != "ts-ars" {
		t.Fatalf("expected alias team id to be sent to P1 query, got %#v", q.lastTeamQuery.AliasTeamIDs)
	}
}

func TestEvidenceCandidateGenerator_CountryConflictIsVetoed(t *testing.T) {
	q := &mockEvidenceQuerier{teams: []EvidenceQueriedTeam{
		{TeamID: "ts-us", TeamName: "Tigers", CompetitionID: "c-us", CompetitionName: "MLS", Country: "United States", RecentMatch: true, Source: "name_country"},
		{TeamID: "ts-gb", TeamName: "Tigers", CompetitionID: "c-gb", CompetitionName: "Premier League", Country: "England", RecentMatch: true, Source: "name_country"},
	}}
	gen := newTestGenerator(q, EvidenceCandidateOptions{LeagueCountry: "England", TeamTopK: 8})
	res, err := gen.Generate(context.Background(), []EvidenceSourceTeam{{ID: "sr-tigers", Name: "Tigers"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var us TeamCandidateEvidence
	for _, cand := range res.TeamsBySourceTeamID["sr-tigers"] {
		if cand.TSTeamID == "ts-us" {
			us = cand
		}
	}
	if !us.Vetoed || !reasonSet(us.ReasonCodes, ReasonGuardVetoCountry) {
		t.Fatalf("expected country conflict veto, got %#v", us)
	}
}

func TestEvidenceCandidateGenerator_GenderAndAgeGuards(t *testing.T) {
	q := &mockEvidenceQuerier{teams: []EvidenceQueriedTeam{
		{TeamID: "ts-men", TeamName: "Arsenal", CompetitionID: "c1", CompetitionName: "Premier League", Country: "England", RecentMatch: true},
		{TeamID: "ts-u21", TeamName: "Arsenal Women U21", CompetitionID: "c2", CompetitionName: "Premier League Women U21", Country: "England", RecentMatch: true},
	}}
	gen := newTestGenerator(q, EvidenceCandidateOptions{LeagueName: "Premier League Women U19", LeagueCountry: "England", TeamTopK: 8})
	res, err := gen.Generate(context.Background(), []EvidenceSourceTeam{{ID: "sr-w", Name: "Arsenal Women U19"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cands := res.TeamsBySourceTeamID["sr-w"]
	if len(cands) < 2 {
		t.Fatalf("expected both guard candidates retained for explanation, got %#v", cands)
	}
	seenGender, seenAge := false, false
	for _, cand := range cands {
		seenGender = seenGender || reasonSet(cand.ReasonCodes, ReasonGuardVetoGender)
		seenAge = seenAge || reasonSet(cand.ReasonCodes, ReasonGuardVetoAge)
		if !cand.Vetoed || cand.StrongConstraintOK {
			t.Fatalf("expected guard-vetoed candidate, got %#v", cand)
		}
	}
	if !seenGender || !seenAge {
		t.Fatalf("expected gender and age veto reasons, got %#v", cands)
	}
}

func TestEvidenceCandidateGenerator_InternationalFallbackAllowsCrossCountry(t *testing.T) {
	q := &mockEvidenceQuerier{teams: []EvidenceQueriedTeam{
		{TeamID: "ts-real", TeamName: "Real Madrid", CompetitionID: "ucl", CompetitionName: "UEFA Champions League", Country: "Spain", RecentMatch: true, Source: "fallback"},
	}}
	gen := NewEvidenceCandidateGenerator(q, EvidenceCandidateOptions{SourceSide: "sr", Sport: "football", LeagueName: "UEFA Champions League", LeagueCountry: "", TeamTopK: 8, InternationalTeamTopK: 15})
	res, err := gen.Generate(context.Background(), []EvidenceSourceTeam{{ID: "sr-real", Name: "Real Madrid"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cands := res.TeamsBySourceTeamID["sr-real"]
	if len(cands) != 1 {
		t.Fatalf("expected one international fallback candidate, got %#v", cands)
	}
	if cands[0].Vetoed || !cands[0].StrongConstraintOK || !reasonSet(cands[0].ReasonCodes, ReasonInternationalFallback) {
		t.Fatalf("expected non-vetoed international fallback reason, got %#v", cands[0])
	}
	if !q.lastTeamQuery.AllowCrossCountry {
		t.Fatalf("expected P1 team query to allow cross-country for international league")
	}
}

func TestEvidenceCandidateGenerator_TeamTopKAndReasonCodes(t *testing.T) {
	teams := make([]EvidenceQueriedTeam, 0, 12)
	for i := 0; i < 12; i++ {
		teams = append(teams, EvidenceQueriedTeam{TeamID: "ts-city-" + string(rune('a'+i)), TeamName: "City FC", CompetitionID: "c", CompetitionName: "Premier League", Country: "England", RecentMatch: true, Source: "name"})
	}
	q := &mockEvidenceQuerier{teams: teams}
	gen := newTestGenerator(q, EvidenceCandidateOptions{TeamTopK: 5})
	res, err := gen.Generate(context.Background(), []EvidenceSourceTeam{{ID: "sr-city", Name: "City FC"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cands := res.TeamsBySourceTeamID["sr-city"]
	if len(cands) != 5 {
		t.Fatalf("expected team TopK=5, got %d: %#v", len(cands), cands)
	}
	for _, cand := range cands {
		if cand.Score == 0 || len(cand.ReasonCodes) == 0 || cand.Source == "" {
			t.Fatalf("expected source, score and reason code on every candidate, got %#v", cand)
		}
		if !reasonSet(cand.ReasonCodes, ReasonCandidateTruncated) {
			t.Fatalf("expected truncation reason on retained candidates after TopK cut, got %#v", cand)
		}
	}
}

func TestEvidenceCandidateGenerator_EventLimitAndP3Shape(t *testing.T) {
	base := time.Now().Add(-1 * time.Hour).Unix()
	q := &mockEvidenceQuerier{teams: []EvidenceQueriedTeam{
		{TeamID: "ts-home", TeamName: "Home FC", CompetitionID: "c1", CompetitionName: "Premier League", Country: "England", RecentMatch: true},
		{TeamID: "ts-away", TeamName: "Away FC", CompetitionID: "c1", CompetitionName: "Premier League", Country: "England", RecentMatch: true},
	}}
	for i := 0; i < 5; i++ {
		q.events = append(q.events, EvidenceQueriedEvent{CompetitionID: "c1", CompetitionName: "Premier League", Event: db.TSEvent{ID: "ts-match-" + string(rune('a'+i)), MatchID: "ts-match-" + string(rune('a'+i)), MatchTime: base + int64(i*60), HomeID: "ts-home", HomeName: "Home FC", AwayID: "ts-away", AwayName: "Away FC"}})
	}
	gen := newTestGenerator(q, EvidenceCandidateOptions{EventCandidateLimit: 2, TeamTopK: 8})
	res, err := gen.Generate(context.Background(), nil, []db.SREvent{{ID: "sr-1", StartUnix: base, HomeID: "sr-home", HomeName: "Home FC", AwayID: "sr-away", AwayName: "Away FC"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TruncatedEventCandidates || res.DroppedEventCandidateCount != 3 || len(res.Events) != 2 {
		t.Fatalf("expected event hard limit with 3 dropped, got %#v", res)
	}
	if len(res.P3EventCandidates) != 2 {
		t.Fatalf("expected P3 consumable candidates, got %#v", res.P3EventCandidates)
	}
	for _, cand := range res.Events {
		if cand.CandidateScore == 0 || cand.HomeTeamCandidateScore == 0 || cand.AwayTeamCandidateScore == 0 || len(cand.ReasonCodes) == 0 {
			t.Fatalf("expected scored event candidate with reasons, got %#v", cand)
		}
		if !cand.StrongConstraintOK || !reasonSet(cand.ReasonCodes, ReasonEventTeamPair) || !reasonSet(cand.ReasonCodes, ReasonCandidateTruncated) {
			t.Fatalf("expected strong team-pair truncated candidate, got %#v", cand)
		}
	}
}

func TestEvidenceCandidateGenerator_ReserveGuard(t *testing.T) {
	q := &mockEvidenceQuerier{teams: []EvidenceQueriedTeam{
		{TeamID: "ts-main", TeamName: "Benfica", CompetitionID: "c1", CompetitionName: "Liga Portugal", Country: "Portugal", RecentMatch: true},
	}}
	gen := newTestGenerator(q, EvidenceCandidateOptions{LeagueName: "Liga Portugal Reserve", LeagueCountry: "Portugal", TeamTopK: 8})
	res, err := gen.Generate(context.Background(), []EvidenceSourceTeam{{ID: "sr-res", Name: "Benfica Reserves"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cands := res.TeamsBySourceTeamID["sr-res"]
	if len(cands) != 1 {
		t.Fatalf("expected reserve candidate retained for explanation, got %#v", cands)
	}
	if !cands[0].Vetoed || cands[0].StrongConstraintOK || !reasonSet(cands[0].ReasonCodes, ReasonGuardVetoReserve) {
		t.Fatalf("expected reserve mismatch veto, got %#v", cands[0])
	}
}
