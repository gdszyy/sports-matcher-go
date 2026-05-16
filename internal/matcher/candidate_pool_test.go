package matcher

import (
	"errors"
	"testing"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// stubTSLoader 实现 TSEventLoader，用于无 DB 单测。
type stubTSLoader struct {
	events    map[string][]db.TSEvent
	teamNames map[string]map[string]string
	errEvents map[string]error
	errTeams  map[string]error
}

func (s *stubTSLoader) GetEvents(competitionID, sport string) ([]db.TSEvent, error) {
	if err, ok := s.errEvents[competitionID]; ok {
		return nil, err
	}
	return s.events[competitionID], nil
}

func (s *stubTSLoader) GetTeamNames(competitionID, sport string) (map[string]string, error) {
	if err, ok := s.errTeams[competitionID]; ok {
		return nil, err
	}
	if s.teamNames[competitionID] == nil {
		return map[string]string{}, nil
	}
	return s.teamNames[competitionID], nil
}

func cpMakeTSEvent(matchID, homeID, homeName, awayID, awayName string, t int64) db.TSEvent {
	return db.TSEvent{
		ID:        matchID,
		MatchID:   matchID,
		MatchTime: t,
		HomeID:    homeID,
		HomeName:  homeName,
		AwayID:    awayID,
		AwayName:  awayName,
	}
}

func TestCandidatePool_BuildEmpty(t *testing.T) {
	b := NewCandidatePoolBuilder(&stubTSLoader{})
	res, err := b.Build(nil, "football")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(res.Candidates))
	}
	if len(res.TSTeamNames) != 0 {
		t.Errorf("expected empty TSTeamNames, got %v", res.TSTeamNames)
	}
	if res.Stats.CompetitionsScanned != 0 {
		t.Errorf("expected CompetitionsScanned=0, got %d", res.Stats.CompetitionsScanned)
	}
}

func TestCandidatePool_BuildSingleCompetition(t *testing.T) {
	loader := &stubTSLoader{
		events: map[string][]db.TSEvent{
			"comp-1": {
				cpMakeTSEvent("m1", "TA", "Alpha FC", "TB", "Beta FC", 1000),
				cpMakeTSEvent("m2", "TC", "Gamma FC", "TD", "Delta FC", 2000),
			},
		},
		teamNames: map[string]map[string]string{
			"comp-1": {"TA": "Alpha FC", "TB": "Beta FC", "TC": "Gamma FC", "TD": "Delta FC"},
		},
	}
	b := NewCandidatePoolBuilder(loader)
	res, err := b.Build([]TSCompetitionCandidate{
		{Competition: db.TSCompetition{ID: "comp-1", Name: "Premier League"}, LeaguePriorScore: 1.0, StrongConstraintOK: true},
	}, "football")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(res.Candidates))
	}
	for _, c := range res.Candidates {
		if c.CompetitionID != "comp-1" {
			t.Errorf("expected CompetitionID=comp-1, got %s", c.CompetitionID)
		}
		if c.CompetitionName != "Premier League" {
			t.Errorf("expected CompetitionName=Premier League, got %s", c.CompetitionName)
		}
		if c.CandidateScore != 1.0 {
			t.Errorf("expected CandidateScore=1.0, got %f", c.CandidateScore)
		}
		if !c.StrongConstraintOK {
			t.Errorf("expected StrongConstraintOK=true")
		}
	}
	if len(res.TSTeamNames) != 4 {
		t.Errorf("expected 4 TSTeamNames, got %d", len(res.TSTeamNames))
	}
	if res.Stats.TotalTSEvents != 2 {
		t.Errorf("expected TotalTSEvents=2, got %d", res.Stats.TotalTSEvents)
	}
	if res.Stats.CompetitionsKept != 1 {
		t.Errorf("expected CompetitionsKept=1, got %d", res.Stats.CompetitionsKept)
	}
}

func TestCandidatePool_BuildMultipleCompetitions_Union(t *testing.T) {
	loader := &stubTSLoader{
		events: map[string][]db.TSEvent{
			"comp-1": {
				cpMakeTSEvent("m1", "TA", "Alpha FC", "TB", "Beta FC", 1000),
				cpMakeTSEvent("m2", "TC", "Gamma FC", "TD", "Delta FC", 2000),
			},
			"comp-2": {
				cpMakeTSEvent("m3", "TE", "Epsilon FC", "TF", "Zeta FC", 3000),
				cpMakeTSEvent("m4", "TG", "Eta FC", "TH", "Theta FC", 4000),
				cpMakeTSEvent("m5", "TI", "Iota FC", "TJ", "Kappa FC", 5000),
			},
		},
		teamNames: map[string]map[string]string{
			"comp-1": {"TA": "Alpha FC", "TB": "Beta FC", "TC": "Gamma FC", "TD": "Delta FC"},
			"comp-2": {"TE": "Epsilon FC", "TF": "Zeta FC", "TG": "Eta FC", "TH": "Theta FC", "TI": "Iota FC", "TJ": "Kappa FC"},
		},
	}
	b := NewCandidatePoolBuilder(loader)
	res, err := b.Build([]TSCompetitionCandidate{
		{Competition: db.TSCompetition{ID: "comp-1", Name: "L1"}, LeaguePriorScore: 1.0, StrongConstraintOK: true},
		{Competition: db.TSCompetition{ID: "comp-2", Name: "L2"}, LeaguePriorScore: 0.75, StrongConstraintOK: true},
	}, "football")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Candidates) != 5 {
		t.Fatalf("expected 5 candidates, got %d", len(res.Candidates))
	}
	if len(res.TSTeamNames) != 10 {
		t.Fatalf("expected 10 unioned TSTeamNames, got %d", len(res.TSTeamNames))
	}
	if res.Stats.TotalTSEvents != 5 {
		t.Errorf("expected TotalTSEvents=5, got %d", res.Stats.TotalTSEvents)
	}
	if res.Stats.CompetitionsKept != 2 {
		t.Errorf("expected CompetitionsKept=2, got %d", res.Stats.CompetitionsKept)
	}
	priors := map[string]float64{}
	for _, c := range res.Candidates {
		priors[c.CompetitionID] = c.CandidateScore
	}
	if priors["comp-1"] != 1.0 || priors["comp-2"] != 0.75 {
		t.Errorf("priors mis-propagated: %v", priors)
	}
}

