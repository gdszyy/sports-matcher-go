package matcher

import (
	"testing"

	"github.com/gdszyy/sports-matcher/internal/db"
)

func tnMakeTour(id, name, sport, category string) *db.SRTournament {
	return &db.SRTournament{ID: id, Name: name, Sport: sport, CategoryName: category}
}

func tnMakeComp(id, name, country, sport string) db.TSCompetition {
	return db.TSCompetition{ID: id, Name: name, CountryName: country, Sport: sport}
}

func TestMatchLeagueTopN_KnownMapTakesTop1(t *testing.T) {
	// Premier League SR tournament 17 → known TS "jednm9whz0ryox8"
	sr := tnMakeTour("sr:tournament:17", "English Premier League", "football", "England")
	tsComps := []db.TSCompetition{
		tnMakeComp("jednm9whz0ryox8", "Premier League", "England", "football"),
		tnMakeComp("other-id-1", "EFL Championship", "England", "football"),
		tnMakeComp("other-id-2", "Premier League", "Scotland", "football"), // 干扰项
	}
	top := MatchLeagueTopN(sr, tsComps, 5)
	if len(top) == 0 {
		t.Fatalf("expected at least 1 candidate, got 0")
	}
	if top[0].TSCompetitionID != "jednm9whz0ryox8" {
		t.Errorf("expected known TS at top, got %s", top[0].TSCompetitionID)
	}
	if top[0].Score != 1.0 || top[0].Rule != RuleLeagueKnown {
		t.Errorf("known candidate should be score=1.0 rule=RuleLeagueKnown, got %v %v",
			top[0].Score, top[0].Rule)
	}
	if !top[0].StrongConstraintOK {
		t.Errorf("known candidate should be StrongConstraintOK=true")
	}
}

func TestMatchLeagueTopN_DefaultsToFiveWhenNZero(t *testing.T) {
	sr := tnMakeTour("sr:unknown:999", "Unknown League", "football", "")
	// 构造 10 个相似度高的 TS（同名）→ 应当被截断到 5
	var comps []db.TSCompetition
	for i := 0; i < 10; i++ {
		comps = append(comps, tnMakeComp("comp-"+string(rune('a'+i)), "Unknown League", "", "football"))
	}
	top := MatchLeagueTopN(sr, comps, 0)
	if len(top) > 5 {
		t.Errorf("expected n<=5 with n=0 default, got %d", len(top))
	}
}

func TestMatchLeagueTopN_RanksByScoreDescending(t *testing.T) {
	sr := tnMakeTour("sr:unknown:888", "Bundesliga", "football", "Germany")
	tsComps := []db.TSCompetition{
		tnMakeComp("c1", "Bundesliga", "Germany", "football"),    // 高分（精确同名+国家）
		tnMakeComp("c2", "Bundesliga 2", "Germany", "football"),  // 中等分
		tnMakeComp("c3", "Some Random League", "USA", "football"), // 低分，会被过滤
	}
	top := MatchLeagueTopN(sr, tsComps, 5)
	if len(top) < 1 {
		t.Fatalf("expected at least 1 candidate, got 0")
	}
	for i := 1; i < len(top); i++ {
		if top[i-1].Score < top[i].Score {
			t.Errorf("Top-N not sorted desc: pos %d=%v < pos %d=%v",
				i-1, top[i-1].Score, i, top[i].Score)
		}
	}
}

func TestMatchLeagueTopN_FilteredByMinScore(t *testing.T) {
	sr := tnMakeTour("sr:unknown:777", "Zzz Obscure Cup", "football", "Nowhere")
	tsComps := []db.TSCompetition{
		tnMakeComp("c-far", "Premier League", "England", "football"),
		tnMakeComp("c-far2", "La Liga", "Spain", "football"),
	}
	top := MatchLeagueTopN(sr, tsComps, 5)
	// 都不像，应为 0 条
	if len(top) != 0 {
		t.Errorf("expected 0 candidates (all below minScore), got %d (%+v)", len(top), top)
	}
}

func TestMatchLeagueTopN_KnownNotInComps_StillTop1(t *testing.T) {
	// KnownLeagueMap 命中但 tsComps 中没有该 ID（例如单联赛模式直接注入了 tsComps）
	sr := tnMakeTour("sr:tournament:17", "English Premier League", "football", "England")
	tsComps := []db.TSCompetition{
		tnMakeComp("other-id-1", "Some Other League", "", "football"),
	}
	top := MatchLeagueTopN(sr, tsComps, 5)
	if len(top) == 0 {
		t.Fatalf("expected known candidate to appear even when not in tsComps")
	}
	if top[0].TSCompetitionID != "jednm9whz0ryox8" || top[0].Score != 1.0 {
		t.Errorf("expected known TS id at top with score=1.0, got %+v", top[0])
	}
}

