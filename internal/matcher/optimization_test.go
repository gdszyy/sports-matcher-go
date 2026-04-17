// Package matcher — 优化建议验证测试
// 覆盖本次优化的六个改进点：
//   1. teamNameSimilarity 引入 Jaro-Winkler 融合
//   2. wordTierMap 多语言序数词扩充
//   3. competitionTypeKeywords 预备队/州联赛识别
//   4. CheckLeagueVeto reserve/regional_league 强约束
//   5. geoSimilarity 地理别名词典
//   6. CalcFeaturePenalty 负向特征惩罚
package matcher

import (
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// 1. teamNameSimilarity — Jaro-Winkler 融合
// ─────────────────────────────────────────────────────────────────────────────

func TestTeamNameSimilarity_JaroWinklerBoost(t *testing.T) {
	cases := []struct {
		a, b    string
		wantMin float64 // 期望相似度不低于此值
		desc    string
	}{
		// 原始 Jaccard 较低，但 Jaro-Winkler 应能识别前缀相似
		{"Chaidari", "Chaidari AO", 0.70, "前缀匹配：Chaidari vs Chaidari AO"},
		{"Manchester United", "Man United", 0.40, "缩写匹配：Manchester United vs Man United"},
		// 完全相同
		{"Arsenal", "Arsenal", 1.0, "完全相同"},
		// 明显不同，相似度应较低
		{"Barcelona", "Juventus", 0.0, "明显不同"},
	}
	for _, c := range cases {
		got := teamNameSimilarity(c.a, c.b)
		if got < c.wantMin {
			t.Errorf("[%s] teamNameSimilarity(%q, %q) = %.3f, want >= %.3f",
				c.desc, c.a, c.b, got, c.wantMin)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. wordTierMap — 多语言序数词
// ─────────────────────────────────────────────────────────────────────────────

func TestWordTierMap_MultilingualOrdinals(t *testing.T) {
	cases := []struct {
		phrase    string
		wantTier  int
		desc      string
	}{
		{"1st division", 1, "英语序数词 1st"},
		{"2nd division", 2, "英语序数词 2nd"},
		{"3rd division", 3, "英语序数词 3rd"},
		{"segunda division", 2, "西班牙语 segunda"},
		{"tercera division", 3, "西班牙语 tercera"},
		{"zweite liga", 2, "德语 zweite"},
		{"dritte liga", 3, "德语 dritte"},
		{"terceira liga", 3, "葡萄牙语 terceira"},
	}
	for _, c := range cases {
		got, ok := wordTierMap[c.phrase]
		if !ok {
			t.Errorf("[%s] wordTierMap[%q] 不存在", c.desc, c.phrase)
			continue
		}
		if got != c.wantTier {
			t.Errorf("[%s] wordTierMap[%q] = %d, want %d", c.desc, c.phrase, got, c.wantTier)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. competitionTypeKeywords — 预备队/州联赛识别
// ─────────────────────────────────────────────────────────────────────────────

func TestExtractLeagueFeatures_ReserveAndRegional(t *testing.T) {
	cases := []struct {
		name     string
		wantType string
		desc     string
	}{
		{"Premier League Reserves", "reserve", "预备队识别"},
		{"B Team Championship", "reserve", "B Team 识别"},
		{"Campeonato Paulista", "regional_league", "巴西州联赛 Paulista"},
		{"Campeonato Carioca", "regional_league", "巴西州联赛 Carioca"},
		{"Campeonato Gaucho", "regional_league", "巴西州联赛 Gaucho"},
		{"Premier League", "league", "普通联赛不受影响"},
	}
	for _, c := range cases {
		f := ExtractLeagueFeatures(c.name)
		if f.CompetitionType != c.wantType {
			t.Errorf("[%s] ExtractLeagueFeatures(%q).CompetitionType = %q, want %q",
				c.desc, c.name, f.CompetitionType, c.wantType)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. CheckLeagueVeto — reserve/regional_league 强约束
// ─────────────────────────────────────────────────────────────────────────────

func TestCheckLeagueVeto_ReserveVsLeague(t *testing.T) {
	reserveFeatures := LeagueFeatures{CompetitionType: "reserve"}
	leagueFeatures := LeagueFeatures{CompetitionType: "league"}
	unknownFeatures := LeagueFeatures{CompetitionType: ""}

	// reserve vs league → 应否决
	result := CheckLeagueVeto(reserveFeatures, leagueFeatures, "hi")
	if !result.Vetoed {
		t.Error("reserve vs league 应被否决，但未否决")
	}

	// league vs reserve → 应否决
	result = CheckLeagueVeto(leagueFeatures, reserveFeatures, "hi")
	if !result.Vetoed {
		t.Error("league vs reserve 应被否决，但未否决")
	}

	// reserve vs unknown → 不应否决（信息不足时保守处理）
	result = CheckLeagueVeto(reserveFeatures, unknownFeatures, "hi")
	if result.Vetoed {
		t.Error("reserve vs unknown 不应被否决（信息不足时保守处理）")
	}
}

func TestCheckLeagueVeto_RegionalVsLeague(t *testing.T) {
	regionalFeatures := LeagueFeatures{CompetitionType: "regional_league"}
	leagueFeatures := LeagueFeatures{CompetitionType: "league"}

	// regional_league vs league → 应否决
	result := CheckLeagueVeto(regionalFeatures, leagueFeatures, "hi")
	if !result.Vetoed {
		t.Error("regional_league vs league 应被否决，但未否决")
	}

	// league vs regional_league → 应否决
	result = CheckLeagueVeto(leagueFeatures, regionalFeatures, "hi")
	if !result.Vetoed {
		t.Error("league vs regional_league 应被否决，但未否决")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. geoSimilarity — 地理别名词典
// ─────────────────────────────────────────────────────────────────────────────

func TestGeoSimilarity_AliasGroups(t *testing.T) {
	cases := []struct {
		a, b    string
		wantMin float64
		desc    string
	}{
		{"usa", "united states", 1.0, "USA vs United States"},
		{"uk", "united kingdom", 1.0, "UK vs United Kingdom"},
		{"south korea", "korea", 1.0, "South Korea vs Korea"},
		{"czech republic", "czechia", 1.0, "Czech Republic vs Czechia"},
		{"ivory coast", "cote divoire", 1.0, "Ivory Coast vs Cote d'Ivoire"},
		{"republic of ireland", "ireland", 1.0, "Republic of Ireland vs Ireland"},
		{"uae", "united arab emirates", 1.0, "UAE vs United Arab Emirates"},
		// 不同国家，相似度应较低
		{"france", "germany", 0.0, "不同国家"},
	}
	for _, c := range cases {
		got := geoSimilarity(c.a, c.b)
		if c.wantMin == 1.0 && got < 0.99 {
			t.Errorf("[%s] geoSimilarity(%q, %q) = %.3f, want 1.0", c.desc, c.a, c.b, got)
		}
		if c.wantMin == 0.0 && got > 0.5 {
			t.Errorf("[%s] geoSimilarity(%q, %q) = %.3f, want low", c.desc, c.a, c.b, got)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. CalcFeaturePenalty — 负向特征惩罚
// ─────────────────────────────────────────────────────────────────────────────

func TestCalcFeaturePenalty_GenderMismatch(t *testing.T) {
	women := LeagueFeatures{Gender: GenderWomen}
	men := LeagueFeatures{Gender: GenderMen}
	unknown := LeagueFeatures{Gender: GenderUnknown}

	// 性别不匹配 → 惩罚系数为 0
	if p := CalcFeaturePenalty(women, men); p != 0.0 {
		t.Errorf("Women vs Men 惩罚系数应为 0.0，got %.3f", p)
	}
	if p := CalcFeaturePenalty(men, women); p != 0.0 {
		t.Errorf("Men vs Women 惩罚系数应为 0.0，got %.3f", p)
	}

	// 性别未知 → 不惩罚
	if p := CalcFeaturePenalty(women, unknown); p != 1.0 {
		t.Errorf("Women vs Unknown 惩罚系数应为 1.0，got %.3f", p)
	}
}

func TestCalcFeaturePenalty_AgeGroupMismatch(t *testing.T) {
	u19 := LeagueFeatures{AgeGroup: "u19"}
	u21 := LeagueFeatures{AgeGroup: "u21"}
	noAge := LeagueFeatures{AgeGroup: ""}

	// 年龄段不同 → 大幅惩罚（0.3）
	p := CalcFeaturePenalty(u19, u21)
	if p > 0.35 || p < 0.25 {
		t.Errorf("U19 vs U21 惩罚系数应约为 0.3，got %.3f", p)
	}

	// 一侧无年龄段 → 不惩罚
	p = CalcFeaturePenalty(u19, noAge)
	if p != 1.0 {
		t.Errorf("U19 vs 无年龄段 惩罚系数应为 1.0，got %.3f", p)
	}
}

func TestCalcFeaturePenalty_ReserveVsUnknown(t *testing.T) {
	reserve := LeagueFeatures{CompetitionType: "reserve"}
	unknown := LeagueFeatures{CompetitionType: ""}

	// reserve vs unknown → 中度惩罚（0.5）
	p := CalcFeaturePenalty(reserve, unknown)
	if p > 0.55 || p < 0.45 {
		t.Errorf("Reserve vs Unknown 惩罚系数应约为 0.5，got %.3f", p)
	}
}

func TestCalcFeaturePenalty_NoMismatch(t *testing.T) {
	a := LeagueFeatures{Gender: GenderMen, AgeGroup: "u21", CompetitionType: "league"}
	b := LeagueFeatures{Gender: GenderMen, AgeGroup: "u21", CompetitionType: "league"}

	// 完全一致 → 无惩罚
	p := CalcFeaturePenalty(a, b)
	if p != 1.0 {
		t.Errorf("完全一致时惩罚系数应为 1.0，got %.3f", p)
	}
}
