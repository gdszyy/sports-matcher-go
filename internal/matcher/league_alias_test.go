// Package matcher — 联赛别名索引单元测试
package matcher

import (
	"testing"
)

// TestLeagueAliasIndex_StaticLookup 测试静态别名词典的查找功能
func TestLeagueAliasIndex_StaticLookup(t *testing.T) {
	idx := GetLeagueAliasIndex()

	cases := []struct {
		input    string
		wantHit  bool
		wantDesc string
	}{
		// 英格兰联赛体系
		{"EFL League One", true, "EFL League One 官方名"},
		{"League One", true, "League One 常用名"},
		{"English Football League One", true, "English Football League One 全称"},
		{"Football League One", true, "Football League One 常用名"},
		{"League 1", true, "League 1 缩写"},
		{"EFL Championship", true, "EFL Championship 官方名"},
		{"Championship", true, "Championship 常用名"},
		{"The Championship", true, "The Championship 带冠词"},
		{"EFL League Two", true, "EFL League Two 官方名"},
		{"League Two", true, "League Two 常用名"},
		// 杯赛
		{"Carabao Cup", true, "Carabao Cup 赞助商名"},
		{"EFL Cup", true, "EFL Cup 官方名"},
		{"League Cup", true, "League Cup 常用名"},
		// 德国联赛
		{"Bundesliga", true, "Bundesliga"},
		{"1. Bundesliga", true, "1. Bundesliga 带层级"},
		{"2. Bundesliga", true, "2. Bundesliga"},
		// 西班牙联赛
		{"LaLiga", true, "LaLiga"},
		{"La Liga", true, "La Liga 带空格"},
		{"Primera Division", true, "Primera Division"},
		// UEFA 赛事
		{"Champions League", true, "Champions League 常用名"},
		{"UCL", true, "UCL 缩写"},
		{"Europa League", true, "Europa League 常用名"},
		// 不在词典中的名称
		{"Unknown League XYZ", false, "不存在的联赛"},
		{"Random Name", false, "随机名称"},
	}

	for _, c := range cases {
		_, found := idx.Lookup(c.input)
		if found != c.wantHit {
			t.Errorf("Lookup(%q) [%s]: got found=%v, want %v", c.input, c.wantDesc, found, c.wantHit)
		}
	}
}

// TestLeagueAliasIndex_SameCanonical 测试同组别名映射到同一规范名称
func TestLeagueAliasIndex_SameCanonical(t *testing.T) {
	idx := GetLeagueAliasIndex()

	groups := []struct {
		name    string
		aliases []string
	}{
		{
			name: "EFL League One 组",
			aliases: []string{
				"EFL League One",
				"League One",
				"English Football League One",
				"Football League One",
				"League 1",
			},
		},
		{
			name: "EFL Cup 组",
			aliases: []string{
				"EFL Cup",
				"League Cup",
				"Carabao Cup",
			},
		},
		{
			name: "UEFA Champions League 组",
			aliases: []string{
				"UEFA Champions League",
				"Champions League",
				"UCL",
			},
		},
	}

	for _, g := range groups {
		// 获取第一个别名的规范名
		canonical0, found0 := idx.Lookup(g.aliases[0])
		if !found0 {
			t.Errorf("[%s] 第一个别名 %q 未找到", g.name, g.aliases[0])
			continue
		}
		// 验证所有别名映射到同一规范名
		for _, alias := range g.aliases[1:] {
			canonical, found := idx.Lookup(alias)
			if !found {
				t.Errorf("[%s] 别名 %q 未找到", g.name, alias)
				continue
			}
			if canonical != canonical0 {
				t.Errorf("[%s] 别名 %q 映射到 %q，期望 %q", g.name, alias, canonical, canonical0)
			}
		}
	}
}

