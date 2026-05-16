package matcher

import (
	"testing"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// 编译期断言：LSSourceAdapter 与 SRSourceAdapter 均必须实现 TopNAdapter（v1.6 起）。
var _ TopNAdapter = (*LSSourceAdapter)(nil)

func tnLSTour(id, name, sport, category string) *db.LSTournament {
	return &db.LSTournament{ID: id, Name: name, Sport: sport, CategoryName: category}
}

func TestLSMatchLeagueTopN_KnownMapTakesTop1(t *testing.T) {
	// LS Premier League (England) id=67 → KnownLSLeagueMap 命中
	ls := tnLSTour("67", "Premier League", "football", "England")
	tsComps := []db.TSCompetition{
		{ID: "jednm9whz0ryox8", Name: "Premier League", CountryName: "England", Sport: "football"},
		{ID: "other-1", Name: "Championship", CountryName: "England", Sport: "football"},
	}
	got := LSMatchLeagueTopN(ls, tsComps, 5, false)
	if len(got) == 0 {
		t.Fatalf("expected at least 1 candidate")
	}
	if got[0].TSCompetitionID != "jednm9whz0ryox8" {
		t.Errorf("expected known TS at top, got %s", got[0].TSCompetitionID)
	}
	if got[0].Score != 1.0 || got[0].Rule != RuleLeagueKnown {
		t.Errorf("expected known-map confidence on top-1, got score=%v rule=%v",
			got[0].Score, got[0].Rule)
	}
	if !got[0].StrongConstraintOK {
		t.Errorf("known candidate should be StrongConstraintOK=true")
	}
}

func TestLSMatchLeagueTopN_NoKnownMap_SkipsKnownEntry(t *testing.T) {
	// 即使 KnownLSLeagueMap 命中，noKnownMap=true 也应跳过
	ls := tnLSTour("67", "Premier League", "football", "England")
	tsComps := []db.TSCompetition{
		{ID: "jednm9whz0ryox8", Name: "Premier League", CountryName: "England", Sport: "football"},
	}
	got := LSMatchLeagueTopN(ls, tsComps, 5, true)
	// 由于名称相似且国家相同，名称兜底应当还能匹配到 jednm9whz0ryox8，
	// 但 Rule 不应是 RuleLeagueKnown，Score 也不应固定为 1.0
	if len(got) > 0 && got[0].Rule == RuleLeagueKnown {
		t.Errorf("noKnownMap=true 时不应出现 RuleLeagueKnown，got %+v", got[0])
	}
}

func TestLSMatchLeagueTopN_KnownNotInComps_StillTop1(t *testing.T) {
	ls := tnLSTour("67", "Premier League", "football", "England")
	tsComps := []db.TSCompetition{
		{ID: "some-other-id", Name: "Unrelated League", CountryName: "England", Sport: "football"},
	}
	got := LSMatchLeagueTopN(ls, tsComps, 5, false)
	if len(got) == 0 {
		t.Fatalf("expected known candidate to appear even when not in tsComps")
	}
	if got[0].TSCompetitionID != "jednm9whz0ryox8" || got[0].Score != 1.0 {
		t.Errorf("expected known TS id at top with score=1.0, got %+v", got[0])
	}
}

func TestLSMatchLeagueTopN_NilTournament(t *testing.T) {
	got := LSMatchLeagueTopN(nil, []db.TSCompetition{{ID: "x", Name: "y", Sport: "football"}}, 5, false)
	if got != nil {
		t.Errorf("expected nil for nil tournament, got %v", got)
	}
}

func TestLSMatchLeagueTopN_DefaultsToFiveWhenNZero(t *testing.T) {
	ls := tnLSTour("9999", "Unknown League", "football", "")
	var comps []db.TSCompetition
	for i := 0; i < 10; i++ {
		comps = append(comps, db.TSCompetition{ID: "c-" + string(rune('a'+i)), Name: "Unknown League", Sport: "football"})
	}
	got := LSMatchLeagueTopN(ls, comps, 0, true)
	if len(got) > 5 {
		t.Errorf("n=0 应取默认 5，got %d", len(got))
	}
}

func TestLSSourceAdapter_MatchLeagueTopN_ForwardsToPackageFunc(t *testing.T) {
	// 直接构造一个最小 LSSourceAdapter（无 DB 调用），手动赋 lsTour
	a := &LSSourceAdapter{
		lsTour: &db.LSTournament{
			ID:           "67",
			Name:         "Premier League",
			Sport:        "football",
			CategoryName: "England",
		},
	}
	tsComps := []db.TSCompetition{
		{ID: "jednm9whz0ryox8", Name: "Premier League", CountryName: "England", Sport: "football"},
	}
	got := a.MatchLeagueTopN(tsComps, 3)
	if len(got) == 0 {
		t.Fatal("expected non-empty TopN from LS adapter")
	}
	if got[0].TSCompetitionID != "jednm9whz0ryox8" {
		t.Errorf("expected known TS at top, got %s", got[0].TSCompetitionID)
	}
}

func TestLSSourceAdapter_NoKnownMap_PropagatesToTopN(t *testing.T) {
	// NoKnownMap=true 的 LS adapter，TopN 应跳过 KnownLSLeagueMap
	a := &LSSourceAdapter{
		NoKnownMap: true,
		lsTour: &db.LSTournament{
			ID:           "67",
			Name:         "Premier League",
			Sport:        "football",
			CategoryName: "England",
		},
	}
	tsComps := []db.TSCompetition{
		{ID: "jednm9whz0ryox8", Name: "Premier League", CountryName: "England", Sport: "football"},
	}
	got := a.MatchLeagueTopN(tsComps, 3)
	// 即使名称兜底命中同一 TS，Rule 也不应是 RuleLeagueKnown
	for _, c := range got {
		if c.Rule == RuleLeagueKnown {
			t.Errorf("NoKnownMap=true 应当 propagate；不应出现 RuleLeagueKnown，got %+v", c)
		}
	}
}
