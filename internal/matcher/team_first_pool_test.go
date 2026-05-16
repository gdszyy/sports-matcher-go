package matcher

import (
	"context"
	"testing"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// stubTFLoader 实现 TeamFirstLoader，用于无 DB 单测。
type stubTFLoader struct {
	allTeams []db.TSTeamBrief
	events   []db.TSEventWithComp
}

func (s *stubTFLoader) GetAllTeamsBriefCtx(ctx context.Context, sport string) ([]db.TSTeamBrief, error) {
	return s.allTeams, nil
}
func (s *stubTFLoader) GetEventsByTeamIDsInRangeCtx(ctx context.Context, sport string, teamIDs []string, tMin, tMax int64) ([]db.TSEventWithComp, error) {
	set := make(map[string]bool, len(teamIDs))
	for _, id := range teamIDs {
		set[id] = true
	}
	var out []db.TSEventWithComp
	for _, e := range s.events {
		if set[e.Event.HomeID] || set[e.Event.AwayID] {
			if e.Event.MatchTime >= tMin && e.Event.MatchTime <= tMax {
				out = append(out, e)
			}
		}
	}
	return out, nil
}

var _ TeamFirstLoader = (*stubTFLoader)(nil)

func TestTeamFirstPool_BasicHit(t *testing.T) {
	loader := &stubTFLoader{
		allTeams: []db.TSTeamBrief{
			{TeamID: "TS-WEHDAT", Name: "Al Wehdat", CompetitionID: "ts-jordan"},
			{TeamID: "TS-BAQAA", Name: "Al Baqaa", CompetitionID: "ts-jordan"},
			{TeamID: "TS-OTHER", Name: "Random FC", CompetitionID: "ts-other"},
		},
		events: []db.TSEventWithComp{
			{CompetitionID: "ts-jordan", Event: db.TSEvent{ID: "m1", MatchID: "m1", MatchTime: 1000, HomeID: "TS-WEHDAT", AwayID: "TS-BAQAA"}},
			{CompetitionID: "ts-jordan", Event: db.TSEvent{ID: "m2", MatchID: "m2", MatchTime: 2000, HomeID: "TS-BAQAA", AwayID: "TS-WEHDAT"}},
		},
	}
	b := NewTeamFirstPoolBuilder(loader)
	res, err := b.Build(context.Background(), map[string]string{
		"sr-1": "Al Wehdat",
		"sr-2": "Al Baqaa",
	}, "football", 0, 5000, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d", len(res.Candidates))
	}
	if len(res.TSTeamNames) < 2 {
		t.Errorf("expected ≥2 TSTeamNames, got %d", len(res.TSTeamNames))
	}
	// 都应该来自 ts-jordan competition
	for _, c := range res.Candidates {
		if c.CompetitionID != "ts-jordan" {
			t.Errorf("expected ts-jordan, got %s", c.CompetitionID)
		}
	}
}

func TestTeamFirstPool_NoMatch(t *testing.T) {
	loader := &stubTFLoader{allTeams: []db.TSTeamBrief{{TeamID: "TS-X", Name: "Random FC"}}}
	b := NewTeamFirstPoolBuilder(loader)
	res, err := b.Build(context.Background(), map[string]string{"sr-1": "Totally Unrelated"}, "football", 0, 5000, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(res.Candidates))
	}
}

func TestTeamFirstPool_NilLoader(t *testing.T) {
	b := NewTeamFirstPoolBuilder(nil)
	_, err := b.Build(context.Background(), nil, "football", 0, 0, 3)
	if err == nil {
		t.Fatal("expected error for nil loader")
	}
}

func TestRuleLeagueTeamFirst_ConstantValue(t *testing.T) {
	if RuleLeagueTeamFirst != "LEAGUE_TEAM_FIRST" {
		t.Errorf("RuleLeagueTeamFirst = %q, want LEAGUE_TEAM_FIRST", RuleLeagueTeamFirst)
	}
}
