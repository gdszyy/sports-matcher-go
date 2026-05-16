package matcher

import (
	"testing"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// 编译期断言：SRSourceAdapter 必须实现 TopNAdapter
var _ TopNAdapter = (*SRSourceAdapter)(nil)

func TestSRSourceAdapter_MatchLeagueTopN_ForwardsToPackageFunc(t *testing.T) {
	// 构造一个最小的 SRSourceAdapter（无需真 DB，直接塞 srTour）
	a := &SRSourceAdapter{
		srTour: &db.SRTournament{
			ID:           "sr:tournament:17",
			Name:         "English Premier League",
			Sport:        "football",
			CategoryName: "England",
		},
	}
	tsComps := []db.TSCompetition{
		{ID: "jednm9whz0ryox8", Name: "Premier League", CountryName: "England", Sport: "football"},
		{ID: "other-1", Name: "Championship", CountryName: "England", Sport: "football"},
	}
	got := a.MatchLeagueTopN(tsComps, 3)
	if len(got) == 0 {
		t.Fatal("expected non-empty TopN from SR adapter")
	}
	// Known map 命中 → Top-1
	if got[0].TSCompetitionID != "jednm9whz0ryox8" {
		t.Errorf("expected known TS at top, got %s", got[0].TSCompetitionID)
	}
	if got[0].Score != 1.0 || got[0].Rule != RuleLeagueKnown {
		t.Errorf("expected known-map confidence on top-1, got score=%v rule=%v",
			got[0].Score, got[0].Rule)
	}
}

func TestUniversalEngine_EvidenceFirstFields_DefaultsOff(t *testing.T) {
	e := NewUniversalEngine(nil, false)
	if e.UseEvidenceFirst {
		t.Errorf("UseEvidenceFirst must default false to preserve P0 path")
	}
	if e.EvidenceFirstTopN != 0 {
		t.Errorf("EvidenceFirstTopN must default 0 (interpreted as defaultRunLeagueTopN=5)")
	}
}

// stubSourceAdapter 不实现 TopNAdapter；用于验证 LS-like adapter 下 EF 模式自动
// 降级回 P0 的语义（这里只检查类型断言失败的预期行为）。
type stubSourceAdapter struct{}

func (s *stubSourceAdapter) SourceSide() string                          { return "stub" }
func (s *stubSourceAdapter) LoadLeague(string, string) error             { return nil }
func (s *stubSourceAdapter) MatchLeague([]db.TSCompetition) *LeagueMatchResult {
	return &LeagueMatchResult{Matched: false, MatchRule: RuleLeagueNoMatch}
}
func (s *stubSourceAdapter) LoadEvents(string) ([]db.SREvent, error) { return nil, nil }
func (s *stubSourceAdapter) LoadTeamNames(string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (s *stubSourceAdapter) DeriveTeamMappings([]EventMatch, map[string]string, map[string]string) []TeamMappingResult {
	return nil
}
func (s *stubSourceAdapter) RunPlayerMatch([]TeamMappingResult, string, *db.TSAdapter) ([]PlayerMatchResult, []TeamMappingResult, []EventMatchResult, bool) {
	return nil, nil, nil, false
}
func (s *stubSourceAdapter) ConvertEvents([]EventMatch) []EventMatchResult { return nil }
func (s *stubSourceAdapter) ApplyBottomUp([]TeamMappingResult, []PlayerMatchResult, []EventMatchResult) ([]TeamMappingResult, []EventMatchResult) {
	return nil, nil
}

func TestUniversalEngine_NonTopNAdapter_FallsBackInterfaceCheck(t *testing.T) {
	var a SourceAdapter = &stubSourceAdapter{}
	if _, ok := a.(TopNAdapter); ok {
		t.Errorf("stub adapter unexpectedly implements TopNAdapter")
	}
	// RunLeague 的 EF 分支会因为 type assertion 失败而走回 P0 路径，
	// 不会调用不存在的 MatchLeagueTopN。这里只验证类型断言行为。
}