func TestCandidatePool_PropagatesStrongConstraintVeto(t *testing.T) {
	loader := &stubTSLoader{
		events: map[string][]db.TSEvent{
			"comp-veto": {cpMakeTSEvent("m1", "TA", "A", "TB", "B", 1000)},
		},
		teamNames: map[string]map[string]string{
			"comp-veto": {"TA": "A", "TB": "B"},
		},
	}
	b := NewCandidatePoolBuilder(loader)
	res, err := b.Build([]TSCompetitionCandidate{
		{
			Competition:            db.TSCompetition{ID: "comp-veto", Name: "Some League"},
			LeaguePriorScore:       0.6,
			StrongConstraintOK:     false,
			StrongConstraintReason: "sex mismatch men vs women",
		},
	}, "football")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(res.Candidates))
	}
	c := res.Candidates[0]
	if c.StrongConstraintOK {
		t.Errorf("expected StrongConstraintOK=false propagated")
	}
	if c.StrongConstraintReason != "sex mismatch men vs women" {
		t.Errorf("StrongConstraintReason mis-propagated: %q", c.StrongConstraintReason)
	}
	if res.Stats.CompetitionsVetoed != 1 {
		t.Errorf("expected CompetitionsVetoed=1, got %d", res.Stats.CompetitionsVetoed)
	}
}

func TestCandidatePool_LoaderEventsError(t *testing.T) {
	wantErr := errors.New("network glitch")
	loader := &stubTSLoader{
		events:    map[string][]db.TSEvent{"comp-1": {cpMakeTSEvent("m1", "TA", "A", "TB", "B", 1)}},
		errEvents: map[string]error{"comp-2": wantErr},
	}
	b := NewCandidatePoolBuilder(loader)
	_, err := b.Build([]TSCompetitionCandidate{
		{Competition: db.TSCompetition{ID: "comp-1"}, LeaguePriorScore: 1.0, StrongConstraintOK: true},
		{Competition: db.TSCompetition{ID: "comp-2"}, LeaguePriorScore: 0.5, StrongConstraintOK: true},
	}, "football")
	if err == nil {
		t.Fatalf("expected error from comp-2")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped wantErr, got %v", err)
	}
}

func TestCandidatePool_RejectsUnsupportedSport(t *testing.T) {
	b := NewCandidatePoolBuilder(&stubTSLoader{})
	_, err := b.Build(nil, "cricket")
	if err == nil {
		t.Fatal("expected error for unsupported sport")
	}
}

func TestCandidatePool_NilLoader(t *testing.T) {
	b := NewCandidatePoolBuilder(nil)
	_, err := b.Build(nil, "football")
	if err == nil {
		t.Fatal("expected error for nil loader")
	}
}

func TestCandidatePool_P0BackwardCompat_SingleCompMatches(t *testing.T) {
	loader := &stubTSLoader{
		events: map[string][]db.TSEvent{
			"comp-solo": {
				cpMakeTSEvent("m1", "TA", "Alpha", "TB", "Beta", 1000),
				cpMakeTSEvent("m2", "TC", "Gamma", "TD", "Delta", 2000),
				cpMakeTSEvent("m3", "TE", "Epsilon", "TF", "Zeta", 3000),
			},
		},
		teamNames: map[string]map[string]string{
			"comp-solo": {"TA": "Alpha", "TB": "Beta", "TC": "Gamma", "TD": "Delta", "TE": "Epsilon", "TF": "Zeta"},
		},
	}
	b := NewCandidatePoolBuilder(loader)
	res, err := b.Build([]TSCompetitionCandidate{
		{Competition: db.TSCompetition{ID: "comp-solo", Name: "Single"}, LeaguePriorScore: 1.0, StrongConstraintOK: true},
	}, "football")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	directEvents, _ := loader.GetEvents("comp-solo", "football")
	if len(res.Candidates) != len(directEvents) {
		t.Errorf("P0 backward compat broken: candidates=%d, direct=%d", len(res.Candidates), len(directEvents))
	}
	directTeams, _ := loader.GetTeamNames("comp-solo", "football")
	if len(res.TSTeamNames) != len(directTeams) {
		t.Errorf("P0 backward compat broken: TSTeamNames=%d, direct=%d", len(res.TSTeamNames), len(directTeams))
	}
}

func TestMergeTSTeamNames(t *testing.T) {
	m1 := map[string]string{"A": "Alpha", "B": "Beta"}
	m2 := map[string]string{"B": "Beta2", "C": "Gamma"}
	out := MergeTSTeamNames(m1, m2)
	if len(out) != 3 {
		t.Fatalf("expected 3 keys, got %d %v", len(out), out)
	}
	if out["B"] != "Beta" {
		t.Errorf("expected first-write-wins on key B, got %q", out["B"])
	}
}