// TestLeagueNameSimilarityWithAlias_DirectHit 测试别名直接命中场景
func TestLeagueNameSimilarityWithAlias_DirectHit(t *testing.T) {
	cases := []struct {
		nameA    string
		nameB    string
		wantHigh bool // 期望相似度 >= 0.90
		desc     string
	}{
		// 核心场景：English Football League One 官方名 vs 常用名
		{"EFL League One", "League One", true, "EFL League One vs League One"},
		{"English Football League One", "League One", true, "English Football League One vs League One"},
		{"Football League One", "EFL League One", true, "Football League One vs EFL League One"},
		// EFL Championship
		{"EFL Championship", "Championship", true, "EFL Championship vs Championship"},
		{"The Championship", "EFL Championship", true, "The Championship vs EFL Championship"},
		// 杯赛
		{"Carabao Cup", "EFL Cup", true, "Carabao Cup vs EFL Cup"},
		{"League Cup", "Carabao Cup", true, "League Cup vs Carabao Cup"},
		// 德国联赛
		{"1. Bundesliga", "Bundesliga", true, "1. Bundesliga vs Bundesliga"},
		// UEFA
		{"Champions League", "UEFA Champions League", true, "Champions League vs UEFA Champions League"},
		{"UCL", "UEFA Champions League", true, "UCL vs UEFA Champions League"},
		// 不相关联赛（应低分）
		{"Premier League", "Bundesliga", false, "Premier League vs Bundesliga（不相关）"},
		{"Serie A", "Ligue 1", false, "Serie A vs Ligue 1（不相关）"},
	}

	for _, c := range cases {
		score := leagueNameSimilarityWithAlias(c.nameA, c.nameB)
		if c.wantHigh && score < 0.90 {
			t.Errorf("[%s] score=%.3f < 0.90 (nameA=%q, nameB=%q)", c.desc, score, c.nameA, c.nameB)
		}
		if !c.wantHigh && score >= 0.90 {
			t.Errorf("[%s] score=%.3f >= 0.90 (nameA=%q, nameB=%q)，期望低分", c.desc, score, c.nameA, c.nameB)
		}
	}
}

// TestLeagueAliasIndex_DynamicRegister 测试动态注册别名
func TestLeagueAliasIndex_DynamicRegister(t *testing.T) {
	// 创建独立索引（不影响全局单例）
	idx := &LeagueAliasIndex{
		normalizedToCanonical: make(map[string]string),
		canonicalToAliases:    make(map[string][]string),
	}

	// 动态注册
	idx.RegisterAlias("Test League", "Test League Alias 1")
	idx.RegisterAlias("Test League", "TLA")

	canonical, found := idx.Lookup("Test League Alias 1")
	if !found {
		t.Error("动态注册的别名 'Test League Alias 1' 未找到")
	}
	if canonical != normalizeName("Test League") {
		t.Errorf("别名 'Test League Alias 1' 映射到 %q，期望 %q", canonical, normalizeName("Test League"))
	}

	canonical2, found2 := idx.Lookup("TLA")
	if !found2 {
		t.Error("动态注册的别名 'TLA' 未找到")
	}
	if canonical2 != canonical {
		t.Errorf("两个别名映射到不同规范名: %q vs %q", canonical, canonical2)
	}
}

// TestLeagueAliasIndex_Size 测试索引大小
func TestLeagueAliasIndex_Size(t *testing.T) {
	idx := GetLeagueAliasIndex()
	size := idx.Size()
	if size < 50 {
		t.Errorf("索引大小 %d 过小，期望 >= 50（静态词典应包含足够多的别名）", size)
	}
	t.Logf("联赛别名索引大小: %d 条", size)
}

// TestNormalizeLeagueName 测试联赛名称归一化
func TestNormalizeLeagueName(t *testing.T) {
	cases := []struct {
		input string
		desc  string
	}{
		{"Premier League 2023/24", "带赛季年份"},
		{"Bundesliga (Germany)", "带括号国家"},
		{"La Liga 2023", "带年份"},
	}
	for _, c := range cases {
		result := normalizeLeagueName(c.input)
		t.Logf("[%s] %q → %q", c.desc, c.input, result)
	}
}