func TestMatchLeagueTopN_NilTournament(t *testing.T) {
	top := MatchLeagueTopN(nil, []db.TSCompetition{tnMakeComp("c", "x", "", "football")}, 5)
	if top != nil {
		t.Errorf("expected nil for nil tournament, got %v", top)
	}
}

func TestMatchLeagueTopN_ToTSCompetitionCandidate(t *testing.T) {
	c := LeagueMatchCandidate{
		TSCompetitionID:    "ts-1",
		TSName:             "Premier League",
		TSCountry:          "England",
		Score:              0.92,
		Rule:               RuleLeagueNameHi,
		StrongConstraintOK: true,
	}
	tcc := c.ToTSCompetitionCandidate("football")
	if tcc.Competition.ID != "ts-1" || tcc.LeaguePriorScore != 0.92 || tcc.Competition.Sport != "football" {
		t.Errorf("conversion lost fields: %+v", tcc)
	}
	if !tcc.StrongConstraintOK {
		t.Errorf("StrongConstraintOK lost")
	}
}

// TestLeagueTopN_To_P1_To_P2_To_P3_FullPipeline 验证完整流水线：
// MatchLeagueTopN → ToTSCompetitionCandidates → CandidatePoolBuilder.Build
//                → EnrichTeamPriors → EvidenceEventMatcher.MatchTwoRound
func TestLeagueTopN_To_P1_To_P2_To_P3_FullPipeline(t *testing.T) {
	// 阶段 1：MatchLeagueTopN 拿到 Top-N 联赛候选
	sr := tnMakeTour("sr:tournament:17", "English Premier League", "football", "England")
	tsComps := []db.TSCompetition{
		tnMakeComp("jednm9whz0ryox8", "Premier League", "England", "football"),
		tnMakeComp("c-shadow", "Premier Lg", "England", "football"), // 名称相似的干扰项
	}
	topN := MatchLeagueTopN(sr, tsComps, 3)
	if len(topN) < 1 {
		t.Fatalf("expected at least 1 Top-N candidate, got 0")
	}

	// 阶段 2：转 P1 输入
	candInputs := ToTSCompetitionCandidates(topN, "football")
	if len(candInputs) != len(topN) {
		t.Fatalf("conversion lost candidates: %d vs %d", len(candInputs), len(topN))
	}

	// 阶段 3：P1 候选池构建（stub Loader）
	const eventTime = int64(1700000000)
	loader := &stubTSLoader{
		events: map[string][]db.TSEvent{
			"jednm9whz0ryox8": {cpMakeTSEvent("m-real", "TS-H", "Arsenal", "TS-A", "Chelsea", eventTime)},
			"c-shadow":        {cpMakeTSEvent("m-fake", "TS-H2", "Arsenal", "TS-A2", "Chelsea", eventTime)},
		},
		teamNames: map[string]map[string]string{
			"jednm9whz0ryox8": {"TS-H": "Arsenal", "TS-A": "Chelsea"},
			"c-shadow":        {"TS-H2": "Arsenal", "TS-A2": "Chelsea"},
		},
	}
	pool, err := NewCandidatePoolBuilder(loader).Build(candInputs, "football")
	if err != nil {
		t.Fatalf("P1 Build error: %v", err)
	}

	// 阶段 4：P2 球队级先验填充
	srTeamNames := map[string]string{"SR-H": "Arsenal", "SR-A": "Chelsea"}
	enriched := EnrichTeamPriors(pool.Candidates, pool.TSTeamNames, srTeamNames, nil)
	if enriched[0].HomeTeamCandidateScore == 0 {
		t.Errorf("P2 should fill HomeTeamCandidateScore")
	}

	// 阶段 5：P3 比赛级匹配
	srEvents := []db.SREvent{{ID: "sr-1", StartUnix: eventTime, HomeID: "SR-H", HomeName: "Arsenal", AwayID: "SR-A", AwayName: "Chelsea"}}
	result := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{}).MatchTwoRound(
		srEvents, enriched, srTeamNames, pool.TSTeamNames,
	)

	// Known 命中（score=1.0）的 competition 应胜出，shadow 进入 Eliminated
	var matched []ResolvedEventMatch
	for _, mm := range result.Matches {
		if mm.Matched {
			matched = append(matched, mm)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 matched event, got %d (Matches=%+v)", len(matched), result.Matches)
	}
	if matched[0].TSCompetitionID != "jednm9whz0ryox8" {
		t.Errorf("expected Known competition as winner, got %s", matched[0].TSCompetitionID)
	}
}
